package llm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAPIError_Error_FormatsAllFields(t *testing.T) {
	e := &APIError{
		StatusCode: 401,
		Type:       "authentication_error",
		Code:       "invalid_api_key",
		Message:    "API key is wrong",
	}
	got := e.Error()
	for _, want := range []string{"401", "authentication_error", "invalid_api_key", "API key is wrong"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}

func TestAPIError_Error_MinimalFields(t *testing.T) {
	e := &APIError{StatusCode: 500}
	got := e.Error()
	if !strings.Contains(got, "500") {
		t.Errorf("Error() = %q, want contains 500", got)
	}
}

func TestAPIError_Error_Nil(t *testing.T) {
	var e *APIError
	if got := e.Error(); got != "<nil>" {
		t.Errorf("nil.Error() = %q", got)
	}
}

func TestAPIError_IsRetryable_ByStatus(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{400, false},
		{401, false},
		{402, false},
		{403, false},
		{404, false},
		{422, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{600, false},
	}
	for _, c := range cases {
		e := &APIError{StatusCode: c.status}
		if got := e.IsRetryable(); got != c.want {
			t.Errorf("status %d: IsRetryable = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestAPIError_IsRetryable_Nil(t *testing.T) {
	var e *APIError
	if e.IsRetryable() {
		t.Error("nil APIError should not be retryable")
	}
}

func TestIsRetryableErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"api 401", &APIError{StatusCode: http.StatusUnauthorized}, false},
		{"api 429", &APIError{StatusCode: http.StatusTooManyRequests}, true},
		{"api 500", &APIError{StatusCode: http.StatusInternalServerError}, true},
		{"network err (default retry)", errors.New("dial tcp: timeout"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsRetryableErr(c.err); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsRetryableErr_WrappedAPIError(t *testing.T) {
	inner := &APIError{StatusCode: 401}
	wrapped := fmt.Errorf("wrapped: %w", inner)
	if IsRetryableErr(wrapped) {
		t.Error("wrapped 401 should not be retryable")
	}
}

func TestEventType_String(t *testing.T) {
	cases := []struct {
		t    EventType
		want string
	}{
		{EventDelta, "delta"},
		{EventDone, "done"},
		{EventError, "error"},
		{EventType(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.t.String(); got != c.want {
			t.Errorf("EventType(%d).String() = %q, want %q", c.t, got, c.want)
		}
	}
}
