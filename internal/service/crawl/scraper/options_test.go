package scraper

import (
	"net/http"
	"testing"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
)

//
// Option Tests
//

func TestWithMaxRequestBodySize(t *testing.T) {
	mockFetcher := &mocks.MockFetcher{}
	defaultSize := int64(defaultMaxBodySize)

	tests := []struct {
		name     string
		input    int64
		expected int64
	}{
		{
			name:     "Positive Value",
			input:    1024,
			expected: 1024,
		},
		{
			name:     "Zero Value - Should Ignore",
			input:    0,
			expected: defaultSize,
		},
		{
			name:     "Negative Value - Should Ignore",
			input:    -1,
			expected: defaultSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(mockFetcher, WithMaxRequestBodySize(tt.input)).(*scraper)
			assert.Equal(t, tt.expected, s.maxRequestBodySize)
		})
	}
}

func TestWithMaxResponseBodySize(t *testing.T) {
	mockFetcher := &mocks.MockFetcher{}
	defaultSize := int64(defaultMaxBodySize)

	tests := []struct {
		name     string
		input    int64
		expected int64
	}{
		{
			name:     "Positive Value",
			input:    2048,
			expected: 2048,
		},
		{
			name:     "Zero Value - Should Ignore",
			input:    0,
			expected: defaultSize,
		},
		{
			name:     "Negative Value - Should Ignore",
			input:    -1,
			expected: defaultSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(mockFetcher, WithMaxResponseBodySize(tt.input)).(*scraper)
			assert.Equal(t, tt.expected, s.maxResponseBodySize)
		})
	}
}

func TestWithResponseCallback(t *testing.T) {
	mockFetcher := &mocks.MockFetcher{}
	callback := func(resp *http.Response) {}

	t.Run("Set Callback", func(t *testing.T) {
		s := New(mockFetcher, WithResponseCallback(callback)).(*scraper)
		assert.NotNil(t, s.responseCallback)
	})

	t.Run("Default - No Callback", func(t *testing.T) {
		s := New(mockFetcher).(*scraper)
		assert.Nil(t, s.responseCallback)
	})

	t.Run("Set Nil Callback", func(t *testing.T) {
		s := New(mockFetcher, WithResponseCallback(nil)).(*scraper)
		assert.Nil(t, s.responseCallback)
	})
}

func TestOption_Overwrite(t *testing.T) {
	mockFetcher := &mocks.MockFetcher{}

	t.Run("MaxRequestBodySize Overwrite", func(t *testing.T) {
		s := New(mockFetcher,
			WithMaxRequestBodySize(100),
			WithMaxRequestBodySize(200), // Should win
		).(*scraper)
		assert.Equal(t, int64(200), s.maxRequestBodySize)
	})

	t.Run("MaxResponseBodySize Overwrite", func(t *testing.T) {
		s := New(mockFetcher,
			WithMaxResponseBodySize(1000),
			WithMaxResponseBodySize(2000), // Should win
		).(*scraper)
		assert.Equal(t, int64(2000), s.maxResponseBodySize)
	})

	t.Run("ResponseCallback Overwrite", func(t *testing.T) {
		cb1 := func(resp *http.Response) {}
		cb2 := func(resp *http.Response) {}

		s := New(mockFetcher,
			WithResponseCallback(cb1),
			WithResponseCallback(cb2), // Should win
		).(*scraper)

		assert.NotNil(t, s.responseCallback)
	})
}
