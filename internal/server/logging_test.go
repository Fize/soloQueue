package server

import (
	"net/http/httptest"
	"testing"
)

func TestRequestPathForLogOmitsQueryCredentials(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?token=one-time-secret", nil)
	if got := requestPathForLog(req); got != "/ws" {
		t.Fatalf("logged path = %q, want /ws without query token", got)
	}
}
