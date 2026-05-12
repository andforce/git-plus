package reposervice

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	repov1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/repo/v1"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxTextBlobBytes = 1024 * 1024
const maxCommitPatchBytes = 2 * 1024 * 1024
const maxBlameLines = 5000
const maxFileSearchResults = 100
const maxCodeSearchResults = 100

type resolvedArchiveRef struct {
	repo       dbsqlc.Repo
	ref        dbsqlc.RepoRefsCurrent
	gitRepo    *git.Repository
	commit     *object.Commit
	archiveDir string
}

type blobPreview struct {
	path        string
	name        string
	mode        string
	hash        string
	size        int64
	content     string
	isBinary    bool
	isTruncated bool
}

type commitDiffBuilder struct {
	oldPath     string
	newPath     string
	status      string
	additions   int32
	deletions   int32
	patch       strings.Builder
	isBinary    bool
	isTruncated bool
}

func (s *serviceServer) ListTree(
	ctx context.Context,
	req *connect.Request[repov1.ListTreeRequest],
) (*connect.Response[repov1.ListTreeResponse], error) {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	cleanPath, err := cleanRepositoryPath(req.Msg.GetPath())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	resolved, err := s.resolveArchiveRef(ctx, queries, repoID, req.Msg.GetRefName())
	if err != nil {
		return nil, err
	}

	rootTree, err := resolved.commit.Tree()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load commit tree: %w", err))
	}

	tree := rootTree
	if cleanPath != "" {
		tree, err = rootTree.Tree(cleanPath)
		if err != nil {
			if errors.Is(err, object.ErrDirectoryNotFound) {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("directory not found"))
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load directory %q: %w", cleanPath, err))
		}
	}

	entries, err := toProtoTreeEntries(resolved.gitRepo, tree, cleanPath)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	readme, err := readRepositoryReadme(rootTree, tree, cleanPath)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&repov1.ListTreeResponse{
		RepoId:     int64Ptr(repoID),
		RefName:    stringPtr(resolved.ref.RefName),
		CommitHash: stringPtr(resolved.commit.Hash.String()),
		Path:       stringPtr(cleanPath),
		Entries:    entries,
		Readme:     readme,
	}), nil
}

func (s *serviceServer) SearchFiles(
	ctx context.Context,
	req *connect.Request[repov1.SearchFilesRequest],
) (*connect.Response[repov1.SearchFilesResponse], error) {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	resolved, err := s.resolveArchiveRef(ctx, queries, repoID, req.Msg.GetRefName())
	if err != nil {
		return nil, err
	}

	rootTree, err := resolved.commit.Tree()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load commit tree: %w", err))
	}

	entries, truncated, err := searchRepositoryFiles(rootTree, req.Msg.GetQuery(), int(req.Msg.GetPageSize()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&repov1.SearchFilesResponse{
		RepoId:      int64Ptr(repoID),
		RefName:     stringPtr(resolved.ref.RefName),
		CommitHash:  stringPtr(resolved.commit.Hash.String()),
		Entries:     entries,
		IsTruncated: boolPtr(truncated),
	}), nil
}

func (s *serviceServer) SearchCode(
	ctx context.Context,
	req *connect.Request[repov1.SearchCodeRequest],
) (*connect.Response[repov1.SearchCodeResponse], error) {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	resolved, err := s.resolveArchiveRef(ctx, queries, repoID, req.Msg.GetRefName())
	if err != nil {
		return nil, err
	}

	rootTree, err := resolved.commit.Tree()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load commit tree: %w", err))
	}

	matches, truncated, err := searchRepositoryCode(rootTree, req.Msg.GetQuery(), int(req.Msg.GetPageSize()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&repov1.SearchCodeResponse{
		RepoId:      int64Ptr(repoID),
		RefName:     stringPtr(resolved.ref.RefName),
		CommitHash:  stringPtr(resolved.commit.Hash.String()),
		Matches:     matches,
		IsTruncated: boolPtr(truncated),
	}), nil
}

func (s *serviceServer) GetBlob(
	ctx context.Context,
	req *connect.Request[repov1.GetBlobRequest],
) (*connect.Response[repov1.GetBlobResponse], error) {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	cleanPath, err := cleanRepositoryPath(req.Msg.GetPath())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if cleanPath == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is required"))
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	resolved, err := s.resolveArchiveRef(ctx, queries, repoID, req.Msg.GetRefName())
	if err != nil {
		return nil, err
	}

	rootTree, err := resolved.commit.Tree()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load commit tree: %w", err))
	}

	file, err := rootTree.File(cleanPath)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load file %q: %w", cleanPath, err))
	}

	preview, err := readBlobPreview(file, cleanPath)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read file %q: %w", cleanPath, err))
	}

	return connect.NewResponse(&repov1.GetBlobResponse{
		RepoId:      int64Ptr(repoID),
		RefName:     stringPtr(resolved.ref.RefName),
		CommitHash:  stringPtr(resolved.commit.Hash.String()),
		Path:        stringPtr(preview.path),
		Name:        stringPtr(preview.name),
		Mode:        stringPtr(preview.mode),
		Hash:        stringPtr(preview.hash),
		Size:        int64Ptr(preview.size),
		Content:     stringPtr(preview.content),
		IsBinary:    boolPtr(preview.isBinary),
		IsTruncated: boolPtr(preview.isTruncated),
	}), nil
}

func (s *serviceServer) GetBlame(
	ctx context.Context,
	req *connect.Request[repov1.GetBlameRequest],
) (*connect.Response[repov1.GetBlameResponse], error) {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	cleanPath, err := cleanRepositoryPath(req.Msg.GetPath())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if cleanPath == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is required"))
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	resolved, err := s.resolveArchiveRef(ctx, queries, repoID, req.Msg.GetRefName())
	if err != nil {
		return nil, err
	}

	blame, err := git.Blame(resolved.commit, cleanPath)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load blame for %q: %w", cleanPath, err))
	}

	lines, truncated := toProtoBlameLines(resolved.gitRepo, blame.Lines)

	return connect.NewResponse(&repov1.GetBlameResponse{
		RepoId:      int64Ptr(repoID),
		RefName:     stringPtr(resolved.ref.RefName),
		CommitHash:  stringPtr(resolved.commit.Hash.String()),
		Path:        stringPtr(cleanPath),
		Lines:       lines,
		IsTruncated: boolPtr(truncated),
	}), nil
}

func (s *serviceServer) ListCommits(
	ctx context.Context,
	req *connect.Request[repov1.ListCommitsRequest],
) (*connect.Response[repov1.ListCommitsResponse], error) {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	pageSize := int(req.Msg.GetPageSize())
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	offset, err := decodePageToken(req.Msg.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid page_token: %w", err))
	}
	pathFilter, err := cleanRepositoryPath(req.Msg.GetPath())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	resolved, err := s.resolveArchiveRef(ctx, queries, repoID, req.Msg.GetRefName())
	if err != nil {
		return nil, err
	}

	logOptions := &git.LogOptions{
		From:  resolved.commit.Hash,
		Order: git.LogOrderCommitterTime,
	}
	if pathFilter != "" {
		logOptions.PathFilter = pathMatchesFilter(pathFilter)
	}

	iter, err := resolved.gitRepo.Log(logOptions)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("open commit log: %w", err))
	}
	defer iter.Close()

	for skipped := 0; skipped < offset; skipped++ {
		if _, err := iter.Next(); err != nil {
			if errors.Is(err, io.EOF) {
				return connect.NewResponse(&repov1.ListCommitsResponse{}), nil
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("skip commits: %w", err))
		}
	}

	commits := make([]*repov1.RepositoryCommit, 0, pageSize)
	hasNext := false
	for len(commits) < pageSize+1 {
		commit, err := iter.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read commit log: %w", err))
		}

		if len(commits) == pageSize {
			hasNext = true
			break
		}
		commits = append(commits, toProtoRepositoryCommit(commit))
	}

	response := &repov1.ListCommitsResponse{
		Commits: commits,
	}
	if hasNext {
		response.NextPageToken = stringPtr(encodePageToken(offset + len(commits)))
	}

	return connect.NewResponse(response), nil
}

func (s *serviceServer) GetCommit(
	ctx context.Context,
	req *connect.Request[repov1.GetCommitRequest],
) (*connect.Response[repov1.GetCommitResponse], error) {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	commitHash := strings.TrimSpace(req.Msg.GetCommitHash())
	if commitHash == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("commit_hash is required"))
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	repo, err := queries.GetRepoById(ctx, repoID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("repository not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get repository: %w", err))
	}

	archiveDir := filepath.Join(s.dataDir, "repos", repo.SourceID, repo.RefID)
	fullHash, err := resolveArchiveCommitHash(ctx, archiveDir, commitHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	gitRepo, err := git.PlainOpen(archiveDir)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("archive repository is not available"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("open archive repository: %w", err))
	}

	commit, err := gitRepo.CommitObject(plumbing.NewHash(fullHash))
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit object not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load commit: %w", err))
	}

	patch, patchTruncated, err := loadCommitPatch(ctx, archiveDir, fullHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load commit patch: %w", err))
	}
	files, additions, deletions := parseCommitPatch(patch, patchTruncated)

	return connect.NewResponse(&repov1.GetCommitResponse{
		Commit:      toProtoRepositoryCommit(commit),
		Files:       files,
		Additions:   int32Ptr(additions),
		Deletions:   int32Ptr(deletions),
		IsTruncated: boolPtr(patchTruncated),
	}), nil
}

func (s *serviceServer) CompareRefs(
	ctx context.Context,
	req *connect.Request[repov1.CompareRefsRequest],
) (*connect.Response[repov1.CompareRefsResponse], error) {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	baseRefName := strings.TrimSpace(req.Msg.GetBaseRefName())
	if baseRefName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("base_ref_name is required"))
	}
	headRefName := strings.TrimSpace(req.Msg.GetHeadRefName())
	if headRefName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("head_ref_name is required"))
	}

	pageSize := int(req.Msg.GetPageSize())
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	baseRef, err := s.resolveArchiveRef(ctx, queries, repoID, baseRefName)
	if err != nil {
		return nil, err
	}
	headRef, err := s.resolveArchiveRef(ctx, queries, repoID, headRefName)
	if err != nil {
		return nil, err
	}

	baseHash := baseRef.commit.Hash.String()
	headHash := headRef.commit.Hash.String()
	aheadCount, err := countRevisionRange(ctx, baseRef.archiveDir, baseHash, headHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("count ahead commits: %w", err))
	}
	behindCount, err := countRevisionRange(ctx, baseRef.archiveDir, headHash, baseHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("count behind commits: %w", err))
	}
	mergeBaseHash, err := findArchiveMergeBase(ctx, baseRef.archiveDir, baseHash, headHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("find merge base: %w", err))
	}
	commits, err := loadRevisionRangeCommits(ctx, baseRef.archiveDir, baseHash, headHash, pageSize)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load compare commits: %w", err))
	}
	patch, patchTruncated, err := loadRevisionDiffPatch(ctx, baseRef.archiveDir, baseHash, headHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load compare diff: %w", err))
	}
	files, additions, deletions := parseCommitPatch(patch, patchTruncated)

	return connect.NewResponse(&repov1.CompareRefsResponse{
		BaseRefName:    stringPtr(baseRef.ref.RefName),
		HeadRefName:    stringPtr(headRef.ref.RefName),
		BaseCommitHash: stringPtr(baseHash),
		HeadCommitHash: stringPtr(headHash),
		MergeBaseHash:  stringPtr(mergeBaseHash),
		AheadCount:     int32Ptr(aheadCount),
		BehindCount:    int32Ptr(behindCount),
		Commits:        commits,
		Files:          files,
		Additions:      int32Ptr(additions),
		Deletions:      int32Ptr(deletions),
		IsTruncated:    boolPtr(patchTruncated),
	}), nil
}

func (s *serviceServer) resolveArchiveRef(
	ctx context.Context,
	queries *dbsqlc.Queries,
	repoID int64,
	refName string,
) (*resolvedArchiveRef, error) {
	repo, err := queries.GetRepoById(ctx, repoID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("repository not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get repository: %w", err))
	}

	refs, err := queries.ListRepoRefsCurrentByRepoID(ctx, repoID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list repository refs: %w", err))
	}

	ref, ok := chooseActiveRepoRef(repo, refs, refName)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("repository ref not found"))
	}

	archiveDir := filepath.Join(s.dataDir, "repos", repo.SourceID, repo.RefID)
	gitRepo, err := git.PlainOpen(archiveDir)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("archive repository is not available"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("open archive repository: %w", err))
	}

	currentHash := ref.CurrentHash
	if ref.ID == 0 && isCommitHashLike(ref.CurrentHash) {
		resolvedHash, err := resolveArchiveCommitHash(ctx, archiveDir, ref.CurrentHash)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		currentHash = resolvedHash
	}

	commit, err := resolveCommitObject(gitRepo, plumbing.NewHash(currentHash), map[plumbing.Hash]struct{}{})
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit object not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("resolve commit object: %w", err))
	}

	return &resolvedArchiveRef{
		repo:       repo,
		ref:        ref,
		gitRepo:    gitRepo,
		commit:     commit,
		archiveDir: archiveDir,
	}, nil
}

func chooseActiveRepoRef(repo dbsqlc.Repo, refs []dbsqlc.RepoRefsCurrent, requestedRef string) (dbsqlc.RepoRefsCurrent, bool) {
	activeRefs := make([]dbsqlc.RepoRefsCurrent, 0, len(refs))
	for _, ref := range refs {
		if ref.Status == "active" {
			activeRefs = append(activeRefs, ref)
		}
	}
	sort.Slice(activeRefs, func(i int, j int) bool {
		if activeRefs[i].RefKind != activeRefs[j].RefKind {
			return activeRefs[i].RefKind < activeRefs[j].RefKind
		}
		return activeRefs[i].RefName < activeRefs[j].RefName
	})

	trimmed := strings.TrimSpace(requestedRef)
	if trimmed != "" {
		candidates := requestedRefCandidates(trimmed)
		for _, candidate := range candidates {
			for _, ref := range activeRefs {
				if ref.RefName == candidate {
					return ref, true
				}
			}
		}
		if isCommitHashLike(trimmed) {
			return syntheticCommitRef(repo, trimmed), true
		}
		return dbsqlc.RepoRefsCurrent{}, false
	}

	if repo.DefaultBranch.Valid {
		defaultRefName := "refs/heads/" + strings.TrimSpace(repo.DefaultBranch.String)
		for _, ref := range activeRefs {
			if ref.RefName == defaultRefName {
				return ref, true
			}
		}
	}

	for _, ref := range activeRefs {
		if ref.RefKind == "head" {
			return ref, true
		}
	}
	if len(activeRefs) > 0 {
		return activeRefs[0], true
	}
	return dbsqlc.RepoRefsCurrent{}, false
}

func requestedRefCandidates(refName string) []string {
	if strings.HasPrefix(refName, "refs/") {
		return []string{refName}
	}
	return []string{
		"refs/heads/" + refName,
		"refs/tags/" + refName,
		refName,
	}
}

func isCommitHashLike(value string) bool {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 7 || len(trimmed) > 40 {
		return false
	}
	for _, char := range trimmed {
		if (char >= '0' && char <= '9') ||
			(char >= 'a' && char <= 'f') ||
			(char >= 'A' && char <= 'F') {
			continue
		}
		return false
	}
	return true
}

func syntheticCommitRef(repo dbsqlc.Repo, commitHash string) dbsqlc.RepoRefsCurrent {
	return dbsqlc.RepoRefsCurrent{
		RepoID:      repo.ID,
		RefName:     commitHash,
		RefKind:     "head",
		CurrentHash: commitHash,
		Status:      "active",
	}
}

func resolveCommitObject(repo *git.Repository, hash plumbing.Hash, seen map[plumbing.Hash]struct{}) (*object.Commit, error) {
	if hash.IsZero() {
		return nil, plumbing.ErrObjectNotFound
	}
	if _, exists := seen[hash]; exists {
		return nil, fmt.Errorf("detected cyclic tag target for %s", hash.String())
	}
	seen[hash] = struct{}{}

	commit, err := repo.CommitObject(hash)
	if err == nil {
		return commit, nil
	}
	if !errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, err
	}

	tag, err := repo.TagObject(hash)
	if err == nil {
		return resolveCommitObject(repo, tag.Target, seen)
	}
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, plumbing.ErrObjectNotFound
	}
	return nil, err
}

func resolveArchiveCommitHash(ctx context.Context, archiveDir string, commitHash string) (string, error) {
	output, err := runArchiveGitOutput(ctx, archiveDir, "rev-parse", strings.TrimSpace(commitHash)+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("commit object not found: %w", err)
	}

	resolved := strings.TrimSpace(string(output))
	if resolved == "" {
		return "", fmt.Errorf("commit object not found")
	}
	return resolved, nil
}

func loadCommitPatch(ctx context.Context, archiveDir string, commitHash string) (string, bool, error) {
	output, err := runArchiveGitOutput(
		ctx,
		archiveDir,
		"show",
		"--format=",
		"--patch",
		"--find-renames",
		"--find-copies",
		"--no-ext-diff",
		"--no-color",
		"--root",
		commitHash,
	)
	if err != nil {
		return "", false, err
	}

	truncated := len(output) > maxCommitPatchBytes
	if truncated {
		output = output[:maxCommitPatchBytes]
	}
	return string(output), truncated, nil
}

func loadRevisionDiffPatch(ctx context.Context, archiveDir string, baseHash string, headHash string) (string, bool, error) {
	output, err := runArchiveGitOutput(
		ctx,
		archiveDir,
		"diff",
		"--patch",
		"--find-renames",
		"--find-copies",
		"--no-ext-diff",
		"--no-color",
		baseHash,
		headHash,
	)
	if err != nil {
		return "", false, err
	}

	truncated := len(output) > maxCommitPatchBytes
	if truncated {
		output = output[:maxCommitPatchBytes]
	}
	return string(output), truncated, nil
}

func countRevisionRange(ctx context.Context, archiveDir string, baseHash string, headHash string) (int32, error) {
	output, err := runArchiveGitOutput(ctx, archiveDir, "rev-list", "--count", baseHash+".."+headHash)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0, fmt.Errorf("parse revision count: %w", err)
	}
	return int32(count), nil
}

func findArchiveMergeBase(ctx context.Context, archiveDir string, baseHash string, headHash string) (string, error) {
	output, err := runArchiveGitOutput(ctx, archiveDir, "merge-base", baseHash, headHash)
	if err != nil {
		if isGitExitCode(err, 1) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func loadRevisionRangeCommits(
	ctx context.Context,
	archiveDir string,
	baseHash string,
	headHash string,
	limit int,
) ([]*repov1.RepositoryCommit, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	output, err := runArchiveGitOutput(
		ctx,
		archiveDir,
		"log",
		fmt.Sprintf("--max-count=%d", limit),
		"--format=%H%x1f%P%x1f%aI%x1f%cI%x1f%an%x1f%ae%x1f%cn%x1f%ce%x1f%B%x1e",
		baseHash+".."+headHash,
	)
	if err != nil {
		return nil, err
	}

	records := strings.Split(string(output), "\x1e")
	commits := make([]*repov1.RepositoryCommit, 0, len(records))
	for _, record := range records {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		commit, err := parseGitCommitRecord(record)
		if err != nil {
			return nil, err
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

func parseGitCommitRecord(record string) (*repov1.RepositoryCommit, error) {
	parts := strings.Split(record, "\x1f")
	if len(parts) < 9 {
		return nil, fmt.Errorf("invalid git commit record")
	}

	return &repov1.RepositoryCommit{
		Hash:           stringPtr(parts[0]),
		ParentHashes:   strings.Fields(parts[1]),
		AuthoredAt:     parseGitTimestamp(parts[2]),
		CommittedAt:    parseGitTimestamp(parts[3]),
		AuthorName:     stringPtr(parts[4]),
		AuthorEmail:    stringPtr(parts[5]),
		CommitterName:  stringPtr(parts[6]),
		CommitterEmail: stringPtr(parts[7]),
		Message:        stringPtr(strings.Join(parts[8:], "\x1f")),
	}, nil
}

func parseGitTimestamp(value string) *timestamppb.Timestamp {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return timestamppb.New(parsed.UTC())
}

func runArchiveGitOutput(ctx context.Context, archiveDir string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"--git-dir", archiveDir}, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return output, nil
}

func isGitExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}

func parseCommitPatch(patch string, patchTruncated bool) ([]*repov1.CommitFileDiff, int32, int32) {
	files := make([]*repov1.CommitFileDiff, 0)
	var totalAdditions int32
	var totalDeletions int32
	var current *commitDiffBuilder

	flush := func() {
		if current == nil {
			return
		}
		if current.status == "" {
			current.status = "modified"
		}
		if current.newPath == "" {
			current.newPath = current.oldPath
		}
		if current.oldPath == "" {
			current.oldPath = current.newPath
		}
		totalAdditions += current.additions
		totalDeletions += current.deletions
		files = append(files, &repov1.CommitFileDiff{
			OldPath:     stringPtr(current.oldPath),
			NewPath:     stringPtr(current.newPath),
			Status:      stringPtr(current.status),
			Additions:   int32Ptr(current.additions),
			Deletions:   int32Ptr(current.deletions),
			Patch:       stringPtr(current.patch.String()),
			IsBinary:    boolPtr(current.isBinary),
			IsTruncated: boolPtr(current.isTruncated),
		})
	}

	for _, line := range strings.SplitAfter(patch, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = newCommitDiffBuilder(line)
			continue
		}
		if current == nil {
			continue
		}

		current.patch.WriteString(line)
		updateCommitDiffBuilder(current, line)
	}

	if patchTruncated && current != nil {
		current.isTruncated = true
	}
	flush()

	return files, totalAdditions, totalDeletions
}

func newCommitDiffBuilder(header string) *commitDiffBuilder {
	builder := &commitDiffBuilder{
		status: "modified",
	}
	builder.patch.WriteString(header)

	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) >= 4 {
		builder.oldPath = trimGitDiffPath(parts[2], "a/")
		builder.newPath = trimGitDiffPath(parts[3], "b/")
	}
	return builder
}

func updateCommitDiffBuilder(builder *commitDiffBuilder, line string) {
	trimmed := strings.TrimRight(line, "\r\n")

	switch {
	case strings.HasPrefix(trimmed, "new file mode "):
		builder.status = "added"
	case strings.HasPrefix(trimmed, "deleted file mode "):
		builder.status = "deleted"
	case strings.HasPrefix(trimmed, "rename from "):
		builder.status = "renamed"
		builder.oldPath = strings.TrimPrefix(trimmed, "rename from ")
	case strings.HasPrefix(trimmed, "rename to "):
		builder.status = "renamed"
		builder.newPath = strings.TrimPrefix(trimmed, "rename to ")
	case strings.HasPrefix(trimmed, "copy from "):
		builder.status = "copied"
		builder.oldPath = strings.TrimPrefix(trimmed, "copy from ")
	case strings.HasPrefix(trimmed, "copy to "):
		builder.status = "copied"
		builder.newPath = strings.TrimPrefix(trimmed, "copy to ")
	case strings.HasPrefix(trimmed, "--- "):
		if value := parsePatchPath(trimmed, "--- ", "a/"); value != "" && value != "/dev/null" {
			builder.oldPath = value
		}
	case strings.HasPrefix(trimmed, "+++ "):
		if value := parsePatchPath(trimmed, "+++ ", "b/"); value != "" && value != "/dev/null" {
			builder.newPath = value
		}
	case strings.HasPrefix(trimmed, "Binary files ") || strings.HasPrefix(trimmed, "GIT binary patch"):
		builder.isBinary = true
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		builder.additions++
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		builder.deletions++
	}
}

func parsePatchPath(line string, prefix string, marker string) string {
	value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	return trimGitDiffPath(value, marker)
}

func trimGitDiffPath(value string, marker string) string {
	trimmed := strings.Trim(value, "\"")
	if trimmed == "/dev/null" {
		return trimmed
	}
	return strings.TrimPrefix(trimmed, marker)
}

func pathMatchesFilter(filter string) func(string) bool {
	return func(candidate string) bool {
		cleanCandidate, err := cleanRepositoryPath(candidate)
		if err != nil {
			return false
		}
		return cleanCandidate == filter || strings.HasPrefix(cleanCandidate, filter+"/")
	}
}

func cleanRepositoryPath(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" || trimmed == "." || trimmed == "/" {
		return "", nil
	}
	if strings.Contains(trimmed, "\x00") {
		return "", fmt.Errorf("path contains invalid character")
	}

	cleaned := path.Clean("/" + trimmed)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." {
		return "", nil
	}
	return cleaned, nil
}

func toProtoTreeEntries(repo *git.Repository, tree *object.Tree, parentPath string) ([]*repov1.TreeEntry, error) {
	entries := make([]*repov1.TreeEntry, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		kind := treeEntryKind(entry.Mode)
		entryPath := joinRepositoryPath(parentPath, entry.Name)

		size := int64(0)
		if kind == "file" || kind == "symlink" {
			blob, err := object.GetBlob(repo.Storer, entry.Hash)
			if err != nil {
				return nil, fmt.Errorf("load blob %q: %w", entryPath, err)
			}
			size = blob.Size
		}

		entries = append(entries, &repov1.TreeEntry{
			Name: stringPtr(entry.Name),
			Path: stringPtr(entryPath),
			Kind: stringPtr(kind),
			Mode: stringPtr(entry.Mode.String()),
			Hash: stringPtr(entry.Hash.String()),
			Size: int64Ptr(size),
		})
	}

	sort.SliceStable(entries, func(i int, j int) bool {
		leftKind := entries[i].GetKind()
		rightKind := entries[j].GetKind()
		if leftKind == "directory" && rightKind != "directory" {
			return true
		}
		if leftKind != "directory" && rightKind == "directory" {
			return false
		}
		return strings.ToLower(entries[i].GetName()) < strings.ToLower(entries[j].GetName())
	})

	return entries, nil
}

func searchRepositoryFiles(tree *object.Tree, query string, pageSize int) ([]*repov1.TreeEntry, bool, error) {
	limit := pageSize
	if limit <= 0 || limit > maxFileSearchResults {
		limit = maxFileSearchResults
	}
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	entries := make([]*repov1.TreeEntry, 0, limit)
	truncated := false

	err := tree.Files().ForEach(func(file *object.File) error {
		if len(entries) >= limit {
			truncated = true
			return storer.ErrStop
		}

		filePath := file.Name
		haystack := strings.ToLower(filePath)
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				return nil
			}
		}

		entries = append(entries, &repov1.TreeEntry{
			Name: stringPtr(path.Base(filePath)),
			Path: stringPtr(filePath),
			Kind: stringPtr(treeEntryKind(file.Mode)),
			Mode: stringPtr(file.Mode.String()),
			Hash: stringPtr(file.Hash.String()),
			Size: int64Ptr(file.Size),
		})
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("search repository files: %w", err)
	}

	return entries, truncated, nil
}

func searchRepositoryCode(tree *object.Tree, query string, pageSize int) ([]*repov1.CodeSearchMatch, bool, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, false, nil
	}

	limit := pageSize
	if limit <= 0 || limit > maxCodeSearchResults {
		limit = maxCodeSearchResults
	}
	needle := strings.ToLower(trimmed)
	matches := make([]*repov1.CodeSearchMatch, 0, limit)
	truncated := false

	err := tree.Files().ForEach(func(file *object.File) error {
		if len(matches) >= limit {
			truncated = true
			return storer.ErrStop
		}

		content, ok, err := readSearchableFileContent(file)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		for index, line := range strings.Split(content, "\n") {
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			if len(matches) >= limit {
				truncated = true
				return storer.ErrStop
			}
			matches = append(matches, &repov1.CodeSearchMatch{
				Path:   stringPtr(file.Name),
				LineNo: int32Ptr(int32(index + 1)),
				Line:   stringPtr(line),
			})
		}

		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("search repository code: %w", err)
	}

	return matches, truncated, nil
}

func readSearchableFileContent(file *object.File) (string, bool, error) {
	reader, err := file.Reader()
	if err != nil {
		return "", false, err
	}
	defer reader.Close()

	data, err := io.ReadAll(io.LimitReader(reader, maxTextBlobBytes+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > maxTextBlobBytes {
		data = data[:maxTextBlobBytes]
	}
	if isBinaryContent(data) {
		return "", false, nil
	}
	return string(data), true, nil
}

func readRepositoryReadme(rootTree *object.Tree, tree *object.Tree, parentPath string) (*repov1.RepositoryReadme, error) {
	readmeEntry := findReadmeEntry(tree)
	if readmeEntry == nil {
		return nil, nil
	}

	readmePath := joinRepositoryPath(parentPath, readmeEntry.Name)
	file, err := rootTree.File(readmePath)
	if err != nil {
		return nil, fmt.Errorf("load readme %q: %w", readmePath, err)
	}

	preview, err := readBlobPreview(file, readmePath)
	if err != nil {
		return nil, err
	}
	if preview.isBinary {
		return nil, nil
	}

	return &repov1.RepositoryReadme{
		Path:        stringPtr(preview.path),
		Name:        stringPtr(preview.name),
		Content:     stringPtr(preview.content),
		IsTruncated: boolPtr(preview.isTruncated),
	}, nil
}

func findReadmeEntry(tree *object.Tree) *object.TreeEntry {
	readmePriority := map[string]int{
		"readme.md":       0,
		"readme.markdown": 1,
		"readme.mdown":    2,
		"readme.mkdn":     3,
	}

	var best *object.TreeEntry
	bestPriority := len(readmePriority)
	for index := range tree.Entries {
		entry := &tree.Entries[index]
		if treeEntryKind(entry.Mode) != "file" {
			continue
		}
		priority, ok := readmePriority[strings.ToLower(entry.Name)]
		if !ok || priority >= bestPriority {
			continue
		}
		best = entry
		bestPriority = priority
	}
	return best
}

func readBlobPreview(file *object.File, repoPath string) (blobPreview, error) {
	reader, err := file.Reader()
	if err != nil {
		return blobPreview{}, err
	}
	defer reader.Close()

	data, err := io.ReadAll(io.LimitReader(reader, maxTextBlobBytes+1))
	if err != nil {
		return blobPreview{}, err
	}

	isTruncated := file.Size > maxTextBlobBytes || len(data) > maxTextBlobBytes
	if len(data) > maxTextBlobBytes {
		data = data[:maxTextBlobBytes]
	}

	isBinary := isBinaryContent(data)
	content := ""
	if !isBinary {
		content = string(data)
	}

	return blobPreview{
		path:        repoPath,
		name:        path.Base(repoPath),
		mode:        file.Mode.String(),
		hash:        file.Hash.String(),
		size:        file.Size,
		content:     content,
		isBinary:    isBinary,
		isTruncated: isTruncated,
	}, nil
}

func isBinaryContent(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	return !utf8.Valid(data)
}

func toProtoBlameLines(repo *git.Repository, lines []*git.Line) ([]*repov1.BlameLine, bool) {
	truncated := len(lines) > maxBlameLines
	if truncated {
		lines = lines[:maxBlameLines]
	}

	summaryByHash := make(map[string]string)
	protoLines := make([]*repov1.BlameLine, 0, len(lines))
	for index, line := range lines {
		hash := line.Hash.String()
		summary, ok := summaryByHash[hash]
		if !ok {
			summary = blameCommitSummary(repo, line.Hash)
			summaryByHash[hash] = summary
		}

		protoLines = append(protoLines, &repov1.BlameLine{
			LineNo:      int32Ptr(int32(index + 1)),
			CommitHash:  stringPtr(hash),
			AuthorName:  stringPtr(line.AuthorName),
			AuthorEmail: stringPtr(line.Author),
			AuthoredAt:  timestamppb.New(line.Date.UTC()),
			Summary:     stringPtr(summary),
			Content:     stringPtr(line.Text),
		})
	}

	return protoLines, truncated
}

func blameCommitSummary(repo *git.Repository, hash plumbing.Hash) string {
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(commit.Message, "\n")[0])
}

func treeEntryKind(mode filemode.FileMode) string {
	switch mode {
	case filemode.Dir:
		return "directory"
	case filemode.Symlink:
		return "symlink"
	case filemode.Submodule:
		return "submodule"
	default:
		return "file"
	}
}

func joinRepositoryPath(parentPath string, name string) string {
	if strings.TrimSpace(parentPath) == "" {
		return name
	}
	return path.Join(parentPath, name)
}

func toProtoRepositoryCommit(commit *object.Commit) *repov1.RepositoryCommit {
	parentHashes := make([]string, 0, len(commit.ParentHashes))
	for _, hash := range commit.ParentHashes {
		parentHashes = append(parentHashes, hash.String())
	}

	return &repov1.RepositoryCommit{
		Hash:           stringPtr(commit.Hash.String()),
		ParentHashes:   parentHashes,
		AuthoredAt:     timestamppb.New(commit.Author.When.UTC()),
		CommittedAt:    timestamppb.New(commit.Committer.When.UTC()),
		AuthorName:     stringPtr(commit.Author.Name),
		AuthorEmail:    stringPtr(commit.Author.Email),
		CommitterName:  stringPtr(commit.Committer.Name),
		CommitterEmail: stringPtr(commit.Committer.Email),
		Message:        stringPtr(commit.Message),
	}
}
