package errors

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Stack Trace Tests
// =============================================================================

// TestCaptureStack_Internal validates the internal captureStack function.
// It verifies:
// 1. Correct file, line, and function name capture.
// 2. Caller skip logic.
// 3. Max frame limit.
func TestCaptureStack_Internal(t *testing.T) {
	t.Parallel()

	// Helper to pin-point expected line numbers
	getLine := func() int {
		_, _, line, _ := runtime.Caller(1)
		return line
	}

	t.Run("Basic Capture", func(t *testing.T) {
		// We use skip=2 to exclude:
		// 0: runtime.Callers
		// 1: captureStack
		// The 0-th frame in the result will be this function (Basic Capture closure)
		expectedLine := getLine() + 1
		frames := captureStack(2)

		require.NotEmpty(t, frames, "Should capture at least one frame")

		// The first frame should be this function
		head := frames[0]
		assert.Equal(t, "stack_test.go", head.File, "File name should be base name")
		assert.Equal(t, expectedLine, head.Line, "Line number should match call site")
		assert.Contains(t, head.Function, "TestCaptureStack_Internal", "Function name should contain test name")
	})

	t.Run("Skip Caller", func(t *testing.T) {
		// captureStack(3) should skip:
		// 0: runtime.Callers
		// 1: captureStack
		// 2: This closure (Skip Caller)
		// So the first frame should be the test runner (TestCaptureStack_Internal)
		frames := captureStack(3)
		require.NotEmpty(t, frames)

		// The first frame should NOT be this closure
		// Note: The function name for the closure will typically end with .func2
		assert.NotContains(t, frames[0].Function, "TestCaptureStack_Internal.func2")
	})

	t.Run("Deep Recursion Limit", func(t *testing.T) {
		var recurse func(n int) []StackFrame
		recurse = func(n int) []StackFrame {
			if n <= 0 {
				return captureStack(0)
			}
			return recurse(n - 1)
		}

		// Recursion depth 10 is > maxFrames (5)
		frames := recurse(10)
		assert.LessOrEqual(t, len(frames), 5, "Stack capture must respect maxFrames limit")
	})

	t.Run("Path Sanitization", func(t *testing.T) {
		// Mock a stack frame with a full path (simulated by checking real capture consistency)
		// captureStack internally uses filepath.Base
		// Use skip=2 to get this file
		frames := captureStack(2)
		require.NotEmpty(t, frames)
		assert.False(t, strings.Contains(frames[0].File, string(filepath.Separator)),
			"Captured filename '%s' should not contain path separators", frames[0].File)
	})
}

// TestStackTrace_PublicAPI verifies the Stack() method on AppError.
func TestStackTrace_PublicAPI(t *testing.T) {
	t.Parallel()

	t.Run("New Error Stack", func(t *testing.T) {
		err := New(InvalidInput, "test")
		appErr, ok := err.(*AppError)
		require.True(t, ok)

		stack := appErr.Stack()
		require.NotEmpty(t, stack)
		assert.Equal(t, "stack_test.go", stack[0].File)
		assert.Contains(t, stack[0].Function, "TestStackTrace_PublicAPI")
	})

	t.Run("Wrapped Error Stack", func(t *testing.T) {
		base := New(System, "base")
		wrapped := Wrap(base, Internal, "wrapped")

		appErr, ok := wrapped.(*AppError)
		require.True(t, ok)

		// Verification: Wrap should capture its own stack trace, distinct from base
		stack := appErr.Stack()
		require.NotEmpty(t, stack)
		assert.Equal(t, "stack_test.go", stack[0].File)
	})

	t.Run("Nil Stack Safety", func(t *testing.T) {
		// Manually create AppError with nil stack to test robustness
		err := &AppError{errType: Internal, message: "empty", stack: nil}
		assert.Nil(t, err.Stack())
	})
}

// TestStackTrace_Formatting verifies that stack traces are printed correctly with %+v.
func TestStackTrace_Formatting(t *testing.T) {
	t.Parallel()

	t.Run("Format Structure", func(t *testing.T) {
		err := New(ExecutionFailed, "fail")
		out := fmt.Sprintf("%+v", err)

		// Must contain standard error parts
		assert.Contains(t, out, "[ExecutionFailed] fail")
		// Must contain stack trace header
		assert.Contains(t, out, "Stack trace:")
		// Must contain file and function
		assert.Contains(t, out, "stack_test.go")
		assert.Contains(t, out, "TestStackTrace_Formatting")
	})

	t.Run("Chain Deduplication", func(t *testing.T) {
		// Detailed verification of the "print stack only at root/boundary" logic
		root := New(NotFound, "root")            // AppError (Leaf)
		mid := Wrap(root, Internal, "mid")       // AppError wrapping AppError
		top := Wrap(mid, ExecutionFailed, "top") // AppError wrapping AppError

		out := fmt.Sprintf("%+v", top)

		// 1. All messages should be present
		assert.Contains(t, out, "[ExecutionFailed] top")
		assert.Contains(t, out, "[Internal] mid")
		assert.Contains(t, out, "[NotFound] root")

		// 2. "Stack trace:" should appear EXACTLY ONCE
		// Logic:
		// - top: cause is 'mid' (AppError) -> Stack SKIP
		// - mid: cause is 'root' (AppError) -> Stack SKIP
		// - root: cause is nil -> Stack PRINT
		count := strings.Count(out, "Stack trace:")
		assert.Equal(t, 1, count, "Stack trace should be deduplicated and printed only once for the root cause")
	})

	t.Run("External Error Boundary", func(t *testing.T) {
		// mixed: AppError -> External Error
		// Logic: AppError wrapping external error -> Treat as boundary -> Stack PRINT
		stdErr := fmt.Errorf("std error")
		wrapped := Wrap(stdErr, System, "wrapper")

		out := fmt.Sprintf("%+v", wrapped)

		assert.Contains(t, out, "[System] wrapper")
		assert.Contains(t, out, "std error")
		assert.Contains(t, out, "Stack trace:", "Should print stack trace at external error boundary")
	})
}

// TestCaptureStack_EdgeCases covers extreme inputs.
func TestCaptureStack_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("Extremely Large Skip", func(t *testing.T) {
		// Should return empty slice, not panic
		frames := captureStack(9999)
		assert.Empty(t, frames)
	})

	t.Run("Zero Skip", func(t *testing.T) {
		// Should capture runtime.Callers itself (or very close)
		frames := captureStack(0)
		require.NotEmpty(t, frames)
	})
}

// TestCaptureStack_Concurrency verifies thread safety.
func TestCaptureStack_Concurrency(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	const routines = 50

	wg.Add(routines)
	for i := 0; i < routines; i++ {
		go func() {
			defer wg.Done()
			// Just verify it doesn't panic and returns valid data
			frames := captureStack(0)
			assert.NotEmpty(t, frames)
			if len(frames) > 0 {
				assert.NotEmpty(t, frames[0].File)
			}
		}()
	}
	wg.Wait()
}
