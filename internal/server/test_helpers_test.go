package server

import (
	"io"
	"net/http"
	"net/http/httptest"
)

func newLocalhostRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Host = "127.0.0.1:57647"
	req.RemoteAddr = "127.0.0.1:12345"
	return req
}
