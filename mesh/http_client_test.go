package mesh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// validMapJSON returns a JSON byte slice for a minimal valid ValetudoMap.
func validMapJSON() []byte {
	m := validMap(1000)
	data, _ := json.Marshal(m)
	return data
}

func TestFetchMapFromAPI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("expected Accept: application/json, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(validMapJSON())
	}))
	defer srv.Close()

	m, err := FetchMapFromAPI(srv.URL, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("FetchMapFromAPI() error: %v", err)
	}
	if m == nil {
		t.Fatal("FetchMapFromAPI() returned nil map")
		return
	}
	if m.MetaData.TotalLayerArea != 1000 {
		t.Errorf("TotalLayerArea = %d, want 1000", m.MetaData.TotalLayerArea)
	}
}

func TestFetchMapFromAPI_EmptyURL(t *testing.T) {
	_, err := FetchMapFromAPI("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "API URL is empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFetchMapFromAPI_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := FetchMapFromAPI(srv.URL, WithHTTPClient(srv.Client()), WithMaxRetries(1))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parsing JSON") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestFetchMapFromAPI_ServerError_Retries(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(validMapJSON())
	}))
	defer srv.Close()

	m, err := FetchMapFromAPI(srv.URL,
		WithHTTPClient(srv.Client()),
		WithMaxRetries(3),
		WithBaseBackoff(1*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("FetchMapFromAPI() error: %v", err)
	}
	if m == nil {
		t.Fatal("FetchMapFromAPI() returned nil map")
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestFetchMapFromAPI_AllRetriesFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := FetchMapFromAPI(srv.URL,
		WithHTTPClient(srv.Client()),
		WithMaxRetries(2),
		WithBaseBackoff(1*time.Millisecond),
	)
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if !strings.Contains(err.Error(), "all 2 attempts failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFetchMapFromAPI_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := FetchMapFromAPIWithContext(ctx, srv.URL,
		WithHTTPClient(srv.Client()),
		WithMaxRetries(3),
		WithBaseBackoff(1*time.Millisecond),
	)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestFetchMapFromAPI_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write(validMapJSON())
	}))
	defer srv.Close()

	_, err := FetchMapFromAPI(srv.URL,
		WithTimeout(10*time.Millisecond),
		WithMaxRetries(1),
	)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestFetchMapFromAPI_NoRetryOnParseError(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		_, _ = w.Write([]byte("{invalid"))
	}))
	defer srv.Close()

	_, err := FetchMapFromAPI(srv.URL,
		WithHTTPClient(srv.Client()),
		WithMaxRetries(3),
		WithBaseBackoff(1*time.Millisecond),
	)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 attempt (no retry on parse error), got %d", got)
	}
}

func TestFetchMapFromAPI_HTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(validMapJSON())
	}))
	defer srv.Close()

	m, err := FetchMapFromAPI(srv.URL, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("FetchMapFromAPI() HTTPS error: %v", err)
	}
	if m == nil {
		t.Fatal("FetchMapFromAPI() returned nil map")
	}
}

func TestFetchOptions_Defaults(t *testing.T) {
	cfg := defaultFetchConfig()
	if cfg.timeout != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", cfg.timeout)
	}
	if cfg.maxRetries != 3 {
		t.Errorf("default maxRetries = %d, want 3", cfg.maxRetries)
	}
	if cfg.baseBackoff != 500*time.Millisecond {
		t.Errorf("default baseBackoff = %v, want 500ms", cfg.baseBackoff)
	}
	if cfg.client != nil {
		t.Error("default client should be nil")
	}
}
