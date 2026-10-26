package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadMigrationOnlyDoesNotRequireRuntimeDependencies(t *testing.T) {
	t.Setenv("MIGRATION_ONLY", "true")
	t.Setenv("POSTGRES_PASSWORD", "password")

	cfg, err := LoadForService("migration")

	require.NoError(t, err)
	require.True(t, cfg.MigrationOnly)
	require.True(t, cfg.RunMigrations)
}

func TestLoadRequiresOTelExporterForApplication(t *testing.T) {
	t.Setenv("MIGRATION_ONLY", "false")
	t.Setenv("POSTGRES_PASSWORD", "password")
	t.Setenv("MINIO_ACCESS_KEY", "access")
	t.Setenv("MINIO_SECRET_KEY", "secret")
	t.Setenv("RABBITMQ_URL", "amqp://localhost")
	t.Setenv("OTEL_EXPORTER_ENDPOINT", "")

	_, err := LoadForService("server")

	require.EqualError(t, err, `required environment variable "OTEL_EXPORTER_ENDPOINT" is not set`)
}
