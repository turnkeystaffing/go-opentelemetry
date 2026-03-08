package opentelemetry

import (
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// NewPgxTracer creates an otelpgx tracer for pgx connection configs.
// It sets DB system to PostgreSQL and includes the database namespace.
//
// Usage:
//
//	poolConfig.ConnConfig.Tracer = opentelemetry.NewPgxTracer("mydb")
func NewPgxTracer(dbName string, extraAttrs ...attribute.KeyValue) pgx.QueryTracer {
	opts := []otelpgx.Option{
		otelpgx.WithTrimSQLInSpanName(),
		otelpgx.WithAttributes(
			append([]attribute.KeyValue{
				semconv.DBSystemPostgreSQL,
				semconv.DBNamespace(dbName),
			}, extraAttrs...)...,
		),
	}
	return otelpgx.NewTracer(opts...)
}

// RecordPgxPoolStats registers pgxpool connection metrics with OTel.
// Returns an error if metric registration fails (non-fatal, log and continue).
//
// Usage:
//
//	if err := opentelemetry.RecordPgxPoolStats(pool); err != nil {
//	    log.Warn("failed to register pgxpool metrics", "error", err)
//	}
func RecordPgxPoolStats(pool *pgxpool.Pool) error {
	return otelpgx.RecordStats(pool)
}
