package s3

import "testing"

func setTestEnv(t *testing.T) {
	t.Helper()

	t.Setenv("POSTGRES_PASSWORD", "avatar")
	t.Setenv("MINIO_ACCESS_KEY", "minio")
	t.Setenv("MINIO_SECRET_KEY", "miniosecret")
	t.Setenv("RABBITMQ_URL", "amqp://avatar:avatar@localhost:5672/")
}
