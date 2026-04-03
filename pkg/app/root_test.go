package app

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"connectrpc.com/connect"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
	cronv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/cron/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/cron/v1/cronv1connect"
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

func TestCronReloadCommandUsesConfiguredServer(t *testing.T) {
	mux := http.NewServeMux()
	path, handler := cronv1connect.NewCronServiceHandler(&stubCronService{})
	mux.Handle(path, handler)
	server := httptest.NewServer(http.StripPrefix("/api", mux))
	t.Cleanup(server.Close)

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"cron", "reload", "--server", server.URL})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cron reload: %v", err)
	}

	if !strings.Contains(stdout.String(), "cron reloaded: enabled") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestCronReloadCommandUsesPortEnvironmentByDefault(t *testing.T) {
	mux := http.NewServeMux()
	path, handler := cronv1connect.NewCronServiceHandler(&stubCronService{})
	mux.Handle(path, handler)
	server := httptest.NewServer(http.StripPrefix("/api", mux))
	t.Cleanup(server.Close)

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	_, port, err := net.SplitHostPort(parsedURL.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	t.Setenv("PORT", port)

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"cron", "reload"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cron reload with PORT: %v", err)
	}

	if !strings.Contains(stdout.String(), "cron reloaded: enabled") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestCronReloadCommandUsesExplicitListenWhenServerIsNotProvided(t *testing.T) {
	mux := http.NewServeMux()
	path, handler := cronv1connect.NewCronServiceHandler(&stubCronService{})
	mux.Handle(path, handler)
	server := httptest.NewServer(http.StripPrefix("/api", mux))
	t.Cleanup(server.Close)

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	_, port, err := net.SplitHostPort(parsedURL.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"cron", "reload", "--listen", port})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cron reload with listen: %v", err)
	}

	if !strings.Contains(stdout.String(), "cron reloaded: enabled") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}

func TestResolveServerURLUsesEmptyServerToDeriveLocalAddress(t *testing.T) {
	if got := resolveServerURL("", defaultListenAddr, false); got != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected default server URL: %q", got)
	}
}

func TestResolveServerURLUsesExplicitServerWhenProvided(t *testing.T) {
	if got := resolveServerURL("http://example.test:9000", defaultListenAddr, false); got != "http://example.test:9000" {
		t.Fatalf("unexpected explicit server URL: %q", got)
	}
}

func TestResolveServerURLUsesListenFlagWhenChanged(t *testing.T) {
	if got := resolveServerURL("", "19090", true); got != "http://127.0.0.1:19090" {
		t.Fatalf("unexpected listen-derived server URL: %q", got)
	}
}

func TestCronCommandFlagsExposeServerAndListen(t *testing.T) {
	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})

	cronCmd, _, err := cmd.Find([]string{"cron", "reload"})
	if err != nil {
		t.Fatalf("find cron reload command: %v", err)
	}

	serverFlag := cronCmd.LocalFlags().Lookup("server")
	if serverFlag == nil {
		t.Fatal("expected --server flag")
	}
	if serverFlag.DefValue != "" {
		t.Fatalf("unexpected default server URL: %q", serverFlag.DefValue)
	}

	listenFlag := cronCmd.LocalFlags().Lookup("listen")
	if listenFlag == nil {
		t.Fatal("expected --listen flag")
	}
	if listenFlag.DefValue != defaultListenAddr {
		t.Fatalf("unexpected default listen: %q", listenFlag.DefValue)
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

type stubCronService struct{}

func (stubCronService) GetCronRuntime(
	_ context.Context,
	_ *connect.Request[cronv1.GetCronRuntimeRequest],
) (*connect.Response[cronv1.GetCronRuntimeResponse], error) {
	return connect.NewResponse(&cronv1.GetCronRuntimeResponse{}), nil
}

func (stubCronService) UpdateCron(
	_ context.Context,
	_ *connect.Request[cronv1.UpdateCronRequest],
) (*connect.Response[cronv1.UpdateCronResponse], error) {
	return connect.NewResponse(&cronv1.UpdateCronResponse{}), nil
}

func (stubCronService) ReloadCron(
	_ context.Context,
	_ *connect.Request[cronv1.ReloadCronRequest],
) (*connect.Response[cronv1.ReloadCronResponse], error) {
	return connect.NewResponse(&cronv1.ReloadCronResponse{
		Runtime: &cronv1.CronRuntime{
			Enabled: testBoolPtr(true),
			Cron:    testStringPtr("0 * * * *"),
		},
	}), nil
}

func testBoolPtr(value bool) *bool {
	return &value
}

func testStringPtr(value string) *string {
	return &value
}
