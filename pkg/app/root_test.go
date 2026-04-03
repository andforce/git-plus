package app

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	appconfig "github.com/ImSingee/git-plus/pkg/config"
)

func TestNewServerConfigRequiresDataDir(t *testing.T) {
	_, err := newServerConfig(defaultListenAddr, false, "", true)
	if err == nil {
		t.Fatal("expected missing data dir to fail")
	}
	if !strings.Contains(err.Error(), "--data-dir is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewServerConfigAcceptsExplicitDataDir(t *testing.T) {
	cfg, err := newServerConfig("18080", true, " ./tmpdata ", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DataDir != "./tmpdata" {
		t.Fatalf("unexpected data dir: %q", cfg.DataDir)
	}

	if cfg.ListenAddr != ":18080" {
		t.Fatalf("unexpected listen addr: %q", cfg.ListenAddr)
	}

	if cfg.AutoMigrate {
		t.Fatal("expected auto migrate to remain false")
	}
}

func TestDBMigrateDoesNotAcceptAutoMigrateFlag(t *testing.T) {
	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"db", "migrate", "--auto-migrate=false"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag: --auto-migrate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEncryptTokenCommandWritesEncryptedToken(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "correct horse battery staple")

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("secret-token\n"))
	cmd.SetArgs([]string{"config", "encrypt-token"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute encrypt-token: %v", err)
	}

	output := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(output, "$encrypted$1$") {
		t.Fatalf("expected encrypted token output, got %q", output)
	}
}

func TestEncryptTokenCommandRequiresPassphrase(t *testing.T) {
	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("secret-token\n"))
	cmd.SetArgs([]string{"config", "encrypt-token"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing passphrase to fail")
	}
	if !strings.Contains(err.Error(), appconfig.TokenPassphraseEnvVar) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEncryptTokenCommandRequiresTokenOnStdin(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "correct horse battery staple")

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("\n"))
	cmd.SetArgs([]string{"config", "encrypt-token"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected empty stdin to fail")
	}
	if !strings.Contains(err.Error(), "token is required on stdin") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStartupEnvironmentRequiresEncryptionEnv(t *testing.T) {
	err := validateStartupEnvironment()
	if err == nil {
		t.Fatal("expected missing encryption env to fail")
	}
	if !strings.Contains(err.Error(), appconfig.TokenPassphraseEnvVar) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStartupEnvironmentAcceptsConfiguredEncryptionEnv(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "correct horse battery staple")

	if err := validateStartupEnvironment(); err != nil {
		t.Fatalf("unexpected startup env error: %v", err)
	}
}
