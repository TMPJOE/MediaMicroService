package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Server struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
}

type JWT struct {
	Secret     string `yaml:"secret"`
	Issuer     string `yaml:"issuer"`
	Expiration string `yaml:"expiration"`
}

type Database struct {
	URL string `yaml:"url"`
}

type Health struct {
	Path      string `yaml:"path"`
	ReadyPath string `yaml:"ready_path"`
}

// MinIOService holds the configuration for connecting to the MinIO service
// via its HTTP API (not direct MinIO SDK access).
type MinIOService struct {
	URL    string `yaml:"url"`    // Base URL of the MinIO service (e.g. "http://minio-service:8080")
	Bucket string `yaml:"bucket"` // Default bucket name to use for uploads
}

type Config struct {
	Server         Server                 `yaml:"server"`
	Logging        map[string]interface{} `yaml:"logging"`
	RateLimit      map[string]interface{} `yaml:"rate_limit"`
	CircuitBreaker map[string]interface{} `yaml:"circuit_breaker"`
	Health         Health                 `yaml:"health"`
	JWT            JWT                    `yaml:"jwt"`
	MinIOService   MinIOService           `yaml:"minio_service"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := os.ExpandEnv(string(b))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}
