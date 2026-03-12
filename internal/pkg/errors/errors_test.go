package errors

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
//  Setup & Helpers
// =============================================================================

var errSentinel = errors.New("std sentinel error")

// Helper struct for As test
type MyCustomError struct{ Msg string }

func (e *MyCustomError) Error() string { return e.Msg }

// =============================================================================
//  Creation Tests (New, Newf)
// =============================================================================

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		errType ErrorType
		msg     string
	}{
		{"Normal", InvalidInput, "invalid parameter"},
		{"Empty Message", Internal, ""},
		{"Special Chars", System, "disk error: /dev/null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(tt.errType, tt.msg)
			require.NotNil(t, err)

			// Interface checks
			assert.Implements(t, (*error)(nil), err)
			assert.Implements(t, (*fmt.Formatter)(nil), err)

			// Value checks
			appErr, ok := err.(*AppError)
			require.True(t, ok, "Should be castable to *AppError")
			assert.Equal(t, tt.errType, appErr.Type())
			assert.Equal(t, tt.msg, appErr.Message())
			assert.Nil(t, appErr.Unwrap(), "New() should produce an error with no cause")
			assert.NotEmpty(t, appErr.Stack(), "Stack should be captured")
		})
	}
}

func TestNewf(t *testing.T) {
	t.Parallel()

	err := Newf(NotFound, "user %d not found", 42)
	require.NotNil(t, err)

	appErr, ok := err.(*AppError)
	require.True(t, ok)
	assert.Equal(t, NotFound, appErr.Type())
	assert.Equal(t, "user 42 not found", appErr.Message())
	assert.NotEmpty(t, appErr.Stack())
}

// =============================================================================
//  Wrapping Tests (Wrap, Wrapf)
// =============================================================================

func TestWrap(t *testing.T) {
	t.Parallel()

	t.Run("Wrap Nil", func(t *testing.T) {
		assert.Nil(t, Wrap(nil, Internal, "msg"))
	})

	t.Run("Wrap Standard Error", func(t *testing.T) {
		err := Wrap(errSentinel, System, "wrapper")
		require.NotNil(t, err)

		appErr, ok := err.(*AppError)
		require.True(t, ok)
		assert.Equal(t, System, appErr.Type())
		assert.Equal(t, "wrapper", appErr.Message())
		assert.Equal(t, errSentinel, appErr.Unwrap())
		assert.NotEmpty(t, appErr.Stack())
	})

	t.Run("Double Wrap (Chain)", func(t *testing.T) {
		root := New(InvalidInput, "root")
		mid := Wrap(root, Conflict, "mid")
		top := Wrap(mid, Internal, "top")

		// Verify chain structure
		assert.Equal(t, Internal, top.(*AppError).Type())
		assert.Equal(t, mid, top.(*AppError).Unwrap())
		assert.Equal(t, root, mid.(*AppError).Unwrap())
	})
}

func TestWrapf(t *testing.T) {
	t.Parallel()

	t.Run("Wrapf Nil", func(t *testing.T) {
		assert.Nil(t, Wrapf(nil, Internal, "msg %s", "val"))
	})

	t.Run("Wrapf Formatting", func(t *testing.T) {
		err := Wrapf(errSentinel, Unauthorized, "access %s", "denied")
		require.NotNil(t, err)
		assert.Equal(t, "access denied", err.(*AppError).Message())
		assert.Equal(t, errSentinel, errors.Unwrap(err))
	})
}

// =============================================================================
//  Inspection Tests (Is, As, RootCause, UnderlyingType)
// =============================================================================

func TestIs(t *testing.T) {
	t.Parallel()

	errNotFound := New(NotFound, "missing")
	errWrapped := Wrap(errNotFound, Internal, "failed")
	errStdWrapped := Wrap(errSentinel, System, "sys")

	// 1. Custom Is(err, ErrorType)
	t.Run("Custom Is Logic", func(t *testing.T) {
		assert.True(t, Is(errNotFound, NotFound))
		assert.False(t, Is(errNotFound, Internal))

		assert.True(t, Is(errWrapped, Internal), "Should match wrapper type")
		assert.True(t, Is(errWrapped, NotFound), "Should match cause type (traversal)")
		assert.False(t, Is(errWrapped, System))

		assert.False(t, Is(nil, Internal))
	})

	// 2. Standard comparisons
	t.Run("Standard errors.Is Interop", func(t *testing.T) {
		// AppError doesn't implement Is(target error) bool custom method anymore,
		// so it relies on basic equality or Unwrap.
		assert.True(t, errors.Is(errWrapped, errNotFound))
		assert.True(t, errors.Is(errStdWrapped, errSentinel))
	})
}

func TestAs(t *testing.T) {
	t.Parallel()

	// 1. Custom As(err, target) - just a wrapper around errors.As
	t.Run("Extract AppError", func(t *testing.T) {
		err := Wrap(New(Timeout, "slow"), System, "down")
		var appErr *AppError
		if assert.True(t, As(err, &appErr)) {
			// Should find the outer error first
			assert.Equal(t, System, appErr.Type())
		}
	})

	t.Run("Extract Standard Error", func(t *testing.T) {
		myErr := &MyCustomError{Msg: "custom"}
		err := Wrap(myErr, Internal, "wrapped")

		var target *MyCustomError
		if assert.True(t, As(err, &target)) {
			assert.Equal(t, "custom", target.Msg)
		}
	})
}

func TestRootCause(t *testing.T) {
	t.Parallel()

	assert.Nil(t, RootCause(nil))
	assert.Equal(t, errSentinel, RootCause(errSentinel))

	err := New(NotFound, "root")
	wrapped := Wrap(Wrap(err, Internal, "m"), System, "t")
	assert.Equal(t, err, RootCause(wrapped))

	// External error root
	extRoot := Wrap(errSentinel, Internal, "w")
	assert.Equal(t, errSentinel, RootCause(extRoot))
}

func TestUnderlyingType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected ErrorType
	}{
		{"Nil", nil, Unknown},
		{"Standard Error", errSentinel, Unknown},
		{"Simple AppError", New(NotFound, "nf"), NotFound},
		{"Wrapped AppError", Wrap(New(Conflict, "c"), Internal, "i"), Conflict},
		{"Wrapped StdError", Wrap(errSentinel, Timeout, "t"), Timeout}, // The AppError wrapper is the deepest AppError
		{"Mixed Chain", Wrap(Wrap(errSentinel, InvalidInput, "iv"), System, "s"), InvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, UnderlyingType(tt.err))
		})
	}
}

// =============================================================================
//  Formatting Tests (Format)
// =============================================================================

func TestAppError_Format(t *testing.T) {
	t.Parallel()

	root := New(InvalidInput, "bad value")
	wrapped := Wrap(root, Internal, "process failed")

	t.Run("String (%s)", func(t *testing.T) {
		assert.Equal(t, "[Internal] process failed: [InvalidInput] bad value", fmt.Sprintf("%s", wrapped))
	})

	t.Run("Value (%v)", func(t *testing.T) {
		assert.Equal(t, "[Internal] process failed: [InvalidInput] bad value", fmt.Sprintf("%v", wrapped))
	})

	t.Run("Quote (%q)", func(t *testing.T) {
		expected := `"[Internal] process failed: [InvalidInput] bad value"`
		assert.Equal(t, expected, fmt.Sprintf("%q", wrapped))
	})

	t.Run("Detailed (%+v)", func(t *testing.T) {
		out := fmt.Sprintf("%+v", wrapped)
		// Should contain messages
		assert.Contains(t, out, "[Internal] process failed")
		assert.Contains(t, out, "[InvalidInput] bad value")
		// Should contain stack trace label
		assert.Contains(t, out, "Stack trace:")
	})
}

// =============================================================================
//  Concurrency Safety
// =============================================================================

func TestConcurrency(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	const routines = 50

	// Shared error to read concurrently
	sharedErr := New(System, "shared")

	for i := 0; i < routines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 1. Concurrent Wrap
			wrapped := Wrapf(sharedErr, Internal, "wrap %d", id)

			// 2. Concurrent Read
			_ = wrapped.Error()
			_ = Is(wrapped, System)
			_ = RootCause(wrapped)

			// 3. Concurrent Format
			_ = fmt.Sprintf("%+v", wrapped)
		}(i)
	}
	wg.Wait()
}

// =============================================================================
//  Example Functions (Documentation)
// =============================================================================

func ExampleNew() {
	err := New(NotFound, "user 123 not found")
	fmt.Println(err)
	// Output: [NotFound] user 123 not found
}

func ExampleWrap() {
	// Original error from lower layer
	cause := New(ExecutionFailed, "db connection lost")

	// Wrap with context in upper layer
	err := Wrap(cause, System, "health check failed")

	fmt.Printf("%s", err)
	// Output: [System] health check failed: [ExecutionFailed] db connection lost
}

func ExampleIs() {
	err := New(Timeout, "request timed out")
	err = Wrap(err, Unavailable, "retry failed")

	if Is(err, Timeout) {
		fmt.Println("Caught timeout error")
	}
	// Output: Caught timeout error
}

func ExampleUnderlyingType() {
	// Scenario: Standard library error wrapped in AppError
	stdErr := io.EOF
	err := Wrap(stdErr, InvalidInput, "unexpected end of stream")

	// Even though the root cause is io.EOF (unknown type),
	// UnderlyingType returns the deepest AppError's type.
	fmt.Println(UnderlyingType(err))
	// Output: InvalidInput
}
