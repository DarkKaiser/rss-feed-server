package scraper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestContextAwareReader_Read_Normal 정상적인 읽기 동작을 테스트합니다.
func TestContextAwareReader_Read_Normal(t *testing.T) {
	data := []byte("Hello, World!")
	reader := bytes.NewReader(data)
	ctx := context.Background()

	cr := &contextAwareReader{
		ctx: ctx,
		r:   reader,
	}

	buf := make([]byte, len(data))
	n, err := cr.Read(buf)

	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf)
}

// TestContextAwareReader_Read_ContextCanceled 컨텍스트가 취소되었을 때 읽기가 실패하는지 테스트합니다.
func TestContextAwareReader_Read_ContextCanceled(t *testing.T) {
	reader := bytes.NewReader([]byte("Should not be read"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 즉시 취소

	cr := &contextAwareReader{
		ctx: ctx,
		r:   reader,
	}

	buf := make([]byte, 10)
	n, err := cr.Read(buf)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Equal(t, 0, n)
}

// TestContextAwareReader_Read_Timeout 컨텍스트 타임아웃 발생 시 읽기가 실패하는지 테스트합니다.
func TestContextAwareReader_Read_Timeout(t *testing.T) {
	reader := bytes.NewReader([]byte("Should not be read"))
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// 타임아웃 발생 대기 (명시적으로 Context가 종료될 때까지 대기하여 테스트 안정성 확보)
	<-ctx.Done()

	cr := &contextAwareReader{
		ctx: ctx,
		r:   reader,
	}

	buf := make([]byte, 10)
	n, err := cr.Read(buf)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.Equal(t, 0, n)
}

// TestContextAwareReader_Read_UnderlyingError 하위 Reader의 에러가 정상 전파되는지 테스트합니다.
func TestContextAwareReader_Read_UnderlyingError(t *testing.T) {
	expectedErr := errors.New("read error")
	reader := &errorReader{err: expectedErr}
	ctx := context.Background()

	cr := &contextAwareReader{
		ctx: ctx,
		r:   reader,
	}

	buf := make([]byte, 10)
	n, err := cr.Read(buf)

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 0, n)
}

// TestContextAwareReader_Read_EOF EOF가 정상적으로 감지되는지 테스트합니다.
func TestContextAwareReader_Read_EOF(t *testing.T) {
	reader := bytes.NewReader([]byte("")) // 빈 Reader
	ctx := context.Background()

	cr := &contextAwareReader{
		ctx: ctx,
		r:   reader,
	}

	buf := make([]byte, 10)
	n, err := cr.Read(buf)

	assert.Error(t, err)
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, 0, n)
}

// TestContextAwareReader_StandardCompliance io.ReadAll과의 호환성을 테스트합니다.
func TestContextAwareReader_StandardCompliance(t *testing.T) {
	data := []byte("Large data chunk for ReadAll test")
	reader := bytes.NewReader(data)
	ctx := context.Background()

	cr := &contextAwareReader{
		ctx: ctx,
		r:   reader,
	}

	result, err := io.ReadAll(cr)

	assert.NoError(t, err)
	assert.Equal(t, data, result)
}

// errorReader 항상 설정된 에러를 반환하는 테스트용 Reader
type errorReader struct {
	err error
}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, e.err
}
