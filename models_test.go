package livetennisapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// serveBody answers everything with one body.
func serveBody(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}
}

// A real upcoming match, recorded from the live API, carries "score": null.
// Decoding it must leave a nil pointer, and every Score helper must stay
// usable on that nil — this is the most likely way for a caller to crash.
func TestRecordedUpcomingMatchHasNilScoreAndDoesNotPanic(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "matches_upcoming.json")))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{Status: StatusUpcoming})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if page.Len() != 3 {
		t.Fatalf("page length = %d, want 3", page.Len())
	}

	// Every match in the recording is upcoming and therefore scoreless.
	for _, match := range page.Data {
		if match.Status != StatusUpcoming {
			t.Errorf("match %d status = %q, want upcoming", match.ID, match.Status)
		}
		if match.Score != nil {
			t.Fatalf("match %d Score = %+v, want nil", match.ID, match.Score)
		}

		// All of these run against a nil *Score.
		if got := match.Score.String(); got != "-" {
			t.Errorf("nil Score String() = %q, want %q", got, "-")
		}
		if got := match.Score.NumSets(); got != 0 {
			t.Errorf("nil Score NumSets() = %d, want 0", got)
		}
		if _, _, ok := match.Score.GamesForSet(0); ok {
			t.Error("nil Score GamesForSet() should report ok=false")
		}
		// An unfinished match has no winner; the key is absent entirely.
		if match.Winner != nil {
			t.Errorf("match %d Winner = %d, want nil", match.ID, *match.Winner)
		}
	}

	first := page.Data[0]
	if first.ID != 21651 {
		t.Errorf("ID = %d, want 21651", first.ID)
	}

	// The second player of the recorded first match has almost no biography,
	// and every absent field must be nil rather than a plausible-looking zero.
	p2 := first.Players.P2
	if p2 == nil {
		t.Fatal("P2 missing")
	}
	if p2.Backhand != nil {
		t.Errorf("Backhand = %d, want nil", *p2.Backhand)
	}
	if !p2.Birthday.IsZero() {
		t.Errorf("Birthday = %v, want the zero time", p2.Birthday)
	}
	if p2.Hand != "" {
		t.Errorf("Hand = %q, want empty", p2.Hand)
	}
	// Ranking is present here and low; the point is that it is not confused
	// with the missing fields around it.
	if p2.Ranking == nil || *p2.Ranking != 2236 {
		t.Errorf("Ranking = %v, want 2236", p2.Ranking)
	}
}

func TestRecordedLiveMatchDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "matches_live.json")))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{Status: StatusLive})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}

	match := page.Data[0]
	if match.ID != 21635 {
		t.Errorf("ID = %d, want 21635", match.ID)
	}
	if match.Status != StatusLive {
		t.Errorf("Status = %q, want live", match.Status)
	}
	if match.Tournament != "M15 Kursumlijska Banja 10" {
		t.Errorf("Tournament = %q", match.Tournament)
	}
	if match.Surface != "clay" || match.Format != "BO3" {
		t.Errorf("surface/format wrong: %q %q", match.Surface, match.Format)
	}
	if match.Indoor || match.IsDoubles {
		t.Error("indoor/is_doubles should both be false")
	}
	if match.EventStatus != "" {
		t.Errorf("EventStatus = %q, want empty for a null", match.EventStatus)
	}

	want := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	if !match.ScheduledTime.Equal(want) {
		t.Errorf("ScheduledTime = %v, want %v", match.ScheduledTime, want)
	}

	p1, p2 := match.Players.P1, match.Players.P2
	if p1 == nil || p2 == nil {
		t.Fatal("players missing")
	}
	if p1.Name != "Vlado Jankanj" || p2.Name != "Alessandro Bellifemine" {
		t.Errorf("names wrong: %q / %q", p1.Name, p2.Name)
	}
	// Country codes come back lower-case from this API.
	if p1.Country != "srb" {
		t.Errorf("Country = %q, want %q", p1.Country, "srb")
	}
	if p1.Ranking == nil || *p1.Ranking != 1036 {
		t.Errorf("P1 ranking = %v, want 1036", p1.Ranking)
	}
	if p1.RankingMovement != "down" {
		t.Errorf("RankingMovement = %q, want down", p1.RankingMovement)
	}
	// P1's hand and backhand are genuinely unknown.
	if p1.Hand != "" || p1.Backhand != nil {
		t.Errorf("P1 hand/backhand should be unset, got %q / %v", p1.Hand, p1.Backhand)
	}
	if p2.Backhand == nil || *p2.Backhand != 2 {
		t.Errorf("P2 backhand = %v, want 2", p2.Backhand)
	}
	if got := p1.Birthday.Format("2006-01-02"); got != "2006-08-05" {
		t.Errorf("P1 birthday = %s, want 2006-08-05", got)
	}

	// A list response carries no stats block.
	if p1.Stats != nil {
		t.Error("Stats should be nil on a list response")
	}
	// FREE tier: the ULTRA model fields are absent.
	if match.Score.WinProbabilityP1 != nil || match.Score.Danger != nil {
		t.Error("ULTRA model fields should be nil on a FREE-tier response")
	}
	// A live match has no winner yet, and the key is absent entirely.
	if match.Winner != nil {
		t.Errorf("Winner = %d, want nil", *match.Winner)
	}
}

// data_completeness went undocumented until recently. It is present on every
// player in a match payload.
func TestRecordedDataCompleteness(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "matches_live.json")))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}

	p1 := page.Data[0].Players.P1
	if p1.DataCompleteness == nil {
		t.Fatal("DataCompleteness missing on a match player")
	}
	dc := p1.DataCompleteness
	if !dc.Applicable() {
		t.Fatal("completeness should apply to a singles player")
	}
	if *dc.Known != 3 || *dc.Of != 5 {
		t.Errorf("completeness = %d of %d, want 3 of 5", *dc.Known, *dc.Of)
	}
	want := []string{"backhand", "hand"}
	if len(dc.Missing) != len(want) {
		t.Fatalf("Missing = %v, want %v", dc.Missing, want)
	}
	for i := range want {
		if dc.Missing[i] != want[i] {
			t.Errorf("Missing[%d] = %q, want %q", i, dc.Missing[i], want[i])
		}
	}
	if dc.Complete() {
		t.Error("Complete() = true, want false when fields are missing")
	}
	// The missing names must agree with the fields that actually decoded empty.
	if p1.Hand != "" || p1.Backhand != nil {
		t.Error("hand/backhand are listed as missing but decoded populated")
	}

	// The opponent's biography is complete.
	p2 := page.Data[0].Players.P2
	if p2.DataCompleteness == nil || !p2.DataCompleteness.Complete() {
		t.Errorf("P2 completeness = %+v, want complete", p2.DataCompleteness)
	}
	if len(p2.DataCompleteness.Missing) != 0 {
		t.Errorf("P2 Missing = %v, want empty", p2.DataCompleteness.Missing)
	}

	// A nil pointer must not panic.
	var absent *DataCompleteness
	if absent.Complete() || absent.Applicable() {
		t.Error("nil DataCompleteness should report neither complete nor applicable")
	}
}

// A doubles team has no single biography, so the API sends known and of as
// null with an explanatory note. Decoding those nulls as 0 would claim the
// team has "0 of 0 fields known", which reads as real data and is not.
func TestRecordedDoublesTeamCompletenessIsNotApplicable(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "matches_doubles.json")))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{Tour: TourATP})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}

	var teams, singles int
	for _, match := range page.Data {
		for _, player := range []*Player{match.Players.P1, match.Players.P2} {
			if player == nil {
				continue
			}
			dc := player.DataCompleteness
			if dc == nil {
				t.Fatalf("player %d has no data_completeness", player.ID)
			}

			if !player.IsDoublesTeam {
				singles++
				if !dc.Applicable() {
					t.Errorf("player %d: completeness should apply to an individual", player.ID)
				}
				continue
			}

			teams++
			if dc.Applicable() {
				t.Errorf("team %d: completeness should not apply to a doubles team", player.ID)
			}
			if dc.Known != nil || dc.Of != nil {
				t.Errorf("team %d: known/of = %v/%v, want nil — null is not zero", player.ID, dc.Known, dc.Of)
			}
			if dc.Complete() {
				t.Errorf("team %d: must never report complete", player.ID)
			}
			if dc.Note == "" {
				t.Errorf("team %d: the API's explanatory note was dropped", player.ID)
			}
			// A doubles team also has no individual ranking.
			if player.Ranking != nil {
				t.Errorf("team %d: Ranking = %d, want nil", player.ID, *player.Ranking)
			}
		}
	}

	// The recording must actually contain both kinds, or this proves nothing.
	if teams == 0 || singles == 0 {
		t.Fatalf("recording has %d teams and %d individuals, want both", teams, singles)
	}
}

// The tour filter returns doubles draws alongside singles, and the tour string
// on a doubles team comes back upper-case where an individual's is lower-case.
// Another reason Player.Tour is not typed.
func TestRecordedTourCoversDoublesDraws(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "matches_doubles.json")))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{Tour: TourATP})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}

	var sawDoubles, sawSingles bool
	for _, match := range page.Data {
		if match.IsDoubles {
			sawDoubles = true
		} else {
			sawSingles = true
		}
	}
	if !sawDoubles || !sawSingles {
		t.Errorf("tour=atp returned doubles=%v singles=%v, want both", sawDoubles, sawSingles)
	}

	// Case differs between the two, which is exactly why this stays a string.
	if got := page.Data[0].Players.P1.Tour; got != "ATP" {
		t.Errorf("doubles team tour = %q, want the recorded %q", got, "ATP")
	}
}

// Points are strings. Parsing them as integers is impossible ("AD") and
// pointless ("40" is not 40), so the type must stay []string.
func TestRecordedScorePointsAreStrings(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "score.json")))

	score, err := client.GetMatchScore(t.Context(), 21635)
	if err != nil {
		t.Fatalf("GetMatchScore: %v", err)
	}

	want := []string{"30", "15"}
	if len(score.Points) != len(want) {
		t.Fatalf("Points = %v, want %v", score.Points, want)
	}
	for i := range want {
		if score.Points[i] != want[i] {
			t.Errorf("Points[%d] = %q, want %q", i, score.Points[i], want[i])
		}
	}
	if s := score.Server; s == nil || *s != 1 {
		t.Errorf("Server = %v, want 1", s)
	}
	if score.IsTiebreak {
		t.Error("IsTiebreak = true, want false")
	}
	// Recorded timestamps carry microsecond precision.
	wantTime := time.Date(2026, 7, 22, 16, 1, 27, 20135000, time.UTC)
	if !score.Timestamp.Equal(wantTime) {
		t.Errorf("Timestamp = %v, want %v", score.Timestamp.Time, wantTime)
	}
}

// Games is player-major: [games_p1, games_p2], each a per-set list. The
// recorded score reads 7-5, 6-5.
func TestRecordedScoreGamesArePlayerMajor(t *testing.T) {
	var score Score
	if err := json.Unmarshal(fixture(t, "score.json"), &score); err != nil {
		t.Fatalf("decoding score: %v", err)
	}

	if got := score.NumSets(); got != 2 {
		t.Fatalf("NumSets = %d, want 2", got)
	}

	tests := []struct {
		setIndex int
		p1, p2   int
		wantOK   bool
	}{
		{setIndex: 0, p1: 7, p2: 5, wantOK: true},
		{setIndex: 1, p1: 6, p2: 5, wantOK: true},
		{setIndex: 2, wantOK: false},
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

	if got, want := score.String(), "7-5 6-5 (30-15)"; got != want {
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

// A completed match carries a derived winner, and no server: nobody is serving
// once the match is over. Both confirmed against a real recording.
func TestRecordedCompletedMatch(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "matches_completed.json")))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{Status: StatusCompleted})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if page.Len() != 2 {
		t.Fatalf("page length = %d, want 2", page.Len())
	}

	decided := page.Data[1]
	if decided.ID != 21821 {
		t.Fatalf("ID = %d, want 21821", decided.ID)
	}
	if w := decided.Winner; w == nil || *w != 1 {
		t.Errorf("Winner = %v, want 1", w)
	}
	if decided.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", decided.Status)
	}
	// server is genuinely null here, which is why it is a pointer.
	if decided.Score.Server != nil {
		t.Errorf("Server = %d, want nil on a completed match", *decided.Score.Server)
	}
	if got, want := decided.Score.String(), "6-1 7-5 (40-40)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	// A completed match can still lack a winner: this one has no winner key at
	// all, and nil must not be mistaken for player 0.
	undecided := page.Data[0]
	if undecided.Status != StatusCompleted {
		t.Errorf("Status = %q, want completed", undecided.Status)
	}
	if undecided.Winner != nil {
		t.Errorf("Winner = %d, want nil when the API omits it", *undecided.Winner)
	}
}

func TestRecordedPlayerStatsAreRawJSON(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "player.json")))

	player, err := client.GetPlayer(t.Context(), 2317)
	if err != nil {
		t.Fatalf("GetPlayer: %v", err)
	}
	if player.ID != 2317 || player.Name != "Vlado Jankanj" {
		t.Errorf("player = %d %q", player.ID, player.Name)
	}
	if player.Stats == nil {
		t.Fatal("Stats missing on the single-player endpoint")
	}

	// The real ratings payload is deeply nested and unpinned by the schema,
	// which is exactly why it is kept as raw JSON rather than typed.
	var ratings struct {
		MatchCount int      `json:"match_count"`
		Elo        *float64 `json:"elo"`
		Meta       struct {
			Country  string `json:"country"`
			PeakRank int    `json:"peak_rank"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(player.Stats.Ratings, &ratings); err != nil {
		t.Fatalf("decoding ratings: %v", err)
	}
	if ratings.MatchCount != 25 {
		t.Errorf("match_count = %d, want 25", ratings.MatchCount)
	}
	if ratings.Elo != nil {
		t.Errorf("elo = %v, want nil", *ratings.Elo)
	}
	if ratings.Meta.Country != "SRB" || ratings.Meta.PeakRank != 1044 {
		t.Errorf("ratings meta = %+v", ratings.Meta)
	}

	// The season array holds its numbers as strings, and sometimes as "".
	// Another reason not to type this half.
	var season []map[string]string
	if err := json.Unmarshal(player.Stats.Season, &season); err != nil {
		t.Fatalf("decoding season: %v", err)
	}
	if len(season) == 0 {
		t.Fatal("season should be preserved")
	}
	if season[len(season)-1]["season"] != "2025" {
		t.Errorf("last season = %q, want 2025", season[len(season)-1]["season"])
	}
}

func TestRecordedPlayerSearchHasNoStats(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "players_search.json")))

	page, err := client.SearchPlayers(t.Context(), SearchPlayersParams{Search: "alcaraz"})
	if err != nil {
		t.Fatalf("SearchPlayers: %v", err)
	}
	if page.Len() != 3 {
		t.Fatalf("results = %d, want 3", page.Len())
	}

	first := page.Data[0]
	if first.ID != 13 || first.Name != "Carlos Alcaraz" {
		t.Errorf("first result = %d %q", first.ID, first.Name)
	}
	if r := first.Ranking; r == nil || *r != 2 {
		t.Errorf("Ranking = %v, want 2", r)
	}
	if rp := first.RankingPoints; rp == nil || *rp != 12960 {
		t.Errorf("RankingPoints = %v, want 12960", rp)
	}
	// The search endpoint never carries stats, on any result.
	for _, player := range page.Data {
		if player.Stats != nil {
			t.Errorf("player %d carries stats on a search response", player.ID)
		}
	}
	if page.Meta.Count != 3 || page.Meta.Limit != 3 {
		t.Errorf("meta = %+v", page.Meta)
	}
}

func TestRecordedFixtures(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "fixtures.json")))

	page, err := client.ListFixtures(t.Context(), ListFixturesParams{})
	if err != nil {
		t.Fatalf("ListFixtures: %v", err)
	}

	f := page.Data[0]
	if f.ID != 87 {
		t.Errorf("ID = %d, want 87", f.ID)
	}
	if f.Player1Name != "M. Kessler" || f.Player2Name != "I. Jovic" {
		t.Errorf("names = %q / %q", f.Player1Name, f.Player2Name)
	}
	if f.Tour != "wta" || f.Surface != "clay" {
		t.Errorf("tour/surface = %q / %q", f.Tour, f.Surface)
	}
	if got := f.EventDate.Format("2006-01-02"); got != "2026-05-07" {
		t.Errorf("EventDate = %s, want 2026-05-07", got)
	}
}

// Filtering by the single tour value "juniors" returns records whose own tour
// field reads "juniors_boys" or "juniors_girls". The filter vocabulary and the
// response vocabulary are not the same, which is why Fixture.Tour is a plain
// string and not a Tour.
func TestRecordedJuniorsTourValueDiffersFromTheFilter(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "fixtures_tour_juniors.json")))

	page, err := client.ListFixtures(t.Context(), ListFixturesParams{Tour: TourJuniors})
	if err != nil {
		t.Fatalf("ListFixtures: %v", err)
	}
	if page.Len() == 0 {
		t.Fatal("no fixtures in the recording")
	}

	for _, f := range page.Data {
		if f.Tour == string(TourJuniors) {
			t.Errorf("fixture %d reports tour %q; the recording shows the API "+
				"splits juniors into boys/girls in responses", f.ID, f.Tour)
		}
		if f.Tour != "juniors_boys" && f.Tour != "juniors_girls" {
			t.Errorf("fixture %d tour = %q, want a juniors_* value", f.ID, f.Tour)
		}
	}
}

func TestRecordedTourFilteredMatches(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "matches_tour_wta.json")))

	page, err := client.ListMatches(t.Context(), ListMatchesParams{Tour: TourWTA})
	if err != nil {
		t.Fatalf("ListMatches: %v", err)
	}
	if page.Len() != 2 {
		t.Fatalf("matches = %d, want 2", page.Len())
	}
	for _, match := range page.Data {
		for _, player := range []*Player{match.Players.P1, match.Players.P2} {
			if player == nil {
				continue
			}
			if player.Tour != string(TourWTA) {
				t.Errorf("match %d has a %q player in a wta-filtered page", match.ID, player.Tour)
			}
		}
	}
}

// The match-detail endpoint on a FREE key omits market and analysis entirely
// rather than sending them as null.
func TestRecordedMatchDetailOmitsGatedEmbeds(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "match_detail.json")))

	match, err := client.GetMatch(t.Context(), 21635)
	if err != nil {
		t.Fatalf("GetMatch: %v", err)
	}
	if match.ID != 21635 {
		t.Errorf("ID = %d, want 21635", match.ID)
	}
	if match.Market != nil {
		t.Error("Market should be absent below PRO")
	}
	if match.Analysis != nil {
		t.Error("Analysis should be absent below ULTRA")
	}
	if match.Score == nil {
		t.Fatal("a live match should carry a score")
	}
}

func TestRecordedHealth(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "health.json")))

	health, err := client.Health(t.Context())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if health.Status != "ok" || health.Version != "v1" {
		t.Errorf("health = %+v", health)
	}
}

// --- synthetic fixtures: PRO/ULTRA payloads a FREE key cannot record --------

func TestSyntheticMarketAndAnalysisDecoding(t *testing.T) {
	t.Run("market prices", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/market_prices.json")))

		market, err := client.GetMarketPrices(t.Context(), 21635, ListParams{})
		if err != nil {
			t.Fatalf("GetMarketPrices: %v", err)
		}
		if market.Liquidity != nil {
			t.Error("a null liquidity should stay nil")
		}
		if len(market.Prices) != 2 {
			t.Fatalf("prices = %d, want 2", len(market.Prices))
		}
		// A bid of 0 means nobody will buy; an absent ask means no quote.
		second := market.Prices[1]
		if second.Bid == nil || *second.Bid != 0 {
			t.Errorf("Bid = %v, want a pointer to 0", second.Bid)
		}
		if second.Ask != nil {
			t.Errorf("Ask = %v, want nil", *second.Ask)
		}
	})

	t.Run("analysis", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/analysis.json")))

		analysis, err := client.GetMatchAnalysis(t.Context(), 21635)
		if err != nil {
			t.Fatalf("GetMatchAnalysis: %v", err)
		}
		if analysis.Thesis == nil || analysis.Profile == nil {
			t.Fatal("analysis halves missing")
		}
		if side := analysis.Thesis.PickSide; side == nil || *side != 2 {
			t.Errorf("PickSide = %v, want 2", side)
		}
		if analysis.Thesis.Notes.Matchup == "" {
			t.Error("thesis notes lost")
		}
		if analysis.Thesis.Notes.Fatigue != "" {
			t.Error("a null note should decode to empty")
		}
		if len(analysis.Thesis.ScenarioPlaybook) == 0 {
			t.Error("scenario_playbook should be preserved as raw JSON")
		}
		if analysis.Profile.VolatilityRating != "high" {
			t.Errorf("VolatilityRating = %q, want high", analysis.Profile.VolatilityRating)
		}
		if len(analysis.Profile.KeyFactors) != 2 {
			t.Errorf("KeyFactors = %v, want 2 entries", analysis.Profile.KeyFactors)
		}
	})

	t.Run("uncovered analysis is both-null", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/analysis_uncovered.json")))

		analysis, err := client.GetMatchAnalysis(t.Context(), 1)
		if err != nil {
			t.Fatalf("GetMatchAnalysis: %v", err)
		}
		if analysis.Thesis != nil || analysis.Profile != nil {
			t.Errorf("expected both halves nil, got %+v", analysis)
		}
	})

	t.Run("events", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/events.json")))

		page, err := client.ListMatchEvents(t.Context(), 21635, ListParams{})
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
		if page.Data[2].Player != nil {
			t.Error("a null player should decode to nil, not 0")
		}
	})

	t.Run("markets meta carries match id", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/markets.json")))

		page, err := client.ListMarkets(t.Context(), 21635)
		if err != nil {
			t.Fatalf("ListMarkets: %v", err)
		}
		if page.Meta.MatchID != 21635 {
			t.Errorf("Meta.MatchID = %d, want 21635", page.Meta.MatchID)
		}
	})
}

// --- time handling ----------------------------------------------------------

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
		{name: "recorded microseconds", json: `"2026-07-22T15:59:47.592289Z"`, wantRaw: "2026-07-22T15:59:47.592289Z", wantUTC: "2026-07-22T15:59:47Z"},
		{name: "offset is normalised to UTC", json: `"2026-07-22T18:31:28+03:00"`, wantRaw: "2026-07-22T18:31:28+03:00", wantUTC: "2026-07-22T15:31:28Z"},
		{name: "date only", json: `"2006-08-05"`, wantRaw: "2006-08-05", wantUTC: "2006-08-05T00:00:00Z"},
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
	const raw = `"2026-07-22T15:59:47.592289Z"`

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
	client := newTestClient(t, serveBody(
		[]byte(`{"data":[{"id":1,"tournament":"Odd Open","scheduled_time":"soon"}],"meta":{"count":1}}`)))

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

// EventStatusUpdatedAt (added 2026-08-19) is the instant the current
// EventStatus was recorded. Set, it decodes as a UTC timestamp; null or
// absent — every match from before the field was introduced — stays zero,
// because it is never backfilled.
func TestEventStatusUpdatedAtDecoding(t *testing.T) {
	var stamped Match
	err := json.Unmarshal([]byte(
		`{"id":1,"event_status":"Walk Over","event_status_updated_at":"2026-08-19T09:15:00Z"}`), &stamped)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := time.Date(2026, 8, 19, 9, 15, 0, 0, time.UTC)
	if !stamped.EventStatusUpdatedAt.Equal(want) {
		t.Errorf("EventStatusUpdatedAt = %v, want %v", stamped.EventStatusUpdatedAt, want)
	}

	var nulled Match
	if err := json.Unmarshal([]byte(`{"id":2,"event_status_updated_at":null}`), &nulled); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !nulled.EventStatusUpdatedAt.IsZero() {
		t.Errorf("null EventStatusUpdatedAt = %v, want zero", nulled.EventStatusUpdatedAt)
	}

	var absent Match
	if err := json.Unmarshal([]byte(`{"id":3}`), &absent); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !absent.EventStatusUpdatedAt.IsZero() {
		t.Errorf("absent EventStatusUpdatedAt = %v, want zero", absent.EventStatusUpdatedAt)
	}
}

// HasAnalysis and HasMarket (added 2026-09-02) carry, on every list row and
// the detail, the same fact GetMatchAnalysis / GetMarketPrices answer 404
// no_analysis / no_market about. Both booleans must survive as given — true
// and false alike — and stay nil, not false, when an older server omits them.
func TestHasAnalysisHasMarketDecoding(t *testing.T) {
	var covered Match
	err := json.Unmarshal([]byte(`{"id":1,"has_analysis":true,"has_market":true}`), &covered)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if covered.HasAnalysis == nil || !*covered.HasAnalysis {
		t.Errorf("HasAnalysis = %v, want true", covered.HasAnalysis)
	}
	if covered.HasMarket == nil || !*covered.HasMarket {
		t.Errorf("HasMarket = %v, want true", covered.HasMarket)
	}

	var bare Match
	if err := json.Unmarshal([]byte(`{"id":2,"has_analysis":false,"has_market":false}`), &bare); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if bare.HasAnalysis == nil || *bare.HasAnalysis {
		t.Errorf("HasAnalysis = %v, want false (present, not nil)", bare.HasAnalysis)
	}
	if bare.HasMarket == nil || *bare.HasMarket {
		t.Errorf("HasMarket = %v, want false (present, not nil)", bare.HasMarket)
	}

	var absent Match
	if err := json.Unmarshal([]byte(`{"id":3}`), &absent); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if absent.HasAnalysis != nil {
		t.Errorf("absent HasAnalysis = %v, want nil", *absent.HasAnalysis)
	}
	if absent.HasMarket != nil {
		t.Errorf("absent HasMarket = %v, want nil", *absent.HasMarket)
	}
}

// The tape carries the fields backtesters live on: per-set tiebreak scores,
// the clean-sequence point winner, and the null timestamp that marks a
// reconstructed row. All three must survive decoding exactly.
func TestTapeDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/tape.json")))

	tape, err := client.GetMatchTape(t.Context(), 21635, TapeParams{Sequence: SequenceClean})
	if err != nil {
		t.Fatalf("GetMatchTape: %v", err)
	}

	// The match header carries the new identity fields.
	m := tape.Match
	if m.Tour != TourITF {
		t.Errorf("Match.Tour = %q, want itf — and typed, so this compares against the constant", m.Tour)
	}
	if m.TournamentID != "itf-m15-kursumlijska-banja-m" {
		t.Errorf("TournamentID = %q", m.TournamentID)
	}
	if m.RoundCode != "R32" {
		t.Errorf("RoundCode = %q, want R32", m.RoundCode)
	}
	if m.Withdrew != nil {
		t.Errorf("Withdrew = %v, want nil on a normally-completed match", *m.Withdrew)
	}
	if m.HasAnalysis == nil || !*m.HasAnalysis {
		t.Errorf("HasAnalysis = %v, want true from the fixture", m.HasAnalysis)
	}
	if m.HasMarket == nil || *m.HasMarket {
		t.Errorf("HasMarket = %v, want false from the fixture", m.HasMarket)
	}

	if len(tape.Tape) != 3 {
		t.Fatalf("tape rows = %d, want 3", len(tape.Tape))
	}

	// Row 0 is reconstructed: zero timestamp, nil model fields, nil winner
	// (the first row has no attributable transition).
	first := tape.Tape[0]
	if !first.Timestamp.IsZero() {
		t.Errorf("reconstructed row Timestamp = %v, want zero", first.Timestamp)
	}
	if first.WinProbabilityP1 != nil || first.Danger != nil {
		t.Error("reconstructed row model fields should be nil")
	}
	if first.PointWinner != nil {
		t.Errorf("first row PointWinner = %d, want nil", *first.PointWinner)
	}

	// Row 1 was observed: real timestamp, model fields, and a point winner.
	second := tape.Tape[1]
	if second.Timestamp.IsZero() {
		t.Error("observed row should carry a real timestamp")
	}
	if second.PointWinner == nil || *second.PointWinner != 1 {
		t.Errorf("PointWinner = %v, want 1", second.PointWinner)
	}
	if second.WinProbabilityP1 == nil || *second.WinProbabilityP1 != 0.52 {
		t.Errorf("WinProbabilityP1 = %v, want 0.52", second.WinProbabilityP1)
	}

	// The embedded Score keeps its helpers: the last row is 6-4 6-6 mid-tiebreak.
	if p1, p2, ok := tape.Tape[2].GamesForSet(1); !ok || p1 != 6 || p2 != 6 {
		t.Errorf("GamesForSet(1) = %d-%d ok=%v, want 6-6 true", p1, p2, ok)
	}
	if !tape.Tape[2].IsTiebreak {
		t.Error("last row should be in a tiebreak")
	}

	// Tiebreaks align to sets: none in set 1, 7-5 in set 2. A set with no
	// breaker is nil, not zero-zero.
	if len(tape.Tiebreaks) != 2 {
		t.Fatalf("tiebreaks = %d entries, want 2", len(tape.Tiebreaks))
	}
	if tape.Tiebreaks[0] != nil {
		t.Errorf("set 1 tiebreak = %+v, want nil", tape.Tiebreaks[0])
	}
	if tb := tape.Tiebreaks[1]; tb == nil || tb.P1 != 7 || tb.P2 != 5 {
		t.Errorf("set 2 tiebreak = %+v, want 7-5", tb)
	}

	if len(tape.Profiles) != 1 || tape.Profiles[0].VolatilityRating != "med" {
		t.Errorf("profiles = %+v, want one with volatility med", tape.Profiles)
	}

	meta := tape.Meta
	if meta.Coverage != CoverageReconstructed || meta.PointSource != "mixed" {
		t.Errorf("coverage/point_source = %q/%q, want reconstructed/mixed", meta.Coverage, meta.PointSource)
	}
	if meta.Sequence != SequenceClean || meta.RawRows != 210 || meta.UniqueStates != 184 {
		t.Errorf("meta = %+v", meta)
	}
}

func TestHeadToHeadDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/h2h.json")))

	h2h, err := client.GetHeadToHead(t.Context(), "nadal", "djokovic")
	if err != nil {
		t.Fatalf("GetHeadToHead: %v", err)
	}

	if h2h.Players == nil || h2h.Players.P1.Name != "Rafael Nadal" {
		t.Fatalf("players = %+v", h2h.Players)
	}
	// Undecided meetings are counted apart, never folded into the wins.
	if h2h.Totals.P1Wins+h2h.Totals.P2Wins+h2h.Totals.Undecided != h2h.Totals.Meetings {
		t.Errorf("totals do not add up: %+v", h2h.Totals)
	}
	if split := h2h.BySurface["clay"]; split.P1 != 20 || split.P2 != 9 {
		t.Errorf("clay split = %+v, want 20-9", split)
	}

	if len(h2h.Meetings) != 4 {
		t.Fatalf("meetings = %d, want 4", len(h2h.Meetings))
	}
	current := h2h.Meetings[0]
	if current.Era != "current" || current.MatchID == nil || *current.MatchID != 31882 {
		t.Errorf("current meeting = %+v, want era current with match_id 31882", current)
	}
	if current.ArchiveMatchID != nil {
		t.Error("a current meeting carries no archive_match_id")
	}
	archive := h2h.Meetings[1]
	if archive.Era != "archive" || archive.ArchiveMatchID == nil || *archive.ArchiveMatchID != 1447213 {
		t.Errorf("archive meeting = %+v", archive)
	}
	if archive.Score != "6-2 4-6 6-2 7-6(4)" || archive.Level != "G" {
		t.Errorf("archive meeting score/level = %q/%q", archive.Score, archive.Level)
	}

	// The walkover stays in the record with a nil winner and its outcome
	// named, so a consumer can exclude it without losing it.
	walkover := h2h.Meetings[3]
	if walkover.Outcome != "walkover" || walkover.Winner != nil {
		t.Errorf("walkover = %+v, want outcome walkover and nil winner", walkover)
	}

	// The ULTRA stats block is unpinned, but must survive verbatim.
	if len(h2h.Stats) == 0 {
		t.Error("Stats should carry the raw ULTRA block")
	}
}

func TestArchiveDecoding(t *testing.T) {
	t.Run("listing", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/archive_matches.json")))

		page, err := client.ListArchiveMatches(t.Context(), ArchiveMatchesParams{})
		if err != nil {
			t.Fatalf("ListArchiveMatches: %v", err)
		}
		if page.Len() != 2 {
			t.Fatalf("page length = %d, want 2", page.Len())
		}

		// The new envelope fields: total and has_more.
		if page.Meta.Total == nil || *page.Meta.Total != 1485752 {
			t.Errorf("Total = %v, want 1485752", page.Meta.Total)
		}
		if page.Meta.HasMore == nil || !*page.Meta.HasMore {
			t.Errorf("HasMore = %v, want true", page.Meta.HasMore)
		}

		modern := page.Data[0]
		if modern.Winner == nil || modern.Winner.Name != "Rafael Nadal" {
			t.Fatalf("winner = %+v", modern.Winner)
		}
		// Rank is the rank AT THE TIME, and the corpus person id is not a
		// roster id.
		if modern.Winner.Rank == nil || *modern.Winner.Rank != 5 {
			t.Errorf("winner rank = %v, want 5", modern.Winner.Rank)
		}
		if modern.Winner.PlayerID == nil || *modern.Winner.PlayerID != 104745 {
			t.Errorf("winner player_id = %v, want 104745", modern.Winner.PlayerID)
		}
		if modern.Stats != nil {
			t.Error("the listing carries no stats block")
		}

		// A 1973 row: the era's silence decodes as nil, and a retirement is
		// parsed from the score's own vocabulary.
		old := page.Data[1]
		if old.Minutes != nil || old.Loser.Age != nil || old.Loser.Rank != nil {
			t.Errorf("1973 row should be silent where the era was: %+v", old)
		}
		if old.Outcome != "retired" || old.Score != "6-3 RET" {
			t.Errorf("outcome/score = %q/%q", old.Outcome, old.Score)
		}
		if old.Loser.Entry != "Q" {
			t.Errorf("loser entry = %q, want Q", old.Loser.Entry)
		}
	})

	t.Run("detail carries stats", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/archive_match.json")))

		match, err := client.GetArchiveMatch(t.Context(), 1447213)
		if err != nil {
			t.Fatalf("GetArchiveMatch: %v", err)
		}
		if match.Stats == nil || match.Stats.Winner == nil {
			t.Fatal("detail should carry the stats block")
		}
		if match.Stats.Winner.Aces == nil || *match.Stats.Winner.Aces != 3 {
			t.Errorf("winner aces = %v, want 3", match.Stats.Winner.Aces)
		}
		if match.Stats.Loser.BPFaced == nil || *match.Stats.Loser.BPFaced != 13 {
			t.Errorf("loser bp_faced = %v, want 13", match.Stats.Loser.BPFaced)
		}
	})

	t.Run("players", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/archive_players.json")))

		page, err := client.ListArchivePlayers(t.Context(), ArchivePlayersParams{})
		if err != nil {
			t.Fatalf("ListArchivePlayers: %v", err)
		}
		nadal := page.Data[0]
		if nadal.CareerHighRank == nil || *nadal.CareerHighRank != 1 {
			t.Errorf("career high = %v, want 1", nadal.CareerHighRank)
		}
		if nadal.CareerHighDate.IsZero() {
			t.Error("career high date should parse")
		}
		// Nulls are the era's silence, never zeros.
		sparse := page.Data[1]
		if sparse.HeightCm != nil || sparse.CareerHighRank != nil || !sparse.DOB.IsZero() {
			t.Errorf("sparse bio should stay nil/zero: %+v", sparse)
		}
	})

	t.Run("career", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/archive_career.json")))

		career, err := client.GetArchiveCareer(t.Context(), "nadal")
		if err != nil {
			t.Fatalf("GetArchiveCareer: %v", err)
		}
		if career.Player.Name != "Rafael Nadal" {
			t.Errorf("name = %q", career.Player.Name)
		}
		if career.Record.Wins != 1068 || career.Record.Titles != 92 {
			t.Errorf("record = %+v", career.Record)
		}
		if clay := career.Record.BySurface["clay"]; clay.Wins != 474 {
			t.Errorf("clay record = %+v", clay)
		}
		if len(career.ByYear) != 2 || career.ByYear[0].Year != 2005 {
			t.Errorf("by_year = %+v", career.ByYear)
		}
		// Coverage is stated, not implied: serve stats exist from 1991 only.
		if career.Serve.MatchesWithStats != 1233 {
			t.Errorf("matches_with_stats = %d", career.Serve.MatchesWithStats)
		}
		if career.Serve.FirstInPct == nil || *career.Serve.FirstInPct != 0.678 {
			t.Errorf("first_in_pct = %v", career.Serve.FirstInPct)
		}
	})
}

func TestRankingsDecoding(t *testing.T) {
	t.Run("listing mode", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/rankings_listing.json")))

		page, err := client.ListRankings(t.Context(), RankingsParams{System: []RankingSystem{RankingATP}})
		if err != nil {
			t.Fatalf("ListRankings: %v", err)
		}
		if page.Len() != 2 {
			t.Fatalf("page length = %d, want 2", page.Len())
		}

		first := page.Data[0]
		if first.Rank == nil || *first.Rank != 1 {
			t.Errorf("rank = %v, want 1", first.Rank)
		}
		if first.PreviousRank == nil || *first.PreviousRank != 2 {
			t.Errorf("previous_rank = %v, want 2", first.PreviousRank)
		}

		// A listing row for a player outside the roster keeps its published
		// name and a nil id — the table has no silent holes.
		outsider := page.Data[1]
		if outsider.PlayerID != nil {
			t.Errorf("outsider player_id = %v, want nil", outsider.PlayerID)
		}
		if outsider.PlayerName != "Invented Outsider" {
			t.Errorf("outsider name = %q", outsider.PlayerName)
		}

		coverage := page.Meta.Coverage
		if len(coverage.SystemsResolved) != 1 || coverage.SystemsResolved[0] != "atp" {
			t.Errorf("systems_resolved = %v", coverage.SystemsResolved)
		}
		if oldest, ok := coverage.OldestAvailable["atp"]; !ok || oldest.IsZero() {
			t.Errorf("oldest_available[atp] = %v", oldest)
		}
		if page.Meta.HasMore == nil || !*page.Meta.HasMore {
			t.Errorf("HasMore = %v, want true", page.Meta.HasMore)
		}
	})

	t.Run("per-player mode never collapses systems", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/rankings_players.json")))

		page, err := client.ListRankings(t.Context(), RankingsParams{Player: []int64{2317}})
		if err != nil {
			t.Fatalf("ListRankings: %v", err)
		}

		atp, utr := page.Data[0], page.Data[1]
		if atp.System != RankingATP || atp.Rank == nil || atp.Points == nil {
			t.Errorf("atp record = %+v", atp)
		}
		// UTR is a rating: rank and points are genuinely null, not zero.
		if utr.System != RankingUTR {
			t.Errorf("system = %q, want utr", utr.System)
		}
		if utr.Rank != nil || utr.Points != nil || utr.PreviousRank != nil {
			t.Errorf("UTR rank/points should be nil: %+v", utr)
		}
		if utr.Rating == nil || *utr.Rating != 16.21 {
			t.Errorf("rating = %v, want 16.21", utr.Rating)
		}
	})
}

func TestMatchStatisticsDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/statistics.json")))

	stats, err := client.GetMatchStatistics(t.Context(), 21635)
	if err != nil {
		t.Fatalf("GetMatchStatistics: %v", err)
	}

	if stats.Coverage != "live" || stats.GamesCounted != 17 {
		t.Errorf("coverage/games = %q/%d", stats.Coverage, stats.GamesCounted)
	}
	if stats.TiebreakGamesExcluded != 1 {
		t.Errorf("tiebreak_games_excluded = %d, want 1", stats.TiebreakGamesExcluded)
	}

	// Each family carries its own coverage and age, on different clocks.
	if stats.Freshness.Derived == nil || stats.Freshness.Measured == nil {
		t.Fatal("both freshness families should be present")
	}
	if stats.Freshness.MeasuredDivergence != nil {
		t.Error("the families agree, so measured_divergence should be nil")
	}
	if got := stats.Freshness.Measured.AgeSeconds; got == nil || *got != 35 {
		t.Errorf("measured age = %v, want 35", got)
	}
	if d := stats.Freshness.Derived.Describes; d == nil || d.TotalGames != 17 {
		t.Errorf("derived describes = %+v", d)
	}

	if stats.Players == nil || stats.Players.P1 == nil || stats.Players.P2 == nil {
		t.Fatal("players missing")
	}
	p1, p2 := stats.Players.P1, stats.Players.P2

	// Derived fields are typed.
	if p1.HoldPct == nil || *p1.HoldPct != 89 {
		t.Errorf("p1 hold_pct = %v, want 89", p1.HoldPct)
	}
	if p1.BreakPointsConverted != 2 || p1.PointsWon != 58 {
		t.Errorf("p1 derived = %+v", p1)
	}

	// Measured is a map keyed by what was actually measured: p1's fixture
	// carries the serve split, p2's only the tier-1 fields. An absent key is
	// absent, and a present 0 is a real measured zero.
	if got := p1.Measured["aces"]; got != 4 {
		t.Errorf("p1 measured aces = %d, want 4", got)
	}
	if got, ok := p1.Measured["winners_total"]; !ok || got != 0 {
		t.Errorf("p1 winners_total = %d ok=%v, want a real measured 0", got, ok)
	}
	if _, ok := p2.Measured["first_serves_in"]; ok {
		t.Error("p2 has no serve split measured, so the key must be absent")
	}
	if got := p2.Measured["double_faults"]; got != 3 {
		t.Errorf("p2 double_faults = %d, want 3", got)
	}
}

func TestRallyAndChartingDecoding(t *testing.T) {
	t.Run("rally listing", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/rally_matches.json")))

		page, err := client.ListRallyMatches(t.Context(), RallyMatchesParams{})
		if err != nil {
			t.Fatalf("ListRallyMatches: %v", err)
		}

		linked := page.Data[0]
		if linked.MatchID == nil || *linked.MatchID != 31882 {
			t.Errorf("linked match_id = %v, want 31882", linked.MatchID)
		}
		// Most charted matches predate the API's own collection: no link.
		unlinked := page.Data[1]
		if unlinked.MatchID != nil {
			t.Errorf("1980 match_id = %v, want nil", unlinked.MatchID)
		}
		if unlinked.Points != 331 || unlinked.PointsParsed != 322 {
			t.Errorf("points = %d/%d", unlinked.Points, unlinked.PointsParsed)
		}
	})

	t.Run("rally detail", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/rally_match.json")))

		detail, err := client.GetRallyMatch(t.Context(), 118203, ListParams{Limit: 2})
		if err != nil {
			t.Fatalf("GetRallyMatch: %v", err)
		}
		if detail.RallyMatchID != 118203 || len(detail.Rally) != 2 {
			t.Fatalf("detail = %d rally rows for %d", len(detail.Rally), detail.RallyMatchID)
		}
		// Meta.Total is the match's full point count, not the page's.
		if detail.Meta.Total == nil || *detail.Meta.Total != 289 {
			t.Errorf("Total = %v, want 289", detail.Meta.Total)
		}

		clean := detail.Rally[0]
		if !clean.Parsed || clean.Outcome != "winner" || clean.ServeDirection != "wide" {
			t.Errorf("clean point = %+v", clean)
		}
		if clean.RallyLength == nil || *clean.RallyLength != 3 || len(clean.Shots) != 3 {
			t.Errorf("rally length/shots = %v/%d", clean.RallyLength, len(clean.Shots))
		}
		if s := clean.Shots[1]; s.Stroke != "groundstroke" || s.Wing != "forehand" || s.Direction != "backhand_side" {
			t.Errorf("shot 2 = %+v", s)
		}

		// The raw notation is always kept, even when parsing failed — and a
		// double fault has rally length 0.
		torn := detail.Rally[1]
		if torn.Parsed {
			t.Error("the second point should be marked unparsed")
		}
		if torn.Raw != "6;5xw@" {
			t.Errorf("raw = %q, want the charter's string verbatim", torn.Raw)
		}
		if !torn.IsDoubleFault || torn.RallyLength == nil || *torn.RallyLength != 0 {
			t.Errorf("double fault = %+v", torn)
		}
	})

	t.Run("charting player and match", func(t *testing.T) {
		client := newTestClient(t, serveBody(fixture(t, "synthetic/charting_player.json")))
		player, err := client.GetChartingPlayer(t.Context(), ChartingPlayerParams{Name: "invented", Gender: "men"})
		if err != nil {
			t.Fatalf("GetChartingPlayer: %v", err)
		}
		if player.MatchesCharted != 412 {
			t.Errorf("matches_charted = %d", player.MatchesCharted)
		}
		// The families are unpinned raw JSON, but must decode on demand.
		var families map[string]map[string]int
		if err := json.Unmarshal(player.Families, &families); err != nil {
			t.Fatalf("families should hold raw JSON: %v", err)
		}
		if families["serve_placement"]["deuce_t"] != 901 {
			t.Errorf("families = %+v", families)
		}

		client = newTestClient(t, serveBody(fixture(t, "synthetic/charting_match.json")))
		match, err := client.GetChartingMatch(t.Context(), 118203)
		if err != nil {
			t.Fatalf("GetChartingMatch: %v", err)
		}
		if match.ChartingMatchID != 118203 || match.MCPID == "" || match.Gender != "M" {
			t.Errorf("charting match = %+v", match)
		}
	})
}

func TestHistoryPackagesDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/history_packages.json")))

	page, err := client.ListHistoryPackages(t.Context(), HistoryPackagesParams{})
	if err != nil {
		t.Fatalf("ListHistoryPackages: %v", err)
	}
	if len(page.Data) != 3 || page.Meta.Count != 3 {
		t.Fatalf("packages = %d (count %d), want 3", len(page.Data), page.Meta.Count)
	}

	// A tape package carries no kind at all, so the zero value means tape.
	tape := page.Data[0]
	if tape.Kind != "" {
		t.Errorf("tape package kind = %q, want empty", tape.Kind)
	}
	if len(tape.Files) != 2 || tape.Files[0].Format != "jsonl" || tape.Files[0].SHA256 == "" {
		t.Errorf("tape files = %+v", tape.Files)
	}
	if tape.MatchCount == nil || *tape.MatchCount != 10412 {
		t.Errorf("match_count = %v", tape.MatchCount)
	}

	rankings := page.Data[1]
	if rankings.Kind != PackageRankings {
		t.Errorf("kind = %q, want rankings", rankings.Kind)
	}

	// An archive package is yearly: its period is the bare year.
	archive := page.Data[2]
	if archive.Kind != PackageArchive {
		t.Errorf("kind = %q, want archive", archive.Kind)
	}
	if archive.Period != "1999" {
		t.Errorf("archive period = %q, want a bare year", archive.Period)
	}
}

// The push-feed token names the channels, including the slate:all firehose.
func TestWSTokenDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/ws_token.json")))

	token, err := client.GetWSToken(t.Context())
	if err != nil {
		t.Fatalf("GetWSToken: %v", err)
	}
	if token.Token == "" || token.ExpiresIn != 300 {
		t.Errorf("token = %+v", token)
	}
	if token.WSURL != "wss://api.livetennisapi.com/connection/websocket" {
		t.Errorf("ws_url = %q", token.WSURL)
	}
	if token.Channels.Match != "match:{match_id}" {
		t.Errorf("match channel = %q", token.Channels.Match)
	}
	if token.Channels.Slate != "slate:all" {
		t.Errorf("slate channel = %q, want slate:all", token.Channels.Slate)
	}
}

func TestTournamentsDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/tournaments.json")))

	page, err := client.ListTournaments(t.Context(), TournamentsParams{})
	if err != nil {
		t.Fatalf("ListTournaments: %v", err)
	}
	if page.Len() != 2 {
		t.Fatalf("page length = %d, want 2", page.Len())
	}

	rg := page.Data[0]
	if rg.ID != "atp-roland-garros-m" {
		t.Errorf("ID = %q — tournament ids are strings, not integers", rg.ID)
	}
	if rg.Tour != TourATP {
		t.Errorf("Tour = %q, want the typed atp", rg.Tour)
	}
	if rg.Category != "grand_slam" || rg.City != "Paris" || rg.Country != "FR" {
		t.Errorf("curated fields = %q/%q/%q", rg.Category, rg.City, rg.Country)
	}

	// Uncurated fields stay empty, never guessed from the name.
	itf := page.Data[1]
	if itf.City != "" || itf.Country != "" {
		t.Errorf("uncurated city/country = %q/%q, want empty", itf.City, itf.Country)
	}

	client = newTestClient(t, serveBody(fixture(t, "synthetic/tournament.json")))
	one, err := client.GetTournament(t.Context(), "atp-roland-garros-m")
	if err != nil {
		t.Fatalf("GetTournament: %v", err)
	}
	if one.Name != "Roland Garros" {
		t.Errorf("Name = %q", one.Name)
	}
}

func TestUsageDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/usage.json")))

	usage, err := client.GetUsage(t.Context())
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}

	// A temporary grant: effective tier above the subscription, with an
	// expiry. The vocabulary is lowercase, the API's own.
	if usage.Tier != "pro" || usage.BaseTier != "basic" {
		t.Errorf("tier/base = %q/%q", usage.Tier, usage.BaseTier)
	}
	if usage.TierExpiresAt.IsZero() {
		t.Error("a grant carries its expiry")
	}
	if usage.Limits.PerDay == nil || *usage.Limits.PerDay != 10000 {
		t.Errorf("per_day = %v, want 10000", usage.Limits.PerDay)
	}
	if usage.Today.RemainingDay == nil || *usage.Today.RemainingDay != 9588 {
		t.Errorf("remaining_day = %v, want 9588", usage.Today.RemainingDay)
	}
	if len(usage.History) != 2 || usage.History[1].Errors != 12 {
		t.Errorf("history = %+v", usage.History)
	}
}

func TestMatchPricesDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/match_prices.json")))

	prices, err := client.ListMatchPrices(t.Context(), 21635, MatchPricesParams{Limit: 2, Minutes: 30})
	if err != nil {
		t.Fatalf("ListMatchPrices: %v", err)
	}

	if len(prices.Data) != 2 {
		t.Fatalf("ticks = %d, want 2", len(prices.Data))
	}
	// A real top-of-book tick versus a synthesised one — the tag keeps them
	// apart.
	real0 := prices.Data[0]
	if real0.Synthetic == nil || *real0.Synthetic {
		t.Errorf("first tick Synthetic = %v, want false (a real book)", real0.Synthetic)
	}
	if real0.PriceSource != "prediction_market" {
		t.Errorf("PriceSource = %q", real0.PriceSource)
	}
	synth := prices.Data[1]
	if synth.Synthetic == nil || !*synth.Synthetic {
		t.Errorf("second tick Synthetic = %v, want true", synth.Synthetic)
	}
	if synth.Bid != nil || synth.Ask != nil {
		t.Error("a synthesised tick has no real bid/ask")
	}

	meta := prices.Meta
	if meta.MatchID != 21635 || !meta.HasMore || meta.Limit != 2 {
		t.Errorf("meta = %+v", meta)
	}
	if meta.Minutes == nil || *meta.Minutes != 30 {
		t.Errorf("Minutes = %v, want 30", meta.Minutes)
	}
}

// The listing never carries the secret — only registration does, and that is
// asserted in TestWebhookMutations.
func TestWebhooksListDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/webhooks.json")))

	page, err := client.ListWebhooks(t.Context())
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if page.Len() != 2 {
		t.Fatalf("webhooks = %d, want 2", page.Len())
	}

	healthy := page.Data[0]
	if healthy.Secret != "" {
		t.Error("the listing must never carry a secret")
	}
	if len(healthy.Events) != 2 || healthy.Events[1] != WebhookBreakPoint {
		t.Errorf("events = %v", healthy.Events)
	}

	failing := page.Data[1]
	if failing.Enabled || failing.ConsecutiveFailures != 17 || failing.LastError == "" {
		t.Errorf("failing webhook = %+v", failing)
	}
}

func TestHistoryPackageManifestDecoding(t *testing.T) {
	client := newTestClient(t, serveBody(fixture(t, "synthetic/package_manifest.json")))

	manifest, err := client.GetHistoryPackage(t.Context(), "2026-07", "")
	if err != nil {
		t.Fatalf("GetHistoryPackage: %v", err)
	}
	if manifest.Period != "2026-07" || manifest.Status != "ready" {
		t.Errorf("manifest = %+v", manifest)
	}
	if len(manifest.Files) != 2 || manifest.Files[0].Bytes != 181203344 || manifest.Files[0].SHA256 == "" {
		t.Errorf("files = %+v", manifest.Files)
	}
}
