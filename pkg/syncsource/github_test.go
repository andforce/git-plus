package syncsource

import (
	"net/http"
	"testing"
)

func TestNewGitHubAPIClientUsesFiniteTimeoutByDefault(t *testing.T) {
	client := newGitHubAPIClient("", nil)

	if client.httpClient == nil {
		t.Fatal("expected default http client")
	}
	if client.httpClient.Timeout != defaultGitHubClientTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultGitHubClientTimeout, client.httpClient.Timeout)
	}
}

func TestNewGitHubAPIClientPreservesInjectedClient(t *testing.T) {
	injected := &http.Client{}
	client := newGitHubAPIClient("", injected)

	if client.httpClient != injected {
		t.Fatal("expected injected client to be preserved")
	}
}
