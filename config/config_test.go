package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsConcurrency(t *testing.T) {
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: secret
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if loaded.Data.Concurrency != DefaultConcurrency {
		t.Fatalf("expected default concurrency %d, got %d", DefaultConcurrency, loaded.Data.Concurrency)
	}
}

func TestValidateConfigReportsExpectedIssues(t *testing.T) {
	configPath := writeConfigFile(t, `
unknown_top_level: true
sources:
  - id: github
    platform: gitlab
    username: octocat
    unknown_source_field: true
  - id: github
    platform: github
    username: octocat
    token: secret
concurrency: 0
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateConfig(loaded)

	assertHasIssue(t, issues, "unknown_field", "unknown_top_level")
	assertHasIssue(t, issues, "unknown_field", "sources[0].unknown_source_field")
	assertHasIssue(t, issues, "missing_required_field", "sources[0].token")
	assertHasIssue(t, issues, "unsupported_platform", "sources[0].platform")
	assertHasIssue(t, issues, "duplicate_source_id", "sources[0].id")
	assertHasIssue(t, issues, "duplicate_source_id", "sources[1].id")
	assertHasIssue(t, issues, "invalid_concurrency", "concurrency")
}

func TestValidateConfigWarnsOnEmptySources(t *testing.T) {
	configPath := writeConfigFile(t, `
sources: []
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateConfig(loaded)
	assertHasIssue(t, issues, "empty_sources", "sources")
}

func TestCheckFileReturnsInvalidYAMLIssue(t *testing.T) {
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: [unterminated
`)

	result := CheckFile(configPath)
	if !result.Exists {
		t.Fatal("expected config file to exist")
	}

	assertHasIssue(t, result.Issues, "invalid_yaml", "")
}

func TestValidateSourceScopesIssuesToTargetSource(t *testing.T) {
	configPath := writeConfigFile(t, `
unknown_top_level: true
sources:
  - id: github
    platform: github
    username: octocat
    token: secret
    unknown_source_field: true
  - id: github
    platform: github
    username: hubot
    token: secret
concurrency: 0
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateSource(loaded, "github")

	assertHasIssue(t, issues, "unknown_field", "sources[0].unknown_source_field")
	assertHasIssue(t, issues, "duplicate_source_id", "sources[0].id")
	assertHasIssue(t, issues, "duplicate_source_id", "sources[1].id")
	assertNoIssue(t, issues, "unknown_field", "unknown_top_level")
	assertNoIssue(t, issues, "invalid_concurrency", "concurrency")
}

func TestValidateSourceReturnsNotFoundWhenIDDoesNotExist(t *testing.T) {
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: secret
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateSource(loaded, "missing")
	assertHasIssue(t, issues, "source_not_found", "sources")
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()

	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, ConfigFilename)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	return configPath
}

func assertHasIssue(t *testing.T, issues []ValidationIssue, code string, path string) {
	t.Helper()

	for _, issue := range issues {
		if issue.Code != code {
			continue
		}
		if path != "" && issue.Path != path {
			continue
		}
		return
	}

	t.Fatalf("expected issue code=%q path=%q, got %#v", code, path, issues)
}

func assertNoIssue(t *testing.T, issues []ValidationIssue, code string, path string) {
	t.Helper()

	for _, issue := range issues {
		if issue.Code != code {
			continue
		}
		if path != "" && issue.Path != path {
			continue
		}

		t.Fatalf("did not expect issue code=%q path=%q, got %#v", code, path, issues)
	}
}
