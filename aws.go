package opentelemetry

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
)

// LoadAWSConfigWithOTel loads an AWS SDK v2 config with OTel instrumentation.
// All AWS API calls (S3, SES, etc.) produce child spans automatically.
//
// Usage:
//
//	cfg, err := opentelemetry.LoadAWSConfigWithOTel(ctx,
//	    awsconfig.WithRegion("us-east-1"),
//	)
//	s3Client := s3.NewFromConfig(cfg)
func LoadAWSConfigWithOTel(ctx context.Context, optFns ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return cfg, err
	}
	otelaws.AppendMiddlewares(&cfg.APIOptions)
	return cfg, nil
}

// InstrumentAWSConfig adds OTel middleware to an existing AWS SDK v2 config.
// Use this when you already have a loaded config and want to add instrumentation.
//
// Usage:
//
//	opentelemetry.InstrumentAWSConfig(&cfg)
//	s3Client := s3.NewFromConfig(cfg)
func InstrumentAWSConfig(cfg *aws.Config) {
	otelaws.AppendMiddlewares(&cfg.APIOptions)
}
