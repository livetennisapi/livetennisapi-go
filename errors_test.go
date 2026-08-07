package livetennisapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestStatusMapsToSentinel pins the mapping every caller branches on. The
// bodies are the ones the API actually sends: a bare {"error": "<code>"}.
func TestStatusMapsToSentinel(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantCode   string
		matches    []error
		notMatches []error
	}{
		{
			// Recorded verbatim from an unauthenticated call to /matches.
			name:       "401 unauthorized",
			status:     http.StatusUnauthorized,
			body:       `{"error":"unauthorized"}`,
			wantCode:   "unauthorized",
			matches:    []error{ErrAPI, ErrUnauthorized},
			notMatches: []error{ErrUpgradeRequired, ErrNotFound, ErrServerError},
		},
		{
			// Recorded verbatim from /matches/21635/analysis with a FREE key.
			name:     "403 upgrade required",
			status:   http.StatusForbidden,
			body:     `{"error":"upgrade_required"}`,
			wantCode: "upgrade_required",
			matches:  []error{ErrAPI, ErrUpgradeRequired},
			// The whole point of the distinction: a tier wall proves the key
			// works, so it must never look like an auth failure.
			notMatches: []error{ErrUnauthorized, ErrNotFound},
		},
		{
			// Recorded verbatim from /matches?tour=bogus.
			name:       "400 bad tour",
			status:     http.StatusBadRequest,
			body:       `{"allowed":["atp","challenger","itf","juniors","wta"],"error":"bad_tour"}`,
			wantCode:   "bad_tour",
			matches:    []error{ErrBadRequest},
			notMatches: []error{ErrUnauthorized, ErrServerError},
		},
		{
			name:       "404 not found",
			status:     http.StatusNotFound,
			body:       `{"error":"not_found"}`,
			wantCode:   "not_found",
			matches:    []error{ErrNotFound},
			notMatches: []error{ErrBadRequest},
		},
		{
			name:       "429 rate limited",
			status:     http.StatusTooManyRequests,
			body:       `{"error":"rate_limited"}`,
			wantCode:   "rate_limited",
			matches:    []error{ErrRateLimited},
			notMatches: []error{ErrServerError},
		},
		{
			name:       "500 server error",
			status:     http.StatusInternalServerError,
			body:       `{"error":"internal"}`,
			wantCode:   "internal",
			matches:    []error{ErrServerError},
			notMatches: []error{ErrServiceUnavailable, ErrBadRequest},
		},
		{
			// 503 satisfies both, matching the Python and JS clients where
			// ServiceUnavailable subclasses ServerError.
			name:       "503 is both unavailable and a server error",
			status:     http.StatusServiceUnavailable,
			body:       `{"error":"service_unavailable"}`,
			wantCode:   "service_unavailable",
			matches:    []error{ErrServiceUnavailable, ErrServerError},
			notMatches: []error{ErrBadRequest},
		},
		{
			name:       "unmapped status still errors",
			status:     http.StatusTeapot,
			body:       `{"error":"teapot"}`,
			wantCode:   "teapot",
			matches:    []error{ErrAPI},
			notMatches: []error{ErrBadRequest, ErrServerError},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}), WithMaxRetries(0))

			_, err := client.ListMatches(t.Context(), ListMatchesParams{})
			if err == nil {
				t.Fatal("expected an error")
			}

			for _, want := range tc.matches {
				if !errors.Is(err, want) {
					t.Errorf("errors.Is(err, %v) = false, want true", want)
				}
			}
			for _, unwanted := range tc.notMatches {
				if errors.Is(err, unwanted) {
					t.Errorf("errors.Is(err, %v) = true, want false", unwanted)
				}
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want an *APIError", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tc.wantCode)
			}
			if string(apiErr.Body) != tc.body {
				t.Errorf("Body = %q, want the raw payload %q", apiErr.Body, tc.body)
			}
		})
	}
}

// A 403 on a tier-gated endpoint must map to ErrUpgradeRequired and name the
// tier, because the API's own body says only "upgrade_required".
func TestUpgradeRequiredNamesTheTier(t *testing.T) {
	tests := []struct {
		name     string
		call     func(context.Context, *Client) error
		wantTier Tier
	}{
		{
			name:     "analysis needs ULTRA",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatchAnalysis(ctx, 1); return err },
			wantTier: TierUltra,
		},
		{
			name: "events need PRO",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatchEvents(ctx, 1, ListParams{})
				return err
			},
			wantTier: TierPro,
		},
		{
			name:     "markets need PRO",
			call:     func(ctx context.Context, c *Client) error { _, err := c.ListMarkets(ctx, 1); return err },
			wantTier: TierPro,
		},
		{
			name: "market prices need PRO",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetMarketPrices(ctx, 1, ListParams{})
				return err
			},
			wantTier: TierPro,
		},
		{
			name: "history needs BASIC",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListCompletedMatches(ctx, ListParams{})
				return err
			},
			wantTier: TierBasic,
		},
		{
			name: "the tape needs BASIC",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetMatchTape(ctx, 1, TapeParams{})
				return err
			},
			wantTier: TierBasic,
		},
		{
			name: "h2h needs BASIC",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetHeadToHead(ctx, "nadal", "djokovic")
				return err
			},
			wantTier: TierBasic,
		},
		{
			name: "the archive needs BASIC",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListArchiveMatches(ctx, ArchiveMatchesParams{})
				return err
			},
			wantTier: TierBasic,
		},
		{
			name: "history packages need PRO",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListHistoryPackages(ctx, HistoryPackagesParams{})
				return err
			},
			wantTier: TierPro,
		},
		{
			// The two rankings modes are gated apart: the listing is PRO...
			name: "rankings listing needs PRO",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListRankings(ctx, RankingsParams{System: []RankingSystem{RankingATP}})
				return err
			},
			wantTier: TierPro,
		},
		{
			// ...while per-player as-of records are ULTRA.
			name: "per-player rankings need ULTRA",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListRankings(ctx, RankingsParams{Player: []int64{2317}})
				return err
			},
			wantTier: TierUltra,
		},
		{
			name:     "statistics need ULTRA",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatchStatistics(ctx, 1); return err },
			wantTier: TierUltra,
		},
		{
			name: "rally needs ULTRA",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListRallyMatches(ctx, RallyMatchesParams{})
				return err
			},
			wantTier: TierUltra,
		},
		{
			// Reached through a /history path, but the ULTRA rally marker must
			// win over the broad BASIC history one.
			name: "rally by our match id needs ULTRA",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetMatchRally(ctx, 1, ListParams{})
				return err
			},
			wantTier: TierUltra,
		},
		{
			name: "charting needs ULTRA",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetChartingPlayer(ctx, ChartingPlayerParams{Name: "invented"})
				return err
			},
			wantTier: TierUltra,
		},
		{
			name:     "the push-feed token needs ULTRA",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetWSToken(ctx); return err },
			wantTier: TierUltra,
		},
		{
			// A FREE-floor endpoint has no upgrade to suggest, so the tier is
			// left empty rather than guessed at.
			name: "a free endpoint names no tier",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{})
				return err
			},
			wantTier: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"upgrade_required"}`))
			}), WithMaxRetries(0))

			err := tc.call(t.Context(), client)

			if !errors.Is(err, ErrUpgradeRequired) {
				t.Fatalf("error = %v, want ErrUpgradeRequired", err)
			}
			if errors.Is(err, ErrUnauthorized) {
				t.Error("a tier wall must not match ErrUnauthorized: the key is valid")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want an *APIError", err)
			}
			if apiErr.RequiredTier != tc.wantTier {
				t.Errorf("RequiredTier = %q, want %q", apiErr.RequiredTier, tc.wantTier)
			}
			if tc.wantTier != "" && !strings.Contains(err.Error(), string(tc.wantTier)) {
				t.Errorf("message %q should name the %s tier", err.Error(), tc.wantTier)
			}
		})
	}
}

// Driven by the recorded error bodies rather than hand-typed copies, so the
// mapping is asserted against exactly what the API sent.
func TestRecordedErrorBodies(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		status   int
		wantCode string
		wantErr  error
		wantTier Tier
		call     func(context.Context, *Client) error
	}{
		{
			name:     "unauthenticated request",
			fixture:  "error_401.json",
			status:   http.StatusUnauthorized,
			wantCode: "unauthorized",
			wantErr:  ErrUnauthorized,
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{})
				return err
			},
		},
		{
			name:     "ULTRA endpoint on a FREE key",
			fixture:  "error_403_upgrade_required.json",
			status:   http.StatusForbidden,
			wantCode: "upgrade_required",
			wantErr:  ErrUpgradeRequired,
			wantTier: TierUltra,
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatchAnalysis(ctx, 21635); return err },
		},
		{
			name:     "BASIC endpoint on a FREE key",
			fixture:  "error_403_history.json",
			status:   http.StatusForbidden,
			wantCode: "upgrade_required",
			wantErr:  ErrUpgradeRequired,
			wantTier: TierBasic,
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListCompletedMatches(ctx, ListParams{})
				return err
			},
		},
		{
			name:     "unknown tour",
			fixture:  "error_bad_tour.json",
			status:   http.StatusBadRequest,
			wantCode: "bad_tour",
			wantErr:  ErrBadRequest,
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{Tour: Tour("bogus")})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := fixture(t, tc.fixture)
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write(body)
			}), WithMaxRetries(0))

			err := tc.call(t.Context(), client)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want match for %v", err, tc.wantErr)
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want an *APIError", err)
			}
			if apiErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tc.wantCode)
			}
			if apiErr.RequiredTier != tc.wantTier {
				t.Errorf("RequiredTier = %q, want %q", apiErr.RequiredTier, tc.wantTier)
			}
		})
	}
}

// A rejected tour names the values it would have accepted, and the client
// surfaces that list rather than burying it in the raw body.
func TestBadTourSurfacesAllowedValues(t *testing.T) {
	body := fixture(t, "error_bad_tour.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}), WithMaxRetries(0))

	_, err := client.ListMatches(t.Context(), ListMatchesParams{Tour: Tour("bogus")})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *APIError", err)
	}
	if apiErr.Code != "bad_tour" {
		t.Errorf("Code = %q, want bad_tour", apiErr.Code)
	}

	want := []string{"atp", "challenger", "itf", "juniors", "wta"}
	if len(apiErr.AllowedValues) != len(want) {
		t.Fatalf("AllowedValues = %v, want %v", apiErr.AllowedValues, want)
	}
	for i := range want {
		if apiErr.AllowedValues[i] != want[i] {
			t.Errorf("AllowedValues[%d] = %q, want %q", i, apiErr.AllowedValues[i], want[i])
		}
	}

	// Every allowed value must be a Tour constant this package exposes, or the
	// typed constants have drifted from the API.
	exposed := map[string]Tour{
		"atp": TourATP, "wta": TourWTA, "challenger": TourChallenger,
		"itf": TourITF, "juniors": TourJuniors,
	}
	for _, allowed := range apiErr.AllowedValues {
		if _, ok := exposed[allowed]; !ok {
			t.Errorf("the API accepts tour %q but this package exposes no constant for it", allowed)
		}
	}
	if len(exposed) != len(apiErr.AllowedValues) {
		t.Errorf("package exposes %d tours, API accepts %d", len(exposed), len(apiErr.AllowedValues))
	}

	// And it should be readable straight off the message.
	if !strings.Contains(err.Error(), "atp, challenger, itf, juniors, wta") {
		t.Errorf("message should list the allowed values, got %q", err.Error())
	}
}

// A body with no "allowed" array must leave the field nil rather than an
// empty non-nil slice that reads as "nothing is allowed".
func TestAllowedValuesAbsentWhenNotOffered(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_request"}`))
	}), WithMaxRetries(0))

	_, err := client.ListMatches(t.Context(), ListMatchesParams{})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *APIError", err)
	}
	if apiErr.AllowedValues != nil {
		t.Errorf("AllowedValues = %v, want nil", apiErr.AllowedValues)
	}
	if strings.Contains(err.Error(), "allowed values") {
		t.Errorf("message should not mention allowed values, got %q", err.Error())
	}
}

// The rate-limit budget must survive onto the error, since 429 is exactly when
// the caller needs to know how long to wait.
func TestAPIErrorCarriesRateLimit(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "30")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1784734349")
		w.Header().Set("Retry-After", "59")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
	}), WithMaxRetries(0))

	_, err := client.ListMatches(t.Context(), ListMatchesParams{})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *APIError", err)
	}
	if got := apiErr.RateLimit.LimitOr(-1); got != 30 {
		t.Errorf("Limit = %d, want 30", got)
	}
	// Zero remaining is real data, not a missing header.
	if got := apiErr.RateLimit.RemainingOr(-1); got != 0 {
		t.Errorf("Remaining = %d, want 0", got)
	}
	if got := apiErr.RateLimit.RetryAfterOr(0); got != 59*time.Second {
		t.Errorf("RetryAfter = %v, want 59s", got)
	}
	if want := time.Unix(1784734349, 0).UTC(); !apiErr.RateLimit.Reset.Equal(want) {
		t.Errorf("Reset = %v, want %v", apiErr.RateLimit.Reset, want)
	}
	if !strings.Contains(err.Error(), "59s") {
		t.Errorf("a 429 message should mention the wait, got %q", err.Error())
	}
}

func TestErrorMessageFallbacks(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
		wantMsg  string
	}{
		{name: "code from body", body: `{"error":"upgrade_required"}`, wantCode: "upgrade_required", wantMsg: "upgrade_required"},
		// A null or empty code must fall through to the status text rather
		// than surface as the literal string "null".
		{name: "null code falls back", body: `{"error":null}`, wantMsg: "Forbidden"},
		{name: "empty code falls back", body: `{"error":""}`, wantMsg: "Forbidden"},
		{name: "non-JSON body falls back", body: `<html>nope</html>`, wantMsg: "Forbidden"},
		{name: "empty body falls back", body: ``, wantMsg: "Forbidden"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(tc.body))
			}), WithMaxRetries(0))

			_, err := client.ListMatches(t.Context(), ListMatchesParams{})

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want an *APIError", err)
			}
			if apiErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tc.wantCode)
			}
			if apiErr.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", apiErr.Message, tc.wantMsg)
			}
		})
	}
}

func TestParseRateLimitHeaders(t *testing.T) {
	tests := []struct {
		name           string
		headers        map[string]string
		wantLimit      int
		wantRemaining  int
		wantRetryAfter time.Duration
		wantResetUnix  int64
		wantKnown      bool
	}{
		{
			// Captured verbatim from a real anonymous call to the production
			// API on 2026-07-22.
			name: "real headers from the API",
			headers: map[string]string{
				"X-RateLimit-Limit":     "30",
				"X-RateLimit-Remaining": "29",
				"X-RateLimit-Reset":     "1784734349",
				"Retry-After":           "60",
			},
			wantLimit: 30, wantRemaining: 29, wantRetryAfter: 60 * time.Second,
			wantResetUnix: 1784734349, wantKnown: true,
		},
		{
			name:      "no headers at all",
			headers:   map[string]string{},
			wantLimit: -1, wantRemaining: -1, wantKnown: false,
		},
		{
			name:      "unparseable values are treated as absent",
			headers:   map[string]string{"X-RateLimit-Limit": "lots", "X-RateLimit-Remaining": ""},
			wantLimit: -1, wantRemaining: -1, wantKnown: false,
		},
		{
			name:      "exhausted budget is not the same as absent",
			headers:   map[string]string{"X-RateLimit-Remaining": "0"},
			wantLimit: -1, wantRemaining: 0, wantKnown: true,
		},
		{
			name:          "a non-positive reset carries no information",
			headers:       map[string]string{"X-RateLimit-Reset": "0"},
			wantLimit:     -1,
			wantRemaining: -1,
			wantKnown:     false,
		},
		{
			name:           "fractional Retry-After",
			headers:        map[string]string{"Retry-After": "1.5"},
			wantLimit:      -1,
			wantRemaining:  -1,
			wantRetryAfter: 1500 * time.Millisecond,
			wantKnown:      true,
		},
		{
			name:          "negative Retry-After is ignored",
			headers:       map[string]string{"Retry-After": "-5"},
			wantLimit:     -1,
			wantRemaining: -1,
			wantKnown:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			for key, value := range tc.headers {
				header.Set(key, value)
			}

			rl := parseRateLimit(header)

			if got := rl.LimitOr(-1); got != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", got, tc.wantLimit)
			}
			if got := rl.RemainingOr(-1); got != tc.wantRemaining {
				t.Errorf("Remaining = %d, want %d", got, tc.wantRemaining)
			}
			if got := rl.RetryAfterOr(0); got != tc.wantRetryAfter {
				t.Errorf("RetryAfter = %v, want %v", got, tc.wantRetryAfter)
			}
			if tc.wantResetUnix != 0 {
				if got := rl.Reset.Unix(); got != tc.wantResetUnix {
					t.Errorf("Reset = %d, want %d", got, tc.wantResetUnix)
				}
			} else if !rl.Reset.IsZero() {
				t.Errorf("Reset = %v, want the zero time", rl.Reset)
			}
			if got := rl.Known(); got != tc.wantKnown {
				t.Errorf("Known() = %v, want %v", got, tc.wantKnown)
			}
		})
	}
}

// Retry-After also accepts an HTTP-date, in case an intermediary rewrites the
// delta-seconds form the API emits.
func TestParseRetryAfterHTTPDate(t *testing.T) {
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	got, ok := parseRetryAfter(future)
	if !ok {
		t.Fatal("an HTTP-date Retry-After should parse")
	}
	if got < 25*time.Second || got > 31*time.Second {
		t.Errorf("delay = %v, want roughly 30s", got)
	}

	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	got, ok = parseRetryAfter(past)
	if !ok || got != 0 {
		t.Errorf("a past date should yield 0, got %v (ok=%v)", got, ok)
	}
}

// The API's three 429 shapes carry different recovery information, and each
// piece must land on the error rather than stay buried in the body.
func TestRateLimitedShapes(t *testing.T) {
	t.Run("per-minute window", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "31")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited","tier":"free","upgrade_url":"https://livetennisapi.com/subscribe/upgrade"}`))
		}), WithMaxRetries(0))

		_, err := client.ListMatches(t.Context(), ListMatchesParams{})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want an *APIError", err)
		}
		if apiErr.Scope != "" {
			t.Errorf("Scope = %q, want empty for the minute window", apiErr.Scope)
		}
		if !apiErr.ResetsAt.IsZero() || !apiErr.RetryAt.IsZero() {
			t.Error("a minute-window 429 carries no ResetsAt or RetryAt")
		}
	})

	t.Run("daily quota", func(t *testing.T) {
		// The resets_at instant is derived from the account's local midnight
		// — an absolute ISO instant, never a fixed UTC hour.
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited","scope":"day","limit_per_day":100,"resets_at":"2026-08-07T21:00:00Z"}`))
		}), WithMaxRetries(0))

		_, err := client.ListMatches(t.Context(), ListMatchesParams{})
		if !errors.Is(err, ErrRateLimited) {
			t.Fatalf("error = %v, want ErrRateLimited", err)
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want an *APIError", err)
		}
		if apiErr.Scope != "day" {
			t.Errorf("Scope = %q, want day", apiErr.Scope)
		}
		if apiErr.LimitPerDay == nil || *apiErr.LimitPerDay != 100 {
			t.Errorf("LimitPerDay = %v, want 100", apiErr.LimitPerDay)
		}
		want := time.Date(2026, 8, 7, 21, 0, 0, 0, time.UTC)
		if !apiErr.ResetsAt.Equal(want) {
			t.Errorf("ResetsAt = %v, want %v", apiErr.ResetsAt, want)
		}
		if !strings.Contains(err.Error(), "2026-08-07T21:00:00Z") {
			t.Errorf("message should carry the reset instant, got %q", err.Error())
		}
	})

	t.Run("abuse throttle", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"abuse_throttled","retry_at_epoch":1786572000}`))
		}), WithMaxRetries(0))

		_, err := client.ListMatches(t.Context(), ListMatchesParams{})
		if !errors.Is(err, ErrRateLimited) {
			t.Fatalf("error = %v, want ErrRateLimited", err)
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want an *APIError", err)
		}
		if apiErr.Code != "abuse_throttled" {
			t.Errorf("Code = %q, want abuse_throttled", apiErr.Code)
		}
		if want := time.Unix(1786572000, 0).UTC(); !apiErr.RetryAt.Equal(want) {
			t.Errorf("RetryAt = %v, want %v", apiErr.RetryAt, want)
		}
		// The block exists because of a broken retry loop; the message says so.
		if !strings.Contains(err.Error(), "retry loop") {
			t.Errorf("message should point at the retry loop, got %q", err.Error())
		}
	})
}

// An ambiguous name fragment is refused with the candidate list, because two
// people summed into one head-to-head is a wrong answer.
func TestAmbiguousNameSurfacesCandidates(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"ambiguous_name","side":"p1","candidates":["Rafael Nadal","Toni Nadal"],"detail":"the fragment matches more than one player — pass a more specific name"}`))
	}), WithMaxRetries(0))

	_, err := client.GetHeadToHead(t.Context(), "nadal", "djokovic")
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("error = %v, want ErrBadRequest", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *APIError", err)
	}
	if apiErr.Code != "ambiguous_name" {
		t.Errorf("Code = %q, want ambiguous_name", apiErr.Code)
	}
	if len(apiErr.Candidates) != 2 || apiErr.Candidates[0] != "Rafael Nadal" {
		t.Errorf("Candidates = %v, want the two Nadals", apiErr.Candidates)
	}
	if apiErr.Detail == "" {
		t.Error("Detail should carry the API's explanation")
	}
	if !strings.Contains(err.Error(), "Toni Nadal") {
		t.Errorf("message should list the candidates, got %q", err.Error())
	}
}

func TestConnectionErrorUnwraps(t *testing.T) {
	cause := errors.New("dial tcp: refused")
	err := error(&ConnectionError{URL: "https://example.test/health", Err: cause})

	if !errors.Is(err, ErrConnection) || !errors.Is(err, ErrAPI) {
		t.Error("a ConnectionError should match ErrConnection and ErrAPI")
	}
	if errors.Is(err, ErrTimeout) {
		t.Error("a refusal is not a timeout")
	}
	if !errors.Is(err, cause) {
		t.Error("the underlying cause should stay reachable")
	}

	timeout := error(&ConnectionError{URL: "u", Timeout: true, Err: context.DeadlineExceeded})
	if !errors.Is(timeout, ErrTimeout) || !errors.Is(timeout, ErrConnection) {
		t.Error("a timeout should match both ErrTimeout and ErrConnection")
	}
}

// The webhook limit is a 409 with its own sentinel, so "delete one first" is
// a branch rather than a status-code comparison.
func TestWebhookLimitIs409(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"webhook_limit","detail":"a key holds at most 3 webhooks"}`))
	}))

	_, err := client.CreateWebhook(t.Context(), WebhookParams{URL: "https://example.test/h"})
	if !errors.Is(err, ErrWebhookLimit) {
		t.Fatalf("error = %v, want ErrWebhookLimit", err)
	}
	if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrUpgradeRequired) {
		t.Error("a 409 is neither a rate limit nor a tier wall")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *APIError", err)
	}
	if apiErr.Code != "webhook_limit" {
		t.Errorf("Code = %q, want webhook_limit", apiErr.Code)
	}
}

// On a marketplace key the webhook endpoints answer 403 direct_key_required —
// a channel restriction, not a tier one, though the tier inference still
// names ULTRA as the endpoint's floor.
func TestWebhooksDirectKeyRequired(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"direct_key_required"}`))
	}))

	_, err := client.ListWebhooks(t.Context())
	if !errors.Is(err, ErrUpgradeRequired) {
		t.Fatalf("error = %v, want ErrUpgradeRequired", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *APIError", err)
	}
	if apiErr.Code != "direct_key_required" {
		t.Errorf("Code = %q, want direct_key_required — the code tells the channel story", apiErr.Code)
	}
	if apiErr.RequiredTier != TierUltra {
		t.Errorf("RequiredTier = %q, want ULTRA", apiErr.RequiredTier)
	}
}
