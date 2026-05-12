package reposervice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"connectrpc.com/connect"
)

func (s *serviceServer) handleRepositoryAsset(w http.ResponseWriter, r *http.Request) {
	if _, ok := parseRawBlobRoute(r.URL.Path); ok {
		s.handleRepositoryRawBlob(w, r)
		return
	}
	s.handleRepositoryDownload(w, r)
}

func (s *serviceServer) handleRepositoryRawBlob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	repoID, ok := parseRawBlobRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	repoPath, err := cleanRepositoryPath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if repoPath == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	queries, cleanup, err := s.openQueries(r.Context())
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer cleanup()

	resolved, err := s.resolveArchiveRef(r.Context(), queries, repoID, r.URL.Query().Get("ref"))
	if err != nil {
		writeRawBlobConnectError(w, err)
		return
	}

	objectSpec := resolved.commit.Hash.String() + ":" + repoPath
	objectType, err := runArchiveGitOutput(r.Context(), resolved.archiveDir, "cat-file", "-t", objectSpec)
	if err != nil || strings.TrimSpace(string(objectType)) != "blob" {
		http.NotFound(w, r)
		return
	}

	size, err := rawBlobSize(r.Context(), resolved.archiveDir, objectSpec)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	filename := path.Base(repoPath)
	w.Header().Set("Content-Type", rawBlobMediaType(repoPath))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set(
		"Content-Disposition",
		rawBlobContentDisposition(filename, rawBlobDownloadRequested(r)),
	)

	if r.Method == http.MethodHead {
		return
	}

	if err := streamRawBlob(r.Context(), w, resolved.archiveDir, objectSpec); err != nil {
		if !errors.Is(err, context.Canceled) {
			return
		}
	}
}

func parseRawBlobRoute(routePath string) (int64, bool) {
	trimmed := strings.TrimPrefix(routePath, repoDownloadRoutePrefix)
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) != 2 || parts[1] != "raw" {
		return 0, false
	}

	repoID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || repoID <= 0 {
		return 0, false
	}
	return repoID, true
}

func rawBlobSize(ctx context.Context, archiveDir string, objectSpec string) (int64, error) {
	output, err := runArchiveGitOutput(ctx, archiveDir, "cat-file", "-s", objectSpec)
	if err != nil {
		return 0, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil || size < 0 {
		return 0, fmt.Errorf("invalid blob size")
	}
	return size, nil
}

func streamRawBlob(ctx context.Context, writer io.Writer, archiveDir string, objectSpec string) error {
	command := exec.CommandContext(ctx, "git", "--git-dir", archiveDir, "cat-file", "-p", objectSpec)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return err
	}

	_, copyErr := io.Copy(writer, stdout)
	waitErr := command.Wait()
	if copyErr != nil {
		return copyErr
	}
	if waitErr != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), waitErr)
	}
	return nil
}

func rawBlobDownloadRequested(r *http.Request) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("download")))
	return value == "1" || value == "true" || value == "yes"
}

func rawBlobMediaType(repoPath string) string {
	extension := strings.ToLower(path.Ext(repoPath))
	switch extension {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".htm", ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx",
		".txt", ".text", ".log", ".go", ".rs", ".py", ".rb", ".java",
		".c", ".cc", ".cpp", ".h", ".hpp", ".xml", ".yaml", ".yml":
		return "text/plain; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	}

	if mediaType := mime.TypeByExtension(extension); mediaType != "" {
		return mediaType
	}
	return "application/octet-stream"
}

func rawBlobContentDisposition(filename string, download bool) string {
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	safe := strings.ReplaceAll(filename, "\"", "")
	return fmt.Sprintf("%s; filename=\"%s\"", disposition, safe)
}

func writeRawBlobConnectError(w http.ResponseWriter, err error) {
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	switch connectErr.Code() {
	case connect.CodeInvalidArgument:
		http.Error(w, connectErr.Message(), http.StatusBadRequest)
	case connect.CodeNotFound:
		http.Error(w, connectErr.Message(), http.StatusNotFound)
	case connect.CodeFailedPrecondition:
		http.Error(w, connectErr.Message(), http.StatusPreconditionFailed)
	default:
		http.Error(w, connectErr.Message(), http.StatusInternalServerError)
	}
}
