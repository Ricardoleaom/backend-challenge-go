# Desafio Backend: Processamento Distribuído de Apostas

Serviço em Go que movimenta carteiras de jogadores via HTTP e SQS, com
idempotência persistente, ledger auditável e autenticação OAuth2/OIDC via
Keycloak.

## Pré-requisitos

- Docker e Docker Compose
- Go 1.23+ (só necessário para rodar `go test` fora do container; o build
  da imagem baixa a toolchain exata via `GOTOOLCHAIN=auto`)

## Subindo o ambiente

```sh
docker compose up --build
```

Isso sobe, nessa ordem: Postgres, as migrations (`migrate` roda uma vez e
sai), Keycloak (importa o realm automaticamente) e LocalStack (cria as
filas SQS via script de init), e por fim a aplicação.

Serviços expostos:

| Serviço | Porta |
| --- | --- |
| API | `http://localhost:8080` |
| Keycloak | `http://localhost:8081` |
| LocalStack (SQS) | `http://localhost:4566` |
| Postgres | `localhost:5432` |

## Variáveis de ambiente

Veja `.env.example`. Ao rodar a aplicação fora do compose (ex.: `go run
./cmd/api`), exporte essas variáveis apontando para `localhost` em vez dos
nomes de serviço do compose.

## Identidades de teste (Keycloak)

O realm `wagering` é importado automaticamente a partir de
`deploy/keycloak/realm-export.json` com três clients `client_credentials`:

| Client | Secret | Identidade |
| --- | --- | --- |
| `provider-a` | `provider-a-secret` | `provider_id=provider-a` |
| `provider-b` | `provider-b-secret` | `provider_id=provider-b` |
| `internal-service` | `internal-service-secret` | role de realm `internal` |

Obtendo um token:

```sh
curl -s -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials \
  -d client_id=provider-a \
  -d client_secret=provider-a-secret \
  | jq -r .access_token
```

Endpoints de carteira (`/wallets/*`) exigem o token do `internal-service`.
Endpoints de apostas (`/wagering/transactions`, `/providers/.../transactions/...`)
exigem um token de provider; o `providerId` do corpo tem que bater com o
`provider_id` do token.

## Migrations

```sh
# aplicar
docker run --rm -v "$(pwd)/migrations:/migrations" --network host \
  migrate/migrate -path=/migrations \
  -database "postgres://app:local_dev_password@localhost:5432/backend_challenge?sslmode=disable" up

# reverter uma
docker run --rm -v "$(pwd)/migrations:/migrations" --network host \
  migrate/migrate -path=/migrations \
  -database "postgres://app:local_dev_password@localhost:5432/backend_challenge?sslmode=disable" down 1
```

No `docker compose up`, isso já roda automaticamente pelo serviço `migrate`.

## Exemplos de chamada

Abrir carteira (internal):

```sh
curl -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $INTERNAL_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"1000.00","currency":"BRL"}}'
```

Enviar uma aposta (provider):

```sh
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $PROVIDER_TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:transaction-123" \
  -d '{
    "providerId": "provider-a",
    "externalTransactionId": "transaction-123",
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "walletId": "<id retornado na abertura>",
    "roundId": "round-987",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "money": {"amount": "25.00", "currency": "BRL"}
  }'
```

Mandar a mesma operação por SQS (equivalente ao HTTP acima):

```sh
aws --endpoint-url http://localhost:4566 sqs send-message \
  --queue-url http://sqs.us-east-1.localhost.localstack.cloud:4566/000000000000/wager-transactions.fifo \
  --message-group-id "<walletId>" \
  --message-deduplication-id "provider-a:transaction-123" \
  --message-body '{
    "messageId": "msg-123",
    "type": "WagerTransactionRequested",
    "occurredAt": "2026-10-14T12:00:00.000Z",
    "data": {
      "providerId": "provider-a",
      "externalTransactionId": "transaction-123",
      "idempotencyKey": "provider-a:transaction-123",
      "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
      "walletId": "<id retornado na abertura>",
      "roundId": "round-987",
      "gameId": "fortune-chimp",
      "kind": "BET",
      "money": {"amount": "25.00", "currency": "BRL"}
    }
  }'
```

Demais endpoints: ver `ARCHITECTURE.md` para a lista completa e os códigos
de erro.

## Testes

```sh
go vet ./...
go test ./...
go test -race ./...
```

Os testes de integração (pacotes `internal/infrastructure/postgres`,
`internal/application/wager` e `internal/application/wallet`) precisam de
um Postgres real e são pulados automaticamente se `DATABASE_URL` não
estiver definida. Para rodá-los:

```sh
docker compose up -d postgres
docker run --rm -v "$(pwd)/migrations:/migrations" --network host \
  migrate/migrate -path=/migrations \
  -database "postgres://app:local_dev_password@localhost:5432/backend_challenge?sslmode=disable" up

DATABASE_URL="postgres://app:local_dev_password@localhost:5432/backend_challenge?sslmode=disable" \
  go test ./... -race
```

Os testes do pacote `internal/application/wager` cobrem o cenário
obrigatório de concorrência (`TestConcurrentBetsOnSameWallet`: carteira
com 100.00 BRL recebendo duas apostas de 80.00 BRL ao mesmo tempo) e o de
duplicidade (`TestFiftyConcurrentDuplicateSubmissions`: a mesma aposta 50
vezes em paralelo). Ambos, mais `TestConcurrentBetsAcrossThreeIndependentInstances`,
rodam cada submissão a partir de um `Service` com seu próprio `*sql.DB`,
simulando processos de aplicação independentes disputando a mesma carteira,
não só goroutines de um único processo.

`cmd/api` tem um teste (`TestFxAppStartsAndStopsCleanly`) que sobe a
composição Fx inteira (todos os módulos e workers do `main.go`) e confere
que o start e o stop terminam sem erro, com liberação dos workers em
background. Ele precisa do ambiente completo (Postgres, Keycloak e
LocalStack) e é pulado se `DATABASE_URL`, `JWKS_URL`,
`SQS_TRANSACTIONS_QUEUE_URL` ou `SQS_EVENTS_QUEUE_URL` não estiverem
definidas. Para rodá-lo, suba a stack completa e exporte as mesmas
variáveis usadas pelo serviço `app` no `compose.yaml`:

```sh
docker compose up -d postgres keycloak localstack

DATABASE_URL="postgres://app:local_dev_password@localhost:5432/backend_challenge?sslmode=disable" \
JWKS_URL="http://localhost:8081/realms/wagering/protocol/openid-connect/certs" \
SQS_ENDPOINT="http://localhost:4566" \
SQS_TRANSACTIONS_QUEUE_URL="http://localhost:4566/000000000000/wager-transactions.fifo" \
SQS_EVENTS_QUEUE_URL="http://localhost:4566/000000000000/wager-events.fifo" \
  go test ./cmd/api/... -race
```

## Lint

```sh
golangci-lint run ./...
```

Configuração em `.golangci.yml`.

## Limitações conhecidas

Ver seção "Limitações e trabalho não concluído" em `ARCHITECTURE.md`.
