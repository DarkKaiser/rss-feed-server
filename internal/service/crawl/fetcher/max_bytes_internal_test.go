package fetcher

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMaxBytesReader_Read(t *testing.T) {
	t.Run("지정된 크기보다 작으면 정상적으로 읽어야 한다", func(t *testing.T) {
		limit := int64(10)
		body := "small"
		mr := &maxBytesReader{
			rc:    http.MaxBytesReader(nil, io.NopCloser(strings.NewReader(body)), limit),
			limit: limit,
		}

		got, err := io.ReadAll(mr)
		require.NoError(t, err)
		assert.Equal(t, body, string(got))
	})

	t.Run("지정된 크기를 초과하면 사용자 정의 에러를 반환해야 한다", func(t *testing.T) {
		limit := int64(5)
		body := "too large"
		mr := &maxBytesReader{
			rc:    http.MaxBytesReader(nil, io.NopCloser(strings.NewReader(body)), limit),
			limit: limit,
		}

		_, err := io.ReadAll(mr)
		require.Error(t, err)

		// http.MaxBytesError가 아니라 apperrors.InvalidInput이어야 함
		assert.True(t, apperrors.Is(err, apperrors.InvalidInput))
		assert.Contains(t, err.Error(), "응답 본문의 크기가 설정된 제한을 초과했습니다")
	})

	t.Run("DataErrReader를 사용하여 읽기 중 에러 발생 시 그대로 전달해야 한다", func(t *testing.T) {
		limit := int64(100)
		expectedErr := errors.New("read error")
		mr := &maxBytesReader{
			// http.MaxBytesReader는 내부 Reader의 에러를 그대로 전달함
			rc:    http.MaxBytesReader(nil, io.NopCloser(iotest.ErrReader(expectedErr)), limit),
			limit: limit,
		}

		_, err := io.ReadAll(mr)
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})
}

func TestMaxBytesReader_Close(t *testing.T) {
	t.Run("Close 호출 시 내부 Reader의 Close가 호출되어야 한다", func(t *testing.T) {
		mockCloser := new(mockReadCloser)
		mockCloser.On("Close").Return(nil)

		// http.MaxBytesReader는 감싸는 ReadCloser의 Close를 호출하지 않을 수 있으므로,
		// maxBytesReader가 rc로 무엇을 가지고 있는지에 따라 다름.
		// max_bytes.go:125 -> rc: http.MaxBytesReader(nil, resp.Body, f.limit)
		// http.MaxBytesReader는 Close() 메서드를 가짐. 하지만 표준 라이브러리 구현상
		// http.MaxBytesReader.Close()는 아무 동작도 안할 수 있음?
		// 확인: http.MaxBytesReader는 ReadCloser를 반환하지만,
		// Go 문서에 따르면 "It returns a ReadCloser."
		// 그리고 "If the server is implemented with http.Server, the Close method of the returned ReadCloser closes the underlying response body."
		// 하지만 클라이언트 사이드에서는?

		// 실제 구현(max_bytes.go:40)을 보면 r.rc.Close()를 호출함.
		// 테스트에서는 r.rc에 Mock 객체를 직접 주입하여 검증.

		mr := &maxBytesReader{
			rc:    mockCloser,
			limit: 10,
		}

		err := mr.Close()
		require.NoError(t, err)
		mockCloser.AssertExpectations(t)
	})

	t.Run("Close 호출 시 에러가 발생하면 그대로 반환해야 한다", func(t *testing.T) {
		expectedErr := errors.New("close error")
		mockCloser := new(mockReadCloser)
		mockCloser.On("Close").Return(expectedErr)

		mr := &maxBytesReader{
			rc:    mockCloser,
			limit: 10,
		}

		err := mr.Close()
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
		mockCloser.AssertExpectations(t)
	})
}

// mockReadCloser is a mock for io.ReadCloser
type mockReadCloser struct {
	mock.Mock
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *mockReadCloser) Close() error {
	args := m.Called()
	return args.Error(0)
}
