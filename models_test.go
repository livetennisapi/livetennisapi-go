package livetennisapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// An upcoming match carries "score": null. Decoding it must leave a nil
// pointer, and every Score helper must stay usable on that nil without
// panicking — this is the single most likely way for a caller to crash.
func TestUpcomingMatchHasNilScoreAndDoesNotPanic(t *testing.T) {
	body := fixture(t, "matches_upcoming.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{Status: StatusUpcoming})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if page.Len() != 1 {
		t.Fatalf("page length = %d, want 1", page.Len())
	}

	match := page.Data[0]
	if match.Score != nil {
		t.Fatalf("Score = %+v, want nil for an upcoming match", match.Score)
	}

	// Every one of these runs against a nil *Score.
	if got := match.Score.String(); got != "-" {
		t.Errorf("nil Score String() = %q, want %q", got, "-")
	}
	if got := match.Score.NumSets(); got != 0 {
		t.Errorf("nil Score NumSets() = %d, want 0", got)
	}
	if _, _, ok := match.Score.GamesForSet(0); ok {
		t.Error("nil Score GamesForSet() should report ok=false")
	}

	// An unranked or unresolved player is nil, never a plausible-looking zero.
	p1 := match.Players.P1
	if p1 == nil {
		t.Fatal("P1 missing")
	}
	if p1.Ranking != nil {
		t.Errorf("Ranking = %v, want nil for an unranked player", *p1.Ranking)
	}
	if p1.RankingPoints != nil {
		t.Errorf("RankingPoints = %v, want nil", *p1.RankingPoints)
	}
	if p1.Backhand != nil {
		t.Errorf("Backhand = %v, want nil", *p1.Backhand)
	}
	if !p1.Birthday.IsZero() {
		t.Errorf("Birthday = %v, want the zero time", p1.Birthday)
	}
	if p1.Country != "" {
		t.Errorf("Country = %q, want empty", p1.Country)
	}
}

func TestLiveMatchDecoding(t *testing.T) {
	body := fixture(t, "matches_live.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{Status: StatusLive})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}

	match := page.Data[0]
	if match.ID != 918273 {
		t.Errorf("ID = %d, want 918273", match.ID)
	}
	if match.Status != StatusLive {
		t.Errorf("Status = %q, want live", match.Status)
	}
	if match.Surface != "hard" || match.Format != "BO3" || match.Round != "QF" {
		t.Errorf("surface/format/round wrong: %+v", match)
	}
	if match.Indoor {
		t.Error("Indoor = true, want false")
	}
	if match.Winner != nil {
		t.Errorf("Winner = %v, want nil while live", *match.Winner)
	}

	want := time.Date(2026, 7, 22, 14, 30, 0, 0, time.UTC)
	if !match.ScheduledTime.Equal(want) {
		t.Errorf("ScheduledTime = %v, want %v", match.ScheduledTime, want)
	}

	if match.Players.P2 == nil || match.Players.P2.Name != "Jannik Sinner" {
		t.Errorf("P2 = %+v, want Sinner", match.Players.P2)
	}
	if r := match.Players.P1.Ranking; r == nil || *r != 2 {
		t.Errorf("P1 ranking = %v, want 2", r)
	}
	if b := match.Players.P1.Birthday; b.Format("2006-01-02") != "2003-05-05" {
		t.Errorf("P1 birthday = %v, want 2003-05-05", b)
	}

	// The list endpoint carries no stats block.
	if match.Players.P1.Stats != nil {
		t.Error("Stats should be nil on a list response")
	}

	// Model fields are ULTRA-only and null here.
	if match.Score.WinProbabilityP1 != nil || match.Score.Danger != nil {
		t.Error("ULTRA model fields should be nil below ULTRA")
	}
}

// Points are strings. Parsing them as integers is impossible ("AD") and
// pointless ("40" is not 40), so the type must stay []string.
func TestScorePointsAreStrings(t *testing.T) {
	body := fixture(t, "score.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	score, err := client.GetMatchScore(t.Context(), 918273)
	if err != nil {
		t.Fatalf("GetMatchScore: %v", err)
	}

	want := []string{"40", "AD"}
	if len(score.Points) != len(want) {
		t.Fatalf("Points = %v, want %v", score.Points, want)
	}
	for i := range want {
		if score.Points[i] != want[i] {
			t.Errorf("Points[%d] = %q, want %q", i, score.Points[i], want[i])
		}
	}
	if s := score.Server; s == nil || *s != 2 {
		t.Errorf("Server = %v, want 2", s)
	}
	if score.IsTiebreak {
		t.Error("IsTiebreak = true, want false")
	}
}

// Games is player-major: [games_p1, games_p2], each a per-set list.
func TestScoreGamesArePlayerMajor(t *testing.T) {
	var score Score
	if err := json.Unmarshal(fixture(t, "score.json"), &score); err != nil {
		t.Fatalf("decoding score: %v", err)
	}

	if got := score.NumSets(); got != 3 {
		t.Fatalf("NumSets = %d, want 3", got)
	}

	tests := []struct {
		setIndex int
		p1, p2   int
		wantOK   bool
	}{
		{setIndex: 0, p1: 6, p2: 4, wantOK: true},
		{setIndex: 1, p1: 3, p2: 6, wantOK: true},
		{setIndex: 2, p1: 2, p2: 1, wantOK: true},
		{setIndex: 3, wantOK: false},
		{setIndex: -1, wantOK: false},
	}

	for _, tc := range tests {
		p1, p2, ok := score.GamesForSet(tc.setIndex)
		if ok != tc.wantOK {
			t.Errorf("GamesForSet(%d) ok = %v, want %v", tc.setIndex, ok, tc.wantOK)
			continue
		}
		if ok && (p1 != tc.p1 || p2 != tc.p2) {
			t.Errorf("GamesForSet(%d) = (%d, %d), want (%d, %d)", tc.setIndex, p1, p2, tc.p1, tc.p2)
		}
	}

	if got, want := score.String(), "6-4 3-6 2-1 (40-AD)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestScoreStringVariants(t *testing.T) {
	tests := []struct {
		name  string
		score *Score
		want  string
	}{
		{name: "nil score", score: nil, want: "-"},
		{name: "empty score", score: &Score{}, want: "-"},
		{
			name:  "games only",
			want:  "6-4 3-6",
			score: &Score{Games: [][]int{{6, 3}, {4, 6}}},
		},
		{
			name:  "sets only, no games",
			want:  "3-1",
			score: &Score{Sets: []int{3, 1}},
		},
		{
			name:  "points only",
			want:  "(15-30)",
			score: &Score{Points: []string{"15", "30"}},
		},
		{
			// A ragged games array must not panic or invent a set.
			name:  "ragged games",
			want:  "6-4",
			score: &Score{Games: [][]int{{6, 3}, {4}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.score.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlayerStatsAreRawJSON(t *testing.T) {
	body := fixture(t, "player.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	player, err := client.GetPlayer(t.Context(), 4021)
	if err != nil {
		t.Fatalf("GetPlayer: %v", err)
	}
	if player.Stats == nil {
		t.Fatal("Stats missing on the single-player endpoint")
	}

	var ratings struct {
		Elo float64 `json:"elo"`
	}
	if err := json.Unmarshal(player.Stats.Ratings, &ratings); err != nil {
		t.Fatalf("decoding ratings: %v", err)
	}
	if ratings.Elo != 2145.6 {
		t.Errorf("elo = %v, want 2145.6", ratings.Elo)
	}
	if len(player.Stats.Season) == 0 {
		t.Error("season should be preserved as raw JSON")
	}
}

func TestMatchDetailEmbedsMarketAndAnalysis(t *testing.T) {
	body := fixture(t, "match_detail.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	match, err := client.GetMatch(t.Context(), 918273)
	if err != nil {
		t.Fatalf("GetMatch: %v", err)
	}

	if match.Market == nil {
		t.Fatal("Market embed missing")
	}
	if len(match.Market.Prices) != 2 {
		t.Fatalf("prices = %d, want 2", len(match.Market.Prices))
	}
	if bid := match.Market.Prices[0].Bid; bid == nil || *bid != 0.41 {
		t.Errorf("first bid = %v, want 0.41", bid)
	}
	// Zero liquidity is a real value and must not decode as absent.
	if liq := match.Market.Liquidity; liq == nil || *liq != 0 {
		t.Errorf("Liquidity = %v, want a pointer to 0", liq)
	}

	if match.Analysis == nil || match.Analysis.Thesis == nil || match.Analysis.Profile == nil {
		t.Fatal("Analysis embed missing")
	}
	if side := match.Analysis.Thesis.PickSide; side == nil || *side != 2 {
		t.Errorf("PickSide = %v, want 2", side)
	}
	if match.Analysis.Thesis.State != "confirmed" {
		t.Errorf("State = %q, want confirmed", match.Analysis.Thesis.State)
	}
	if match.Analysis.Thesis.Notes.Matchup == "" {
		t.Error("thesis notes lost")
	}
	if match.Analysis.Thesis.Notes.Fatigue != "" {
		t.Error("a null note should decode to empty")
	}
	if len(match.Analysis.Thesis.ScenarioPlaybook) == 0 {
		t.Error("scenario_playbook should be preserved as raw JSON")
	}
	if match.Analysis.Profile.VolatilityRating != "high" {
		t.Errorf("VolatilityRating = %q, want high", match.Analysis.Profile.VolatilityRating)
	}
	if len(match.Analysis.Profile.KeyFactors) != 2 {
		t.Errorf("KeyFactors = %v, want 2 entries", match.Analysis.Profile.KeyFactors)
	}

	// The detail endpoint is where ULTRA's live model fields appear.
	if wp := match.Score.WinProbabilityP1; wp == nil || *wp != 0.4127 {
		t.Errorf("WinProbabilityP1 = %v, want 0.4127", wp)
	}
}

// The model does not cover every match, and an uncovered one answers with both
// halves null rather than a 404.
func TestAnalysisMayBeEmpty(t *testing.T) {
	body := fixture(t, "analysis_uncovered.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	analysis, err := client.GetMatchAnalysis(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetMatchAnalysis: %v", err)
	}
	if analysis.Thesis != nil || analysis.Profile != nil {
		t.Errorf("expected both halves nil, got %+v", analysis)
	}
}

func TestCompletedMatchHasWinner(t *testing.T) {
	body := fixture(t, "history_matches.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	page, err := client.ListCompletedMatches(t.Context(), ListParams{})
	if err != nil {
		t.Fatalf("ListCompletedMatches: %v", err)
	}

	match := page.Data[0]
	if w := match.Winner; w == nil || *w != 1 {
		t.Errorf("Winner = %v, want 1", w)
	}
	if match.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", match.Status)
	}
	// A finished match has no server.
	if match.Score.Server != nil {
		t.Error("Server should be nil on a completed match")
	}
	if got, want := match.Score.String(), "6-3 4-6 6-4 7-5"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestEventsAndFixturesDecode(t *testing.T) {
	t.Run("events", func(t *testing.T) {
		body := fixture(t, "events.json")
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(body)
		}))

		page, err := client.ListMatchEvents(t.Context(), 918273, ListParams{})
		if err != nil {
			t.Fatalf("ListMatchEvents: %v", err)
		}
		if page.Len() != 3 {
			t.Fatalf("events = %d, want 3", page.Len())
		}
		if page.Data[0].Type != EventBreak {
			t.Errorf("Type = %q, want break", page.Data[0].Type)
		}
		if p := page.Data[0].Player; p == nil || *p != 2 {
			t.Errorf("Player = %v, want 2", p)
		}
		// An event belonging to neither player stays nil, not 0.
		if page.Data[2].Player != nil {
			t.Error("a null player should decode to nil, not 0")
		}
	})

	t.Run("fixtures", func(t *testing.T) {
		body := fixture(t, "fixtures.json")
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(body)
		}))

		page, err := client.ListFixtures(t.Context(), ListParams{})
		if err != nil {
			t.Fatalf("ListFixtures: %v", err)
		}
		f := page.Data[0]
		if f.Player1Name != "Marketa Vondrousova" || f.Player2Name != "Karolina Muchova" {
			t.Errorf("fixture names wrong: %+v", f)
		}
		if f.EventDate.Format("2006-01-02") != "2026-07-24" {
			t.Errorf("EventDate = %v, want 2026-07-24", f.EventDate)
		}
	})
}

func TestListMarketsMetaCarriesMatchID(t *testing.T) {
	body := fixture(t, "markets.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	page, err := client.ListMarkets(t.Context(), 918273)
	if err != nil {
		t.Fatalf("ListMarkets: %v", err)
	}
	if page.Meta.MatchID != 918273 {
		t.Errorf("Meta.MatchID = %d, want 918273", page.Meta.MatchID)
	}
	if page.Meta.Count != 1 {
		t.Errorf("Meta.Count = %d, want 1", page.Meta.Count)
	}
}

func TestGetMarketPricesNullableFields(t *testing.T) {
	body := fixture(t, "market_prices.json")
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))

	market, err := client.GetMarketPrices(t.Context(), 918273, ListParams{})
	if err != nil {
		t.Fatalf("GetMarketPrices: %v", err)
	}
	if market.Liquidity != nil {
		t.Error("a null liquidity should stay nil")
	}

	second := market.Prices[1]
	// A bid of 0 means nobody will buy; an absent ask means no quote at all.
	if second.Bid == nil || *second.Bid != 0 {
		t.Errorf("Bid = %v, want a pointer to 0", second.Bid)
	}
	if second.Ask != nil {
		t.Errorf("Ask = %v, want nil", *second.Ask)
	}
}

// A timestamp the parser does not recognise must not take the whole response
// down with it.
func TestTimeDecoding(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantZero bool
		wantRaw  string
		wantUTC  string
	}{
		{name: "RFC 3339 with Z", json: `"2026-07-22T15:31:28Z"`, wantRaw: "2026-07-22T15:31:28Z", wantUTC: "2026-07-22T15:31:28Z"},
		{name: "fractional seconds", json: `"2026-07-22T15:31:28.512Z"`, wantRaw: "2026-07-22T15:31:28.512Z", wantUTC: "2026-07-22T15:31:28Z"},
		{name: "offset is normalised to UTC", json: `"2026-07-22T18:31:28+03:00"`, wantRaw: "2026-07-22T18:31:28+03:00", wantUTC: "2026-07-22T15:31:28Z"},
		{name: "date only", json: `"2003-05-05"`, wantRaw: "2003-05-05", wantUTC: "2003-05-05T00:00:00Z"},
		{name: "no zone", json: `"2026-07-22T15:31:28"`, wantRaw: "2026-07-22T15:31:28", wantUTC: "2026-07-22T15:31:28Z"},
		{name: "null", json: `null`, wantZero: true},
		{name: "empty string", json: `""`, wantZero: true},
		{name: "unparseable is kept verbatim", json: `"last Tuesday"`, wantZero: true, wantRaw: "last Tuesday"},
		{name: "a number is kept verbatim", json: `1784734349`, wantZero: true, wantRaw: "1784734349"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got Time
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("unmarshal returned an error, want tolerance: %v", err)
			}
			if got.IsZero() != tc.wantZero {
				t.Errorf("IsZero() = %v, want %v", got.IsZero(), tc.wantZero)
			}
			if got.Raw != tc.wantRaw {
				t.Errorf("Raw = %q, want %q", got.Raw, tc.wantRaw)
			}
			if tc.wantUTC != "" {
				if formatted := got.UTC().Format(time.RFC3339); formatted != tc.wantUTC {
					t.Errorf("time = %s, want %s", formatted, tc.wantUTC)
				}
			}
		})
	}
}

func TestTimeRoundTrips(t *testing.T) {
	const raw = `"2026-07-22T15:31:28Z"`

	var value Time
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != raw {
		t.Errorf("round trip = %s, want %s", out, raw)
	}

	empty, err := json.Marshal(Time{})
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if string(empty) != "null" {
		t.Errorf("zero Time marshals to %s, want null", empty)
	}
}

// A malformed timestamp inside a match must not fail the surrounding page.
func TestBadTimestampDoesNotFailTheResponse(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":1,"tournament":"Odd Open","scheduled_time":"soon"}],"meta":{"count":1}}`))
	}))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if page.Data[0].Tournament != "Odd Open" {
		t.Fatalf("match lost: %+v", page.Data)
	}
	if !page.Data[0].ScheduledTime.IsZero() {
		t.Error("an unparseable time should be zero")
	}
	if page.Data[0].ScheduledTime.Raw != "soon" {
		t.Errorf("Raw = %q, want the original string preserved", page.Data[0].ScheduledTime.Raw)
	}
}
