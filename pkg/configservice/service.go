package configservice

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"connectrpc.com/connect"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
	configv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/config/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/config/v1/configv1connect"
)

type serviceServer struct {
	dataDir string
}

func NewHandler(dataDir string) http.Handler {
	rpcMux := http.NewServeMux()
	path, handler := configv1connect.NewConfigServiceHandler(&serviceServer{
		dataDir: dataDir,
	})
	rpcMux.Handle(path, handler)

	return http.StripPrefix("/api", rpcMux)
}

func (s *serviceServer) CheckConfig(
	_ context.Context,
	_ *connect.Request[configv1.CheckConfigRequest],
) (*connect.Response[configv1.CheckConfigResponse], error) {
	result := appconfig.CheckFile(appconfig.PathForDataDir(s.dataDir))
	appconfig.SortIssues(result.Issues)

	return connect.NewResponse(&configv1.CheckConfigResponse{
		Path:    result.Path,
		Exists:  result.Exists,
		Issues:  toProtoIssues(result.Issues),
		Summary: summarizeProtoIssues(result.Issues),
	}), nil
}

func (s *serviceServer) CheckSourceConfig(
	_ context.Context,
	req *connect.Request[configv1.CheckSourceConfigRequest],
) (*connect.Response[configv1.CheckSourceConfigResponse], error) {
	sourceID := req.Msg.GetSourceId()
	result := appconfig.CheckSource(appconfig.PathForDataDir(s.dataDir), sourceID)
	appconfig.SortIssues(result.Issues)

	return connect.NewResponse(&configv1.CheckSourceConfigResponse{
		Path:     result.Path,
		Exists:   result.Exists,
		SourceId: sourceID,
		Issues:   toProtoIssues(result.Issues),
		Summary:  summarizeProtoIssues(result.Issues),
	}), nil
}

func LogIssuesOnStartup(dataDir string, logger *log.Logger) {
	result := appconfig.CheckFile(appconfig.PathForDataDir(dataDir))
	if !result.Exists {
		return
	}

	appconfig.SortIssues(result.Issues)
	for _, issue := range result.Issues {
		message := "config check " + string(issue.Severity) + " [" + issue.Code + "] " + issue.Message
		if issue.Path != "" {
			message += " (path: " + issue.Path + ")"
		}
		if issue.Line > 0 {
			message += " (line: " + strconv.Itoa(issue.Line) + ")"
		}

		logger.Print(message)
	}
}

func summarizeProtoIssues(issues []appconfig.ValidationIssue) *configv1.IssueSummary {
	summary := appconfig.Summary(issues)

	return &configv1.IssueSummary{
		Error:   int32(summary[appconfig.SeverityError]),
		Warning: int32(summary[appconfig.SeverityWarning]),
		Info:    int32(summary[appconfig.SeverityInfo]),
	}
}

func toProtoIssues(issues []appconfig.ValidationIssue) []*configv1.ValidationIssue {
	protoIssues := make([]*configv1.ValidationIssue, 0, len(issues))

	for _, issue := range issues {
		protoIssues = append(protoIssues, &configv1.ValidationIssue{
			Severity: toProtoSeverity(issue.Severity),
			Code:     issue.Code,
			Message:  issue.Message,
			Path:     issue.Path,
			SourceId: issue.SourceID,
			Line:     int32(issue.Line),
		})
	}

	return protoIssues
}

func toProtoSeverity(severity appconfig.Severity) configv1.ValidationIssue_Severity {
	switch severity {
	case appconfig.SeverityError:
		return configv1.ValidationIssue_SEVERITY_ERROR
	case appconfig.SeverityWarning:
		return configv1.ValidationIssue_SEVERITY_WARNING
	case appconfig.SeverityInfo:
		return configv1.ValidationIssue_SEVERITY_INFO
	default:
		return configv1.ValidationIssue_SEVERITY_UNSPECIFIED
	}
}
