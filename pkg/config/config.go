package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	ConfigFilename        = "config.yaml"
	DefaultConcurrency    = 5
	TokenPassphraseEnvVar = "ENCRYPT_PASSPHRASE"
)

type Config struct {
	Sources     []SourceConfig `yaml:"sources"`
	Concurrency int            `yaml:"concurrency"`
}

type SourceConfig struct {
	ID               string   `yaml:"id"`
	Platform         string   `yaml:"platform"`
	Username         string   `yaml:"username"`
	Token            string   `yaml:"token"`
	OnlyIncludeRepos []string `yaml:"only_include_repos"`
	ExcludeRepos     []string `yaml:"exclude_repos"`
}

type LoadedConfig struct {
	Path string
	Node *yaml.Node
	Data Config
}

type SecretOptions struct {
	Passphrase string
}

func PathForDataDir(dataDir string) string {
	return filepath.Join(dataDir, ConfigFilename)
}

func Load(path string) (LoadedConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return LoadedConfig{}, fmt.Errorf("read config file: %w", err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return LoadedConfig{}, fmt.Errorf("parse config file: %w", err)
	}

	cfg := Config{
		Concurrency: DefaultConcurrency,
	}
	if err := document.Decode(&cfg); err != nil {
		return LoadedConfig{}, fmt.Errorf("decode config file: %w", err)
	}

	return LoadedConfig{
		Path: path,
		Node: &document,
		Data: cfg,
	}, nil
}

func LoadFromDataDir(dataDir string) (LoadedConfig, error) {
	return Load(PathForDataDir(dataDir))
}

func LoadResolved(path string, opts SecretOptions) (LoadedConfig, error) {
	loaded, err := Load(path)
	if err != nil {
		return LoadedConfig{}, err
	}

	resolvedData, err := resolveConfigSecrets(loaded.Data, opts)
	if err != nil {
		return LoadedConfig{}, fmt.Errorf("resolve config secrets: %w", err)
	}

	loaded.Data = resolvedData

	return loaded, nil
}
