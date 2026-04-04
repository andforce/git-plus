package archivegit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

const (
	RemoteName         = git.DefaultRemoteName
	RefKindHead        = "head"
	RefKindTag         = "tag"
	RefStatusActive    = "active"
	RefStatusDeleted   = "deleted"
	ChangeActionCreate = "create"
	ChangeActionUpdate = "update"
	ChangeActionDelete = "delete"
	ChangeActionNoop   = "unchanged"
	defaultListTimeout = 30 * time.Second
)

type CurrentRef struct {
	Name           string
	Kind           string
	Hash           string
	Status         string
	ArchiveRefName string
}

type RemoteRef struct {
	Name string
	Kind string
	Hash string
}

type CommitInfo struct {
	AuthoredAt  time.Time
	CommittedAt time.Time
	AuthorName  string
	AuthorEmail string
	Message     string
}

type Change struct {
	RefName        string
	RefKind        string
	OldHash        string
	NewHash        string
	Action         string
	ArchiveRefName string
}

func OpenArchive(path string, remoteURL string) (*git.Repository, error) {
	repo, err := openOrInitBareRepository(path)
	if err != nil {
		return nil, err
	}

	if err := ensureRemote(repo, remoteURL); err != nil {
		return nil, err
	}

	return repo, nil
}

func ListRemoteRefs(ctx context.Context, repo *git.Repository, auth transport.AuthMethod) ([]RemoteRef, error) {
	if repo == nil {
		return nil, fmt.Errorf("repository is required")
	}

	remote, err := repo.Remote(RemoteName)
	if err != nil {
		return nil, fmt.Errorf("get remote %q: %w", RemoteName, err)
	}

	refs, err := remote.ListContext(ctx, &git.ListOptions{
		Auth:    auth,
		Timeout: int(defaultListTimeout.Seconds()),
	})
	if err != nil {
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			return []RemoteRef{}, nil
		}

		return nil, fmt.Errorf("list remote refs: %w", err)
	}

	filtered := make([]RemoteRef, 0, len(refs))
	for _, ref := range refs {
		if ref == nil || ref.Type() != plumbing.HashReference {
			continue
		}

		refName := ref.Name().String()
		if strings.HasSuffix(refName, "^{}") {
			continue
		}

		refKind := refKindForName(refName)
		if refKind == "" {
			continue
		}

		filtered = append(filtered, RemoteRef{
			Name: refName,
			Kind: refKind,
			Hash: ref.Hash().String(),
		})
	}

	sort.Slice(filtered, func(i int, j int) bool {
		return filtered[i].Name < filtered[j].Name
	})

	return filtered, nil
}

func DiffRefs(previous []CurrentRef, current []RemoteRef) []Change {
	previousByName := make(map[string]CurrentRef, len(previous))
	for _, ref := range previous {
		if ref.Status != RefStatusActive {
			continue
		}
		previousByName[ref.Name] = ref
	}

	currentByName := make(map[string]RemoteRef, len(current))
	allNames := make(map[string]struct{}, len(previousByName)+len(currentByName))
	for _, ref := range current {
		currentByName[ref.Name] = ref
		allNames[ref.Name] = struct{}{}
	}
	for refName := range previousByName {
		allNames[refName] = struct{}{}
	}

	names := make([]string, 0, len(allNames))
	for refName := range allNames {
		names = append(names, refName)
	}
	sort.Strings(names)

	changes := make([]Change, 0, len(names))
	for _, refName := range names {
		previousRef, hadPrevious := previousByName[refName]
		currentRef, hasCurrent := currentByName[refName]

		change := Change{
			RefName: refName,
		}
		if hasCurrent {
			change.RefKind = currentRef.Kind
			change.NewHash = currentRef.Hash
		} else {
			change.RefKind = previousRef.Kind
			change.OldHash = previousRef.Hash
		}

		switch {
		case !hadPrevious && hasCurrent:
			change.Action = ChangeActionCreate
			change.ArchiveRefName = mustArchiveRefName(currentRef.Name, currentRef.Hash)
		case hadPrevious && !hasCurrent:
			change.Action = ChangeActionDelete
			change.OldHash = previousRef.Hash
		case hadPrevious && hasCurrent && previousRef.Hash != currentRef.Hash:
			change.Action = ChangeActionUpdate
			change.OldHash = previousRef.Hash
			change.ArchiveRefName = mustArchiveRefName(currentRef.Name, currentRef.Hash)
		default:
			change.Action = ChangeActionNoop
			change.OldHash = previousRef.Hash
		}

		changes = append(changes, change)
	}

	return changes
}

func FetchArchiveRefs(ctx context.Context, repo *git.Repository, auth transport.AuthMethod, changes []Change) (bool, error) {
	if repo == nil {
		return false, fmt.Errorf("repository is required")
	}

	refSpecs := make([]config.RefSpec, 0, len(changes))
	for _, change := range changes {
		if change.Action != ChangeActionCreate && change.Action != ChangeActionUpdate {
			continue
		}

		if change.ArchiveRefName == "" {
			return false, fmt.Errorf("archive ref name is required for %s %q", change.Action, change.RefName)
		}

		refSpecs = append(refSpecs, config.RefSpec("+"+change.RefName+":"+change.ArchiveRefName))
	}

	if len(refSpecs) == 0 {
		return false, nil
	}

	err := repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: RemoteName,
		RefSpecs:   refSpecs,
		Auth:       auth,
		Tags:       git.NoTags,
		Prune:      false,
		Force:      true,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("fetch archive refs: %w", err)
	}

	return true, nil
}

func ArchiveRefName(refName string, hash string) (string, error) {
	trimmedHash := strings.TrimSpace(hash)
	if trimmedHash == "" {
		return "", fmt.Errorf("hash is required")
	}

	switch {
	case strings.HasPrefix(refName, "refs/heads/"):
		return "refs/archive/heads/" + strings.TrimPrefix(refName, "refs/heads/") + "/" + trimmedHash, nil
	case strings.HasPrefix(refName, "refs/tags/"):
		return "refs/archive/tags/" + strings.TrimPrefix(refName, "refs/tags/") + "/" + trimmedHash, nil
	default:
		return "", fmt.Errorf("unsupported ref name %q", refName)
	}
}

func ResolveCommitInfo(repo *git.Repository, hash string) (*CommitInfo, error) {
	if repo == nil {
		return nil, fmt.Errorf("repository is required")
	}

	trimmedHash := strings.TrimSpace(hash)
	if trimmedHash == "" {
		return nil, nil
	}

	return resolveCommitInfoHash(repo, plumbing.NewHash(trimmedHash), map[plumbing.Hash]struct{}{})
}

func resolveCommitInfoHash(repo *git.Repository, hash plumbing.Hash, seen map[plumbing.Hash]struct{}) (*CommitInfo, error) {
	if hash.IsZero() {
		return nil, nil
	}
	if _, exists := seen[hash]; exists {
		return nil, fmt.Errorf("detected cyclic tag target for %s", hash.String())
	}
	seen[hash] = struct{}{}

	commit, err := repo.CommitObject(hash)
	if err == nil {
		return &CommitInfo{
			AuthoredAt:  commit.Author.When.UTC(),
			CommittedAt: commit.Committer.When.UTC(),
			AuthorName:  commit.Author.Name,
			AuthorEmail: commit.Author.Email,
			Message:     commit.Message,
		}, nil
	}
	if !errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, fmt.Errorf("load commit %s: %w", hash.String(), err)
	}

	tag, err := repo.TagObject(hash)
	if err == nil {
		return resolveCommitInfoHash(repo, tag.Target, seen)
	}
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, nil
	}

	return nil, fmt.Errorf("load tag %s: %w", hash.String(), err)
}

func openOrInitBareRepository(path string) (*git.Repository, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create archive repo parent directory: %w", err)
	}

	repo, err := git.PlainOpen(path)
	if err == nil {
		return repo, nil
	}
	if !errors.Is(err, git.ErrRepositoryNotExists) {
		return nil, fmt.Errorf("open archive repository: %w", err)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create archive repo directory: %w", err)
	}

	repo, err = git.PlainInit(path, true)
	if err != nil {
		return nil, fmt.Errorf("init archive repository: %w", err)
	}

	return repo, nil
}

func ensureRemote(repo *git.Repository, remoteURL string) error {
	if repo == nil {
		return fmt.Errorf("repository is required")
	}
	if strings.TrimSpace(remoteURL) == "" {
		return fmt.Errorf("remote URL is required")
	}

	remote, err := repo.Remote(RemoteName)
	switch {
	case err == nil:
		if slices.Equal(remote.Config().URLs, []string{remoteURL}) {
			return nil
		}

		if err := repo.DeleteRemote(RemoteName); err != nil {
			return fmt.Errorf("delete remote %q: %w", RemoteName, err)
		}
	case errors.Is(err, git.ErrRemoteNotFound):
	default:
		return fmt.Errorf("get remote %q: %w", RemoteName, err)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: RemoteName,
		URLs: []string{remoteURL},
	})
	if err != nil {
		return fmt.Errorf("create remote %q: %w", RemoteName, err)
	}

	return nil
}

func refKindForName(refName string) string {
	switch {
	case strings.HasPrefix(refName, "refs/heads/"):
		return RefKindHead
	case strings.HasPrefix(refName, "refs/tags/"):
		return RefKindTag
	default:
		return ""
	}
}

func mustArchiveRefName(refName string, hash string) string {
	archiveRefName, err := ArchiveRefName(refName, hash)
	if err != nil {
		return ""
	}

	return archiveRefName
}
