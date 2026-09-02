package config

import (
	"fmt"
	"os"
)

type Config struct {
	HTTPPort string

	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string

	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIOUseSSL    bool

	RabbitMQURL string
}

func Load() (Config, error) {
	return Config{
		HTTPPort:         getEnv("HTTP_PORT", "8080"),
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5433"),
		PostgresUser:     getEnv("POSTGRES_USER", "avatar"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "avatar"),
		PostgresDB:       getEnv("POSTGRES_DB", "avatar"),

		MinIOEndpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey: getEnv("MINIO_ACCESS_KEY", "minio"),
		MinIOSecretKey: getEnv("MINIO_SECRET_KEY", "miniosecret"),
		MinIOBucket:    getEnv("MINIO_BUCKET", "avatars"),
		MinIOUseSSL:    getEnv("MINIO_USE_SSL", "false") == "true",

		RabbitMQURL: getEnv(
			"RABBITMQ_URL",
			"amqp://avatar:avatar@localhost:5672/",
		),
	}, nil
}

func (c Config) HTTPAddress() string {
	return fmt.Sprintf(":%s", c.HTTPPort)
}

func (c Config) PostgresURL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.PostgresUser,
		c.PostgresPassword,
		c.PostgresHost,
		c.PostgresPort,
		c.PostgresDB,
	)
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
