// Package sqsx wires the application to AWS SQS (or a LocalStack endpoint
// standing in for it locally).
package sqsx

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type Config struct {
	Region               string
	Endpoint             string // non-empty to target LocalStack instead of real AWS
	AccessKeyID          string
	SecretAccessKey      string
	TransactionsQueueURL string
	EventsQueueURL       string
}

func NewClient(ctx context.Context, cfg Config) (*sqs.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	return sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint
		}
	}), nil
}
