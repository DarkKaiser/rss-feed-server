package errors

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// definedTypes is the source of truth for all defined ErrorType constants in tests.
var definedTypes = []struct {
	errType ErrorType
	str     string
}{
	{Unknown, "Unknown"},
	{Internal, "Internal"},
	{System, "System"},
	{Unauthorized, "Unauthorized"},
	{Forbidden, "Forbidden"},
	{InvalidInput, "InvalidInput"},
	{Conflict, "Conflict"},
	{NotFound, "NotFound"},
	{ExecutionFailed, "ExecutionFailed"},
	{ParsingFailed, "ParsingFailed"},
	{Timeout, "Timeout"},
	{Unavailable, "Unavailable"},
}

// TestErrorType_String verifies strict compliance of the String() method.
// It checks that all defined types return their exact string representation,
// and undefined values fall back to the "ErrorType(N)" format.
func TestErrorType_String(t *testing.T) {
	t.Parallel()

	t.Run("Defined Types", func(t *testing.T) {
		for _, tt := range definedTypes {
			t.Run(tt.str, func(t *testing.T) {
				assert.Equal(t, tt.str, tt.errType.String())
			})
		}
	})

	t.Run("Undefined Values", func(t *testing.T) {
		tests := []struct {
			name    string
			input   ErrorType
			matcher func(string) bool
		}{
			{
				name:  "Negative Value",
				input: -1,
				matcher: func(s string) bool {
					return s == "ErrorType(-1)"
				},
			},
			{
				name:  "Out of Range (Positive)",
				input: 999,
				matcher: func(s string) bool {
					return s == "ErrorType(999)"
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.True(t, tt.matcher(tt.input.String()), "String() output %q did not match expectation", tt.input.String())
			})
		}
	})
}

// TestErrorType_Invariants enforces the structural integrity of the ErrorType enum.
func TestErrorType_Invariants(t *testing.T) {
	t.Parallel()

	t.Run("Zero Value is Unknown", func(t *testing.T) {
		// Go specs guarantee that the zero value of an int type is 0.
		// We explicitly enforce that our 0 value corresponds to Unknown.
		// This is critical for ensuring that uninitialized AppErrors result in Unknown type.
		var zeroType ErrorType
		assert.Equal(t, Unknown, zeroType, "The zero value of ErrorType MUST be Unknown")
		assert.Equal(t, 0, int(Unknown), "Unknown constant MUST be 0")
	})

	t.Run("Uniqueness", func(t *testing.T) {
		// Ensure that no two defined constants share the same underlying integer value.
		seen := make(map[ErrorType]string)
		for _, entry := range definedTypes {
			if existingName, found := seen[entry.errType]; found {
				t.Fatalf("Collision detected: %s and %s share value %d", existingName, entry.str, entry.errType)
			}
			seen[entry.errType] = entry.str
		}
	})

	t.Run("Contiguity", func(t *testing.T) {
		// Optional: Verify that types are contiguous (0, 1, 2...).
		// This is often desirable for 'iota' enums to prevent gaps, though not strictly required.
		// However, the generated 'stringer' code is most efficient when values are contiguous.
		for i, entry := range definedTypes {
			if int(entry.errType) != i {
				t.Logf("Notice: ErrorType enum is not contiguous at %s. Expected %d, got %d. (This may be intentional)",
					entry.str, i, entry.errType)
			}
		}
	})
}

// TestErrorType_Exhaustiveness ensures that all defined ErrorType constants
// are present in the 'definedTypes' slice used for testing.
// It iterates from 0 upwards until it finds the first undefined ErrorType (one that returns "ErrorType(N)").
// If it encounters a defined type (valid string output) that is NOT in 'definedTypes', the test fails.
func TestErrorType_Exhaustiveness(t *testing.T) {
	t.Parallel()

	// Create a lookup map for O(1) checking
	known := make(map[ErrorType]bool)
	for _, dt := range definedTypes {
		known[dt.errType] = true
	}

	// Dynamic discovery: iterate from 0
	// We assume types are somewhat contiguous or at least start from 0 (iota).
	// We'll stop after finding a sequence of undefined values to be safe,
	// or we can rely on String() behavior.
	// Since generated String() returns "ErrorType(d)" for unknown values, we check for that.

	const maxScan = 255 // Arbitrary reasonable limit for enum scan

	for i := 0; i < maxScan; i++ {
		et := ErrorType(i)
		str := et.String()

		// Check if this appears to be a generated fallback string "ErrorType(N)"
		// Ideally we would check `str == fmt.Sprintf("ErrorType(%d)", i)` but exact format matching is robust enough.
		isFallback := str == fmt.Sprintf("ErrorType(%d)", i)

		if !isFallback {
			// This is a DEFINED constant (it has a string name).
			// It MUST be in our 'known' map.
			if !known[et] {
				t.Errorf("Critical: ErrorType constant '%s' (value %d) is defined in types.go but MISSING in types_test.go/definedTypes", str, i)
			}
		}
	}
}

// TestErrorType_Printability confirms that ErrorType implements fmt.Stringer correctly
// and works with standard formatting verbs.
func TestErrorType_Printability(t *testing.T) {
	t.Parallel()

	et := NotFound
	assert.Equal(t, "NotFound", fmt.Sprintf("%s", et))
	assert.Equal(t, "NotFound", fmt.Sprintf("%v", et))
	assert.Equal(t, "\"NotFound\"", fmt.Sprintf("%q", et))
}
