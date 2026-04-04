package taskservice

import (
	"os"
	"testing"

	appconfig "github.com/ImSingee/git-plus/pkg/config"
)

const serviceTestPassphrase = "correct horse battery staple"

func TestLoadResolvedSourceDecryptsOnlyRequestedSource(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serviceTestPassphrase)

	validToken, err := appconfig.EncryptToken("good-token", serviceTestPassphrase)
	if err != nil {
		t.Fatalf("encrypt valid token: %v", err)
	}

	otherToken, err := appconfig.EncryptToken("bad-token", "wrong-passphrase")
	if err != nil {
		t.Fatalf("encrypt invalid token: %v", err)
	}

	dataDir := t.TempDir()
	configPath := appconfig.PathForDataDir(dataDir)
	if err := appconfig.Save(configPath, appconfig.Config{
		Sources: []appconfig.SourceConfig{
			{
				ID:              "healthy",
				Platform:        "github",
				Username:        "octocat",
				Token:           validToken,
				IncludeDefaults: true,
			},
			{
				ID:              "broken",
				Platform:        "github",
				Username:        "hubot",
				Token:           otherToken,
				IncludeDefaults: true,
			},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	server := newServiceServer(dataDir, nil)
	source, err := server.loadResolvedSource("healthy")
	if err != nil {
		t.Fatalf("load resolved source: %v", err)
	}

	if source.ID != "healthy" {
		t.Fatalf("unexpected source id: %q", source.ID)
	}
	if source.Token != "good-token" {
		t.Fatalf("expected decrypted token, got %q", source.Token)
	}
}

func TestLoadResolvedSourceReturnsNotFoundWhenSourceMissing(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serviceTestPassphrase)

	dataDir := t.TempDir()
	configPath := appconfig.PathForDataDir(dataDir)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := appconfig.Save(configPath, appconfig.Config{
		Sources: []appconfig.SourceConfig{
			{
				ID:              "healthy",
				Platform:        "github",
				Username:        "octocat",
				Token:           "$encrypted$1$abc",
				IncludeDefaults: true,
			},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	server := newServiceServer(dataDir, nil)
	if _, err := server.loadResolvedSource("missing"); err == nil {
		t.Fatal("expected missing source to fail")
	}
}
