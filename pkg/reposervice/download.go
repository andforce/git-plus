package reposervice

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	"github.com/ImSingee/git-plus/pkg/repodownload"
	repov1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/repo/v1"
)

const repoDownloadRoutePrefix = "/repos/"

type downloadEstimate struct {
	archiveSizeBytes      int64
	estimatedProcessBytes int64
	estimatedDownloadSize int64
	processingLabel       string
}

type preparedDownload struct {
	rootPath string
	zipPath  string
	filename string
}

type stagedError struct {
	stage repov1.DownloadStage
	err   error
}

func (err stagedError) Error() string {
	return err.err.Error()
}

func (err stagedError) Unwrap() error {
	return err.err
}

func (s *serviceServer) StreamRepositoryDownload(
	ctx context.Context,
	req *connect.Request[repov1.StreamRepositoryDownloadRequest],
	stream *connect.ServerStream[repov1.StreamRepositoryDownloadResponse],
) error {
	repoID := req.Msg.GetRepoId()
	if repoID <= 0 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_id is required"))
	}

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	repo, err := queries.GetRepoById(ctx, repoID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("repository not found"))
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("get repository: %w", err))
	}

	rows, err := queries.ListRepoRefsCurrentByRepoID(ctx, repoID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("list repository refs: %w", err))
	}

	activeRefs := filterActiveDownloadRefs(rows)
	estimate := estimateDownload(repo.ArchiveRepoSizeBytes)

	sendEvent := func(event *repov1.StreamRepositoryDownloadResponse) error {
		event.RepoId = int64Ptr(repoID)
		event.EstimatedProcessingLabel = stringPtr(estimate.processingLabel)
		if estimate.estimatedProcessBytes > 0 {
			event.EstimatedProcessingBytes = int64Ptr(estimate.estimatedProcessBytes)
		}
		if estimate.estimatedDownloadSize > 0 {
			event.EstimatedDownloadBytes = int64Ptr(estimate.estimatedDownloadSize)
		}
		if estimate.archiveSizeBytes > 0 {
			event.ArchiveSizeBytes = int64Ptr(estimate.archiveSizeBytes)
		}
		return stream.Send(event)
	}

	fail := func(stage repov1.DownloadStage, summary string, cause error) error {
		event := &repov1.StreamRepositoryDownloadResponse{
			State:           repov1.DownloadState_DOWNLOAD_STATE_FAILED.Enum(),
			Stage:           stage.Enum(),
			Summary:         stringPtr(summary),
			ProgressPercent: int32Ptr(100),
		}
		if cause != nil {
			event.ErrorMessage = stringPtr(cause.Error())
		}
		if err := sendEvent(event); err != nil {
			return err
		}
		return nil
	}

	if len(activeRefs) == 0 {
		return fail(
			repov1.DownloadStage_DOWNLOAD_STAGE_UNSPECIFIED,
			"No active branches or tags are available for download.",
			nil,
		)
	}

	reportProgress := func(stage repov1.DownloadStage, summary string, percent int32) error {
		return sendEvent(&repov1.StreamRepositoryDownloadResponse{
			State:           repov1.DownloadState_DOWNLOAD_STATE_RUNNING.Enum(),
			Stage:           stage.Enum(),
			Summary:         stringPtr(summary),
			ProgressPercent: int32Ptr(percent),
		})
	}

	artifact, err := s.prepareRepositoryDownload(ctx, repo, activeRefs, reportProgress)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		stage := repov1.DownloadStage_DOWNLOAD_STAGE_UNSPECIFIED
		var stageErr stagedError
		if errors.As(err, &stageErr) {
			stage = stageErr.stage
		}
		return fail(stage, "Failed to prepare download.", err)
	}

	session := s.downloads.Register(repoID, artifact.zipPath, artifact.filename, artifact.rootPath)
	return sendEvent(&repov1.StreamRepositoryDownloadResponse{
		State:            repov1.DownloadState_DOWNLOAD_STATE_READY.Enum(),
		Stage:            repov1.DownloadStage_DOWNLOAD_STAGE_READY.Enum(),
		Summary:          stringPtr("Download is ready."),
		ProgressPercent:  int32Ptr(100),
		DownloadId:       stringPtr(session.ID),
		DownloadFilename: stringPtr(session.Filename),
	})
}

func (s *serviceServer) handleRepositoryDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	repoID, downloadID, ok := parseDownloadRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	session, err := s.downloads.Get(repoID, downloadID)
	if err != nil {
		switch {
		case errors.Is(err, repodownload.ErrSessionNotReady):
			http.Error(w, "download is not ready", http.StatusConflict)
		case errors.Is(err, repodownload.ErrRepoMismatch),
			errors.Is(err, repodownload.ErrNotFound),
			errors.Is(err, repodownload.ErrSessionExpired):
			http.NotFound(w, r)
		default:
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	file, err := os.Open(session.ZipPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDispositionFilename(session.Filename))
	http.ServeContent(w, r, session.Filename, stat.ModTime(), file)
}

func (s *serviceServer) prepareRepositoryDownload(
	ctx context.Context,
	repo dbsqlc.Repo,
	activeRefs []dbsqlc.RepoRefsCurrent,
	reportProgress func(stage repov1.DownloadStage, summary string, percent int32) error,
) (_ preparedDownload, err error) {
	sourcePath := filepath.Join(s.dataDir, "repos", repo.SourceID, repo.RefID)
	if _, statErr := os.Stat(sourcePath); statErr != nil {
		return preparedDownload{}, fmt.Errorf("archive repository not found: %w", statErr)
	}

	rootPath, err := os.MkdirTemp("", "git-plus-repo-download-*")
	if err != nil {
		return preparedDownload{}, fmt.Errorf("create temporary directory: %w", err)
	}

	cleanupRoot := true
	defer func() {
		if cleanupRoot {
			_ = os.RemoveAll(rootPath)
		}
	}()

	barePath := filepath.Join(rootPath, "archive.git")
	workPath := filepath.Join(rootPath, sanitizeRepoPathName(repo.Name))
	filename := buildDownloadFilename(repo)
	zipPath := filepath.Join(rootPath, filename)

	if err := reportProgress(repov1.DownloadStage_DOWNLOAD_STAGE_COPY_BARE, "Copying bare repository to a temporary directory...", 10); err != nil {
		return preparedDownload{}, err
	}
	if err := copyDirectory(ctx, sourcePath, barePath); err != nil {
		return preparedDownload{}, stagedError{
			stage: repov1.DownloadStage_DOWNLOAD_STAGE_COPY_BARE,
			err:   fmt.Errorf("copy bare repository: %w", err),
		}
	}

	if err := reportProgress(repov1.DownloadStage_DOWNLOAD_STAGE_MATERIALIZE_REFS, "Restoring active branches and tags...", 30); err != nil {
		return preparedDownload{}, err
	}
	headBranch, err := materializeActiveRefs(ctx, barePath, repo, activeRefs)
	if err != nil {
		return preparedDownload{}, stagedError{
			stage: repov1.DownloadStage_DOWNLOAD_STAGE_MATERIALIZE_REFS,
			err:   err,
		}
	}

	if err := reportProgress(repov1.DownloadStage_DOWNLOAD_STAGE_MATERIALIZE_REFS, "Creating a normal repository snapshot...", 60); err != nil {
		return preparedDownload{}, err
	}
	if err := cloneWorkingRepository(ctx, barePath, workPath); err != nil {
		return preparedDownload{}, stagedError{
			stage: repov1.DownloadStage_DOWNLOAD_STAGE_MATERIALIZE_REFS,
			err:   err,
		}
	}
	if err := createLocalBranches(ctx, workPath, activeRefs, headBranch); err != nil {
		return preparedDownload{}, stagedError{
			stage: repov1.DownloadStage_DOWNLOAD_STAGE_MATERIALIZE_REFS,
			err:   err,
		}
	}

	if err := reportProgress(repov1.DownloadStage_DOWNLOAD_STAGE_PACKAGE_ZIP, "Packaging repository archive...", 80); err != nil {
		return preparedDownload{}, err
	}
	if err := zipDirectory(ctx, workPath, zipPath, sanitizeRepoPathName(repo.Name)); err != nil {
		return preparedDownload{}, stagedError{
			stage: repov1.DownloadStage_DOWNLOAD_STAGE_PACKAGE_ZIP,
			err:   err,
		}
	}

	cleanupRoot = false
	return preparedDownload{
		rootPath: rootPath,
		zipPath:  zipPath,
		filename: filename,
	}, nil
}

func parseDownloadRoute(path string) (int64, string, bool) {
	trimmed := strings.TrimPrefix(path, repoDownloadRoutePrefix)
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) != 4 || parts[1] != "downloads" || parts[3] != "archive" {
		return 0, "", false
	}

	repoID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || repoID <= 0 {
		return 0, "", false
	}

	downloadID := strings.TrimSpace(parts[2])
	if downloadID == "" {
		return 0, "", false
	}

	return repoID, downloadID, true
}

func filterActiveDownloadRefs(rows []dbsqlc.RepoRefsCurrent) []dbsqlc.RepoRefsCurrent {
	active := make([]dbsqlc.RepoRefsCurrent, 0, len(rows))
	for _, row := range rows {
		if row.Status != "active" {
			continue
		}
		if row.RefKind != "head" && row.RefKind != "tag" {
			continue
		}
		active = append(active, row)
	}
	sort.Slice(active, func(i int, j int) bool {
		return active[i].RefName < active[j].RefName
	})
	return active
}

func estimateDownload(value sql.NullInt64) downloadEstimate {
	if !value.Valid || value.Int64 <= 0 {
		return downloadEstimate{}
	}

	archiveSize := value.Int64
	return downloadEstimate{
		archiveSizeBytes:      archiveSize,
		estimatedProcessBytes: archiveSize,
		estimatedDownloadSize: int64(float64(archiveSize) * 0.7),
		processingLabel:       processingTimeEstimateLabel(archiveSize),
	}
}

func processingTimeEstimateLabel(sizeBytes int64) string {
	const (
		miB = 1024 * 1024
		giB = 1024 * miB
	)

	switch {
	case sizeBytes <= 50*miB:
		return "< 10s"
	case sizeBytes <= 250*miB:
		return "10-30s"
	case sizeBytes <= giB:
		return "30-90s"
	default:
		return "1-3 min"
	}
}

func materializeActiveRefs(ctx context.Context, barePath string, repo dbsqlc.Repo, activeRefs []dbsqlc.RepoRefsCurrent) (string, error) {
	branchNames := make([]string, 0)
	branchHashes := make(map[string]string)

	for _, ref := range activeRefs {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := runGit(ctx, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", ref.RefName, ref.CurrentHash); err != nil {
			return "", fmt.Errorf("restore ref %q: %w", ref.RefName, err)
		}
		if ref.RefKind == "head" {
			shortName := strings.TrimPrefix(ref.RefName, "refs/heads/")
			branchNames = append(branchNames, shortName)
			branchHashes[shortName] = ref.CurrentHash
		}
	}

	sort.Strings(branchNames)
	headBranch := chooseHeadBranch(repo.DefaultBranch, branchNames)
	if headBranch != "" {
		if err := runGit(ctx, filepath.Dir(barePath), "--git-dir", barePath, "symbolic-ref", "HEAD", "refs/heads/"+headBranch); err != nil {
			return "", fmt.Errorf("set HEAD to %q: %w", headBranch, err)
		}
	}

	return headBranch, nil
}

func chooseHeadBranch(defaultBranch sql.NullString, branchNames []string) string {
	if len(branchNames) == 0 {
		return ""
	}

	if defaultBranch.Valid {
		trimmed := strings.TrimSpace(defaultBranch.String)
		for _, branchName := range branchNames {
			if branchName == trimmed {
				return branchName
			}
		}
	}

	return branchNames[0]
}

func cloneWorkingRepository(ctx context.Context, barePath string, workPath string) error {
	if err := runGit(ctx, filepath.Dir(workPath), "clone", barePath, workPath); err != nil {
		return fmt.Errorf("clone working repository: %w", err)
	}
	return nil
}

func createLocalBranches(ctx context.Context, workPath string, activeRefs []dbsqlc.RepoRefsCurrent, headBranch string) error {
	for _, ref := range activeRefs {
		if ref.RefKind != "head" {
			continue
		}
		branchName := strings.TrimPrefix(ref.RefName, "refs/heads/")
		if branchName == headBranch {
			continue
		}
		if err := runGit(ctx, workPath, "branch", "--force", branchName, ref.CurrentHash); err != nil {
			return fmt.Errorf("create local branch %q: %w", branchName, err)
		}
	}
	return nil
}

func copyDirectory(ctx context.Context, src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := dst
		if relPath != "." {
			targetPath = filepath.Join(dst, relPath)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		switch {
		case d.IsDir():
			return os.MkdirAll(targetPath, info.Mode())
		case info.Mode()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, targetPath)
		default:
			return copyFile(path, targetPath, info.Mode())
		}
	})
}

func copyFile(src string, dst string, mode fs.FileMode) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	targetFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer targetFile.Close()

	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}

	return nil
}

func zipDirectory(ctx context.Context, srcDir string, zipPath string, rootName string) error {
	file, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("create zip file: %w", err)
	}
	closeFile := true
	defer func() {
		if closeFile {
			_ = file.Close()
		}
	}()

	zipWriter := zip.NewWriter(file)
	closeZipWriter := true
	defer func() {
		if closeZipWriter {
			_ = zipWriter.Close()
		}
	}()

	if err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		archivePath := filepath.ToSlash(filepath.Join(rootName, relPath))
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = archivePath
		if d.IsDir() {
			header.Name += "/"
			_, err = zipWriter.CreateHeader(header)
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			header.Method = zip.Store
			writer, err := zipWriter.CreateHeader(header)
			if err != nil {
				return err
			}
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, err = writer.Write([]byte(linkTarget))
			return err
		}

		header.Method = zip.Deflate
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}

		_, err = io.Copy(writer, sourceFile)
		closeErr := sourceFile.Close()
		if err != nil {
			return err
		}
		return closeErr
	}); err != nil {
		return fmt.Errorf("write zip archive: %w", err)
	}

	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("close zip writer: %w", err)
	}
	closeZipWriter = false
	if err := file.Close(); err != nil {
		return fmt.Errorf("close zip file: %w", err)
	}
	closeFile = false

	return nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func sanitizeRepoPathName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "repository"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "\x00", "")
	return replacer.Replace(trimmed)
}

func buildDownloadFilename(repo dbsqlc.Repo) string {
	base := strings.TrimSpace(repo.FullName)
	if base == "" {
		base = strings.TrimSpace(repo.Name)
	}
	base = sanitizeRepoPathName(base)
	return base + "-snapshot.zip"
}

func contentDispositionFilename(filename string) string {
	safe := strings.ReplaceAll(filename, "\"", "")
	return fmt.Sprintf("attachment; filename=\"%s\"", safe)
}
