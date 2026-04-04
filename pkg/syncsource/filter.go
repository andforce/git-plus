package syncsource

import (
	"path"
	"slices"
	"strings"
)

type filterResult struct {
	AfterOnlyIncludeTotal int
	AfterExcludeTotal     int
}

func filterResolvedRepos(repos []ResolvedRepo, onlyInclude []string, exclude []string) ([]ResolvedRepo, filterResult) {
	filtered := slices.Clone(repos)

	if len(onlyInclude) > 0 {
		next := make([]ResolvedRepo, 0, len(filtered))
		for _, repo := range filtered {
			if matchesPatterns(repo, onlyInclude) {
				next = append(next, repo)
			}
		}
		filtered = next
	}

	afterOnlyIncludeTotal := len(filtered)

	if len(exclude) > 0 {
		next := make([]ResolvedRepo, 0, len(filtered))
		for _, repo := range filtered {
			if !matchesPatterns(repo, exclude) {
				next = append(next, repo)
			}
		}
		filtered = next
	}

	return filtered, filterResult{
		AfterOnlyIncludeTotal: afterOnlyIncludeTotal,
		AfterExcludeTotal:     len(filtered),
	}
}

func matchesPatterns(repo ResolvedRepo, patterns []string) bool {
	fullName := strings.ToLower(strings.TrimSpace(repo.FullName))
	name := strings.ToLower(strings.TrimSpace(repo.Name))

	for _, rawPattern := range patterns {
		pattern := strings.ToLower(strings.TrimSpace(rawPattern))
		if pattern == "" {
			continue
		}

		if matchesPattern(fullName, pattern) {
			return true
		}

		if !strings.Contains(pattern, "/") && matchesPattern(name, pattern) {
			return true
		}
	}

	return false
}

func matchesPattern(candidate string, pattern string) bool {
	matched, err := path.Match(pattern, candidate)
	if err != nil {
		return candidate == pattern
	}

	return matched
}
