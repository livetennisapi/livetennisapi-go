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
