package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestNewWebHandlerReturns404ForMissingAssetsAndFallbackForNavigation(t *testing.T) {
	webFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>app</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('ok')")},
	}
	h := NewWebHandler(webFS, "http://127.0.0.1:57689")

	for _, test := range []struct {
		name string
		path string
		want int
	}{
		{name: "missing javascript", path: "/assets/missing.js", want: http.StatusNotFound},
		{name: "missing stylesheet", path: "/assets/missing.css", want: http.StatusNotFound},
		{name: "client route", path: "/chat/new", want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != test.want {
				t.Fatalf("GET %s status = %d, want %d", test.path, res.Code, test.want)
			}
		})
	}

	if _, err := fs.ReadFile(webFS, "index.html"); err != nil {
		t.Fatalf("test fixture missing index.html: %v", err)
	}
}
