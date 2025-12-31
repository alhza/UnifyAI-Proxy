package util

import "testing"

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		str      string
		expected bool
	}{
		// Exact matches
		{"exact match", "gpt-4", "gpt-4", true},
		{"exact mismatch", "gpt-4", "gpt-3.5", false},
		{"empty pattern", "", "anything", false},
		{"empty string", "pattern", "", false},
		{"both empty", "", "", false},

		// Wildcard matches
		{"wildcard match prefix", "gpt-*", "gpt-4", true},
		{"wildcard match longer", "gpt-*", "gpt-4-turbo", true},
		{"wildcard no match", "gpt-*", "claude-3", false},
		{"wildcard only asterisk", "*", "anything", true},
		{"wildcard empty after prefix", "prefix*", "prefix", true},

		// Claude models
		{"claude wildcard", "claude-3-*", "claude-3-opus", true},
		{"claude wildcard sonnet", "claude-3-*", "claude-3-sonnet", true},
		{"claude exact", "claude-3-opus-20240229", "claude-3-opus-20240229", true},

		// Gemini models
		{"gemini wildcard", "gemini-*", "gemini-pro", true},
		{"gemini 1.5 wildcard", "gemini-1.5-*", "gemini-1.5-pro", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchWildcard(tt.pattern, tt.str)
			if result != tt.expected {
				t.Errorf("MatchWildcard(%q, %q) = %v, want %v", tt.pattern, tt.str, result, tt.expected)
			}
		})
	}
}

func TestMatchAnyWildcard(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		str      string
		expected bool
	}{
		{"empty patterns", []string{}, "anything", false},
		{"single match", []string{"gpt-*"}, "gpt-4", true},
		{"single no match", []string{"gpt-*"}, "claude-3", false},
		{"multiple first matches", []string{"gpt-*", "claude-*"}, "gpt-4", true},
		{"multiple second matches", []string{"gpt-*", "claude-*"}, "claude-3", true},
		{"multiple no match", []string{"gpt-*", "claude-*"}, "gemini-pro", false},
		{"mixed exact and wildcard", []string{"exact", "prefix-*"}, "prefix-test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchAnyWildcard(tt.patterns, tt.str)
			if result != tt.expected {
				t.Errorf("MatchAnyWildcard(%v, %q) = %v, want %v", tt.patterns, tt.str, result, tt.expected)
			}
		})
	}
}

func TestMatchAnyExact(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		str      string
		expected bool
	}{
		{"empty patterns", []string{}, "anything", false},
		{"single match", []string{"gpt-4"}, "gpt-4", true},
		{"single no match", []string{"gpt-4"}, "gpt-3.5", false},
		{"multiple match", []string{"gpt-4", "gpt-3.5"}, "gpt-3.5", true},
		{"wildcard not expanded", []string{"gpt-*"}, "gpt-4", false}, // Exact match only, no wildcard
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchAnyExact(tt.patterns, tt.str)
			if result != tt.expected {
				t.Errorf("MatchAnyExact(%v, %q) = %v, want %v", tt.patterns, tt.str, result, tt.expected)
			}
		})
	}
}

func TestMatchAnyWildcardOrExact(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		str      string
		expected bool
	}{
		{"exact match", []string{"gpt-4"}, "gpt-4", true},
		{"wildcard match", []string{"gpt-*"}, "gpt-4", true},
		{"both types match exact", []string{"gpt-4", "claude-*"}, "gpt-4", true},
		{"both types match wildcard", []string{"gpt-4", "claude-*"}, "claude-3", true},
		{"no match", []string{"gpt-4", "claude-*"}, "gemini-pro", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchAnyWildcardOrExact(tt.patterns, tt.str)
			if result != tt.expected {
				t.Errorf("MatchAnyWildcardOrExact(%v, %q) = %v, want %v", tt.patterns, tt.str, result, tt.expected)
			}
		})
	}
}

