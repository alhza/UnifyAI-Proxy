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

// NormalizeBaseURL removes trailing slashes from a base URL to prevent
// double slashes when joining with path segments
func NormalizeBaseURL(baseURL string) string {
	if baseURL == "" {
		return baseURL
	}
	// Remove all trailing slashes
	for len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}
	return baseURL
}

// JoinURL joins a base URL with path segments, handling trailing/leading slashes
func JoinURL(baseURL string, paths ...string) string {
	result := NormalizeBaseURL(baseURL)
	for _, path := range paths {
		// Remove leading slash from path
		for len(path) > 0 && path[0] == '/' {
			path = path[1:]
		}
		// Remove trailing slash from path (except last segment)
		for len(path) > 0 && path[len(path)-1] == '/' {
			path = path[:len(path)-1]
		}
		if path != "" {
			result = result + "/" + path
		}
	}
	return result
}
