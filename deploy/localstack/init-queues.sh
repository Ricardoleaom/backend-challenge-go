#!/bin/sh
set -eu

create_fifo_with_dlq() {
  name="$1"
  dlq_name="${name%.fifo}-dlq.fifo"

  dlq_url=$(awslocal sqs create-queue \
    --queue-name "$dlq_name" \
    --attributes FifoQueue=true,ContentBasedDeduplication=false \
    --query QueueUrl --output text)

  dlq_arn=$(awslocal sqs get-queue-attributes \
    --queue-url "$dlq_url" \
    --attribute-names QueueArn \
    --query 'Attributes.QueueArn' --output text)

  attrs_file=$(mktemp)
  cat > "$attrs_file" <<JSON
{
  "FifoQueue": "true",
  "ContentBasedDeduplication": "false",
  "VisibilityTimeout": "30",
  "RedrivePolicy": "{\"deadLetterTargetArn\":\"$dlq_arn\",\"maxReceiveCount\":\"5\"}"
}
JSON

  awslocal sqs create-queue \
    --queue-name "$name" \
    --attributes "file://$attrs_file"

  rm -f "$attrs_file"
}

create_fifo_with_dlq wager-transactions.fifo
create_fifo_with_dlq wager-events.fifo

echo "SQS queues ready."
