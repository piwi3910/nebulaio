package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gopkg.in/yaml.v3"
)

// File permission constants.
const (
	dirPermissions  = 0700
	filePermissions = 0600
	// S3 URI prefix length ("s3://").
	s3URIPrefixLen = 5
)

// ClientConfig holds the CLI configuration.
type ClientConfig struct {
	Endpoint   string `yaml:"endpoint"`
	AccessKey  string `yaml:"access_key"`
	SecretKey  string `yaml:"secret_key"`
	Region     string `yaml:"region"`
	UseSSL     bool   `yaml:"use_ssl"`
	SkipVerify bool   `yaml:"skip_verify"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		Endpoint: "http://localhost:9000",
		Region:   "us-east-1",
		UseSSL:   false,
	}
}

// configPath returns the path to the config file.
func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nebulaio", "config.yaml")
}

// LoadConfig loads the configuration from file or environment.
func LoadConfig() (*ClientConfig, error) {
	cfg := DefaultConfig()

	// Try to load from file
	data, err := os.ReadFile(configPath())
	if err == nil {
		err := yaml.Unmarshal(data, cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid config file: %w", err)
		}
	}

	// Override with environment variables
	if endpoint := os.Getenv("NEBULAIO_ENDPOINT"); endpoint != "" {
		cfg.Endpoint = endpoint
	}

	if accessKey := os.Getenv("NEBULAIO_ACCESS_KEY"); accessKey != "" {
		cfg.AccessKey = accessKey
	} else if accessKey := os.Getenv("AWS_ACCESS_KEY_ID"); accessKey != "" {
		cfg.AccessKey = accessKey
	}

	if secretKey := os.Getenv("NEBULAIO_SECRET_KEY"); secretKey != "" {
		cfg.SecretKey = secretKey
	} else if secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY"); secretKey != "" {
		cfg.SecretKey = secretKey
	}

	if region := os.Getenv("NEBULAIO_REGION"); region != "" {
		cfg.Region = region
	} else if region := os.Getenv("AWS_REGION"); region != "" {
		cfg.Region = region
	}

	return cfg, nil
}

// SaveConfig saves the configuration to file.
func SaveConfig(cfg *ClientConfig) error {
	path := configPath()

	// Create directory if needed
	err := os.MkdirAll(filepath.Dir(path), dirPermissions)
	if err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	err = os.WriteFile(path, data, filePermissions)
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// NewS3Client creates an S3 client from the configuration.
func NewS3Client(ctx context.Context) (*s3.Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("credentials not configured. Use 'nebulaio-cli config set access-key <key>' or set environment variables")
	}

	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with path-style addressing and custom endpoint
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(cfg.Endpoint)
	})

	return client, nil
}

// ParseS3URI parses an S3 URI (s3://bucket/key) into bucket and key.
func ParseS3URI(uri string) (string, string, bool) {
	if len(uri) < s3URIPrefixLen || uri[:s3URIPrefixLen] != "s3://" {
		return "", "", false
	}

	path := uri[s3URIPrefixLen:]
	if path == "" {
		return "", "", false
	}

	// Find first slash after bucket name
	for i := range len(path) {
		if path[i] == '/' {
			return path[:i], path[i+1:], true
		}
	}

	// No key, just bucket
	return path, "", true
}

// FormatSize formats a byte size to human-readable format.
func FormatSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case size >= TB:
		return fmt.Sprintf("%.2f TB", float64(size)/TB)
	case size >= GB:
		return fmt.Sprintf("%.2f GB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.2f MB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.2f KB", float64(size)/KB)
	default:
		return fmt.Sprintf("%d B", size)
	}
}
