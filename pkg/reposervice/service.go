package reposervice

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	connectvalidate "connectrpc.com/validate"
	appdb "github.com/ImSingee/git-plus/db"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	repov1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/repo/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/repo/v1/repov1connect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultPageSize = 20

type serviceServer struct {
	dataDir string
	db      *sql.DB
}

type Option func(*serviceServer)

func NewHandler(dataDir string, options ...Option) http.Handler {
	rpcMux := http.NewServeMux()
	RegisterHandlers(rpcMux, dataDir, options...)
	return http.StripPrefix("/api", rpcMux)
}

func RegisterHandlers(mux *http.ServeMux, dataDir string, options ...Option) {
	path, handler := repov1connect.NewRepoServiceHandler(
		newServiceServer(dataDir, options...),
		connect.WithInterceptors(mustValidateInterceptor()),
	)
	mux.Handle(path, handler)
}

func WithDatabase(db *sql.DB) Option {
	return func(server *serviceServer) {
		if db != nil {
			server.db = db
		}
	}
}

func newServiceServer(dataDir string, options ...Option) *serviceServer {
	server := &serviceServer{
		dataDir: dataDir,
	}
	for _, option := range options {
		option(server)
	}
	return server
}

func (s *serviceServer) ListRepositories(
	ctx context.Context,
	req *connect.Request[repov1.ListRepositoriesRequest],
) (*connect.Response[repov1.ListRepositoriesResponse], error) {
	pageSize := int(req.Msg.GetPageSize())
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	offset, err := decodePageToken(req.Msg.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid page_token: %w", err))
	}

	sourceID := optionalFilter(req.Msg.GetSourceId())
	search := optionalFilter(req.Msg.GetSearch())

	queries, cleanup, err := s.openQueries(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer cleanup()

	totalCount, err := queries.CountReposFiltered(ctx, dbsqlc.CountReposFilteredParams{
		Column1: sourceID,
		Column2: search,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("count repos: %w", err))
	}

	repos, err := queries.ListReposFiltered(ctx, dbsqlc.ListReposFilteredParams{
		Column1: sourceID,
		Column2: search,
		Limit:   int64(pageSize),
		Offset:  int64(offset),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list repos: %w", err))
	}

	protoRepos := make([]*repov1.Repository, 0, len(repos))
	for _, repo := range repos {
		protoRepos = append(protoRepos, toProtoRepository(repo))
	}

	response := &repov1.ListRepositoriesResponse{
		Repositories: protoRepos,
		TotalCount:   int32Ptr(int32(totalCount)),
	}
	if int64(offset)+int64(len(repos)) < totalCount {
		response.NextPageToken = stringPtr(encodePageToken(offset + len(repos)))
	}

	return connect.NewResponse(response), nil
}

func (s *serviceServer) openQueries(ctx context.Context) (*dbsqlc.Queries, func(), error) {
	if s.db != nil {
		return dbsqlc.New(s.db), func() {}, nil
	}

	db, err := appdb.Open(ctx, s.dataDir)
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite database: %w", err)
	}

	return dbsqlc.New(db), func() {
		_ = db.Close()
	}, nil
}

func toProtoRepository(repo dbsqlc.Repo) *repov1.Repository {
	protoRepo := &repov1.Repository{
		Id:         int64Ptr(repo.ID),
		SourceId:   stringPtr(repo.SourceID),
		Platform:   stringPtr(repo.Platform),
		Name:       stringPtr(repo.Name),
		FullName:   stringPtr(repo.FullName),
		Owner:      stringPtr(repo.Owner),
		IsPrivate:  boolPtr(repo.IsPrivate != 0),
		IsFork:     boolPtr(repo.IsFork != 0),
		IsArchived: boolPtr(repo.IsArchived != 0),
		Status:     stringPtr(repo.Status),
		LastSeenAt: toProtoTimestamp(repo.LastSeenAt),
		CreatedAt:  toProtoTimestamp(repo.CreatedAt),
		UpdatedAt:  toProtoTimestamp(repo.UpdatedAt),
	}

	if repo.Description.Valid {
		protoRepo.Description = stringPtr(repo.Description.String)
	}
	if repo.HtmlUrl.Valid {
		protoRepo.HtmlUrl = stringPtr(repo.HtmlUrl.String)
	}
	if repo.CloneUrl.Valid {
		protoRepo.CloneUrl = stringPtr(repo.CloneUrl.String)
	}
	if repo.SshUrl.Valid {
		protoRepo.SshUrl = stringPtr(repo.SshUrl.String)
	}
	if repo.DefaultBranch.Valid {
		protoRepo.DefaultBranch = stringPtr(repo.DefaultBranch.String)
	}
	if repo.Visibility.Valid {
		protoRepo.Visibility = stringPtr(repo.Visibility.String)
	}
	if meta := toProtoMetaStruct(repo.Meta); meta != nil {
		protoRepo.Meta = meta
	}

	return protoRepo
}

func toProtoMetaStruct(value string) *structpb.Struct {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		log.Printf("repo meta conversion failed: %v", err)
		return nil
	}

	result, err := structpb.NewStruct(decoded)
	if err != nil {
		log.Printf("repo meta struct conversion failed: %v", err)
		return nil
	}

	return result
}

func toProtoTimestamp(value string) *timestamppb.Timestamp {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		log.Printf("repo timestamp conversion failed: %v", err)
		return nil
	}

	return timestamppb.New(parsed)
}

func mustValidateInterceptor() connect.Interceptor {
	interceptor, err := connectvalidate.NewInterceptor()
	if err != nil {
		panic(fmt.Sprintf("create connect validate interceptor: %v", err))
	}

	return interceptor
}

func optionalFilter(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	return trimmed
}

func decodePageToken(token string) (int, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return 0, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return 0, err
	}

	offset, err := strconv.Atoi(string(decoded))
	if err != nil {
		return 0, err
	}
	if offset < 0 {
		return 0, fmt.Errorf("offset must be non-negative")
	}

	return offset, nil
}

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func stringPtr(value string) *string {
	return &value
}

func int32Ptr(value int32) *int32 {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
