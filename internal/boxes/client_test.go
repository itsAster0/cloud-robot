package boxes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A successful validation response must not be decoded into a nil output.
func TestValidateMainSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/boxes/box-1/validate-main" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	defer server.Close()
	if err := NewClient(server.URL, "token").ValidateMain(context.Background(), "box-1", "return 1"); err != nil {
		t.Fatalf("valid source reported as error: %v", err)
	}
}

func TestValidateMainReportsSyntaxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("main.lua:1: unexpected symbol\n"))
	}))
	defer server.Close()
	if err := NewClient(server.URL, "token").ValidateMain(context.Background(), "box-1", "x ="); err == nil {
		t.Fatal("syntax error not reported")
	}
}
