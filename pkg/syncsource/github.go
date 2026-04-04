package syncsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appconfig "github.com/ImSingee/git-plus/pkg/config"
)

const defaultGitHubAPIBaseURL = "https://api.github.com"
const defaultGitHubClientTimeout = 30 * time.Second

type gitHubAPIClient struct {
	baseURL    string
	httpClient *http.Client
}

type gitHubRepository struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	FullName      string  `json:"full_name"`
	Description   *string `json:"description"`
	HTMLURL       string  `json:"html_url"`
	CloneURL      string  `json:"clone_url"`
	SSHURL        string  `json:"ssh_url"`
	DefaultBranch string  `json:"default_branch"`
	Visibility    string  `json:"visibility"`
	Private       bool    `json:"private"`
	Fork          bool    `json:"fork"`
	Archived      bool    `json:"archived"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type gitHubErrorResponse struct {
	Message string `json:"message"`
}

func newGitHubAPIClient(baseURL string, httpClient *http.Client) *gitHubAPIClient {
	normalizedBaseURL := strings.TrimSpace(baseURL)
	if normalizedBaseURL == "" {
		normalizedBaseURL = defaultGitHubAPIBaseURL
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultGitHubClientTimeout,
		}
	}

	return &gitHubAPIClient{
		baseURL:    strings.TrimRight(normalizedBaseURL, "/"),
		httpClient: httpClient,
	}
}

func (client *gitHubAPIClient) ListDefaultRepositories(ctx context.Context, source appconfig.SourceConfig, page int, perPage int) (githubPage, error) {
	return client.listRepositories(ctx, source.Token, "/user/repos", url.Values{
		"affiliation": []string{"owner,collaborator,organization_member"},
		"page":        []string{strconv.Itoa(page)},
		"per_page":    []string{strconv.Itoa(perPage)},
	})
}

func (client *gitHubAPIClient) ListStarredRepositories(ctx context.Context, source appconfig.SourceConfig, page int, perPage int) (githubPage, error) {
	return client.listRepositories(ctx, source.Token, "/user/starred", url.Values{
		"page":     []string{strconv.Itoa(page)},
		"per_page": []string{strconv.Itoa(perPage)},
	})
}

func (client *gitHubAPIClient) ListWatchingRepositories(ctx context.Context, source appconfig.SourceConfig, page int, perPage int) (githubPage, error) {
	return client.listRepositories(ctx, source.Token, "/user/subscriptions", url.Values{
		"page":     []string{strconv.Itoa(page)},
		"per_page": []string{strconv.Itoa(perPage)},
	})
}

func (client *gitHubAPIClient) listRepositories(ctx context.Context, token string, endpoint string, query url.Values) (githubPage, error) {
	requestURL := client.baseURL + endpoint
	if encodedQuery := query.Encode(); encodedQuery != "" {
		requestURL += "?" + encodedQuery
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return githubPage{}, fmt.Errorf("create github request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return githubPage{}, fmt.Errorf("execute github request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return githubPage{}, fmt.Errorf("read github response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var errorResponse gitHubErrorResponse
		if err := json.Unmarshal(responseBody, &errorResponse); err == nil && strings.TrimSpace(errorResponse.Message) != "" {
			return githubPage{}, fmt.Errorf("github api %s returned %d: %s", endpoint, resp.StatusCode, errorResponse.Message)
		}

		return githubPage{}, fmt.Errorf("github api %s returned %d", endpoint, resp.StatusCode)
	}

	var payloads []json.RawMessage
	if err := json.Unmarshal(responseBody, &payloads); err != nil {
		return githubPage{}, fmt.Errorf("decode github repositories: %w", err)
	}

	repos := make([]ResolvedRepo, 0, len(payloads))
	for _, payload := range payloads {
		var repo gitHubRepository
		if err := json.Unmarshal(payload, &repo); err != nil {
			return githubPage{}, fmt.Errorf("decode github repository: %w", err)
		}

		repos = append(repos, ResolvedRepo{
			Platform:      "github",
			RefID:         strconv.FormatInt(repo.ID, 10),
			Name:          repo.Name,
			FullName:      repo.FullName,
			Owner:         repo.Owner.Login,
			Description:   derefString(repo.Description),
			HTMLURL:       repo.HTMLURL,
			CloneURL:      repo.CloneURL,
			SSHURL:        repo.SSHURL,
			DefaultBranch: repo.DefaultBranch,
			Visibility:    repo.Visibility,
			IsPrivate:     repo.Private,
			IsFork:        repo.Fork,
			IsArchived:    repo.Archived,
			MetaJSON:      string(payload),
		})
	}

	return githubPage{
		Repos:       repos,
		HasNextPage: len(repos) == queryPerPage(query),
	}, nil
}

func queryPerPage(query url.Values) int {
	perPage, err := strconv.Atoi(query.Get("per_page"))
	if err != nil || perPage <= 0 {
		return 0
	}

	return perPage
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
