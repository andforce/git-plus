package cronservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	connectvalidate "connectrpc.com/validate"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
	"github.com/ImSingee/git-plus/pkg/cronruntime"
	cronv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/cron/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/cron/v1/cronv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type serviceServer struct {
	dataDir  string
	runtime  *cronruntime.Runtime
	peekRuns int
}

func NewHandler(dataDir string, runtime *cronruntime.Runtime) http.Handler {
	rpcMux := http.NewServeMux()
	RegisterHandlers(rpcMux, dataDir, runtime)
	return http.StripPrefix("/api", rpcMux)
}

func RegisterHandlers(mux *http.ServeMux, dataDir string, runtime *cronruntime.Runtime) {
	path, handler := cronv1connect.NewCronServiceHandler(
		&serviceServer{
			dataDir:  dataDir,
			runtime:  runtime,
			peekRuns: cronruntime.DefaultNextRunsLimit,
		},
		connect.WithInterceptors(mustValidateInterceptor()),
	)
	mux.Handle(path, handler)
}

func (s *serviceServer) GetCronRuntime(
	_ context.Context,
	_ *connect.Request[cronv1.GetCronRuntimeRequest],
) (*connect.Response[cronv1.GetCronRuntimeResponse], error) {
	runtimeSnapshot, err := s.runtime.Snapshot(s.peekRuns)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get cron runtime: %w", err))
	}

	return connect.NewResponse(&cronv1.GetCronRuntimeResponse{
		Runtime: toProtoRuntime(runtimeSnapshot),
	}), nil
}

func (s *serviceServer) UpdateCron(
	_ context.Context,
	req *connect.Request[cronv1.UpdateCronRequest],
) (*connect.Response[cronv1.UpdateCronResponse], error) {
	cronExpr := strings.TrimSpace(req.Msg.GetCron())
	if err := appconfig.ValidateCronExpression(cronExpr); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cron must be a valid 5-field cron expression"))
	}

	loaded, _, err := appconfig.LoadOrDefault(appconfig.PathForDataDir(s.dataDir))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("load config: %w", err))
	}

	loaded.Data.Cron = cronExpr
	if err := appconfig.Save(loaded.Path, loaded.Data); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("save config: %w", err))
	}
	if err := s.runtime.ApplyCron(cronExpr); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("apply cron: %w", err))
	}

	runtimeSnapshot, err := s.runtime.Snapshot(s.peekRuns)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get cron runtime: %w", err))
	}

	return connect.NewResponse(&cronv1.UpdateCronResponse{
		Runtime: toProtoRuntime(runtimeSnapshot),
	}), nil
}

func (s *serviceServer) ReloadCron(
	_ context.Context,
	_ *connect.Request[cronv1.ReloadCronRequest],
) (*connect.Response[cronv1.ReloadCronResponse], error) {
	if err := s.runtime.LoadFromFileAndApply(); err != nil {
		if errors.Is(err, cronruntime.ErrInvalidCronConfig) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reload cron: %w", err))
	}

	runtimeSnapshot, err := s.runtime.Snapshot(s.peekRuns)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get cron runtime: %w", err))
	}

	return connect.NewResponse(&cronv1.ReloadCronResponse{
		Runtime: toProtoRuntime(runtimeSnapshot),
	}), nil
}

func toProtoRuntime(snapshot cronruntime.Snapshot) *cronv1.CronRuntime {
	protoRuntime := &cronv1.CronRuntime{
		Enabled:   boolPtr(snapshot.Enabled),
		Cron:      stringPtr(snapshot.Cron),
		LastError: stringPtr(snapshot.LastError),
	}
	if !snapshot.UpdatedAt.IsZero() {
		protoRuntime.UpdatedAt = timestamppb.New(snapshot.UpdatedAt)
	}
	if len(snapshot.NextRuns) > 0 {
		protoRuntime.NextRuns = make([]*timestamppb.Timestamp, 0, len(snapshot.NextRuns))
		for _, nextRun := range snapshot.NextRuns {
			protoRuntime.NextRuns = append(protoRuntime.NextRuns, timestamppb.New(nextRun))
		}
	}

	return protoRuntime
}

func mustValidateInterceptor() connect.Interceptor {
	interceptor, err := connectvalidate.NewInterceptor()
	if err != nil {
		panic(fmt.Sprintf("create connect validate interceptor: %v", err))
	}

	return interceptor
}

func boolPtr(value bool) *bool {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
