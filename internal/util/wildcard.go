package util

// MatchWildcard checks if pattern matches str (supports trailing * wildcard)
// Examples:
//   - MatchWildcard("gpt-*", "gpt-4") returns true
//   - MatchWildcard("claude-3-*", "claude-3-opus") returns true
//   - MatchWildcard("exact", "exact") returns true
//   - MatchWildcard("exact", "other") returns false
func MatchWildcard(pattern, str string) bool {
	if len(pattern) == 0 {
		return false
	}
	if pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(str) >= len(prefix) && str[:len(prefix)] == prefix
	}
	return pattern == str
}

// MatchAnyWildcard checks if any pattern in patterns matches str
func MatchAnyWildcard(patterns []string, str string) bool {
	for _, pattern := range patterns {
		if MatchWildcard(pattern, str) {
			return true
		}
	}
	return false
}

// MatchAnyExact checks if str exactly matches any pattern in patterns
func MatchAnyExact(patterns []string, str string) bool {
	for _, pattern := range patterns {
		if pattern == str {
			return true
		}
	}
	return false
}

// MatchAnyWildcardOrExact checks if str matches any pattern (exact or wildcard)
func MatchAnyWildcardOrExact(patterns []string, str string) bool {
	for _, pattern := range patterns {
		if pattern == str || MatchWildcard(pattern, str) {
			return true
		}
	}
	return false
}

