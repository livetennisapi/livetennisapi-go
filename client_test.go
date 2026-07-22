package livetennisapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixture loads a recorded response body from testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return body
}

// newTestClient points a client at an httptest server. Backoff is collapsed to
// zero so the retry tests assert the policy without sleeping through it.
func newTestClient(t *testing.T, handler http.Handler, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := New("twjp_test_key", append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	client.backoff = func(int, time.Duration) time.Duration { return 0 }
	return client
}

// serveFixture answers every request with one recorded body, recording the
// request that asked for it.
func serveFixture(t *testing.T, name string, got *http.Request) http.HandlerFunc {
	t.Helper()
	body := fixture(t, name)
	return func(w http.ResponseWriter, r *http.Request) {
		*got = *r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func TestEndpointRequestShape(t *testing.T) {
	const matchID = int64(918273)

	tests := []struct {
		name      string
		fixture   string
		call      func(context.Context, *Client) error
		wantPath  string
		wantQuery url.Values
	}{
		{
			name:     "health",
			fixture:  "health.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.Health(ctx); return err },
			wantPath: "/health",
		},
		{
			name:    "list matches with filters",
			fixture: "matches_live.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{
					Status:     StatusLive,
					ListParams: ListParams{Limit: 10, Offset: 20},
				})
				return err
			},
			wantPath:  "/matches",
			wantQuery: url.Values{"status": {"live"}, "limit": {"10"}, "offset": {"20"}},
		},
		{
			name:    "zero params send no query at all",
			fixture: "matches_live.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{})
				return err
			},
			wantPath:  "/matches",
			wantQuery: url.Values{},
		},
		{
			name:    "limit is clamped to MaxLimit",
			fixture: "matches_live.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{ListParams: ListParams{Limit: 5000}})
				return err
			},
			wantPath:  "/matches",
			wantQuery: url.Values{"limit": {"200"}},
		},
		{
			name:     "get match",
			fixture:  "match_detail.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatch(ctx, matchID); return err },
			wantPath: "/matches/918273",
		},
		{
			name:     "get match score",
			fixture:  "score.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatchScore(ctx, matchID); return err },
			wantPath: "/matches/918273/score",
		},
		{
			name:    "list match events",
			fixture: "events.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatchEvents(ctx, matchID, ListParams{Limit: 5})
				return err
			},
			wantPath:  "/matches/918273/events",
			wantQuery: url.Values{"limit": {"5"}},
		},
		{
			name:     "get match analysis",
			fixture:  "analysis_uncovered.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatchAnalysis(ctx, matchID); return err },
			wantPath: "/matches/918273/analysis",
		},
		{
			name:    "search players",
			fixture: "players_search.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.SearchPlayers(ctx, SearchPlayersParams{Search: "nadal", ListParams: ListParams{Limit: 2}})
				return err
			},
			wantPath:  "/players",
			wantQuery: url.Values{"search": {"nadal"}, "limit": {"2"}},
		},
		{
			name:     "get player",
			fixture:  "player.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetPlayer(ctx, 4021); return err },
			wantPath: "/players/4021",
		},
		{
			name:      "list markets",
			fixture:   "markets.json",
			call:      func(ctx context.Context, c *Client) error { _, err := c.ListMarkets(ctx, matchID); return err },
			wantPath:  "/markets",
			wantQuery: url.Values{"match_id": {"918273"}},
		},
		{
			name:    "get market prices",
			fixture: "market_prices.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetMarketPrices(ctx, matchID, ListParams{Limit: 2, Offset: 99})
				return err
			},
			wantPath: "/markets/918273/prices",
			// Offset is deliberately dropped: this endpoint takes no offset.
			wantQuery: url.Values{"limit": {"2"}},
		},
		{
			name:    "list completed matches",
			fixture: "history_matches.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListCompletedMatches(ctx, ListParams{Limit: 1})
				return err
			},
			wantPath:  "/history/matches",
			wantQuery: url.Values{"limit": {"1"}},
		},
		{
			name:     "list fixtures",
			fixture:  "fixtures.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.ListFixtures(ctx, ListParams{}); return err },
			wantPath: "/fixtures",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Request
			client := newTestClient(t, serveFixture(t, tc.fixture, &got))

			if err := tc.call(t.Context(), client); err != nil {
				t.Fatalf("call returned error: %v", err)
			}

			if got.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", got.Method)
			}
			if got.URL.Path != tc.wantPath {
				t.Errorf("path = %q, want %q", got.URL.Path, tc.wantPath)
			}

			want := tc.wantQuery
			if want == nil {
				want = url.Values{}
			}
			if gotQuery := got.URL.Query(); !equalValues(gotQuery, want) {
				t.Errorf("query = %v, want %v", gotQuery, want)
			}
		})
	}
}

func equalValues(a, b url.Values) bool {
	if len(a) != len(b) {
		return false
	}
	for key, want := range b {
		got, ok := a[key]
		if !ok || len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
	}
	return true
}

func TestAuthHeaders(t *testing.T) {
	tests := []struct {
		name          string
		apiKey        string
		opts          []Option
		wantAPIKey    string
		wantAuthoriz  string
		wantUserAgent string
	}{
		{
			name:          "X-API-Key by default",
			apiKey:        "twjp_live_123",
			wantAPIKey:    "twjp_live_123",
			wantUserAgent: "livetennisapi-go/" + Version,
		},
		{
			name:         "bearer when asked",
			apiKey:       "twjp_live_123",
			opts:         []Option{WithAuthMethod(AuthBearer)},
			wantAuthoriz: "Bearer twjp_live_123",
		},
		{
			name:   "no auth header without a key",
			apiKey: "",
		},
		{
			name:          "custom user agent",
			apiKey:        "twjp_live_123",
			opts:          []Option{WithUserAgent("my-app/2.0")},
			wantAPIKey:    "twjp_live_123",
			wantUserAgent: "my-app/2.0",
		},
		{
			name:       "key is trimmed",
			apiKey:     "  twjp_live_123\n",
			wantAPIKey: "twjp_live_123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Request
			srv := httptest.NewServer(serveFixture(t, "health.json", &got))
			t.Cleanup(srv.Close)

			client := New(tc.apiKey, append([]Option{WithBaseURL(srv.URL)}, tc.opts...)...)
			if _, err := client.Health(t.Context()); err != nil {
				t.Fatalf("Health: %v", err)
			}

			if v := got.Header.Get("X-API-Key"); v != tc.wantAPIKey {
				t.Errorf("X-API-Key = %q, want %q", v, tc.wantAPIKey)
			}
			if v := got.Header.Get("Authorization"); v != tc.wantAuthoriz {
				t.Errorf("Authorization = %q, want %q", v, tc.wantAuthoriz)
			}
			if tc.wantUserAgent != "" {
				if v := got.Header.Get("User-Agent"); v != tc.wantUserAgent {
					t.Errorf("User-Agent = %q, want %q", v, tc.wantUserAgent)
				}
			}
			if v := got.Header.Get("Accept"); v != "application/json" {
				t.Errorf("Accept = %q, want application/json", v)
			}
		})
	}
}

func TestRetryPolicy(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		maxRetries   int
		wantAttempts int32
		wantErr      error
	}{
		{name: "429 is retried", status: http.StatusTooManyRequests, maxRetries: 2, wantAttempts: 3, wantErr: ErrRateLimited},
		{name: "500 is retried", status: http.StatusInternalServerError, maxRetries: 2, wantAttempts: 3, wantErr: ErrServerError},
		{name: "503 is retried", status: http.StatusServiceUnavailable, maxRetries: 1, wantAttempts: 2, wantErr: ErrServiceUnavailable},
		{name: "400 is not retried", status: http.StatusBadRequest, maxRetries: 2, wantAttempts: 1, wantErr: ErrBadRequest},
		{name: "401 is not retried", status: http.StatusUnauthorized, maxRetries: 2, wantAttempts: 1, wantErr: ErrUnauthorized},
		{name: "403 is not retried", status: http.StatusForbidden, maxRetries: 2, wantAttempts: 1, wantErr: ErrUpgradeRequired},
		{name: "404 is not retried", status: http.StatusNotFound, maxRetries: 2, wantAttempts: 1, wantErr: ErrNotFound},
		{name: "retries can be disabled", status: http.StatusInternalServerError, maxRetries: 0, wantAttempts: 1, wantErr: ErrServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"nope"}`))
			}), WithMaxRetries(tc.maxRetries))

			_, err := client.ListMatches(t.Context(), ListMatchesParams{})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want match for %v", err, tc.wantErr)
			}
			if got := attempts.Load(); got != tc.wantAttempts {
				t.Errorf("attempts = %d, want %d", got, tc.wantAttempts)
			}
		})
	}
}

func TestRetryEventuallySucceeds(t *testing.T) {
	var attempts atomic.Int32
	body := fixture(t, "matches_live.json")

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(body)
	}))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if page.Len() != 1 {
		t.Errorf("page length = %d, want 1", page.Len())
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

// The server's Retry-After must win over the local backoff schedule, since the
// API knows its own window better than any client-side heuristic.
func TestBackoffHonoursRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	body := fixture(t, "matches_live.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	var sawRetryAfter time.Duration
	client := New("k", WithBaseURL(srv.URL))
	client.backoff = func(_ int, retryAfter time.Duration) time.Duration {
		sawRetryAfter = retryAfter
		return 0
	}

	if _, err := client.ListMatches(t.Context(), ListMatchesParams{}); err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if sawRetryAfter != 7*time.Second {
		t.Errorf("backoff saw Retry-After %v, want 7s", sawRetryAfter)
	}
}

func TestDefaultBackoffHonoursRetryAfterAndCaps(t *testing.T) {
	if got := defaultBackoff(0, 5*time.Second); got != 5*time.Second {
		t.Errorf("with Retry-After: got %v, want 5s", got)
	}
	if got := defaultBackoff(0, 10*time.Minute); got != time.Minute {
		t.Errorf("long Retry-After should cap at 1m, got %v", got)
	}
	// Without a server hint the delay grows but stays bounded and positive.
	for attempt := range 40 {
		got := defaultBackoff(attempt, 0)
		if got <= 0 || got > 10*time.Second {
			t.Fatalf("attempt %d: backoff %v out of bounds", attempt, got)
		}
	}
}

func TestRateLimitObserver(t *testing.T) {
	var seen []RateLimit
	body := fixture(t, "health.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "30")
		w.Header().Set("X-RateLimit-Remaining", "29")
		w.Header().Set("X-RateLimit-Reset", "1784734349")
		_, _ = w.Write(body)
	}), WithRateLimitObserver(func(rl RateLimit) { seen = append(seen, rl) }))

	if _, err := client.Health(t.Context()); err != nil {
		t.Fatalf("Health: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("observer called %d times, want 1", len(seen))
	}
	if got := seen[0].LimitOr(-1); got != 30 {
		t.Errorf("Limit = %d, want 30", got)
	}
	if got := seen[0].RemainingOr(-1); got != 29 {
		t.Errorf("Remaining = %d, want 29", got)
	}
	if !seen[0].Known() {
		t.Error("Known() = false, want true")
	}
}

func TestContextCancellationIsNotRetried(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		<-r.Context().Done()
	}))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := client.ListMatches(ctx, ListMatchesParams{})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("error = %v, want ErrTimeout", err)
	}
	if !errors.Is(err, ErrConnection) {
		t.Error("a timeout should also match ErrConnection")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("the context cause should stay visible to errors.Is")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1: an expired context must not be retried", got)
	}
}

func TestConnectionFailure(t *testing.T) {
	// A server that is closed immediately gives a URL that cannot connect.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	client := New("k", WithBaseURL(deadURL), WithMaxRetries(0))
	_, err := client.Health(t.Context())

	if !errors.Is(err, ErrConnection) {
		t.Fatalf("error = %v, want ErrConnection", err)
	}
	var connErr *ConnectionError
	if !errors.As(err, &connErr) {
		t.Fatalf("error = %v, want a *ConnectionError", err)
	}
	if connErr.Timeout {
		t.Error("a refused connection is not a timeout")
	}
	if !strings.Contains(connErr.URL, "/health") {
		t.Errorf("ConnectionError.URL = %q, want it to name the endpoint", connErr.URL)
	}
}

func TestMalformedJSONIsAnError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data": [ truncated`))
	}))

	if _, err := client.ListMatches(t.Context(), ListMatchesParams{}); err == nil {
		t.Fatal("expected an error decoding a malformed body")
	}
}

// Unknown fields must be ignored, never rejected: the API ships additive
// changes within v1 and an older client has to survive them.
func TestUnknownFieldsAreIgnored(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"data": [{"id": 5, "tournament": "Future Open", "brand_new_field": {"nested": true}}],
			"meta": {"count": 1, "another_new_field": 7}
		}`))
	}))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if page.Len() != 1 || page.Data[0].Tournament != "Future Open" {
		t.Fatalf("known fields lost: %+v", page.Data)
	}
}

func TestWithBaseURLTrimsTrailingSlash(t *testing.T) {
	var got http.Request
	srv := httptest.NewServer(serveFixture(t, "health.json", &got))
	t.Cleanup(srv.Close)

	client := New("k", WithBaseURL(srv.URL+"/"))
	if _, err := client.Health(t.Context()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got.URL.Path != "/health" {
		t.Errorf("path = %q, want /health (no doubled slash)", got.URL.Path)
	}
}

func TestWithHTTPClientIsUsed(t *testing.T) {
	var got http.Request
	srv := httptest.NewServer(serveFixture(t, "health.json", &got))
	t.Cleanup(srv.Close)

	custom := &http.Client{Timeout: 5 * time.Second}
	client := New("k", WithBaseURL(srv.URL), WithHTTPClient(custom))
	if client.httpClient != custom {
		t.Fatal("WithHTTPClient did not install the client")
	}
	if _, err := client.Health(t.Context()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestNilOptionsAndDefaults(t *testing.T) {
	client := New("k", nil, WithBaseURL(""), WithUserAgent("  "), WithMaxRetries(-5))

	if client.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want the default", client.baseURL)
	}
	if client.userAgent != "livetennisapi-go/"+Version {
		t.Errorf("userAgent = %q, want the default", client.userAgent)
	}
	if client.maxRetries != 0 {
		t.Errorf("maxRetries = %d, want 0 after clamping", client.maxRetries)
	}
}
