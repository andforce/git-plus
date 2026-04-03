package config

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type ValidationIssue struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Path     string   `json:"path,omitempty"`
	SourceID string   `json:"source_id,omitempty"`
	Line     int      `json:"line,omitempty"`
}

type CheckResult struct {
	Path   string            `json:"path"`
	Exists bool              `json:"exists"`
	Issues []ValidationIssue `json:"issues"`
}

var linePattern = regexp.MustCompile(`line ([0-9]+)`)

func CheckFile(path string) CheckResult {
	result := CheckResult{
		Path: path,
	}

	loaded, err := Load(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result
		}

		result.Exists = !errors.Is(err, fs.ErrNotExist)
		result.Issues = []ValidationIssue{loadErrorIssue(err)}
		return result
	}

	result.Exists = true
	result.Issues = ValidateConfig(loaded)
	return result
}

func CheckSource(path string, sourceID string) CheckResult {
	result := CheckResult{
		Path: path,
	}

	loaded, err := Load(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result
		}

		result.Exists = !errors.Is(err, fs.ErrNotExist)
		result.Issues = []ValidationIssue{loadErrorIssue(err)}
		return result
	}

	result.Exists = true
	result.Issues = ValidateSource(loaded, sourceID)
	return result
}

func ValidateConfig(loaded LoadedConfig) []ValidationIssue {
	root := documentRoot(loaded.Node)
	issues := make([]ValidationIssue, 0)

	issues = append(issues, validateUnknownFields(root, "", topLevelFields)...)
	issues = append(issues, validateSourceCollection(loaded.Data.Sources, root)...)
	issues = append(issues, validateConcurrency(loaded.Data, root)...)

	return issues
}

func ValidateSource(loaded LoadedConfig, sourceID string) []ValidationIssue {
	root := documentRoot(loaded.Node)
	sourcesNode, _, _ := mappingValue(root, "sources")
	sourceNodes := sequenceItems(sourcesNode)
	issues := make([]ValidationIssue, 0)
	matchCount := 0

	for index, source := range loaded.Data.Sources {
		if source.ID != sourceID {
			continue
		}

		matchCount++
		issues = append(issues, validateUnknownFields(sourceNodeAt(sourceNodes, index), fmt.Sprintf("sources[%d].", index), sourceFields)...)
		issues = append(issues, validateSourceFields(source, sourceNodeAt(sourceNodes, index), index)...)
	}

	issues = append(issues, validateDuplicateSourceIDs(loaded.Data.Sources, sourceNodes, sourceID)...)

	if matchCount == 0 {
		issues = append(issues, ValidationIssue{
			Severity: SeverityError,
			Code:     "source_not_found",
			Message:  fmt.Sprintf("source %q was not found", sourceID),
			Path:     "sources",
			SourceID: sourceID,
		})
	}

	return issues
}

var topLevelFields = []string{
	"sources",
	"concurrency",
}

var sourceFields = []string{
	"id",
	"platform",
	"username",
	"token",
	"only_include_repos",
	"exclude_repos",
}

func validateSourceCollection(sources []SourceConfig, root *yaml.Node) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	sourcesNode, sourcesKeyNode, sourcesExists := mappingValue(root, "sources")
	sourceNodes := sequenceItems(sourcesNode)

	if len(sources) == 0 {
		line := 0
		if sourcesExists && sourcesKeyNode != nil {
			line = sourcesKeyNode.Line
		} else if root != nil {
			line = root.Line
		}

		issues = append(issues, ValidationIssue{
			Severity: SeverityWarning,
			Code:     "empty_sources",
			Message:  "sources is empty",
			Path:     "sources",
			Line:     line,
		})
	}

	for index, source := range sources {
		node := sourceNodeAt(sourceNodes, index)
		issues = append(issues, validateUnknownFields(node, fmt.Sprintf("sources[%d].", index), sourceFields)...)
		issues = append(issues, validateSourceFields(source, node, index)...)
	}

	issues = append(issues, validateDuplicateSourceIDs(sources, sourceNodes, "")...)

	return issues
}

func validateSourceFields(source SourceConfig, node *yaml.Node, index int) []ValidationIssue {
	issues := make([]ValidationIssue, 0)

	requiredFields := []struct {
		name  string
		value string
	}{
		{name: "id", value: source.ID},
		{name: "platform", value: source.Platform},
		{name: "username", value: source.Username},
		{name: "token", value: source.Token},
	}

	for _, field := range requiredFields {
		if strings.TrimSpace(field.value) != "" {
			continue
		}

		line := node.Line
		if valueNode, _, ok := mappingValue(node, field.name); ok && valueNode != nil {
			line = valueNode.Line
		}

		issues = append(issues, ValidationIssue{
			Severity: SeverityError,
			Code:     "missing_required_field",
			Message:  fmt.Sprintf("%s is required", field.name),
			Path:     fmt.Sprintf("sources[%d].%s", index, field.name),
			SourceID: source.ID,
			Line:     line,
		})
	}

	if platform := strings.TrimSpace(source.Platform); platform != "" && platform != "github" {
		line := node.Line
		if valueNode, _, ok := mappingValue(node, "platform"); ok && valueNode != nil {
			line = valueNode.Line
		}

		issues = append(issues, ValidationIssue{
			Severity: SeverityError,
			Code:     "unsupported_platform",
			Message:  fmt.Sprintf("platform %q is not supported", source.Platform),
			Path:     fmt.Sprintf("sources[%d].platform", index),
			SourceID: source.ID,
			Line:     line,
		})
	}

	return issues
}

func validateDuplicateSourceIDs(sources []SourceConfig, sourceNodes []*yaml.Node, sourceID string) []ValidationIssue {
	occurrences := make(map[string][]int)
	issues := make([]ValidationIssue, 0)

	for index, source := range sources {
		normalizedID := strings.TrimSpace(source.ID)
		if normalizedID == "" {
			continue
		}

		occurrences[normalizedID] = append(occurrences[normalizedID], index)
	}

	for duplicateID, indexes := range occurrences {
		if len(indexes) < 2 {
			continue
		}
		if sourceID != "" && duplicateID != sourceID {
			continue
		}

		for _, index := range indexes {
			node := sourceNodeAt(sourceNodes, index)
			line := node.Line
			if valueNode, _, ok := mappingValue(node, "id"); ok && valueNode != nil {
				line = valueNode.Line
			}

			issues = append(issues, ValidationIssue{
				Severity: SeverityError,
				Code:     "duplicate_source_id",
				Message:  fmt.Sprintf("source id %q is duplicated", duplicateID),
				Path:     fmt.Sprintf("sources[%d].id", index),
				SourceID: duplicateID,
				Line:     line,
			})
		}
	}

	return issues
}

func validateConcurrency(cfg Config, root *yaml.Node) []ValidationIssue {
	if !mappingHasKey(root, "concurrency") {
		return nil
	}
	if cfg.Concurrency > 0 {
		return nil
	}

	valueNode, _, _ := mappingValue(root, "concurrency")
	line := 0
	if valueNode != nil {
		line = valueNode.Line
	}

	return []ValidationIssue{
		{
			Severity: SeverityError,
			Code:     "invalid_concurrency",
			Message:  "concurrency must be greater than 0",
			Path:     "concurrency",
			Line:     line,
		},
	}
}

func validateUnknownFields(node *yaml.Node, pathPrefix string, allowed []string) []ValidationIssue {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	issues := make([]ValidationIssue, 0)
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		allowedSet[field] = struct{}{}
	}

	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		if _, ok := allowedSet[keyNode.Value]; ok {
			continue
		}

		issues = append(issues, ValidationIssue{
			Severity: SeverityWarning,
			Code:     "unknown_field",
			Message:  fmt.Sprintf("field %q is not recognized", keyNode.Value),
			Path:     pathPrefix + keyNode.Value,
			Line:     keyNode.Line,
		})
	}

	return issues
}

func loadErrorIssue(err error) ValidationIssue {
	code := "config_read_failed"
	message := err.Error()

	if strings.Contains(err.Error(), "parse config file") || strings.Contains(err.Error(), "decode config file") {
		code = "invalid_yaml"
	}

	return ValidationIssue{
		Severity: SeverityError,
		Code:     code,
		Message:  message,
		Line:     parseLineFromError(err),
	}
}

func parseLineFromError(err error) int {
	match := linePattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return 0
	}

	line, parseErr := strconv.Atoi(match[1])
	if parseErr != nil {
		return 0
	}

	return line
}

func documentRoot(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}

	return node
}

func mappingValue(node *yaml.Node, key string) (valueNode *yaml.Node, keyNode *yaml.Node, ok bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil, false
	}

	for index := 0; index+1 < len(node.Content); index += 2 {
		currentKeyNode := node.Content[index]
		if currentKeyNode.Value != key {
			continue
		}

		return node.Content[index+1], currentKeyNode, true
	}

	return nil, nil, false
}

func mappingHasKey(node *yaml.Node, key string) bool {
	_, _, ok := mappingValue(node, key)
	return ok
}

func sequenceItems(node *yaml.Node) []*yaml.Node {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}

	return node.Content
}

func sourceNodeAt(nodes []*yaml.Node, index int) *yaml.Node {
	if index < 0 || index >= len(nodes) {
		return nil
	}

	return nodes[index]
}

func Summary(issues []ValidationIssue) map[Severity]int {
	summary := map[Severity]int{
		SeverityError:   0,
		SeverityWarning: 0,
		SeverityInfo:    0,
	}

	for _, issue := range issues {
		summary[issue.Severity]++
	}

	return summary
}

func SortIssues(issues []ValidationIssue) {
	slices.SortStableFunc(issues, func(a ValidationIssue, b ValidationIssue) int {
		switch {
		case a.Line != b.Line:
			return a.Line - b.Line
		case a.Path != b.Path:
			return strings.Compare(a.Path, b.Path)
		default:
			return strings.Compare(a.Code, b.Code)
		}
	})
}
