package fetcher

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_isSensitiveKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		// 1. Exact Match (정확히 일치)
		{name: "Exact match: token", key: "token", expected: true},
		{name: "Exact match: secret", key: "secret", expected: true},
		{name: "Exact match: password", key: "password", expected: true},
		{name: "Exact match: api_key", key: "api_key", expected: true},
		{name: "Exact match: Case Insensitive", key: "ToKeN", expected: true},

		// 2. Suffix Match (접미사 일치)
		{name: "Suffix match: _token", key: "access_token", expected: true}, // both exact list and suffix match
		{name: "Suffix match: custom_token", key: "custom_token", expected: true},
		{name: "Suffix match: _secret", key: "app_secret", expected: true},
		{name: "Suffix match: _password", key: "db_password", expected: true},
		{name: "Suffix match: case insensitive suffix", key: "My_SeCrEt", expected: true},

		// 3. False Positives (오탐 방지) - Partial Match
		{name: "Partial match: monkey (contains key)", key: "monkey", expected: false},           // "key" exact match list
		{name: "Partial match: broken (contains token)", key: "broken", expected: false},         // "token" exact match list
		{name: "Partial match: passage (contains pass)", key: "passage", expected: false},        // "pass" exact match list
		{name: "Partial match: compass (contains pass)", key: "compass", expected: false},        // "pass" exact match list
		{name: "Partial match: keyword (contains key)", key: "keyword", expected: false},         // "key" exact match list
		{name: "Partial match: oss_signature (not _sig)", key: "oss_signature", expected: false}, // "signature" is exact match, "oss_signature" doesn't match suffix

		// 4. False Positives - Suffix Mismatch
		{name: "Suffix mismatch: _key (too aggressive suffix excluded)", key: "my_key", expected: false}, // "_key" is NOT in sensitiveSuffixes
		{name: "Suffix mismatch: token_id (prefix match)", key: "token_id", expected: false},
		{name: "Suffix mismatch: secret_agent", key: "secret_agent", expected: false},

		// 5. Non-sensitive keys
		{name: "Common key: id", key: "id", expected: false},
		{name: "Common key: page", key: "page", expected: false},
		{name: "Common key: sort", key: "sort", expected: false},
		{name: "Common key: view", key: "view", expected: false},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isSensitiveKey(tt.key)
			assert.Equal(t, tt.expected, result, "key: %s", tt.key)
		})
	}
}

func Test_redactHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    http.Header
		expected http.Header
	}{
		{
			name:     "Nil header returns nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "Empty header returns empty",
			input:    http.Header{},
			expected: http.Header{},
		},
		{
			name: "No sensitive headers are preserved",
			input: http.Header{
				"Content-Type": []string{"application/json"},
				"Accept":       []string{"*/*"},
				"Host":         []string{"example.com"},
			},
			expected: http.Header{
				"Content-Type": []string{"application/json"},
				"Accept":       []string{"*/*"},
				"Host":         []string{"example.com"},
			},
		},
		{
			name: "Sensitive headers are redacted",
			input: http.Header{
				"Authorization":       []string{"Bearer secret-token"},
				"Proxy-Authorization": []string{"Basic user:pass"},
				"Cookie":              []string{"session=abc"},
				"Set-Cookie":          []string{"id=123"},
				"X-Custom-Header":     []string{"value"}, // Should be preserved
			},
			expected: http.Header{
				"Authorization":       []string{"***"},
				"Proxy-Authorization": []string{"***"},
				"Cookie":              []string{"***"},
				"Set-Cookie":          []string{"***"},
				"X-Custom-Header":     []string{"value"},
			},
		},
		{
			name: "Headers are case-insensitive",
			input: func() http.Header {
				h := http.Header{}
				h.Set("authorization", "Bearer lower") // Canonicalizes to "Authorization"
				h.Set("COOKIE", "session=upper")       // Canonicalizes to "Cookie"
				return h
			}(),
			expected: http.Header{
				"Authorization": []string{"***"},
				"Cookie":        []string{"***"},
			},
		},
		{
			name: "Multiple values in sensitive headers (Set-Cookie)",
			input: http.Header{
				"Set-Cookie": []string{"session=abc", "track=xyz"},
			},
			expected: http.Header{
				"Set-Cookie": []string{"***"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := redactHeaders(tt.input)
			assert.Equal(t, tt.expected, result)

			if tt.input != nil {
				// Immutability check: modifying result should not affect input
				result.Set("New-Header", "value")
				assert.Empty(t, tt.input.Get("New-Header"), "Original header should not be modified")
			}
		})
	}
}

func Test_redactURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string // String input for convenience
		expected string
	}{
		{
			name:     "Nil URL",
			input:    "", // Handled specially in test logic
			expected: "",
		},
		{
			name:     "Simple URL without secrets",
			input:    "https://example.com/path",
			expected: "https://example.com/path",
		},
		{
			name:     "URL with user info (password)",
			input:    "https://user:password@example.com/path",
			expected: "https://user:xxxxx@example.com/path",
		},
		{
			name:     "URL with user info (no password, treated as token)",
			input:    "https://token@example.com/path",
			expected: "https://xxxxx@example.com/path",
		},
		// Selective Redaction Tests
		{
			name:     "Selective redaction: specific keys masked",
			input:    "https://example.com/path?token=secret&api_key=12345",
			expected: "https://example.com/path?api_key=xxxxx&token=xxxxx", // Sorted
		},
		{
			name:     "Selective redaction: non-sensitive keys preserved",
			input:    "https://example.com/path?id=123&page=1&sort=desc",
			expected: "https://example.com/path?id=123&page=1&sort=desc",
		},
		{
			name:     "Selective redaction: Mixed sensitive and non-sensitive",
			input:    "https://example.com/path?token=secret&id=123&mode=view",
			expected: "https://example.com/path?id=123&mode=view&token=xxxxx",
		},
		// Edge Cases for Matching
		{
			name:     "False positive check: broken",
			input:    "https://example.com?broken=value",
			expected: "https://example.com?broken=value",
		},
		{
			name:     "False positive check: monkey",
			input:    "https://example.com?monkey=banana",
			expected: "https://example.com?monkey=banana",
		},
		{
			name:     "Suffix match check: my_token",
			input:    "https://example.com?my_token=secret",
			expected: "https://example.com?my_token=xxxxx",
		},
		// Complex URLs
		{
			name:     "Complex: User auth + Query params + Fragment",
			input:    "https://admin:pass@example.com:8443/api?q=search&token=jwt#fragment",
			expected: "https://admin:xxxxx@example.com:8443/api?q=search&token=xxxxx#fragment",
		},
		{
			name:     "URL with multiple values for same key",
			input:    "https://example.com?id=1&token=a&token=b",
			expected: "https://example.com?id=1&token=xxxxx",
		},
		{
			name:     "Opaque URL",
			input:    "mailto:user@example.com",
			expected: "mailto:user@example.com", // Opaque URLs don't have user/pass via User field usually
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.input == "" {
				assert.Equal(t, "", redactURL(nil))
				return
			}

			u, err := url.Parse(tt.input)
			require.NoError(t, err)

			result := redactURL(u)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_redactRawURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// 1. Valid URLs (Delegates to redactURL)
		{
			name:     "Valid URL standard",
			input:    "https://user:pass@example.com/path?token=secret",
			expected: "https://user:xxxxx@example.com/path?token=xxxxx",
		},

		// 2. Fallback Logic
		{
			name:     "Scheme-less proxy URL (user:pass@host:port)",
			input:    "user:pass@proxy.example.com:8080",
			expected: "xxxxx:xxxxx@proxy.example.com:8080",
		},
		{
			name:     "Scheme-less auth (user:pass@host)",
			input:    "admin:1234@internal-service",
			expected: "xxxxx:xxxxx@internal-service",
		},
		{
			name:     "@ in query param (Should utilize redactURL logic)",
			input:    "example.com/search?email=user@test.com",
			expected: "example.com/search?email=user@test.com",
		},
		{
			name:     "@ in path (Fallback handles aggressive)",
			input:    "user@host-without-scheme",
			expected: "xxxxx:xxxxx@host-without-scheme",
		},
		{
			name:     "Malformed URL with @ (Fallback triggers)",
			input:    "http://user:pass@invalid\nnewline.com",
			expected: "http://xxxxx:xxxxx@invalid\nnewline.com",
		},

		// 3. No change scenarios
		{
			name:     "Simple string",
			input:    "simple-string",
			expected: "simple-string",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := redactRawURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_redactRefererURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Nil input",
			input:    "",
			expected: "",
		},
		{
			name:     "Removes user info (RFC 7231)",
			input:    "https://user:pass@example.com/page",
			expected: "https://example.com/page",
		},
		{
			name:     "Removes user info (token style)",
			input:    "https://token@example.com/page",
			expected: "https://example.com/page",
		},
		{
			name:     "Masks sensitive query params",
			input:    "https://example.com/search?q=cat&token=secret",
			expected: "https://example.com/search?q=cat&token=xxxxx",
		},
		{
			name:     "Mixed removal and masking",
			input:    "https://user:pass@example.com/path?api_key=123&mode=dark",
			expected: "https://example.com/path?api_key=xxxxx&mode=dark",
		},
		{
			name:     "Preserves non-sensitive components",
			input:    "https://example.com/path#fragment",
			expected: "https://example.com/path#fragment",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.input == "" {
				assert.Equal(t, "", redactRefererURL(nil))
				return
			}

			u, err := url.Parse(tt.input)
			require.NoError(t, err)

			result := redactRefererURL(u)
			assert.Equal(t, tt.expected, result)
		})
	}
}
