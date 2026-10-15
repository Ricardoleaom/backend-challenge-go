# ARCHITECTURE.md

## 1. Dinheiro (`internal/domain/money`)

`Money` guarda o valor em `int64` unidades mínimas (centavos) e a moeda
(`currency string`, ISO 4217). Nunca passa por `float32`/`float64` em
nenhum ponto: parsing, aritmética e persistência (coluna `BIGINT`).

`New(amount, currency string)` só aceita escala fixa de duas casas
(`"25.00"`), rejeita vazio, sinal `+`, notação científica e qualquer
caractere não numérico. Entradas negativas são rejeitadas por `New`; o
tipo só produz valores negativos internamente via `Subtract`/`Negate`,
usado em cálculos de diferença (ex.: reconciliação). `Add`/`Subtract`/
`Negate` verificam overflow de `int64` explicitamente. `Compare` e a
aritmética exigem moedas iguais (`ErrCurrencyMismatch`).

Persistência: `balance_minor_units BIGINT`, `amount_minor_units BIGINT`.
Reconstrução para `Money` passa pela mesma validação de `New`, formatando
`"%d.%02d"` a partir das unidades mínimas. Não há caminho que reintroduza
float.

## 2. WagerTransaction e máquina de estados (`internal/domain/wager`)

Tipos: `OPENING`, `BET`, `WIN`, `LOSS`, `REFUND`, `ROLLBACK`.

Estados: `PENDING → PROCESSED | REJECTED | FAILED`, com `PENDING_REFERENCE`
como estado intermediário só para `REFUND`/`ROLLBACK`. Transições são
validadas em `canTransition()`: só `PENDING` e `PENDING_REFERENCE` podem
transicionar; os três estados finais (`PROCESSED`, `REJECTED`, `FAILED`)
são absorventes. Qualquer tentativa de nova transição retorna
`ErrInvalidTransition`.

`New()` constrói sempre em `PENDING`; `Rehydrate()` reconstrói um estado já
persistido sem reaplicar a operação (não dispara movimentação nem eventos).
Essa separação é o que garante que carregar uma transação do banco para
exibir num `GET` nunca tem efeito colateral.

A transação carrega dois campos para referência: `referenceExternalID`
(o `referenceExternalTransactionId` bruto, como veio do provider) e
`resolvedReferenceID` (o ID interno da transação original, preenchido só
quando a referência é resolvida, via `ResolveReference`). Isso é o que o
edital pede em "persista também a referência interna resolvida": o dado
bruto e o resolvido ficam em colunas separadas (`reference_external_id` e
`reference_id`).

`OPENING` é um tipo interno. A aplicação (`internal/application/wallet`)
nunca expõe esse kind por HTTP/SQS: o handler HTTP rejeita explicitamente
`kind: "OPENING"` com 400 antes mesmo de chamar o serviço, e o próprio
`wager.Service.Submit` retorna `ErrOpeningNotAllowed` como segunda
barreira. `OPENING` dispensa `providerId`/`externalId`/`idempotencyKey`/
`roundId`/`gameId`; a migration `000008` tornou essas colunas nullable e
adicionou uma constraint (`transactions_external_fields_required`) que
exige esses campos para qualquer kind que não seja `OPENING`.

## 3. Wallet (`internal/domain/wallet`)

Carteira é a raiz do agregado. Chave natural `(playerId, currency)` com
índice único (`idx_wallets_player_currency_unique`, migration `000010`).
`version` começa em `1`; `Credit`/`Debit` só incrementam a versão quando o
saldo muda de fato (não há incremento para operações que não movimentam
dinheiro, como `LOSS`).

### Concorrência: por que não é otimista com retry nem lock explícito

A estratégia escolhida é **atualização atômica condicionada**, em SQL puro
(`internal/infrastructure/postgres/wallet_writer.go`):

```sql
UPDATE wallets
SET balance_minor_units = balance_minor_units - $1, version = version + 1, ...
WHERE id = $2 AND currency = $3 AND balance_minor_units >= $1 AND version < ...
RETURNING balance_minor_units, version
```

Um único `UPDATE` com a condição de saldo suficiente embutida no `WHERE`.
O Postgres resolve a corrida no nível da linha (lock implícito de linha
durante o `UPDATE`), sem precisar de `SELECT ... FOR UPDATE` prévio, sem
loop de retry por conflito de versão, e sem lock a nível de aplicação.
Isso satisfaz diretamente "carteiras independentes avançam em paralelo":
cada wallet é uma linha, cada `UPDATE` trava só a sua própria linha. E
satisfaz "atualizações de saldo impedem lost updates" porque o `WHERE`
reavalia o saldo atual no mesmo statement que escreve, sem janela entre
ler e escrever.

Quando o `UPDATE` afeta 0 linhas, uma segunda consulta (fora de qualquer
lock) distingue a causa exata (carteira inexistente, moeda incompatível,
saldo insuficiente ou overflow de versão) para devolver o `failureCode`
certo em vez de um erro genérico.

### O teste de 100.00 / duas apostas de 80.00

`TestConcurrentBetsOnSameWallet` (`internal/application/wager/service_test.go`)
dispara duas goroutines reais contra o mesmo `*sql.DB`, cada uma com sua
própria chave de idempotência, e verifica: uma processada, uma rejeitada
por `INSUFFICIENT_BALANCE`, saldo final `20.00`, e exatamente um lançamento
no ledger. O mesmo cenário foi reproduzido manualmente via HTTP contra o
stack completo em Docker (dois `curl` em paralelo) com o mesmo resultado.

## 4. Idempotência

Chave primária de dedup: `(provider_id, idempotency_key)`, único na tabela
`transactions` (índice parcial, excluindo `OPENING`). Hash determinístico
do payload de negócio (`ComputePayloadHash` em
`internal/application/wager/payload_hash.go`): um `struct` com campos em
ordem alfabética fixa (`amount, currency, externalId, gameId, kind,
playerId, providerId, referenceExternalTransactionId, roundId, walletId`),
serializado com `encoding/json` (que preserva a ordem dos campos do
struct) e assinado com SHA-256. A chave de idempotência e qualquer
metadado de transporte (headers HTTP, envelope SQS) ficam de fora do
hash. Por isso HTTP e SQS, que montam o mesmo `SubmitInput`, produzem
exatamente o mesmo hash para a mesma operação de negócio.

Fluxo em `Service.Submit`:

1. busca por `(providerId, idempotencyKey)`: se achar, compara o hash
   armazenado com o recém-calculado. Igual, devolve o resultado
   persistido com `idempotentReplay: true`; diferente,
   `ErrIdempotencyConflict` (409);
2. busca por `(providerId, externalId)`: se achar uma transação com uma
   chave de idempotência *diferente* da recebida, `ErrIdempotencyKeyMismatch`
   (409). A mesma operação financeira não pode ser reaplicada sob outra
   chave;
3. nenhuma das duas achou nada: cria a transação nova.

**Corrida entre duas primeiras submissões simultâneas com a mesma chave**:
os dois passos acima podem não achar nada para ambas (ainda não
commitado), e as duas tentam inserir. O índice único garante que só uma
`INSERT` vence; a perdedora recebe um erro de unicidade do Postgres, que o
código traduz (`translateCreateErr`) num sentinel interno
(`errIdempotencyRace`). `Submit` então refaz a busca por
`(providerId, idempotencyKey)` e devolve o resultado da vencedora como
replay, em vez de propagar erro para o cliente. `TestFiftyConcurrentDuplicateSubmissions`
dispara 50 submissões idênticas em paralelo e confirma um único débito.

A idempotência é **persistida** (coluna, não cache em memória) e sobrevive
a reinício do processo, sem depender de nenhum estado local.

## 5. Ledger (`internal/domain/ledger` + migration `000007`)

`ledger.Entry` é imutável; `New()` valida
`balanceAfter = balanceBefore ± amount` conforme a direção antes de
permitir a construção. A invariante é verificada no domínio, não só no
banco. No banco, `ledger_entries` tem `UNIQUE (wallet_id, transaction_id)`,
`CHECK (balance_after_minor_units >= 0)`, `CHECK (amount_delta_minor_units <> 0)`
e um trigger (`prevent_ledger_entry_mutation`) que recusa qualquer
`UPDATE`/`DELETE` na tabela. Append-only garantido pelo schema, não por
convenção da aplicação. `LOSS` e transações rejeitadas não geram
lançamento (não há movimentação a registrar).

## 6. Referências pendentes (REFUND/ROLLBACK)

Quando o `REFUND`/`ROLLBACK` chega e a referência (`providerId` +
`referenceExternalTransactionId`) ainda não existe, ou existe mas ainda
não está em `PROCESSED`, a transação é persistida como `PENDING_REFERENCE`
(commit próprio, sem esperar a resolução) e agendada para retry com
backoff exponencial (`referenceBackoff`: 5s, 10s, 20s... até um teto de
5 minutos). Um worker (`RetryWorker`, chamado pelo Fx a cada
`REFERENCE_RETRY_INTERVAL`) varre as `PENDING_REFERENCE` cujo
`reference_next_attempt_at` já venceu e tenta resolver de novo. Depois de
`maxReferenceAttempts` (8) tentativas sem sucesso, a transação é rejeitada
com `REFERENCE_NOT_FOUND_TIMEOUT`.

Quando a referência existe mas terminou **sem sucesso** (`REJECTED`/
`FAILED`), não adianta esperar. O código rejeita na hora com
`REFERENCE_NOT_PROCESSED`, sem entrar em `PENDING_REFERENCE`.

**Uma única reversão bem-sucedida por transação original**: a migration
`000004` adiciona um índice único parcial,
`(provider_id, reference_id) WHERE status = 'PROCESSED' AND kind IN ('REFUND','ROLLBACK')`.
Não importa se é `REFUND` ou `ROLLBACK`: a *segunda* reversão bem-sucedida
sobre a mesma aposta original é impedida pelo banco. Antes de chegar nesse
ponto, a aplicação já tenta travar a linha original com
`SELECT ... FOR UPDATE` (`LockByID`) e checar explicitamente se já existe
uma reversão (`HasSuccessfulReversal`), serializando tentativas
concorrentes sobre a mesma referência. O índice único é a garantia de
banco por trás disso, caso a checagem na aplicação falhe por algum motivo
(outra instância, bug, etc.).

`ROLLBACK` desfaz o movimento contrário ao original: se a aposta original
foi `BET` (débito), o rollback credita de volta; se foi `WIN`/`REFUND`
(crédito), o rollback debita, e esse débito pode falhar por saldo
insuficiente, caso em que o `failureCode` é `ROLLBACK_INSUFFICIENT_BALANCE`,
deliberadamente diferente de `INSUFFICIENT_BALANCE` (usado só para `BET`),
conforme pedido no edital.

## 7. Inbox / Outbox

**Outbox** (`internal/infrastructure/postgres/outbox_repository.go`):
todo evento é gravado na mesma transação SQL que a mudança de domínio que
o originou (`outboxevents.AppendProcessed/AppendRejected/AppendPendingReference`,
chamados dentro do mesmo `*sql.Tx` do `UPDATE`/`INSERT` de negócio). Isso
é o que garante "publicação só depois do commit da transação que a
originou": o evento só existe se a transação de negócio também existir, e
vice-versa.

Um worker separado (`OutboxPublisher`) drena a tabela por polling,
reivindicando lotes com `SELECT ... FOR UPDATE SKIP LOCKED`. Múltiplos
publishers rodando ao mesmo tempo nunca disputam a mesma linha (`SKIP
LOCKED` pula o que já está travado); se um publisher morre no meio do
lote, o lock cai com a conexão e outro publisher pega as linhas na
próxima rodada, então não existe "trabalho abandonado" permanentemente
preso. Falha ao publicar uma linha não aborta o lote: só aquela linha
recebe `attempts++` e um novo `next_attempt_at` com backoff exponencial;
`published_at` é o único id da operação, então republicar (reenviar a
mesma linha por engano) preserva o `eventId`.

**Inbox** (`internal/infrastructure/postgres/inbox_repository.go`):
unicidade em `(consumer_name, message_id)`. **Simplificação assumida,
documentada aqui por transparência**: o registro no inbox e a mudança de
domínio **não** compartilham uma única transação SQL. Rodam em duas
transações curtas separadas (`Consumer.recordInbox` e a chamada a
`wager.Service.Submit`, que abre sua própria transação internamente).
Threadear um `*sql.Tx` fornecido pelo chamador por toda a camada de
aplicação do wager (que hoje decide sozinha quando abrir/commitar cada
transação, inclusive abrindo mais de uma por operação nos fluxos de
reversão) estava fora do tempo disponível. Isso não compromete a garantia
real contra duplicação: quem impede a movimentação duplicada é o índice
único de idempotência da tabela `transactions`, o mesmo mecanismo provado
sob 50 submissões concorrentes idênticas
(`TestFiftyConcurrentDuplicateSubmissions`). O inbox, aqui, serve para
registrar a chegada da mensagem e permitir que uma redelivery seja
reconhecida (`completed_at` preenchido) sem reprocessar a lógica de
negócio: uma otimização e trilha de auditoria, não a fonte da garantia
financeira.

A mensagem só é removida da fila (`DeleteMessage`) depois que o
`Submit()` retorna sem erro **e** o inbox é marcado como `completed_at`.
Qualquer falha nesse caminho deixa a mensagem na fila para redelivery;
nada é perdido.

## 8. Autenticação e autorização

Keycloak, realm `wagering` (import automático via `deploy/keycloak/realm-export.json`).
Três clients `client_credentials`: `provider-a` e `provider-b` (cada um
com um *protocol mapper* `oidc-hardcoded-claim-mapper` que grava
`provider_id` no access token) e `internal-service` (sem `provider_id`,
mas com a role de realm `internal` atribuída à sua service account).

`httpapi.Authenticator` valida o JWT (assinatura, expiração, emissor) via
JWKS (`github.com/MicahParks/keyfunc/v3` + `github.com/golang-jwt/jwt/v5`),
extrai `provider_id` e a lista de `realm_access.roles` para o contexto da
requisição. Endpoints de carteira exigem a role `internal`; endpoints de
apostas exigem um `provider_id`, e o `providerId` do corpo da requisição é
comparado contra o do token. Se divergir, 403 (a identidade autenticada
decide o provider autorizado, o corpo nunca sobrepõe isso). Leituras de
transação por ID ou por `(providerId, externalId)` conferem que o dono da
transação é o provider autenticado; se não for, 404 (não 403, para não
confirmar a existência da transação de outro provider).

Escolha do IdP: Keycloak, por ser o recomendado no edital e ter suporte
nativo a `client_credentials` e mapeamento de claims customizados sem
código adicional (hardcoded claim mapper), o que elimina a necessidade de
um serviço de autorização próprio.

## 9. Uso do Fx (`cmd/api`)

Módulos (`fx.Module`): `config` (configuração, logger e métricas),
`persistence` (conexão e repositórios Postgres), `application` (casos de
uso), `messaging` (cliente SQS, consumidor, publisher), `http`
(autenticador e handler). `fx.Invoke` registra os quatro componentes de
ciclo de vida: servidor HTTP, consumidor SQS, publisher do outbox e
worker de retry de referência.

Cada um dos três workers em background usa o mesmo padrão
(`runBackground` em `cmd/api/lifecycle.go`): no `OnStart`, abre um
`context.WithCancel` próprio e sobe uma goroutine; no `OnStop`, cancela o
contexto e **espera** (com timeout do próprio contexto de shutdown do Fx)
a goroutine sinalizar que terminou, antes de devolver o controle. Isso é
o que torna o encerramento observável em vez de "atira e esquece". O
servidor HTTP usa `http.Server.Shutdown(ctx)`, que já implementa
"para de aceitar conexões novas, espera as em andamento" nativamente.

O domínio (`internal/domain/...`) não importa Fx, HTTP, SQS nem bibliotecas
de persistência. Só `internal/application` e `internal/infrastructure`
dependem dessas peças, e só `cmd/api` depende de Fx.

## 10. Observabilidade

Logs em JSON via `log/slog` (`cmd/api/providers.go:newLogger`). Métricas
Prometheus (`internal/observability/metrics.go`), expostas em
`GET /metrics` sem autenticação (scraping interno): `http_requests_total`
por rota/método/status, `wager_transactions_total` por status,
replays idempotentes, conflitos de concorrência (corrida na chave de
idempotência), eventos de outbox publicados/falhos, retries de
referência pendente e divergências de reconciliação.

## 11. Consistência entre `playerId` e `walletId`

`Service.Submit` carrega a carteira por `walletId` e confere que
`wallet.PlayerID() == input.PlayerID` antes de qualquer movimentação. Sem
isso, um provider autenticado poderia enviar qualquer combinação de
`walletId`/`playerId` que conseguisse adivinhar ou tivesse visto vazada em
outro lugar, movimentando uma carteira de um jogador diferente do que o
corpo da requisição diz. Divergência retorna `ErrWalletNotFound` (404),
sem distinguir "não existe" de "não é desse jogador", para não confirmar a
existência da carteira para quem não deveria enxergá-la. Isso é aplicado
na camada de aplicação (`internal/application/wager/service.go`), então
vale tanto para HTTP quanto para SQS.

**Não implementado, por escopo**: uma associação explícita
provider-para-jogador (por exemplo, uma tabela `provider_players` ou um
token de sessão que o provider emite para o jogador) que impedisse um
provider de operar sobre um jogador que nunca jogou nos jogos dele. O
edital não define esse relacionamento nem dá um contrato para ele; a
checagem de carteira/jogador acima é o controle de mais valor que dava
para implementar sem inventar uma entidade de domínio nova.

## 12. Limitações e trabalho não concluído

- **Inbox e transação de domínio em commits separados** (seção 7): maior
  simplificação deliberada do projeto, documentada ali com a justificativa
  de por que a garantia financeira não depende disso.
- `WIN` aceita uma `referenceExternalTransactionId` opcional mas **não a
  resolve nem valida** contra a aposta referenciada. O edital só exige
  isso para `REFUND`/`ROLLBACK`; para `WIN` é só informativo.
- Sem publishers do outbox disputando a mesma tabela sob um teste de carga
  dedicado. O mecanismo (`SKIP LOCKED`) foi implementado e é logicamente
  correto, mas não há um teste automatizado específico de dois publishers
  concorrentes.
- Sem partidas dobradas (double-entry ledger), tracing com
  OpenTelemetry, dashboards ou testes de carga formais. São diferenciais
  opcionais pelo próprio edital, deixados de fora dado o prazo.
- O retry de JWKS em `newAuthenticator` (`cmd/api/providers.go`) é uma
  rede de segurança para os primeiros segundos; na prática, quem resolve
  o caso de Keycloak subir depois da aplicação é o refresh em background
  da própria biblioteca `keyfunc`, validado manualmente no ambiente
  Docker (o primeiro request de autenticação falha no log, mas a
  aplicação se recupera sozinha assim que o Keycloak fica acessível, sem
  reiniciar).
- Sem testes HTTP end-to-end automatizados (só manuais, via curl, contra
  o stack Docker completo). A cobertura automatizada está concentrada no
  domínio e na camada de aplicação, que é onde vivem as garantias
  financeiras eliminatórias.
