package livetennisapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	const matchID = int64(21635)

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
			name:    "matches filtered by tour",
			fixture: "matches_tour_wta.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{Tour: TourWTA, ListParams: ListParams{Limit: 2}})
				return err
			},
			wantPath:  "/matches",
			wantQuery: url.Values{"tour": {"wta"}, "limit": {"2"}},
		},
		{
			name:    "status and tour combine",
			fixture: "matches_live.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{Status: StatusUpcoming, Tour: TourChallenger})
				return err
			},
			wantPath:  "/matches",
			wantQuery: url.Values{"status": {"upcoming"}, "tour": {"challenger"}},
		},
		{
			name:     "get match",
			fixture:  "match_detail.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatch(ctx, matchID); return err },
			wantPath: "/matches/21635",
		},
		{
			name:     "get match score",
			fixture:  "score.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatchScore(ctx, matchID); return err },
			wantPath: "/matches/21635/score",
		},
		{
			name:    "list match events",
			fixture: "synthetic/events.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatchEvents(ctx, matchID, ListParams{Limit: 5})
				return err
			},
			wantPath:  "/matches/21635/events",
			wantQuery: url.Values{"limit": {"5"}},
		},
		{
			name:     "get match analysis",
			fixture:  "synthetic/analysis_uncovered.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatchAnalysis(ctx, matchID); return err },
			wantPath: "/matches/21635/analysis",
		},
		{
			name:    "search players",
			fixture: "players_search.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.SearchPlayers(ctx, SearchPlayersParams{Search: "alcaraz", ListParams: ListParams{Limit: 3}})
				return err
			},
			wantPath:  "/players",
			wantQuery: url.Values{"search": {"alcaraz"}, "limit": {"3"}},
		},
		{
			name:     "get player",
			fixture:  "player.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetPlayer(ctx, 2317); return err },
			wantPath: "/players/2317",
		},
		{
			name:      "list markets",
			fixture:   "synthetic/markets.json",
			call:      func(ctx context.Context, c *Client) error { _, err := c.ListMarkets(ctx, matchID); return err },
			wantPath:  "/markets",
			wantQuery: url.Values{"match_id": {"21635"}},
		},
		{
			name:    "get market prices",
			fixture: "synthetic/market_prices.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetMarketPrices(ctx, matchID, ListParams{Limit: 2, Offset: 99})
				return err
			},
			wantPath: "/markets/21635/prices",
			// Offset is deliberately dropped: this endpoint takes no offset.
			wantQuery: url.Values{"limit": {"2"}},
		},
		{
			// The history envelope is identical to /matches, so a recorded
			// completed-matches page stands in: a FREE key cannot reach
			// /history/matches, which answers it with the 403 recorded in
			// testdata/error_403_history.json.
			name:    "list completed matches",
			fixture: "matches_completed.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListCompletedMatches(ctx, ListParams{Limit: 1})
				return err
			},
			wantPath:  "/history/matches",
			wantQuery: url.Values{"limit": {"1"}},
		},
		{
			name:    "list fixtures",
			fixture: "fixtures.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListFixtures(ctx, ListFixturesParams{})
				return err
			},
			wantPath: "/fixtures",
		},
		{
			name:    "fixtures filtered by tour",
			fixture: "fixtures_tour_juniors.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListFixtures(ctx, ListFixturesParams{Tour: TourJuniors, ListParams: ListParams{Limit: 3}})
				return err
			},
			wantPath:  "/fixtures",
			wantQuery: url.Values{"tour": {"juniors"}, "limit": {"3"}},
		},
		{
			// The player filter repeats; country, from and to ride alongside.
			name:    "matches filtered by players, country and dates",
			fixture: "matches_live.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatches(ctx, ListMatchesParams{
					Player:  []int64{2317, 9001},
					Country: "esp",
					From:    "2026-08-01",
					To:      "2026-08-07",
				})
				return err
			},
			wantPath: "/matches",
			wantQuery: url.Values{
				"player": {"2317", "9001"}, "country": {"esp"},
				"from": {"2026-08-01"}, "to": {"2026-08-07"},
			},
		},
		{
			name:    "history matches with the full filter set",
			fixture: "matches_completed.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListHistoryMatches(ctx, HistoryMatchesParams{
					From:       "2026-07-01",
					To:         "2026-07-31",
					Coverage:   CoverageFromStart,
					Tour:       TourATP,
					Player:     []int64{2317},
					Country:    "esp",
					ListParams: ListParams{Limit: 5},
				})
				return err
			},
			wantPath: "/history/matches",
			wantQuery: url.Values{
				"from": {"2026-07-01"}, "to": {"2026-07-31"},
				"coverage": {"from_start"}, "tour": {"atp"},
				"player": {"2317"}, "country": {"esp"}, "limit": {"5"},
			},
		},
		{
			name:    "match tape with clean sequence",
			fixture: "synthetic/tape.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetMatchTape(ctx, matchID, TapeParams{Sequence: SequenceClean})
				return err
			},
			wantPath:  "/history/matches/21635",
			wantQuery: url.Values{"sequence": {"clean"}},
		},
		{
			// The raw default is the API's own, so no query is sent at all.
			name:    "match tape with default sequence",
			fixture: "synthetic/tape.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetMatchTape(ctx, matchID, TapeParams{})
				return err
			},
			wantPath: "/history/matches/21635",
		},
		{
			name:    "head to head",
			fixture: "synthetic/h2h.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetHeadToHead(ctx, "nadal", "djokovic")
				return err
			},
			wantPath: "/h2h",
			wantQuery: url.Values{
				"p1": {"nadal"}, "p2": {"djokovic"},
			},
		},
		{
			name:    "archive matches with filters",
			fixture: "synthetic/archive_matches.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListArchiveMatches(ctx, ArchiveMatchesParams{
					Tour:       TourATP,
					Name:       "nadal",
					From:       "2005-01-01",
					To:         "2005-12-31",
					Round:      "F",
					Level:      "G",
					ListParams: ListParams{Limit: 2},
				})
				return err
			},
			wantPath: "/history/archive/matches",
			wantQuery: url.Values{
				"tour": {"atp"}, "name": {"nadal"},
				"from": {"2005-01-01"}, "to": {"2005-12-31"},
				"round": {"F"}, "level": {"G"}, "limit": {"2"},
			},
		},
		{
			name:     "get archive match",
			fixture:  "synthetic/archive_match.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetArchiveMatch(ctx, 1447213); return err },
			wantPath: "/history/archive/matches/1447213",
		},
		{
			name:    "archive players",
			fixture: "synthetic/archive_players.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListArchivePlayers(ctx, ArchivePlayersParams{Name: "nadal", Tour: TourATP})
				return err
			},
			wantPath:  "/history/archive/players",
			wantQuery: url.Values{"name": {"nadal"}, "tour": {"atp"}},
		},
		{
			name:      "archive career",
			fixture:   "synthetic/archive_career.json",
			call:      func(ctx context.Context, c *Client) error { _, err := c.GetArchiveCareer(ctx, "nadal"); return err },
			wantPath:  "/history/archive/career",
			wantQuery: url.Values{"name": {"nadal"}},
		},
		{
			// Listing mode: no player ids, exactly one system.
			name:    "rankings listing mode",
			fixture: "synthetic/rankings_listing.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListRankings(ctx, RankingsParams{
					System:     []RankingSystem{RankingATP},
					AsOf:       "2026-08-06",
					ListParams: ListParams{Limit: 2},
				})
				return err
			},
			wantPath:  "/rankings",
			wantQuery: url.Values{"system": {"atp"}, "as_of": {"2026-08-06"}, "limit": {"2"}},
		},
		{
			// Per-player mode: player and system both repeat.
			name:    "rankings per-player mode",
			fixture: "synthetic/rankings_players.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListRankings(ctx, RankingsParams{
					Player: []int64{2317, 9001},
					System: []RankingSystem{RankingATP, RankingUTR},
				})
				return err
			},
			wantPath:  "/rankings",
			wantQuery: url.Values{"player": {"2317", "9001"}, "system": {"atp", "utr"}},
		},
		{
			name:     "match statistics",
			fixture:  "synthetic/statistics.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetMatchStatistics(ctx, matchID); return err },
			wantPath: "/matches/21635/statistics",
		},
		{
			name:    "rally matches with filters",
			fixture: "synthetic/rally_matches.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListRallyMatches(ctx, RallyMatchesParams{
					Player:  "invented",
					From:    "2026-01-01",
					To:      "2026-06-30",
					Surface: "clay",
					Gender:  "M",
				})
				return err
			},
			wantPath: "/rally/matches",
			wantQuery: url.Values{
				"player": {"invented"}, "from": {"2026-01-01"}, "to": {"2026-06-30"},
				"surface": {"clay"}, "gender": {"M"},
			},
		},
		{
			name:    "get rally match",
			fixture: "synthetic/rally_match.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetRallyMatch(ctx, 118203, ListParams{Limit: 2})
				return err
			},
			wantPath:  "/rally/matches/118203",
			wantQuery: url.Values{"limit": {"2"}},
		},
		{
			name:    "rally by our match id",
			fixture: "synthetic/rally_match.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetMatchRally(ctx, matchID, ListParams{})
				return err
			},
			wantPath: "/history/matches/21635/rally",
		},
		{
			name:    "charting player",
			fixture: "synthetic/charting_player.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetChartingPlayer(ctx, ChartingPlayerParams{Name: "invented", Gender: "men"})
				return err
			},
			wantPath:  "/charting/players",
			wantQuery: url.Values{"name": {"invented"}, "gender": {"men"}},
		},
		{
			name:     "charting match",
			fixture:  "synthetic/charting_match.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetChartingMatch(ctx, 118203); return err },
			wantPath: "/charting/matches/118203",
		},
		{
			name:    "history packages by kind and year",
			fixture: "synthetic/history_packages.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListHistoryPackages(ctx, HistoryPackagesParams{Kind: PackageRankings, Year: "2026"})
				return err
			},
			wantPath:  "/history/packages",
			wantQuery: url.Values{"kind": {"rankings"}, "year": {"2026"}},
		},
		{
			name:     "ws token",
			fixture:  "synthetic/ws_token.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetWSToken(ctx); return err },
			wantPath: "/ws-token",
		},
		{
			name:    "tournaments with search and tour",
			fixture: "synthetic/tournaments.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListTournaments(ctx, TournamentsParams{
					Search:     "roland",
					Tour:       TourATP,
					ListParams: ListParams{Limit: 2},
				})
				return err
			},
			wantPath:  "/tournaments",
			wantQuery: url.Values{"search": {"roland"}, "tour": {"atp"}, "limit": {"2"}},
		},
		{
			name:    "get tournament by string id",
			fixture: "synthetic/tournament.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetTournament(ctx, "atp-roland-garros-m")
				return err
			},
			wantPath: "/tournaments/atp-roland-garros-m",
		},
		{
			name:     "usage",
			fixture:  "synthetic/usage.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.GetUsage(ctx); return err },
			wantPath: "/usage",
		},
		{
			// This endpoint's cap is 500, not MaxLimit — the clamp must use
			// the right ceiling.
			name:    "match prices clamp to MaxPriceTicks",
			fixture: "synthetic/match_prices.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatchPrices(ctx, matchID, MatchPricesParams{Limit: 9999, Minutes: 30})
				return err
			},
			wantPath:  "/matches/21635/prices",
			wantQuery: url.Values{"limit": {"500"}, "minutes": {"30"}},
		},
		{
			// Zero params defer to the API's own defaults.
			name:    "match prices with defaults",
			fixture: "synthetic/match_prices.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListMatchPrices(ctx, matchID, MatchPricesParams{})
				return err
			},
			wantPath: "/matches/21635/prices",
		},
		{
			name:    "history package manifest",
			fixture: "synthetic/package_manifest.json",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetHistoryPackage(ctx, "2026-07", PackageTape)
				return err
			},
			wantPath:  "/history/packages/2026-07",
			wantQuery: url.Values{"kind": {"tape"}},
		},
		{
			name:     "list webhooks",
			fixture:  "synthetic/webhooks.json",
			call:     func(ctx context.Context, c *Client) error { _, err := c.ListWebhooks(ctx); return err },
			wantPath: "/webhooks",
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
			// Bearer is the default, matching the Python and JS clients. Both
			// headers are accepted by the API; this was verified live.
			name:          "bearer by default",
			apiKey:        "twjp_live_123",
			wantAuthoriz:  "Bearer twjp_live_123",
			wantUserAgent: "livetennisapi-go/" + Version,
		},
		{
			name:       "X-API-Key when asked",
			apiKey:     "twjp_live_123",
			opts:       []Option{WithAuthMethod(AuthAPIKey)},
			wantAPIKey: "twjp_live_123",
		},
		{
			name:   "no auth header without a key",
			apiKey: "",
		},
		{
			name:          "custom user agent",
			apiKey:        "twjp_live_123",
			opts:          []Option{WithUserAgent("my-app/2.0")},
			wantAuthoriz:  "Bearer twjp_live_123",
			wantUserAgent: "my-app/2.0",
		},
		{
			name:         "key is trimmed",
			apiKey:       "  twjp_live_123\n",
			wantAuthoriz: "Bearer twjp_live_123",
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
	// The recorded page holds three live matches.
	if page.Len() != 3 {
		t.Errorf("page length = %d, want 3", page.Len())
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
	// Bearer is the default, matching the Python and JS clients.
	if client.auth != AuthBearer {
		t.Errorf("auth = %v, want AuthBearer", client.auth)
	}
}

// Webhook registration must POST a JSON body; deletion must DELETE. The
// request-shape table above covers only GETs, so the mutations are asserted
// here, body included.
func TestWebhookMutations(t *testing.T) {
	t.Run("create sends POST with url and events", func(t *testing.T) {
		var got http.Request
		var gotBody []byte
		body := fixture(t, "synthetic/webhook_created.json")
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = *r
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(body)
		}))

		webhook, err := client.CreateWebhook(t.Context(), WebhookParams{
			URL:    "https://example.test/hooks/tennis",
			Events: []WebhookEvent{WebhookScore, WebhookBreakPoint},
		})
		if err != nil {
			t.Fatalf("CreateWebhook: %v", err)
		}

		if got.Method != http.MethodPost || got.URL.Path != "/webhooks" {
			t.Errorf("request = %s %s, want POST /webhooks", got.Method, got.URL.Path)
		}
		if ct := got.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var sent struct {
			URL    string   `json:"url"`
			Events []string `json:"events"`
		}
		if err := json.Unmarshal(gotBody, &sent); err != nil {
			t.Fatalf("request body is not JSON: %v", err)
		}
		if sent.URL != "https://example.test/hooks/tennis" || len(sent.Events) != 2 {
			t.Errorf("body = %+v", sent)
		}

		// The secret arrives exactly here and nowhere else.
		if webhook.Secret != "whsec_invented_only_shown_once" {
			t.Errorf("Secret = %q, want the one-time secret", webhook.Secret)
		}
		if webhook.ID != 42 || !webhook.Enabled {
			t.Errorf("webhook = %+v", webhook)
		}
	})

	t.Run("create omits empty events for the API default", func(t *testing.T) {
		var gotBody []byte
		body := fixture(t, "synthetic/webhook_created.json")
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(body)
		}))

		if _, err := client.CreateWebhook(t.Context(), WebhookParams{URL: "https://example.test/h"}); err != nil {
			t.Fatalf("CreateWebhook: %v", err)
		}
		if strings.Contains(string(gotBody), "events") {
			t.Errorf("body %q should omit events so the API default applies", gotBody)
		}
	})

	t.Run("delete sends DELETE", func(t *testing.T) {
		var got http.Request
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = *r
			_, _ = w.Write([]byte(`{"deleted":1}`))
		}))

		if err := client.DeleteWebhook(t.Context(), 42); err != nil {
			t.Fatalf("DeleteWebhook: %v", err)
		}
		if got.Method != http.MethodDelete || got.URL.Path != "/webhooks/42" {
			t.Errorf("request = %s %s, want DELETE /webhooks/42", got.Method, got.URL.Path)
		}
	})
}

// A mutation is attempted exactly once: a POST that timed out may still have
// been applied, and re-sending it could register a duplicate webhook.
func TestMutationsAreNotRetried(t *testing.T) {
	var attempts atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}), WithMaxRetries(2))

	_, err := client.CreateWebhook(t.Context(), WebhookParams{URL: "https://example.test/h"})
	if !errors.Is(err, ErrServerError) {
		t.Fatalf("error = %v, want ErrServerError", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("POST attempts = %d, want exactly 1", got)
	}

	attempts.Store(0)
	if err := client.DeleteWebhook(t.Context(), 1); !errors.Is(err, ErrServerError) {
		t.Fatalf("delete error = %v, want ErrServerError", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("DELETE attempts = %d, want exactly 1", got)
	}
}

// The package download streams the body back verbatim and maps error
// statuses like every other call.
func TestDownloadHistoryPackage(t *testing.T) {
	const payload = `{"match":{"id":1}}` + "\n" + `{"match":{"id":2}}` + "\n"

	var got http.Request
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(payload))
	}))

	body, err := client.DownloadHistoryPackage(t.Context(), "2026-07", PackageTape, "jsonl")
	if err != nil {
		t.Fatalf("DownloadHistoryPackage: %v", err)
	}
	defer body.Close()

	if got.URL.Path != "/history/packages/2026-07" {
		t.Errorf("path = %q", got.URL.Path)
	}
	if q := got.URL.Query(); q.Get("format") != "jsonl" || q.Get("kind") != "tape" {
		t.Errorf("query = %v", q)
	}

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading stream: %v", err)
	}
	if string(data) != payload {
		t.Errorf("stream = %q, want the file verbatim", data)
	}

	// A tier wall on the download maps like everywhere else.
	walled := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"upgrade_required"}`))
	}))
	if _, err := walled.DownloadHistoryPackage(t.Context(), "2026-07", "", "csv"); !errors.Is(err, ErrUpgradeRequired) {
		t.Fatalf("error = %v, want ErrUpgradeRequired", err)
	}
}
