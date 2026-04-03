package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func DefaultConfig() Config {
	return Config{
		Sources:       []SourceConfig{},
		Concurrency:   DefaultConcurrency,
		MaxRetryTimes: DefaultMaxRetryTimes,
	}
}

func LoadOrDefault(path string) (LoadedConfig, bool, error) {
	loaded, err := Load(path)
	if err == nil {
		return loaded, true, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return LoadedConfig{}, false, err
	}

	return LoadedConfig{
		Path: path,
		Data: DefaultConfig(),
	}, false, nil
}

func Save(path string, cfg Config) error {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.MaxRetryTimes < 0 {
		cfg.MaxRetryTimes = DefaultMaxRetryTimes
	}
	if cfg.Sources == nil {
		cfg.Sources = []SourceConfig{}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&cfg); err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close config encoder: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(buffer.Bytes()); err != nil {
		tempFile.Close()
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp config file: %w", err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp config file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}

	return nil
}
