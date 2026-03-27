package rss

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/feed"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---
type MockFeedRepo struct {
	mock.Mock
}

func (m *MockFeedRepo) SaveArticles(ctx context.Context, providerID string, articles []*feed.Article) (int, error) {
	args := m.Called(ctx, providerID, articles)
	return args.Int(0), args.Error(1)
}

func (m *MockFeedRepo) GetArticles(ctx context.Context, providerID string, boardIDs []string, limit uint) ([]*feed.Article, error) {
	args := m.Called(ctx, providerID, boardIDs, limit)
	var res []*feed.Article
	if v := args.Get(0); v != nil {
		res = v.([]*feed.Article)
	}
	return res, args.Error(1)
}

func (m *MockFeedRepo) GetCrawlingCursor(ctx context.Context, providerID, boardID string) (string, time.Time, error) {
	args := m.Called(ctx, providerID, boardID)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *MockFeedRepo) UpsertLatestCrawledArticleID(ctx context.Context, providerID, boardID, articleID string) error {
	args := m.Called(ctx, providerID, boardID, articleID)
	return args.Error(0)
}

type dummyTemplateRenderer struct{}

func (t *dummyTemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	_, err := w.Write([]byte("rendered"))
	return err
}

// --- Handler Logic Tests ---

func TestHasHTMLTags(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "순수 텍스트",
			content:  "안녕하세요.\n반갑습니다.",
			expected: false,
		},
		{
			name:     "br 태그 포함",
			content:  "안녕하세요.<br>반갑습니다.",
			expected: true,
		},
		{
			name:     "대소문자 혼용 태그",
			content:  "<P>테스트</P>",
			expected: true,
		},
		{
			name:     "img 태그 포함",
			content:  `<div><img src="test.jpg" alt="test"></div>`,
			expected: true,
		},
		{
			name:     "의미없는 단순 부등호",
			content:  "a < b 이고 c > d 이다.",
			expected: false,
		},
		{
			name:     "지원하지 않는 html 태그는 스킵",
			content:  "<span>그냥 텍스트</span>",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := htmlTagRegex.MatchString(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContentReplacerLogic(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "순수 텍스트 줄바꿈 변환",
			content:  "1번줄\n2번줄\r\n3번줄",
			expected: "1번줄<br/>2번줄<br/>3번줄",
		},
		{
			name:     "HTML 태그 포함 시 변환 안함",
			content:  "<div>\n1번줄\n</div>",
			expected: "<div>\n1번줄\n</div>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.content
			if !htmlTagRegex.MatchString(result) {
				result = nl2brReplacer.Replace(result)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNew(t *testing.T) {
	cfg := &config.RSSFeedConfig{
		Providers: []*config.ProviderConfig{
			{
				ID: "TEST-PROVIDER",
				Config: &config.ProviderDetailConfig{
					Boards: []*config.BoardConfig{
						&config.BoardConfig{ID: "board1", Name: "Board One"},
					},
				},
			},
		},
	}

	t.Run("panic if config is nil", func(t *testing.T) {
		assert.PanicsWithValue(t, "config.RSSFeedConfig는 필수입니다", func() {
			New(nil, new(MockFeedRepo), nil)
		})
	})

	t.Run("panic if repo is nil", func(t *testing.T) {
		assert.PanicsWithValue(t, "feed.Repository는 필수입니다", func() {
			New(cfg, nil, nil)
		})
	})

	t.Run("success", func(t *testing.T) {
		h := New(cfg, new(MockFeedRepo), nil)
		assert.NotNil(t, h)
		// Check case-insensitive map key initialization
		pc, ok := h.providers["test-provider"]
		assert.True(t, ok)
		assert.Equal(t, []string{"board1"}, pc.boardIDs)
		assert.Equal(t, "Board One", pc.boardNameByID["board1"])
		assert.False(t, h.startedAt.IsZero())
	})
}

func TestHandler_ViewSummary(t *testing.T) {
	e := echo.New()
	e.Renderer = &dummyTemplateRenderer{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := New(&config.RSSFeedConfig{}, new(MockFeedRepo), nil)
	err := h.ViewSummary(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "rendered")
}

func TestHandler_GetFeed(t *testing.T) {
	cfg := &config.RSSFeedConfig{
		MaxItemCount: 10,
		Providers: []*config.ProviderConfig{
			{
				ID: "provider1",
				Config: &config.ProviderDetailConfig{
					Name:        "Test Provider",
					URL:         "http://test.com",
					Description: "Test Desc",
					Boards: []*config.BoardConfig{
						&config.BoardConfig{ID: "b1", Name: "Board 1"},
					},
				},
			},
		},
	}

	t.Run("Feed Not Found", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("unknown")

		h := New(cfg, new(MockFeedRepo), nil)
		err := h.GetFeed(c)

		var he *echo.HTTPError
		assert.True(t, errors.As(err, &he))
		assert.Equal(t, http.StatusNotFound, he.Code)
	})

	t.Run("Success with DB results", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("provider1.xml") // Should trim .xml properly

		mockRepo := new(MockFeedRepo)
		now := time.Now()
		mockRepo.On("GetArticles", mock.Anything, "provider1", []string{"b1"}, uint(10)).Return([]*feed.Article{
			{
				ArticleID: "1",
				BoardID:   "b1",
				Title:     "Title 1",
				Content:   "Content 1<br/>",
				Link:      "http://test.com/1",
				CreatedAt: now,
			},
		}, nil)

		h := New(cfg, mockRepo, nil)
		err := h.GetFeed(c)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Cache-Control"), "max-age=60")
		assert.Contains(t, rec.Body.String(), "Title 1")
		assert.Contains(t, rec.Body.String(), "Board 1") // Name mapped from b1
		mockRepo.AssertExpectations(t)
	})

	t.Run("DB Context Cancelled (Client Timeout)", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("provider1")

		mockRepo := new(MockFeedRepo)
		mockRepo.On("GetArticles", mock.Anything, "provider1", []string{"b1"}, uint(10)).Return(nil, context.Canceled)

		h := New(cfg, mockRepo, nil)
		err := h.GetFeed(c)

		// Context errors are bubbled up directly
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("DB Unknown Error (Server Error, 500)", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("provider1")

		mockRepo := new(MockFeedRepo)
		mockRepo.On("GetArticles", mock.Anything, "provider1", []string{"b1"}, uint(10)).Return(nil, errors.New("db connection lost"))

		h := New(cfg, mockRepo, nil)
		err := h.GetFeed(c)

		var he *echo.HTTPError
		assert.True(t, errors.As(err, &he))
		assert.Equal(t, http.StatusInternalServerError, he.Code)
	})

	t.Run("No Articles Fallback to Server Start Time", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("provider1")

		mockRepo := new(MockFeedRepo)
		mockRepo.On("GetArticles", mock.Anything, "provider1", []string{"b1"}, uint(10)).Return([]*feed.Article{}, nil)

		h := New(cfg, mockRepo, nil)
		err := h.GetFeed(c)

		assert.NoError(t, err)
		assert.Contains(t, rec.Body.String(), h.startedAt.Format(time.RFC1123Z))
	})
}
