package syncsource

import (
	"context"
	"sort"

	appconfig "github.com/ImSingee/git-plus/pkg/config"
)

const (
	StatusActive       = "active"
	StatusAutoExcluded = "auto_excluded"
)

type ProgressReporter interface {
	SetProgress(summary string, meta map[string]any) error
}

type SyncRequest struct {
	RunID         string
	Source        appconfig.SourceConfig
	Concurrency   int
	MaxRetryTimes int
}

type ResolvedRepo struct {
	SourceID      string
	Platform      string
	RefID         string
	Name          string
	FullName      string
	Owner         string
	Description   string
	HTMLURL       string
	CloneURL      string
	SSHURL        string
	DefaultBranch string
	Visibility    string
	IsPrivate     bool
	IsFork        bool
	IsArchived    bool
	MetaJSON      string

	originKinds map[string]struct{}
}

type SnapshotResult struct {
	ResolvedTotal int
	Inserted      int
	Updated       int
	Reactivated   int
	AutoExcluded  int
}

type ArchiveResult struct {
	TargetTotal     int
	Processed       int
	Succeeded       int
	Failed          int
	Retried         int
	ChangeCount     int
	CreatedRefCount int
	UpdatedRefCount int
	DeletedRefCount int
}

type githubPage struct {
	Repos       []ResolvedRepo
	HasNextPage bool
}

type githubClient interface {
	ListDefaultRepositories(ctx context.Context, source appconfig.SourceConfig, page int, perPage int) (githubPage, error)
	ListStarredRepositories(ctx context.Context, source appconfig.SourceConfig, page int, perPage int) (githubPage, error)
	ListWatchingRepositories(ctx context.Context, source appconfig.SourceConfig, page int, perPage int) (githubPage, error)
}

func (repo *ResolvedRepo) AddOriginKind(kind string) {
	if repo.originKinds == nil {
		repo.originKinds = make(map[string]struct{})
	}
	repo.originKinds[kind] = struct{}{}
}

func (repo ResolvedRepo) OriginKinds() []string {
	if len(repo.originKinds) == 0 {
		return []string{}
	}

	kinds := make([]string, 0, len(repo.originKinds))
	for kind := range repo.originKinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	return kinds
}
