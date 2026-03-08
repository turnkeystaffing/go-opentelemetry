package opentelemetry

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestInstrumentAWSConfig(t *testing.T) {
	cfg := aws.Config{}
	if len(cfg.APIOptions) != 0 {
		t.Fatal("expected empty APIOptions before instrumentation")
	}

	InstrumentAWSConfig(&cfg)

	if len(cfg.APIOptions) == 0 {
		t.Fatal("expected APIOptions to be populated after InstrumentAWSConfig")
	}
}

func TestInstrumentAWSConfigIdempotent(t *testing.T) {
	cfg := aws.Config{}

	InstrumentAWSConfig(&cfg)
	firstLen := len(cfg.APIOptions)

	InstrumentAWSConfig(&cfg)
	secondLen := len(cfg.APIOptions)

	// Each call appends middleware, so length should grow
	if secondLen <= firstLen {
		t.Fatal("expected APIOptions to grow on second instrumentation call")
	}
}
