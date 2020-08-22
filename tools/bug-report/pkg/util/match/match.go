package match

import (
	"path/filepath"
	"strings"

	"istio.io/pkg/log"
)

func MatchesMap(selection, cluster map[string]string) bool {
	if len(selection) == 0 {
		return true
	}
	if len(cluster) == 0 {
		return false
	}

	for ks, vs := range selection {
		vc, ok := cluster[ks]
		if !ok {
			return false
		}
		if !MatchesGlob(vc, vs) {
			return false
		}
	}
	return true
}

func MatchesGlobs(matchString string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	if len(patterns) == 1 {
		p := strings.TrimSpace(patterns[0])
		if p == "" || p == "*" {
			return true
		}
	}

	for _, p := range patterns {
		if MatchesGlob(matchString, p) {
			return true
		}
	}
	return false
}

func MatchesGlob(matchString, pattern string) bool {
	match, err := filepath.Match(pattern, matchString)
	if err != nil {
		// Shouldn't be here as prior validation is assumed.
		log.Errorf("Unexpected filepath error for %s match %s: %s", pattern, matchString, err)
		return false
	}
	return match
}
