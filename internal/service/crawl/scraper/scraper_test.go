package scraper

import (
	"testing"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher/mocks"
	"github.com/stretchr/testify/assert"
)

//
// Interface Implementation Check
//

func TestScraper_Implementation(t *testing.T) {
	var _ Scraper = (*scraper)(nil)
	var _ HTMLScraper = (*scraper)(nil)
	var _ JSONScraper = (*scraper)(nil)
}

//
// Constant Verification
//

func TestConstants(t *testing.T) {
	assert.Equal(t, int64(10*1024*1024), int64(defaultMaxBodySize), "defaultMaxBodySize should be 10MB")
	assert.Equal(t, "crawl.scraper", component, "component name mismatch")
}

//
// Constructor Tests
//

func TestNew(t *testing.T) {
	t.Run("Panic on nil Fetcher", func(t *testing.T) {
		assert.PanicsWithValue(t, "Fetcher는 필수입니다", func() {
			New(nil)
		})
	})

	t.Run("Initialize with Defaults", func(t *testing.T) {
		mockFetcher := &mocks.MockFetcher{}
		s := New(mockFetcher).(*scraper)

		// Assert internal state
		assert.Equal(t, mockFetcher, s.fetcher)
		assert.Equal(t, int64(defaultMaxBodySize), s.maxRequestBodySize)
		assert.Equal(t, int64(defaultMaxBodySize), s.maxResponseBodySize)
		assert.Nil(t, s.responseCallback)
	})

	t.Run("Initialize with Options", func(t *testing.T) {
		mockFetcher := &mocks.MockFetcher{}
		opts := []Option{
			WithMaxRequestBodySize(1024),
			WithMaxResponseBodySize(2048),
		}

		s := New(mockFetcher, opts...).(*scraper)

		// Assert options are applied
		assert.Equal(t, int64(1024), s.maxRequestBodySize)
		assert.Equal(t, int64(2048), s.maxResponseBodySize)
	})
}
