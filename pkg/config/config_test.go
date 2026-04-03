package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testPassphrase = "correct horse battery staple"

func TestLoadDefaultsConcurrency(t *testing.T) {
	encryptedToken := mustEncryptToken(t, "secret", testPassphrase)
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if loaded.Data.Concurrency != DefaultConcurrency {
		t.Fatalf("expected default concurrency %d, got %d", DefaultConcurrency, loaded.Data.Concurrency)
	}
	if !loaded.Data.Sources[0].IncludeDefaults {
		t.Fatal("expected include_defaults to default to true")
	}
	if loaded.Data.Sources[0].IncludeStarred {
		t.Fatal("expected include_starred to default to false")
	}
	if loaded.Data.Sources[0].IncludeWatching {
		t.Fatal("expected include_watching to default to false")
	}
}

func TestLoadPreservesExplicitIncludeDefaultsFalse(t *testing.T) {
	encryptedToken := mustEncryptToken(t, "secret", testPassphrase)
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
    include_defaults: false
    include_starred: true
    include_watching: true
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	source := loaded.Data.Sources[0]
	if source.IncludeDefaults {
		t.Fatal("expected explicit include_defaults=false to be preserved")
	}
	if !source.IncludeStarred {
		t.Fatal("expected include_starred=true to be preserved")
	}
	if !source.IncludeWatching {
		t.Fatal("expected include_watching=true to be preserved")
	}
}

func TestValidateConfigReportsExpectedIssues(t *testing.T) {
	encryptedToken := mustEncryptToken(t, "secret", testPassphrase)
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
    token: `+encryptedToken+`
concurrency: 0
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateConfig(loaded, SecretOptions{Passphrase: testPassphrase})

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
	assertIssueMessage(t, issues, "empty_sources", "sources", "no source configured")
}

func TestValidateConfigWarnsWhenSourcesIsMissing(t *testing.T) {
	configPath := writeConfigFile(t, `
concurrency: 3
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateConfig(loaded)
	assertHasIssue(t, issues, "empty_sources", "sources")
	assertIssueMessage(t, issues, "empty_sources", "sources", "no source configured")
}

func TestValidateConfigWarnsWhenSourcesHasNoValue(t *testing.T) {
	configPath := writeConfigFile(t, `
sources:
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateConfig(loaded)
	assertHasIssue(t, issues, "empty_sources", "sources")
	assertIssueMessage(t, issues, "empty_sources", "sources", "no source configured")
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
	encryptedToken := mustEncryptToken(t, "secret", testPassphrase)
	configPath := writeConfigFile(t, `
unknown_top_level: true
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
    unknown_source_field: true
  - id: github
    platform: github
    username: hubot
    token: `+encryptedToken+`
concurrency: 0
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateSource(loaded, "github", SecretOptions{Passphrase: testPassphrase})

	assertHasIssue(t, issues, "unknown_field", "sources[0].unknown_source_field")
	assertHasIssue(t, issues, "duplicate_source_id", "sources[0].id")
	assertHasIssue(t, issues, "duplicate_source_id", "sources[1].id")
	assertNoIssue(t, issues, "unknown_field", "unknown_top_level")
	assertNoIssue(t, issues, "invalid_concurrency", "concurrency")
}

func TestValidateSourceReturnsNotFoundWhenIDDoesNotExist(t *testing.T) {
	encryptedToken := mustEncryptToken(t, "secret", testPassphrase)
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateSource(loaded, "missing", SecretOptions{Passphrase: testPassphrase})
	assertHasIssue(t, issues, "source_not_found", "sources")
}

func TestValidateConfigRejectsPlaintextToken(t *testing.T) {
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

	issues := ValidateConfig(loaded, SecretOptions{Passphrase: testPassphrase})
	assertHasIssue(t, issues, "unencrypted_token", "sources[0].token")
}

func TestValidateConfigRejectsInvalidEncryptedToken(t *testing.T) {
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: $encrypted$1$!!!
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateConfig(loaded, SecretOptions{Passphrase: testPassphrase})
	assertHasIssue(t, issues, "invalid_encrypted_token", "sources[0].token")
}

func TestValidateConfigRejectsWrongPassphrase(t *testing.T) {
	encryptedToken := mustEncryptToken(t, "secret", testPassphrase)
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	issues := ValidateConfig(loaded, SecretOptions{Passphrase: "wrong passphrase"})
	assertHasIssue(t, issues, "token_decryption_failed", "sources[0].token")
}

func TestLoadResolvedDecryptsTokens(t *testing.T) {
	encryptedToken := mustEncryptToken(t, "secret", testPassphrase)
	configPath := writeConfigFile(t, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)

	loaded, err := LoadResolved(configPath, SecretOptions{Passphrase: testPassphrase})
	if err != nil {
		t.Fatalf("unexpected resolved load error: %v", err)
	}

	if len(loaded.Data.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(loaded.Data.Sources))
	}
	if loaded.Data.Sources[0].Token != "secret" {
		t.Fatalf("expected decrypted token, got %q", loaded.Data.Sources[0].Token)
	}
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

func mustEncryptToken(t *testing.T, plaintext string, passphrase string) string {
	t.Helper()

	encryptedToken, err := EncryptToken(plaintext, passphrase)
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}

	return encryptedToken
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

func assertIssueMessage(t *testing.T, issues []ValidationIssue, code string, path string, message string) {
	t.Helper()

	for _, issue := range issues {
		if issue.Code != code {
			continue
		}
		if path != "" && issue.Path != path {
			continue
		}
		if issue.Message != message {
			t.Fatalf("expected issue code=%q path=%q to have message %q, got %q", code, path, message, issue.Message)
		}
		return
	}

	t.Fatalf("expected issue code=%q path=%q message=%q, got %#v", code, path, message, issues)
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
