package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the Genoma framework.
type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Sandbox   SandboxConfig
	Embedding EmbeddingConfig
	Auth      AuthConfig
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// DatabaseConfig holds PostgreSQL connection configuration.
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
	MaxConns int32
	MinConns int32
}

// DSN returns the PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.DBName, d.SSLMode,
	)
}

// RedisConfig holds Redis connection configuration.
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// SandboxConfig holds sandbox execution configuration.
type SandboxConfig struct {
	Image           string
	DockerHost      string
	DefaultCPUQuota int64
	DefaultMemoryMB int64
	DefaultTimeout  time.Duration
	MaxOutputBytes  int64
	NetworkDisabled bool
	MaxPids         int64
}

// EmbeddingConfig holds embedding service configuration.
type EmbeddingConfig struct {
	ServiceURL string
	Dimensions int
	Timeout    time.Duration
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	APIKey string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         getEnv("GENOMA_HOST", "0.0.0.0"),
			Port:         getEnvInt("GENOMA_PORT", 8080),
			ReadTimeout:  getEnvDuration("GENOMA_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: getEnvDuration("GENOMA_WRITE_TIMEOUT", 60*time.Second),
			IdleTimeout:  getEnvDuration("GENOMA_IDLE_TIMEOUT", 120*time.Second),
		},
		Database: DatabaseConfig{
			Host:     getEnv("GENOMA_DB_HOST", "localhost"),
			Port:     getEnvInt("GENOMA_DB_PORT", 5432),
			User:     getEnv("GENOMA_DB_USER", "genoma"),
			Password: getEnv("GENOMA_DB_PASSWORD", "genoma"),
			DBName:   getEnv("GENOMA_DB_NAME", "genoma"),
			SSLMode:  getEnv("GENOMA_DB_SSLMODE", "disable"),
			MaxConns: int32(getEnvInt("GENOMA_DB_MAX_CONNS", 20)),
			MinConns: int32(getEnvInt("GENOMA_DB_MIN_CONNS", 5)),
		},
		Redis: RedisConfig{
			Addr:     getEnv("GENOMA_REDIS_ADDR", "localhost:6379"),
			Password: getEnv("GENOMA_REDIS_PASSWORD", ""),
			DB:       getEnvInt("GENOMA_REDIS_DB", 0),
		},
		Sandbox: SandboxConfig{
			Image:           getEnv("GENOMA_SANDBOX_IMAGE", "genoma-sandbox:latest"),
			DockerHost:      getEnv("GENOMA_DOCKER_HOST", "unix:///var/run/docker.sock"),
			DefaultCPUQuota: int64(getEnvInt("GENOMA_SANDBOX_CPU_QUOTA", 50000)),
			DefaultMemoryMB: int64(getEnvInt("GENOMA_SANDBOX_MEMORY_MB", 256)),
			DefaultTimeout:  getEnvDuration("GENOMA_SANDBOX_TIMEOUT", 30*time.Second),
			MaxOutputBytes:  int64(getEnvInt("GENOMA_SANDBOX_MAX_OUTPUT", 1048576)), // 1MB
			NetworkDisabled: getEnvBool("GENOMA_SANDBOX_NO_NETWORK", true),
			MaxPids:         int64(getEnvInt("GENOMA_SANDBOX_MAX_PIDS", 50)),
		},
		Embedding: EmbeddingConfig{
			ServiceURL: getEnv("GENOMA_EMBEDDING_URL", "http://localhost:5050"),
			Dimensions: getEnvInt("GENOMA_EMBEDDING_DIMS", 384),
			Timeout:    getEnvDuration("GENOMA_EMBEDDING_TIMEOUT", 10*time.Second),
		},
		Auth: AuthConfig{
			APIKey: getEnv("GENOMA_API_KEY", ""),
		},
	}
}

// --- Helper functions ---

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
