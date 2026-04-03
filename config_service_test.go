package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	appconfig "github.com/ImSingee/git-plus/config"
	configv1 "github.com/ImSingee/git-plus/rpc/gitplus/config/v1"
	"github.com/ImSingee/git-plus/rpc/gitplus/config/v1/configv1connect"
)

func TestConfigServiceCheckConfigReturnsExistsFalseWhenConfigMissing(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config: %v", err)
	}

	if response.Msg.GetExists() {
		t.Fatal("expected config file to be reported as missing")
	}
	if len(response.Msg.GetIssues()) != 0 {
		t.Fatalf("expected no issues when config is missing, got %#v", response.Msg.GetIssues())
	}
	assertSummaryCounts(t, response.Msg.GetSummary(), 0, 0, 0)
}

func TestConfigServiceCheckConfigReturnsInvalidYAMLIssue(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: [unterminated
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config: %v", err)
	}

	if !response.Msg.GetExists() {
		t.Fatal("expected config file to exist")
	}
	assertHasIssueCode(t, response.Msg.GetIssues(), "invalid_yaml")
	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 0, 0)
}

func TestConfigServiceCheckConfigSummaryMatchesIssues(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources: []
concurrency: 0
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config: %v", err)
	}

	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 1, 0)
}

func TestConfigServiceCheckSourceScopesIssuesToRequestedSource(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
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
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckSourceConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckSourceConfigRequest{SourceId: "github"}),
	)
	if err != nil {
		t.Fatalf("check source config: %v", err)
	}

	assertHasIssue(t, response.Msg.GetIssues(), "unknown_field", "sources[0].unknown_source_field")
	assertHasIssue(t, response.Msg.GetIssues(), "duplicate_source_id", "sources[0].id")
	assertHasIssue(t, response.Msg.GetIssues(), "duplicate_source_id", "sources[1].id")
	assertNoIssue(t, response.Msg.GetIssues(), "unknown_field", "unknown_top_level")
	assertNoIssue(t, response.Msg.GetIssues(), "invalid_concurrency", "concurrency")
	assertSummaryCounts(t, response.Msg.GetSummary(), 2, 1, 0)
}

func TestConfigServiceCheckSourceReturnsNotFoundIssue(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: secret
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckSourceConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckSourceConfigRequest{SourceId: "missing"}),
	)
	if err != nil {
		t.Fatalf("check source config: %v", err)
	}

	assertHasIssue(t, response.Msg.GetIssues(), "source_not_found", "sources")
	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 0, 0)
}

func TestServerHandlerRoutesConnectAPIAndKeepsLegacyRoutesGone(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)
	client := server.Client()

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config over /api route: %v", err)
	}
	if response.Msg.GetPath() != appconfig.PathForDataDir(dataDir) {
		t.Fatalf("unexpected config path: %q", response.Msg.GetPath())
	}

	for _, route := range []string{
		"/api/config/check",
		"/api/config/sources/github/check",
		"/api/test",
		"/api",
		"/api/",
	} {
		resp, getErr := client.Get(server.URL + route)
		if getErr != nil {
			t.Fatalf("get %s: %v", route, getErr)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected %s to return 404, got %d", route, resp.StatusCode)
		}
	}
}

func TestServerHandlerKeepsHealthzReadyAndFrontendRoutes(t *testing.T) {
	dataDir := t.TempDir()
	server := httptest.NewServer(newServerHandler(dataDir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("frontend-ok"))
	})))
	t.Cleanup(server.Close)

	client := server.Client()

	for _, route := range []string{"/healthz", "/ready"} {
		resp, err := client.Get(server.URL + route)
		if err != nil {
			t.Fatalf("get %s: %v", route, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s body: %v", route, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected %s to return 200, got %d", route, resp.StatusCode)
		}
		if string(body) != "ok\n" {
			t.Fatalf("unexpected %s body: %q", route, string(body))
		}
	}

	resp, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get frontend route: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read frontend body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected frontend route to return 200, got %d", resp.StatusCode)
	}
	if string(body) != "frontend-ok" {
		t.Fatalf("unexpected frontend body: %q", string(body))
	}
}

func newTestServer(t *testing.T, dataDir string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(newServerHandler(dataDir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})))
	t.Cleanup(server.Close)

	return server
}

func newConfigServiceClient(serverURL string) configv1connect.ConfigServiceClient {
	return configv1connect.NewConfigServiceClient(
		http.DefaultClient,
		serverURL+"/api",
	)
}

func writeConfigFile(t *testing.T, dataDir string, content string) {
	t.Helper()

	configPath := filepath.Join(dataDir, appconfig.ConfigFilename)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}

func assertSummaryCounts(t *testing.T, summary *configv1.IssueSummary, errors int32, warnings int32, info int32) {
	t.Helper()

	if summary == nil {
		t.Fatal("expected summary to be present")
	}
	if summary.GetError() != errors || summary.GetWarning() != warnings || summary.GetInfo() != info {
		t.Fatalf(
			"unexpected summary counts: error=%d warning=%d info=%d",
			summary.GetError(),
			summary.GetWarning(),
			summary.GetInfo(),
		)
	}
}

func assertHasIssueCode(t *testing.T, issues []*configv1.ValidationIssue, code string) {
	t.Helper()

	for _, issue := range issues {
		if issue.GetCode() == code {
			return
		}
	}

	t.Fatalf("expected issue code=%q, got %#v", code, issues)
}

func assertHasIssue(t *testing.T, issues []*configv1.ValidationIssue, code string, path string) {
	t.Helper()

	for _, issue := range issues {
		if issue.GetCode() != code {
			continue
		}
		if issue.GetPath() != path {
			continue
		}
		return
	}

	t.Fatalf("expected issue code=%q path=%q, got %#v", code, path, issues)
}

func assertNoIssue(t *testing.T, issues []*configv1.ValidationIssue, code string, path string) {
	t.Helper()

	for _, issue := range issues {
		if issue.GetCode() != code {
			continue
		}
		if issue.GetPath() != path {
			continue
		}
		t.Fatalf("did not expect issue code=%q path=%q, got %#v", code, path, issues)
	}
}
