package tests

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
)

// TestRateLimitMiddleware_AllowsUnderLimit verifies that requests well below
// the burst capacity all receive 200 OK.
func TestRateLimitMiddleware_AllowsUnderLimit(t *testing.T) {
	handler := api.RateLimitMiddleware(10, 10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/recall", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rr.Code)
		}
	}
}

// TestRateLimitMiddleware_Returns429OnBurst verifies that once the token bucket
// is exhausted, the middleware returns 429 Too Many Requests.
// rps=1, burst=2: after 2 immediate requests the bucket is empty; the third
// (and beyond) must receive 429.
func TestRateLimitMiddleware_Returns429OnBurst(t *testing.T) {
	handler := api.RateLimitMiddleware(1, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/recall", nil)
	req.RemoteAddr = "10.0.0.1:9999"

	got429 := false
	for i := 0; i < 10; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("expected 429 after burst exhausted, never received one in 10 requests")
	}
}

// TestRateLimitMiddleware_ExemptPaths verifies that /healthz is never
// rate-limited regardless of how tight the limiter is configured.
func TestRateLimitMiddleware_ExemptPaths(t *testing.T) {
	// rps=0.001, burst=1 — essentially a closed limiter.
	// Any non-exempt path would 429 immediately after the first request.
	handler := api.RateLimitMiddleware(0.001, 1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust the single token with a non-exempt path.
	exhaust := httptest.NewRequest(http.MethodGet, "/v1/recall", nil)
	exhaust.RemoteAddr = "192.168.1.1:5000"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, exhaust)
	// First request may 200 or 429 depending on timing; we don't assert here.

	// Now /healthz must always pass, even with zero tokens remaining.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "192.168.1.1:5000"
		rr2 := httptest.NewRecorder()
		handler.ServeHTTP(rr2, req)
		if rr2.Code != http.StatusOK {
			t.Fatalf("iteration %d: /healthz got %d, want 200", i, rr2.Code)
		}
	}
}

// TestRateLimitMiddleware_PerIPIsolation verifies that two different IPs have
// independent token buckets — exhausting one does not affect the other.
func TestRateLimitMiddleware_PerIPIsolation(t *testing.T) {
	handler := api.RateLimitMiddleware(1, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust IP A's bucket.
	reqA := httptest.NewRequest(http.MethodGet, "/v1/recall", nil)
	reqA.RemoteAddr = "10.1.1.1:1111"
	for i := 0; i < 10; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, reqA)
	}

	// IP B should still have a full bucket.
	reqB := httptest.NewRequest(http.MethodGet, "/v1/recall", nil)
	reqB.RemoteAddr = "10.2.2.2:2222"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, reqB)
	if rr.Code != http.StatusOK {
		t.Fatalf("IP B should not be rate-limited by IP A exhaustion; got %d", rr.Code)
	}
}

// TestRateLimitMiddleware_429ResponseBody verifies the JSON error body format.
func TestRateLimitMiddleware_429ResponseBody(t *testing.T) {
	// rps=0, burst=0 is not valid for the token bucket; use rps=1, burst=1
	// and fire 3 immediate requests so the second/third will be 429.
	handler := api.RateLimitMiddleware(1, 1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/remember", nil)
	req.RemoteAddr = "172.16.0.1:8080"

	var body429 string
	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			body429 = rr.Body.String()
			break
		}
	}
	if body429 == "" {
		t.Fatal("never received 429; cannot check body")
	}
	const want = `{"error":"rate limit exceeded"}`
	// http.Error appends a newline; trim it.
	got := body429
	if len(got) > 0 && got[len(got)-1] == '\n' {
		got = got[:len(got)-1]
	}
	if got != want {
		t.Fatalf("429 body: got %q, want %q", got, want)
	}
}

// TestServeCmd_FlagsRegistered builds the binary and checks that the three new
// flags introduced in this track appear in `serve --help` output.
// It is skipped in -short mode (requires a full go build).
func TestServeCmd_FlagsRegistered(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build smoke test in -short mode")
	}

	bin := filepath.Join(t.TempDir(), "openclaw-cortex-flagtest")
	buildCmd := exec.Command("go", "build", "-o", bin, "github.com/ajitpratap0/openclaw-cortex/cmd/openclaw-cortex")
	buildCmd.Dir = ".." // repo root relative to tests/
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	// cobra exits 0 for --help; capture output regardless of exit code.
	helpOut, _ := exec.Command(bin, "serve", "--help").CombinedOutput()
	helpStr := string(helpOut)

	for _, flag := range []string{"--unsafe-no-auth", "--tls-cert", "--tls-key"} {
		if !strings.Contains(helpStr, flag) {
			t.Errorf("flag %q not found in `serve --help` output:\n%s", flag, helpStr)
		}
	}
}
