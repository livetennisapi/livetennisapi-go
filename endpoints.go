package livetennisapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ListParams is the pagination shared by every list endpoint.
//
// A zero field is omitted and the API's own default applies: 50 items from
// offset 0. Limit is capped at [MaxLimit].
type ListParams struct {
	// Limit is how many items to return, 1 to 200.
	Limit int

	// Offset is how many items to skip.
	Offset int
}

func (p ListParams) apply(q url.Values) {
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(min(p.Limit, MaxLimit)))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
}

// applyPlayerFilter appends repeated player ids to a query.
func applyPlayerFilter(q url.Values, ids []int64) {
	for _, id := range ids {
		q.Add("player", strconv.FormatInt(id, 10))
	}
}

// ListMatchesParams filters [Client.ListMatches].
type ListMatchesParams struct {
	// Status selects the lifecycle stage: [StatusLive], [StatusUpcoming] or
	// [StatusCompleted]. Empty means the API's default, which is live.
	Status MatchStatus

	// Tour restricts results to one circuit, its singles and doubles draws
	// alike. Empty means every tour. An unrecognised value is rejected with
	// [ErrBadRequest] rather than quietly ignored.
	Tour Tour

	// Player restricts results to matches where any listed player id is
	// EITHER participant, up to 50 ids; multiple ids return the deduplicated
	// union. An unknown id yields an empty list, not an error.
	Player []int64

	// Country restricts results to matches where either participant's
	// country equals this lowercase 3-letter code — the same IOC-style
	// vocabulary [Player.Country] returns (e.g. "ned", "sui"), NOT ISO-3166.
	// Players with no recorded country never match.
	Country string

	// From and To bound the play date: "YYYY-MM-DD" or an ISO-8601 UTC
	// datetime. A bare date is a UTC day boundary, and To includes the whole
	// day. An unparseable value is rejected with [ErrBadRequest] and code
	// "bad_date"; To must not precede From.
	From string
	To   string

	// ListParams paginates the result.
	ListParams
}

// ListFixturesParams filters [Client.ListFixtures].
type ListFixturesParams struct {
	// Tour restricts results to one circuit. Empty means every tour.
	Tour Tour

	// ListParams paginates the result.
	ListParams
}

// SearchPlayersParams filters [Client.SearchPlayers].
type SearchPlayersParams struct {
	// Search is a name fragment to match. Empty returns the ranked list.
	Search string

	// ListParams paginates the result.
	ListParams
}

// Health reports whether the API is serving. It is the one endpoint that needs
// no API key, which makes it usable as a reachability check before you have
// credentials.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var out Health
	if err := c.get(ctx, "/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListMatches returns matches by lifecycle status, with each match's latest
// score. FREE.
//
// A match that has not started carries a nil [Match.Score]:
//
//	page, err := client.ListMatches(ctx, livetennisapi.ListMatchesParams{
//		Status: livetennisapi.StatusUpcoming,
//		Tour:   livetennisapi.TourWTA,
//	})
func (c *Client) ListMatches(ctx context.Context, params ListMatchesParams) (*Page[Match], error) {
	q := url.Values{}
	if params.Status != "" {
		q.Set("status", string(params.Status))
	}
	if params.Tour != "" {
		q.Set("tour", string(params.Tour))
	}
	applyPlayerFilter(q, params.Player)
	if params.Country != "" {
		q.Set("country", params.Country)
	}
	if params.From != "" {
		q.Set("from", params.From)
	}
	if params.To != "" {
		q.Set("to", params.To)
	}
	params.apply(q)

	var out Page[Match]
	if err := c.get(ctx, "/matches", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMatch returns one match in full. FREE, with [Match.Market] embedded from
// PRO and [Match.Analysis] from ULTRA.
func (c *Client) GetMatch(ctx context.Context, matchID int64) (*Match, error) {
	var out Match
	if err := c.get(ctx, "/matches/"+strconv.FormatInt(matchID, 10), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMatchScore returns just the current score, the lowest-latency read the
// REST API offers. FREE; ULTRA additionally populates
// [Score.WinProbabilityP1] and [Score.Danger].
//
// It returns [ErrNotFound] for a match with no score yet, which is the normal
// answer for a fixture that has not started rather than a failure.
func (c *Client) GetMatchScore(ctx context.Context, matchID int64) (*Score, error) {
	var out Score
	if err := c.get(ctx, "/matches/"+strconv.FormatInt(matchID, 10)+"/score", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListMatchEvents returns a match's events, newest first. PRO — below that it
// returns [ErrUpgradeRequired].
func (c *Client) ListMatchEvents(ctx context.Context, matchID int64, params ListParams) (*Page[Event], error) {
	q := url.Values{}
	params.apply(q)

	var out Page[Event]
	if err := c.get(ctx, "/matches/"+strconv.FormatInt(matchID, 10)+"/events", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMatchAnalysis returns the model's analysis of a match. ULTRA — below that
// it returns [ErrUpgradeRequired].
//
// Both halves of the result may be nil for a match the model has not covered.
func (c *Client) GetMatchAnalysis(ctx context.Context, matchID int64) (*Analysis, error) {
	var out Analysis
	if err := c.get(ctx, "/matches/"+strconv.FormatInt(matchID, 10)+"/analysis", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchPlayers searches players by name, ranked players first. FREE.
//
// The list carries no [Player.Stats]; use [Client.GetPlayer] for that.
func (c *Client) SearchPlayers(ctx context.Context, params SearchPlayersParams) (*Page[Player], error) {
	q := url.Values{}
	if params.Search != "" {
		q.Set("search", params.Search)
	}
	params.apply(q)

	var out Page[Player]
	if err := c.get(ctx, "/players", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPlayer returns one player's bio, ranking and cached stats. FREE.
func (c *Client) GetPlayer(ctx context.Context, playerID int64) (*Player, error) {
	var out Player
	if err := c.get(ctx, "/players/"+strconv.FormatInt(playerID, 10), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListMarkets returns the match-winner market or markets for a match. PRO —
// below that it returns [ErrUpgradeRequired].
//
// The markets carry no price ticks; use [Client.GetMarketPrices] for those.
func (c *Client) ListMarkets(ctx context.Context, matchID int64) (*Page[Market], error) {
	q := url.Values{"match_id": {strconv.FormatInt(matchID, 10)}}

	var out Page[Market]
	if err := c.get(ctx, "/markets", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMarketPrices returns a match's market with its recent price ticks per
// side, newest first. PRO — below that it returns [ErrUpgradeRequired].
//
// Only [ListParams.Limit] is honoured here; the endpoint takes no offset.
func (c *Client) GetMarketPrices(ctx context.Context, matchID int64, params ListParams) (*Market, error) {
	q := url.Values{}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(min(params.Limit, MaxLimit)))
	}

	var out Market
	if err := c.get(ctx, "/markets/"+strconv.FormatInt(matchID, 10)+"/prices", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCompletedMatches returns finished matches, newest first, each with
// [Match.Winner] derived from the final sets and [Match.Tape] saying what
// point-by-point data exists for it. BASIC, or any History plan — below that
// it returns [ErrUpgradeRequired].
//
// This is the unfiltered shorthand; [Client.ListHistoryMatches] takes the
// full filter set (dates, coverage, tour, players, country).
func (c *Client) ListCompletedMatches(ctx context.Context, params ListParams) (*Page[Match], error) {
	return c.ListHistoryMatches(ctx, HistoryMatchesParams{ListParams: params})
}

// HistoryMatchesParams filters [Client.ListHistoryMatches].
type HistoryMatchesParams struct {
	// From and To bound the play date, as on [ListMatchesParams].
	From string
	To   string

	// Coverage keeps only matches whose tape has this coverage. Empty keeps
	// everything. NOTE the filter is applied AFTER the page is cut, so a
	// filtered page is routinely shorter than the limit (and may be empty)
	// while later pages still hold matching matches — a short filtered page
	// is not an end-of-data signal; read [ListMeta.HasMore], which
	// [Paginate] does automatically.
	Coverage Coverage

	// Tour restricts results to one circuit. Empty means every tour.
	Tour Tour

	// Player restricts to matches involving any of these player ids, ≤50.
	Player []int64

	// Country restricts by either participant's country code, as on
	// [ListMatchesParams].
	Country string

	// ListParams paginates the result.
	ListParams
}

// ListHistoryMatches returns finished matches, newest first, each with
// [Match.Winner] derived from the final sets and [Match.Tape] saying what
// point-by-point data exists for it — so a whole page can be qualified in
// one call instead of one request per match. BASIC, or any History plan —
// below that it returns [ErrUpgradeRequired].
func (c *Client) ListHistoryMatches(ctx context.Context, params HistoryMatchesParams) (*Page[Match], error) {
	q := url.Values{}
	if params.From != "" {
		q.Set("from", params.From)
	}
	if params.To != "" {
		q.Set("to", params.To)
	}
	if params.Coverage != "" {
		q.Set("coverage", string(params.Coverage))
	}
	if params.Tour != "" {
		q.Set("tour", string(params.Tour))
	}
	applyPlayerFilter(q, params.Player)
	if params.Country != "" {
		q.Set("country", params.Country)
	}
	params.apply(q)

	var out Page[Match]
	if err := c.get(ctx, "/history/matches", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TapeParams selects the shape of [Client.GetMatchTape].
type TapeParams struct {
	// Sequence is [SequenceRaw] (the API's default: every committed row) or
	// [SequenceClean] (one row per distinct score state, the only shape that
	// carries [TapeRow.PointWinner]).
	Sequence Sequence
}

// GetMatchTape returns a match's point-by-point tape: the score sequence,
// per-point model probabilities where they exist, per-set tiebreak scores,
// and the coverage metadata that says how much of the match the tape holds.
// BASIC, or any History plan — below that it returns [ErrUpgradeRequired].
//
// It WORKS ON A LIVE MATCH, not only a completed one: the tape is assembled
// from whatever has been committed so far, including games played before you
// started watching. [Client.GetMatchScore] is one state; this is the
// sequence of states. Check [TapeMeta.Coverage] before backtesting — the
// tape is not guaranteed to cover the whole match.
func (c *Client) GetMatchTape(ctx context.Context, matchID int64, params TapeParams) (*MatchTape, error) {
	q := url.Values{}
	if params.Sequence != "" {
		q.Set("sequence", string(params.Sequence))
	}

	var out MatchTape
	if err := c.get(ctx, "/history/matches/"+strconv.FormatInt(matchID, 10), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetHeadToHead returns the record between two players across both halves of
// the product: the results archive (1968–2022) and the API's own completed
// matches (2023 onward). BASIC, or any History plan — below that it returns
// [ErrUpgradeRequired]. On ULTRA the response additionally carries the
// [HeadToHead.Stats] aggregate block.
//
// p1 and p2 are name fragments of at least 3 characters. A fragment matching
// more than one player is refused with [ErrBadRequest], code
// "ambiguous_name" and the candidate list on [APIError.AllowedValues] —
// because two people summed into one record would be a wrong answer, not a
// convenience.
func (c *Client) GetHeadToHead(ctx context.Context, p1, p2 string) (*HeadToHead, error) {
	q := url.Values{"p1": {p1}, "p2": {p2}}

	var out HeadToHead
	if err := c.get(ctx, "/h2h", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ArchiveMatchesParams filters [Client.ListArchiveMatches].
type ArchiveMatchesParams struct {
	// Tour is [TourATP] or [TourWTA] — the archive covers only those two.
	// Empty means both.
	Tour Tour

	// Name is a case-insensitive substring match on EITHER player's name,
	// minimum 3 characters.
	Name string

	// From and To bound the tournament start date, "YYYY-MM-DD".
	From string
	To   string

	// Round filters by round code: "F", "SF", "QF", "R16" through "R128",
	// "RR", "BR", "Q1" to "Q4", "ER".
	Round string

	// Level filters by source tier code (see [ArchiveMatch.Level]).
	Level string

	// ListParams paginates the result.
	ListParams
}

// ListArchiveMatches returns deep historical results, newest tournament
// first: ATP and WTA main draws, qualifying, challengers and futures, 1968
// through 2022. BASIC, or any History plan — below that it returns
// [ErrUpgradeRequired].
//
// The archive is a separate id space from /matches — archive people are
// identified by name, not by roster player ids — and it ends where the API's
// own point-by-point coverage begins (2023-01), so no match is ever served
// from two datasets.
func (c *Client) ListArchiveMatches(ctx context.Context, params ArchiveMatchesParams) (*Page[ArchiveMatch], error) {
	q := url.Values{}
	if params.Tour != "" {
		q.Set("tour", string(params.Tour))
	}
	if params.Name != "" {
		q.Set("name", params.Name)
	}
	if params.From != "" {
		q.Set("from", params.From)
	}
	if params.To != "" {
		q.Set("to", params.To)
	}
	if params.Round != "" {
		q.Set("round", params.Round)
	}
	if params.Level != "" {
		q.Set("level", params.Level)
	}
	params.apply(q)

	var out Page[ArchiveMatch]
	if err := c.get(ctx, "/history/archive/matches", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetArchiveMatch returns one archive result with serve statistics where the
// era recorded them: [ArchiveMatch.Stats] is nil for the (mostly pre-1991)
// rows the source never recorded statistics for — never synthesised. BASIC,
// or any History plan.
func (c *Client) GetArchiveMatch(ctx context.Context, archiveID int64) (*ArchiveMatch, error) {
	var out ArchiveMatch
	if err := c.get(ctx, "/history/archive/matches/"+strconv.FormatInt(archiveID, 10), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ArchivePlayersParams filters [Client.ListArchivePlayers].
type ArchivePlayersParams struct {
	// Name is a case-insensitive substring filter, minimum 3 characters.
	Name string

	// Tour is [TourATP] or [TourWTA]. Empty means both.
	Tour Tour

	// ListParams paginates the result.
	ListParams
}

// ListArchivePlayers returns archive player bios — hand, date of birth,
// country, height and career-high rank — ordered by name. BASIC, or any
// History plan.
//
// These are the people of the results archive, in the archive's own id
// space: [ArchivePlayerBio.ID] is the corpus person id that archive match
// rows carry as [ArchivePlayer.PlayerID], never a roster id.
func (c *Client) ListArchivePlayers(ctx context.Context, params ArchivePlayersParams) (*Page[ArchivePlayerBio], error) {
	q := url.Values{}
	if params.Name != "" {
		q.Set("name", params.Name)
	}
	if params.Tour != "" {
		q.Set("tour", string(params.Tour))
	}
	params.apply(q)

	var out Page[ArchivePlayerBio]
	if err := c.get(ctx, "/history/archive/players", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetArchiveCareer returns one player's whole archive career in one
// response: W-L record overall, by surface, by level and by year, titles,
// and the summed serve-stat block with derived ratios. BASIC, or any History
// plan.
//
// name is a fragment of at least 3 characters and must resolve to one
// person; an ambiguous fragment is refused with the candidates, the same
// rule as [Client.GetHeadToHead].
func (c *Client) GetArchiveCareer(ctx context.Context, name string) (*ArchiveCareer, error) {
	q := url.Values{"name": {name}}

	var out ArchiveCareer
	if err := c.get(ctx, "/history/archive/career", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RankingsParams filters [Client.ListRankings], and selects between its two
// modes. See that method for the mode rules.
type RankingsParams struct {
	// Player selects the per-player mode (ULTRA): point-in-time records for
	// these player ids, up to 50. Leave it empty for the listing mode (PRO).
	Player []int64

	// AsOf asks for the newest record effective ON OR BEFORE this date,
	// "YYYY-MM-DD" — never one dated after it. Empty means the latest known
	// record.
	AsOf string

	// System restricts to one or more ranking systems. The listing mode
	// requires exactly one, and [RankingUTR] has no listing (a rating, not a
	// ranking).
	System []RankingSystem

	// ListParams paginates the result.
	ListParams
}

// ListRankings returns ranking records in force at a point in time. Every
// other ranking field in this API is the player's CURRENT value joined at
// read time — this endpoint is the point-in-time answer.
//
// It has TWO modes with different tiers:
//
//   - Listing (PRO): leave Player empty and name exactly one System. Returns
//     the full published table in rank order, the newest week at or before
//     AsOf. Rows carry [RankingRecord.PlayerName] as published and a nil
//     [RankingRecord.PlayerID] for players outside the roster, so the table
//     has no silent holes.
//   - Per-player (ULTRA): set Player ids. Returns, per system, the newest
//     record effective on or before AsOf for each player.
//
// Below the mode's tier it returns [ErrUpgradeRequired]. Read
// [RankingsMeta.Coverage] before trusting an empty result — ITF and UTR
// history begins 2026-07-29 and cannot be reconstructed earlier.
func (c *Client) ListRankings(ctx context.Context, params RankingsParams) (*RankingsPage, error) {
	q := url.Values{}
	applyPlayerFilter(q, params.Player)
	if params.AsOf != "" {
		q.Set("as_of", params.AsOf)
	}
	for _, system := range params.System {
		q.Add("system", string(system))
	}
	params.apply(q)

	var out RankingsPage
	if err := c.get(ctx, "/rankings", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMatchStatistics returns in-play statistics for one match — aces, double
// faults, the serve split, hold/break percentages, break points, service and
// return points — in two families that are deliberately not merged (see
// [StatisticsSide]). ULTRA — below that it returns [ErrUpgradeRequired].
//
// A match the API holds nothing for answers 200 with coverage "none" and nil
// [MatchStatistics.Players], not 404 — the match exists, and holding nothing
// for it is the honest answer. Statistics can be further behind the match
// than the score, which is why they carry their own as-of and are not on the
// [Score] object.
func (c *Client) GetMatchStatistics(ctx context.Context, matchID int64) (*MatchStatistics, error) {
	var out MatchStatistics
	if err := c.get(ctx, "/matches/"+strconv.FormatInt(matchID, 10)+"/statistics", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RallyMatchesParams filters [Client.ListRallyMatches].
type RallyMatchesParams struct {
	// Player is a substring match on either player name.
	Player string

	// From and To bound the match date, "YYYY-MM-DD".
	From string
	To   string

	// Surface filters by court surface.
	Surface string

	// Gender is "M" or "W". Empty means both.
	Gender string

	// ListParams paginates the result.
	ListParams
}

// ListRallyMatches returns charted matches with shot-by-shot data, newest
// first. ULTRA — below that it returns [ErrUpgradeRequired].
//
// Ask this endpoint for the authoritative coverage list rather than assuming
// a match is charted: charting is human work, so coverage is deep, not
// universal.
func (c *Client) ListRallyMatches(ctx context.Context, params RallyMatchesParams) (*Page[RallyMatch], error) {
	q := url.Values{}
	if params.Player != "" {
		q.Set("player", params.Player)
	}
	if params.From != "" {
		q.Set("from", params.From)
	}
	if params.To != "" {
		q.Set("to", params.To)
	}
	if params.Surface != "" {
		q.Set("surface", params.Surface)
	}
	if params.Gender != "" {
		q.Set("gender", params.Gender)
	}
	params.apply(q)

	var out Page[RallyMatch]
	if err := c.get(ctx, "/rally/matches", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRallyMatch returns one charted match with its points in play order,
// paged by params; [RallyMatchDetail.Meta] carries the match's full point
// count in Total. ULTRA — below that it returns [ErrUpgradeRequired].
func (c *Client) GetRallyMatch(ctx context.Context, rallyMatchID int64, params ListParams) (*RallyMatchDetail, error) {
	q := url.Values{}
	params.apply(q)

	var out RallyMatchDetail
	if err := c.get(ctx, "/rally/matches/"+strconv.FormatInt(rallyMatchID, 10), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMatchRally returns rally construction addressed by the API's OWN match
// id, resolved through the optional link on [RallyMatch.MatchID]. ULTRA.
//
// It answers [ErrNotFound] with code "not_charted" when the API holds the
// match but nobody charted it — deliberately distinct from "no such match",
// because most matches are not charted and a consumer walking the archive
// must tell the two apart.
func (c *Client) GetMatchRally(ctx context.Context, matchID int64, params ListParams) (*RallyMatchDetail, error) {
	q := url.Values{}
	params.apply(q)

	var out RallyMatchDetail
	if err := c.get(ctx, "/history/matches/"+strconv.FormatInt(matchID, 10)+"/rally", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChartingPlayerParams identifies the player for
// [Client.GetChartingPlayer].
type ChartingPlayerParams struct {
	// Name is the player name, minimum 3 characters. A fragment matching
	// more than one charted person is refused with the candidates.
	Name string

	// Gender is "men" or "women", to disambiguate a name that appears on
	// both tours. Empty when not needed. (Mind the vocabulary: this endpoint
	// takes "men"/"women" where the rally listing takes "M"/"W".)
	Gender string
}

// GetChartingPlayer returns one player's career shot-level charting
// aggregate — the deepest serve/return profile the API holds. ULTRA — below
// that it returns [ErrUpgradeRequired].
func (c *Client) GetChartingPlayer(ctx context.Context, params ChartingPlayerParams) (*ChartingPlayer, error) {
	q := url.Values{"name": {params.Name}}
	if params.Gender != "" {
		q.Set("gender", params.Gender)
	}

	var out ChartingPlayer
	if err := c.get(ctx, "/charting/players", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetChartingMatch returns one charted match with every stat family for both
// players, per-set split included. ULTRA — below that it returns
// [ErrUpgradeRequired].
func (c *Client) GetChartingMatch(ctx context.Context, chartingMatchID int64) (*ChartingMatch, error) {
	var out ChartingMatch
	if err := c.get(ctx, "/charting/matches/"+strconv.FormatInt(chartingMatchID, 10), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HistoryPackagesParams filters [Client.ListHistoryPackages].
type HistoryPackagesParams struct {
	// Kind is the package family: [PackageTape] (the API's default),
	// [PackageRally] or [PackageRankings]. The non-tape kinds need ULTRA.
	Kind PackageKind

	// Year asks for the year-archive listing — every published month of
	// "YYYY". That needs core ULTRA, History Business, or a one-year
	// package.
	Year string
}

// ListHistoryPackages returns the pre-built monthly bulk packages, newest
// period first. PRO, or a package subscription — the tape kind's floor;
// rally and rankings kinds and the year listing are gated as described on
// [HistoryPackagesParams].
//
// Coverage is not a contiguous run of months and is still being extended
// backwards, so treat this listing as the authoritative set of months that
// exist. The package files themselves stream from
// /history/packages/{period}?format=jsonl|csv.
func (c *Client) ListHistoryPackages(ctx context.Context, params HistoryPackagesParams) (*HistoryPackagesPage, error) {
	q := url.Values{}
	if params.Kind != "" {
		q.Set("kind", string(params.Kind))
	}
	if params.Year != "" {
		q.Set("year", params.Year)
	}

	var out HistoryPackagesPage
	if err := c.get(ctx, "/history/packages", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetWSToken mints a short-lived connection token for the high-fan-out push
// feed, returning the token, the WebSocket URL and the channel vocabulary —
// "match:{id}" per-match streams and "slate:all" for every live score frame.
// ULTRA — below that it returns [ErrUpgradeRequired].
//
// Mint a fresh token on every reconnect; the token expires after
// [WSToken.ExpiresIn] seconds.
func (c *Client) GetWSToken(ctx context.Context) (*WSToken, error) {
	var out WSToken
	if err := c.get(ctx, "/ws-token", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListFixtures returns upcoming scheduled fixtures, earliest first. FREE.
//
// Fixtures are primarily name-keyed: [Fixture.Player1ID] and
// [Fixture.Player2ID] are set only where a participant resolved to a roster
// record, so use [Client.ListMatches] with [StatusUpcoming] when you need
// full player objects.
func (c *Client) ListFixtures(ctx context.Context, params ListFixturesParams) (*Page[Fixture], error) {
	q := url.Values{}
	if params.Tour != "" {
		q.Set("tour", string(params.Tour))
	}
	params.apply(q)

	var out Page[Fixture]
	if err := c.get(ctx, "/fixtures", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TournamentsParams filters [Client.ListTournaments].
type TournamentsParams struct {
	// Search is a case-insensitive substring match on the tournament name.
	Search string

	// Tour restricts results to one circuit. Empty means every tour.
	Tour Tour

	// ListParams paginates the result.
	ListParams
}

// ListTournaments returns the tournament catalogue in name order — the id
// space [Match.TournamentID] joins. FREE.
//
// Tier/category is populated only where the catalogues agree unambiguously
// (see [Tournament.Category]); it is never derived from the name.
func (c *Client) ListTournaments(ctx context.Context, params TournamentsParams) (*Page[Tournament], error) {
	q := url.Values{}
	if params.Search != "" {
		q.Set("search", params.Search)
	}
	if params.Tour != "" {
		q.Set("tour", string(params.Tour))
	}
	params.apply(q)

	var out Page[Tournament]
	if err := c.get(ctx, "/tournaments", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTournament returns one tournament by the stable id carried on match
// objects as [Match.TournamentID]. FREE.
func (c *Client) GetTournament(ctx context.Context, tournamentID string) (*Tournament, error) {
	var out Tournament
	if err := c.get(ctx, "/tournaments/"+url.PathEscape(tournamentID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetUsage returns the calling key's own usage against its quota: tier,
// limits, today's calls current to the second, and a 30-day history. Any
// tier, and QUOTA-EXEMPT — polling it does not spend the budget it reports.
//
// The per-minute window is not in the response; it rides on the
// X-RateLimit-* headers of every call (observe them with
// [WithRateLimitObserver]).
func (c *Client) GetUsage(ctx context.Context) (*Usage, error) {
	var out Usage
	if err := c.get(ctx, "/usage", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MatchPricesParams bounds [Client.ListMatchPrices].
type MatchPricesParams struct {
	// Limit is how many ticks to return, 1 to [MaxPriceTicks]. Zero means
	// the API's default of 100. Values above the cap are clamped.
	Limit int

	// Minutes bounds the lookback window, 1 to 1440. Zero means no window.
	Minutes int
}

// ListMatchPrices returns the bare recent price ticks of a match's mapped
// match-winner market, newest first — no market wrapper, which makes it the
// lightest polling read for prices. PRO — below that it returns
// [ErrUpgradeRequired], and [ErrNotFound] when the match has no mapped
// market.
//
// There is no offset: when [MatchPricesMeta.HasMore] reports the window was
// clipped, raise the limit or narrow the minutes window. Prices are
// prediction-market quotes in [0,1], not an official line, and can lag live
// scores; [Price.Synthetic] tags a quote estimated from mid rather than a
// live order book.
func (c *Client) ListMatchPrices(ctx context.Context, matchID int64, params MatchPricesParams) (*MatchPrices, error) {
	q := url.Values{}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(min(params.Limit, MaxPriceTicks)))
	}
	if params.Minutes > 0 {
		q.Set("minutes", strconv.Itoa(params.Minutes))
	}

	var out MatchPrices
	if err := c.get(ctx, "/matches/"+strconv.FormatInt(matchID, 10)+"/prices", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetHistoryPackage returns one monthly package's manifest — its files with
// sizes and SHA-256 checksums. Same gating as [Client.ListHistoryPackages].
// The file itself streams through [Client.DownloadHistoryPackage].
func (c *Client) GetHistoryPackage(ctx context.Context, period string, kind PackageKind) (*HistoryPackage, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", string(kind))
	}

	var out HistoryPackage
	if err := c.get(ctx, "/history/packages/"+url.PathEscape(period), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadHistoryPackage streams one monthly package file. format is "jsonl"
// (one whole tape object per line, coverage meta included) or "csv" (one row
// per point, no coverage columns); an unknown format is rejected with
// [ErrBadRequest]. Same gating as [Client.ListHistoryPackages].
//
// The caller owns the returned body and must Close it. Package files run to
// hundreds of megabytes, which is why this streams rather than buffering —
// verify what you stored against the manifest's SHA-256 from
// [Client.GetHistoryPackage].
func (c *Client) DownloadHistoryPackage(ctx context.Context, period string, kind PackageKind, format string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, errors.New("livetennisapi: nil context")
	}

	path := "/history/packages/" + url.PathEscape(period)
	q := url.Values{"format": {format}}
	if kind != "" {
		q.Set("kind", string(kind))
	}
	endpoint := c.baseURL + path + "?" + q.Encode()

	// One attempt, no retries: a download that dies midway is for the caller
	// to restart, not for this client to silently re-request.
	resp, err := c.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, connectionError(endpoint, err, ctx.Err())
	}

	rl := parseRateLimit(resp.Header)
	if c.observe != nil {
		c.observe(rl)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := c.apiError(resp, path, q, endpoint, rl)
		resp.Body.Close()
		return nil, apiErr
	}
	return resp.Body, nil
}

// WebhookParams configures [Client.CreateWebhook].
type WebhookParams struct {
	// URL is the delivery endpoint. HTTPS only, publicly routable.
	URL string `json:"url"`

	// Events selects the frame kinds to deliver. Empty means the API's
	// default, [WebhookScore] only.
	Events []WebhookEvent `json:"events,omitempty"`
}

// CreateWebhook registers an outbound webhook: the API POSTs the same frames
// the push WebSocket sends to the given HTTPS endpoint on every matching
// commit. ULTRA, and DIRECT KEYS ONLY — a marketplace key is refused with a
// 403 carrying code "direct_key_required", which is a channel restriction,
// not a tier one.
//
// The response is the ONLY time [Webhook.Secret] is shown — store it
// immediately. A key holds at most 3 webhooks; the 4th registration is
// refused with [ErrWebhookLimit], so delete one first.
//
// Registration is never retried automatically: a request that timed out may
// still have been applied, and re-sending it could register a duplicate.
func (c *Client) CreateWebhook(ctx context.Context, params WebhookParams) (*Webhook, error) {
	var out Webhook
	if err := c.call(ctx, http.MethodPost, "/webhooks", nil, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWebhooks returns your registered webhooks with their delivery health —
// never the secret, which is shown only at registration. ULTRA, direct keys
// only.
func (c *Client) ListWebhooks(ctx context.Context) (*Page[Webhook], error) {
	var out Page[Webhook]
	if err := c.get(ctx, "/webhooks", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWebhook removes one of your webhooks. ULTRA, direct keys only. It
// returns [ErrNotFound] for an id that is not yours or is already gone.
//
// Deletion is never retried automatically, so a timeout is reported rather
// than papered over — re-check with [Client.ListWebhooks] before assuming
// either outcome.
func (c *Client) DeleteWebhook(ctx context.Context, webhookID int64) error {
	return c.call(ctx, http.MethodDelete, "/webhooks/"+strconv.FormatInt(webhookID, 10), nil, nil, nil)
}
