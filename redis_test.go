package opentelemetry

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestInstrumentRedisTracing(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	err := InstrumentRedisTracing(rdb)
	if err != nil {
		t.Fatalf("InstrumentRedisTracing failed: %v", err)
	}
}

func TestInstrumentRedisMetrics(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	err := InstrumentRedisMetrics(rdb)
	if err != nil {
		t.Fatalf("InstrumentRedisMetrics failed: %v", err)
	}
}

func TestInstrumentRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	tracingErr, metricsErr := InstrumentRedis(rdb)
	if tracingErr != nil {
		t.Fatalf("InstrumentRedis tracing failed: %v", tracingErr)
	}
	if metricsErr != nil {
		t.Fatalf("InstrumentRedis metrics failed: %v", metricsErr)
	}
}
