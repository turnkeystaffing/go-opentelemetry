package opentelemetry

import (
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// InstrumentRedisTracing adds OTel tracing hooks to a go-redis client.
// All Redis commands produce child spans with db.system=redis.
//
// Usage:
//
//	if err := opentelemetry.InstrumentRedisTracing(rdb); err != nil {
//	    log.Warn("failed to instrument Redis tracing", "error", err)
//	}
func InstrumentRedisTracing(rdb redis.UniversalClient, opts ...redisotel.TracingOption) error {
	return redisotel.InstrumentTracing(rdb, opts...)
}

// InstrumentRedisMetrics adds OTel metrics hooks to a go-redis client.
//
// Usage:
//
//	if err := opentelemetry.InstrumentRedisMetrics(rdb); err != nil {
//	    log.Warn("failed to instrument Redis metrics", "error", err)
//	}
func InstrumentRedisMetrics(rdb redis.UniversalClient, opts ...redisotel.MetricsOption) error {
	return redisotel.InstrumentMetrics(rdb, opts...)
}

// InstrumentRedis instruments a go-redis client with both tracing and metrics.
// Tracing and metrics failures are returned separately; neither blocks the other.
func InstrumentRedis(rdb redis.UniversalClient) (tracingErr, metricsErr error) {
	tracingErr = InstrumentRedisTracing(rdb)
	metricsErr = InstrumentRedisMetrics(rdb)
	return
}
