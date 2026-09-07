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
	postgresPassword, err := getRequiredEnv("POSTGRES_PASSWORD")
	if err != nil {
		return Config{}, err
	}

	minIOAccessKey, err := getRequiredEnv("MINIO_ACCESS_KEY")
	if err != nil {
		return Config{}, err
	}

	minIOSecretKey, err := getRequiredEnv("MINIO_SECRET_KEY")
	if err != nil {
		return Config{}, err
	}

	rabbitMQURL, err := getRequiredEnv("RABBITMQ_URL")
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPPort:         getEnv("HTTP_PORT", "8080"),
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5433"),
		PostgresUser:     getEnv("POSTGRES_USER", "avatar"),
		PostgresPassword: postgresPassword,
		PostgresDB:       getEnv("POSTGRES_DB", "avatar"),
		MinIOEndpoint:    getEnv("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:   minIOAccessKey,
		MinIOSecretKey:   minIOSecretKey,
		MinIOBucket:      getEnv("MINIO_BUCKET", "avatars"),
		MinIOUseSSL:      getEnv("MINIO_USE_SSL", "false") == "true",
		RabbitMQURL:      rabbitMQURL,
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

func getRequiredEnv(key string) (string, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return "", fmt.Errorf("required environment variable %q is not set", key)
	}

	return value, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
