package livetennisapi

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Tour is a circuit accepted by the tour filter on [Client.ListMatches] and
// [Client.ListFixtures].
//
// Each value covers its singles and doubles draws, so [TourATP] includes ATP
// doubles and [TourJuniors] covers both the boys' and girls' Grand Slam draws.
//
// This is the vocabulary the API accepts as a *filter*, which is not the one it
// returns in [Player.Tour] and [Fixture.Tour]: filtering by [TourJuniors]
// yields records whose own tour reads "juniors_boys" or "juniors_girls". Those
// fields are plain strings for that reason — do not compare a Tour against
// them.
type Tour string

// The tours the filter accepts. An unrecognised value is rejected with a 400
// carrying code "bad_tour" rather than silently ignored, so a caller never
// receives a tour it did not ask for. See [APIError.AllowedValues].
const (
	TourATP        Tour = "atp"
	TourWTA        Tour = "wta"
	TourChallenger Tour = "challenger"
	TourITF        Tour = "itf"
	TourJuniors    Tour = "juniors"
)

// MatchStatus is a match's lifecycle state.
type MatchStatus string

// The lifecycle states a match moves through. Only the first three are
// accepted as a filter by [Client.ListMatches]; "cancelled" appears in
// payloads but is not a query value.
const (
	StatusLive      MatchStatus = "live"
	StatusUpcoming  MatchStatus = "upcoming"
	StatusCompleted MatchStatus = "completed"
	StatusCancelled MatchStatus = "cancelled"
)

// Health is the response from [Client.Health].
type Health struct {
	// Status is "ok" when the API is serving.
	Status string `json:"status"`

	// Version is the API version, "v1".
	Version string `json:"version"`
}

// ListMeta is the pagination envelope returned beside a list response.
//
// Count describes the page just returned, not the size of the whole
// collection, so it cannot be used to compute a page count. Detect the end of
// a list by receiving fewer items than you asked for — which is what
// [Paginate] does.
type ListMeta struct {
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
	Count  int `json:"count,omitempty"`

	// Total is the size of the whole filtered set. nil when the API cannot
	// count it cheaply — completed-match listings return null here — so nil
	// means "unknown", never "zero results".
	Total *int `json:"total,omitempty"`

	// HasMore reports whether results exist beyond this page. nil when the
	// endpoint did not say, in which case the only end-of-data signal is a
	// short page. [Paginate] prefers this field when it is present, which
	// matters on the coverage-filtered history listing, where a filtered page
	// is routinely shorter than the limit while later pages still hold
	// matches.
	HasMore *bool `json:"has_more,omitempty"`

	// MatchID echoes the filter on [Client.ListMarkets], which is the only
	// endpoint that sets it. Zero elsewhere.
	MatchID int64 `json:"match_id,omitempty"`
}

// Page is one page of a list endpoint: the API's {data, meta} envelope.
type Page[T any] struct {
	// Data holds the items for this page. Never nil after a successful call;
	// an empty page decodes to an empty slice.
	Data []T `json:"data"`

	// Meta is the pagination envelope.
	Meta ListMeta `json:"meta,omitzero"`
}

// Len returns the number of items on this page.
func (p *Page[T]) Len() int {
	if p == nil {
		return 0
	}
	return len(p.Data)
}

// Score is a match score at a point in time.
//
// Two shapes here reliably trip people up:
//
// Games is player-major, not set-major. It is [games_p1, games_p2] where each
// side is a per-set list, so [[6,3,2],[4,6,1]] reads 6-4, 3-6, 2-1. Use
// [Score.GamesForSet] rather than indexing by hand.
//
// Points is a list of strings, not numbers, because tennis scores points as
// "0", "15", "30", "40" and "AD". Do not parse them as integers.
type Score struct {
	// Sets is [sets_p1, sets_p2] — sets won by each player.
	Sets []int `json:"sets,omitempty"`

	// Games is [games_p1, games_p2], each a per-set list. See the type doc.
	Games [][]int `json:"games,omitempty"`

	// Points is the current game's points as strings: "0", "15", "30", "40",
	// "AD". During a tiebreak they are numeric strings. Never integers.
	Points []string `json:"points,omitempty"`

	// Server is which player is serving, 1 or 2. nil when unknown.
	Server *int `json:"server,omitempty"`

	// IsTiebreak reports whether the current game is a tiebreak.
	IsTiebreak bool `json:"is_tiebreak,omitempty"`

	// WinProbabilityP1 is the live model's probability that player 1 wins,
	// from 0 to 1. ULTRA only; nil on every lower tier.
	WinProbabilityP1 *float64 `json:"win_probability_p1,omitempty"`

	// Danger is the live model's danger rating for the leading side. ULTRA
	// only; nil on every lower tier.
	Danger *float64 `json:"danger,omitempty"`

	// Timestamp is when the score was observed. Zero if absent.
	Timestamp Time `json:"timestamp,omitzero"`
}

// GamesForSet returns the games each player won in one set, guarding the
// player-major layout of [Score.Games]. ok is false when the set is out of
// range or the score is nil, which is the normal case for a match that has
// not reached that set.
func (s *Score) GamesForSet(setIndex int) (p1, p2 int, ok bool) {
	if s == nil || len(s.Games) < 2 || setIndex < 0 {
		return 0, 0, false
	}
	first, second := s.Games[0], s.Games[1]
	if setIndex >= len(first) || setIndex >= len(second) {
		return 0, 0, false
	}
	return first[setIndex], second[setIndex], true
}

// NumSets returns how many sets have been played or started.
func (s *Score) NumSets() int {
	if s == nil || len(s.Games) < 2 {
		return 0
	}
	return min(len(s.Games[0]), len(s.Games[1]))
}

// String renders the score as "6-4 3-6 2-1 (40-30)". A nil Score, which is
// what an upcoming match carries, renders as "-".
func (s *Score) String() string {
	if s == nil {
		return "-"
	}

	var parts []string
	if n := s.NumSets(); n > 0 {
		for i := range n {
			p1, p2, ok := s.GamesForSet(i)
			if !ok {
				continue
			}
			parts = append(parts, strconv.Itoa(p1)+"-"+strconv.Itoa(p2))
		}
	} else if len(s.Sets) >= 2 {
		parts = append(parts, strconv.Itoa(s.Sets[0])+"-"+strconv.Itoa(s.Sets[1]))
	}

	if len(s.Points) >= 2 {
		parts = append(parts, "("+s.Points[0]+"-"+s.Points[1]+")")
	}

	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

// Player is a player's identity, ranking and, on the single-player endpoint,
// cached statistics.
//
// A doubles entry is modelled as a single Player with IsDoublesTeam set and
// both names in Name.
type Player struct {
	// ID is the player's stable identifier.
	ID int64 `json:"id,omitempty"`

	// Name is the player's display name.
	Name string `json:"name,omitempty"`

	// Tour is the circuit this player is recorded on: "atp", "wta",
	// "challenger", "itf", "juniors_boys" or "juniors_girls". Empty when
	// unknown.
	//
	// This is a plain string, not a [Tour], because the values the API returns
	// here are not the values it accepts as a filter — the juniors draws split
	// into two here but are selected with the single filter [TourJuniors].
	Tour string `json:"tour,omitempty"`

	// Country is the country code. Empty when unknown.
	Country string `json:"country,omitempty"`

	// Ranking is the current world ranking. nil when the player is unranked,
	// which is not the same as being ranked 0.
	Ranking *int `json:"ranking,omitempty"`

	// RankingPoints is the current ranking points total. nil when unknown.
	RankingPoints *int `json:"ranking_points,omitempty"`

	// RankingMovement is "up", "down" or "same". Empty when unknown.
	RankingMovement string `json:"ranking_movement,omitempty"`

	// Hand is the playing hand, "R" or "L". Empty when unknown.
	Hand string `json:"hand,omitempty"`

	// Backhand is 1 for one-handed, 2 for two-handed. nil when unknown.
	Backhand *int `json:"backhand,omitempty"`

	// Birthday is the date of birth, a calendar date with no time. Zero when
	// unknown.
	Birthday Time `json:"birthday,omitzero"`

	// IsDoublesTeam reports whether this entry is a doubles pairing rather
	// than an individual.
	IsDoublesTeam bool `json:"is_doubles_team,omitempty"`

	// DataCompleteness says how much biographical detail is known for this
	// player, so you can tell "not in the feed" from "not yet fetched" without
	// probing. Present on every player in a match payload; lower tours carry
	// far less detail than the main tour.
	DataCompleteness *DataCompleteness `json:"data_completeness,omitempty"`

	// Stats is populated by [Client.GetPlayer] only, never by the search
	// endpoint. nil elsewhere.
	Stats *PlayerStats `json:"stats,omitempty"`
}

// DataCompleteness reports how much of a player's biography is populated.
//
// It describes the biographical fields only — hand, backhand, birthday and the
// like — not the ranking or the identity, so 2 known of 5 still describes a
// perfectly usable player record.
//
// It does not apply to a doubles team, which has no single biography. There
// the API sends Known and Of as null with an explanatory Note, so both are
// pointers: null means "not applicable", which is emphatically not zero.
// Check [DataCompleteness.Applicable] before reading them.
type DataCompleteness struct {
	// Known is how many of the considered fields are populated. nil for a
	// doubles team, where the question does not apply.
	Known *int `json:"known,omitempty"`

	// Of is how many fields were considered. nil for a doubles team.
	Of *int `json:"of,omitempty"`

	// Missing names the unpopulated fields, for example ["backhand", "hand"].
	// Empty when nothing is missing, and for a doubles team.
	Missing []string `json:"missing,omitempty"`

	// Note explains why completeness does not apply, when it does not. Set for
	// a doubles team; empty for an individual.
	Note string `json:"note,omitempty"`
}

// Applicable reports whether per-player completeness is meaningful for this
// record. It is false for a doubles team, and for a nil receiver.
func (d *DataCompleteness) Applicable() bool {
	return d != nil && d.Known != nil && d.Of != nil
}

// Complete reports whether every considered biographical field is populated.
// It is false when completeness does not apply at all, so a doubles team is
// never reported complete; use [DataCompleteness.Applicable] to tell the two
// apart.
func (d *DataCompleteness) Complete() bool {
	return d.Applicable() && *d.Of > 0 && *d.Known >= *d.Of
}

// PlayerStats is the cached statistics block on a single player.
//
// Both halves are carried as raw JSON: the API does not pin their shape in the
// v1 schema, so decoding them into fixed structs here would break the moment
// the model behind them changes. Unmarshal them yourself when you need them.
type PlayerStats struct {
	// Ratings is the ratings object, or nil.
	Ratings json.RawMessage `json:"ratings,omitempty"`

	// Season is the season-by-season array, or nil.
	Season json.RawMessage `json:"season,omitempty"`
}

// Players is the pair of players in a match.
type Players struct {
	// P1 is player 1, the side that "1" refers to everywhere else: in
	// [Score.Server], [Match.Winner], [Price.Side] and [Event.Player].
	P1 *Player `json:"p1,omitempty"`

	// P2 is player 2.
	P2 *Player `json:"p2,omitempty"`
}

// Match is a tennis match.
//
// Market is present from PRO and Analysis from ULTRA. Below those tiers the
// API omits them entirely, so nil means "not entitled or not available" and
// never "no market exists".
type Match struct {
	// ID is the match's stable identifier.
	ID int64 `json:"id,omitempty"`

	// Tournament is the event name.
	Tournament string `json:"tournament,omitempty"`

	// Tour is the match's circuit, in the SAME vocabulary the tour filter
	// accepts — unlike [Player.Tour] and [Fixture.Tour], this one is safe to
	// compare against the [Tour] constants, and a match selected by a tour
	// filter always carries that value here. Empty when the feed never stated
	// a tour or the event has no public tour name (exhibitions, team and
	// mixed events).
	Tour Tour `json:"tour,omitempty"`

	// TournamentID is the stable tournament identity: one id per tournament
	// and event type, stable across seasons. Empty on matches ingested before
	// the catalogue covered their tournament.
	TournamentID string `json:"tournament_id,omitempty"`

	// Surface is "hard", "clay" or "grass". Empty when unknown.
	Surface string `json:"surface,omitempty"`

	// Indoor reports whether the court is indoors.
	Indoor bool `json:"indoor,omitempty"`

	// Format is "BO3" or "BO5". Empty when unknown.
	Format string `json:"format,omitempty"`

	// Round is the round name, such as "QF". Empty when unknown.
	Round string `json:"round,omitempty"`

	// RoundCode is the round in the archive's controlled vocabulary — "F",
	// "SF", "QF", "R16" through "R128", "RR", "BR", "Q" (a qualifying round
	// the feed did not number), "Q1" to "Q4", "ER" — normalised from the
	// free-text Round above. Empty when the label is unrecognised, never
	// guessed.
	RoundCode string `json:"round_code,omitempty"`

	// Status is the lifecycle state: live, upcoming, completed or cancelled.
	Status MatchStatus `json:"status,omitempty"`

	// EventStatus is the API's finer-grained status string, such as a
	// suspension reason. Empty when unset.
	EventStatus string `json:"event_status,omitempty"`

	// EventStatusUpdatedAt is the instant the current EventStatus was
	// recorded, UTC (added 2026-08-19). It bumps only when the value changes
	// — a re-read of the same status never moves it — and a clear back to
	// empty bumps it too. Zero while the status has never changed since the
	// field was introduced: never backfilled, never guessed.
	EventStatusUpdatedAt Time `json:"event_status_updated_at,omitzero"`

	// IsDoubles reports whether this is a doubles match.
	IsDoubles bool `json:"is_doubles,omitempty"`

	// ScheduledTime is the scheduled start. Zero when not yet scheduled.
	ScheduledTime Time `json:"scheduled_time,omitzero"`

	// Players holds both sides.
	Players Players `json:"players,omitzero"`

	// Score is the latest score, and is nil for an upcoming match that has
	// not started. Always check it before dereferencing; the [Score] methods
	// are nil-safe for exactly this reason.
	Score *Score `json:"score,omitempty"`

	// Winner is 1 or 2 on a completed match, derived from the final sets.
	// nil while the match is unfinished or the result is indeterminate.
	Winner *int `json:"winner,omitempty"`

	// Withdrew is which player retired or conceded the walkover, 1 or 2.
	// Present only on a completed match whose EventStatus is "Retired" or
	// "Walk Over" and whose winner is derivable — the withdrawer is the loser
	// by the rules of the sport. nil everywhere else.
	Withdrew *int `json:"withdrew,omitempty"`

	// Tape says what point-by-point data the API holds for this match.
	// Populated by [Client.ListHistoryMatches] and [Client.ListCompletedMatches]
	// only; nil everywhere else.
	Tape *TapeInfo `json:"tape,omitempty"`

	// Market is the match-winner market, embedded by [Client.GetMatch] for
	// PRO and above. nil otherwise.
	Market *Market `json:"market,omitempty"`

	// Analysis is the model analysis, embedded by [Client.GetMatch] for
	// ULTRA. nil otherwise.
	Analysis *Analysis `json:"analysis,omitempty"`
}

// Price is one price tick on a match-winner market.
type Price struct {
	// Side is 1 for player 1's outcome, 2 for player 2's. nil when unknown.
	Side *int `json:"side,omitempty"`

	// Bid is the best bid, from 0 to 1. nil when absent — which is different
	// from a bid of 0, meaning no one will buy at any price.
	Bid *float64 `json:"bid,omitempty"`

	// Ask is the best ask, from 0 to 1. nil when absent.
	Ask *float64 `json:"ask,omitempty"`

	// Mid is the midpoint between bid and ask. nil when absent.
	Mid *float64 `json:"mid,omitempty"`

	// Spread is the gap between bid and ask. nil when absent.
	Spread *float64 `json:"spread,omitempty"`

	// PriceSource is the feed category, for example "prediction_market".
	// Empty when unstated.
	PriceSource string `json:"price_source,omitempty"`

	// Synthetic reports whether the bid/ask were estimated from mid rather
	// than read from a live order book: true = estimated, false = real
	// top-of-book, nil = unknown (older ticks) — tagged so a synthesised
	// quote is never mistaken for a live book.
	Synthetic *bool `json:"synthetic,omitempty"`

	// Timestamp is when the tick was observed. Zero if absent.
	Timestamp Time `json:"timestamp,omitzero"`
}

// Market is a match-winner market. PRO and above.
type Market struct {
	// ID is the market's identifier.
	ID int64 `json:"id,omitempty"`

	// Question is the market's question text. Empty when unset.
	Question string `json:"question,omitempty"`

	// Status is "active", "resolved" or "closed". Empty when unknown.
	Status string `json:"status,omitempty"`

	// Volume is traded volume. nil when absent, which is not zero volume.
	Volume *float64 `json:"volume,omitempty"`

	// Liquidity is available liquidity. nil when absent.
	Liquidity *float64 `json:"liquidity,omitempty"`

	// EndDate is when the market closes. Zero if absent.
	EndDate Time `json:"end_date,omitzero"`

	// Prices holds recent ticks, newest first. Populated by
	// [Client.GetMarketPrices] and by the match-detail embed; empty on
	// [Client.ListMarkets].
	Prices []Price `json:"prices,omitempty"`
}

// Analysis is the model's read on a match. ULTRA only.
//
// Either half may be nil: the model does not cover every match, and a match it
// has not analysed returns a payload with both halves null rather than a 404.
type Analysis struct {
	// Thesis is the directional call, or nil if the model has none.
	Thesis *Thesis `json:"thesis,omitempty"`

	// Profile is the pre-match shape of the contest, or nil.
	Profile *Profile `json:"profile,omitempty"`
}

// Thesis is the model's directional call on a match.
type Thesis struct {
	// PickSide is the player the model favours, 1 or 2. nil when unset.
	PickSide *int `json:"pick_side,omitempty"`

	// Confidence is the model's confidence, from 0 to 1. nil when unset.
	Confidence *float64 `json:"confidence,omitempty"`

	// WinProbabilityPick is the probability the picked side wins, from 0 to 1.
	// nil when unset.
	WinProbabilityPick *float64 `json:"win_probability_pick,omitempty"`

	// State is how the thesis has held up in play: "valid", "confirmed",
	// "weakened" or "broken". Empty when unset.
	State string `json:"state,omitempty"`

	// Reasoning is the prose argument. Empty when unset.
	Reasoning string `json:"reasoning,omitempty"`

	// Notes breaks the reasoning into named factors.
	Notes ThesisNotes `json:"notes,omitzero"`

	// ScenarioPlaybook holds if-then scenarios as raw JSON; the v1 schema does
	// not pin their shape.
	ScenarioPlaybook json.RawMessage `json:"scenario_playbook,omitempty"`

	// CreatedAt is when the thesis was generated. Zero if absent.
	CreatedAt Time `json:"created_at,omitzero"`
}

// ThesisNotes are the named factors behind a [Thesis].
type ThesisNotes struct {
	// Matchup is how the two games interact. Empty when unset.
	Matchup string `json:"matchup,omitempty"`

	// Environment is surface, altitude, conditions. Empty when unset.
	Environment string `json:"environment,omitempty"`

	// Fatigue is recent workload. Empty when unset.
	Fatigue string `json:"fatigue,omitempty"`
}

// Profile is the model's pre-match shape of a contest.
type Profile struct {
	// WinProbabilityP1 is the probability player 1 wins, from 0 to 1. nil
	// when unset.
	WinProbabilityP1 *float64 `json:"win_probability_p1,omitempty"`

	// ExpectedCloseness is how close the model expects the match to be. nil
	// when unset.
	ExpectedCloseness *float64 `json:"expected_closeness,omitempty"`

	// VolatilityRating is "low", "med" or "high". Empty when unset.
	VolatilityRating string `json:"volatility_rating,omitempty"`

	// KeyFactors are the drivers behind the profile.
	KeyFactors []string `json:"key_factors,omitempty"`

	// CreatedAt is when the profile was generated. Zero if absent.
	CreatedAt Time `json:"created_at,omitzero"`
}

// EventType is the kind of thing that happened in a match.
type EventType string

// The event types the API emits.
const (
	EventBreak       EventType = "break"
	EventSetWon      EventType = "set_won"
	EventGameWon     EventType = "game_won"
	EventMomentumRun EventType = "momentum_run"
)

// Event is something that happened in a match. PRO and above.
type Event struct {
	// Type is the kind of event.
	Type EventType `json:"type,omitempty"`

	// Player is which side it happened to, 1 or 2. nil when it applies to
	// neither.
	Player *int `json:"player,omitempty"`

	// Timestamp is when it happened. Zero if absent.
	Timestamp Time `json:"timestamp,omitzero"`
}

// Fixture is a scheduled match on the calendar.
//
// Fixtures are primarily name-keyed: Player1ID and Player2ID are set only
// when a participant resolves to a roster player record (an exact-key match,
// never a name guess), and names are always present regardless. Once a
// fixture goes live it appears through [Client.ListMatches] as a [Match] with
// full [Player] objects.
type Fixture struct {
	// ID is the fixture's identifier.
	ID int64 `json:"id,omitempty"`

	// EventDate is the calendar date of the fixture. Zero if absent.
	EventDate Time `json:"event_date,omitzero"`

	// StartTime is the scheduled start (UTC). Zero until the order of play
	// assigns a time — a date-only fixture is a real state, not missing data.
	StartTime Time `json:"start_time,omitzero"`

	// Player1ID is player 1's roster id, when resolved. nil otherwise.
	Player1ID *int64 `json:"player1_id,omitempty"`

	// Player2ID is player 2's roster id, when resolved. nil otherwise.
	Player2ID *int64 `json:"player2_id,omitempty"`

	// Tour is the circuit, for example "wta" or "juniors_boys". A plain string
	// rather than a [Tour] for the reason given on [Player.Tour]: the returned
	// vocabulary is wider than the filter's.
	Tour string `json:"tour,omitempty"`

	// Tournament is the event name. Empty when unknown.
	Tournament string `json:"tournament,omitempty"`

	// Round is the round name. Empty when unknown.
	Round string `json:"round,omitempty"`

	// RoundCode is the normalised round, in the same vocabulary as
	// [Match.RoundCode]. Empty when the label is unrecognised.
	RoundCode string `json:"round_code,omitempty"`

	// Surface is the court surface. Empty when unknown.
	Surface string `json:"surface,omitempty"`

	// Player1Name is player 1's name as printed on the calendar.
	Player1Name string `json:"player1_name,omitempty"`

	// Player2Name is player 2's name as printed on the calendar.
	Player2Name string `json:"player2_name,omitempty"`

	// Status is the fixture's status. Empty when unknown.
	Status string `json:"status,omitempty"`
}

// Coverage says how a match's point-by-point tape came to exist — and
// specifically whether the API watched the match.
//
// It is a statement about how the rows were obtained, not a synonym for
// "complete": [CoverageFromStart] means every row was committed live from
// 0-0; [CoverageReconstructedPartial] means the tape is known not to cover
// the whole match and must not be backtested as if it did.
type Coverage string

// The coverage vocabulary. An unknown value passed as a filter is rejected
// with [ErrBadRequest] and code "bad_coverage", with the accepted values on
// [APIError.AllowedValues].
const (
	// CoverageFromStart: watched live from 0-0; every row carries a real
	// timestamp.
	CoverageFromStart Coverage = "from_start"

	// CoveragePartial: watched, but recording began after play had started
	// and no reconstruction repairs the opening.
	CoveragePartial Coverage = "partial"

	// CoverageReconstructed: the tape opens with rows expanded after the fact
	// from a finished-match point record — true about the score, silent about
	// the clock.
	CoverageReconstructed Coverage = "reconstructed"

	// CoverageReconstructedPartial: reconstructed, and known not to cover the
	// whole match — it opens after 0-0 or stops short of the final score.
	CoverageReconstructedPartial Coverage = "reconstructed_partial"

	// CoverageNone: no rows at all.
	CoverageNone Coverage = "none"
)

// TapeInfo is the tape summary carried on each match of a history listing,
// so a whole page can be qualified in one call instead of one request per
// match.
type TapeInfo struct {
	// Coverage says how the tape came to exist. See [Coverage].
	Coverage Coverage `json:"coverage,omitempty"`

	// Rows is how many rows were observed (watched live). It is not the
	// length of the tape you will be served — a reconstructed or mixed tape
	// also includes reconstructed rows. Use [TapeMeta.Rows] for that.
	Rows int `json:"rows,omitempty"`

	// ReconstructedRows is how many reconstructed rows are available.
	ReconstructedRows int `json:"reconstructed_rows,omitempty"`
}

// Sequence selects which shape of the tape [Client.GetMatchTape] returns.
type Sequence string

// The tape sequences. An unknown value is rejected with [ErrBadRequest] and
// code "bad_sequence".
const (
	// SequenceRaw is every committed row — deliberately non-monotonic, since
	// independent sources race and a higher-trust one may correct a
	// lower-trust one backwards. This is the API's default.
	SequenceRaw Sequence = "raw"

	// SequenceClean is one row per distinct score state, keeping the last
	// assertion of each. Only clean rows carry [TapeRow.PointWinner].
	SequenceClean Sequence = "clean"
)

// TapeRow is one row of a match's point-by-point score sequence.
//
// Rows watched live carry a real Timestamp. Rows expanded after the fact from
// a finished-match point record carry a zero Timestamp and nil model fields,
// because neither a wall clock nor a model output ever existed for them —
// nothing is synthesised. A zero Timestamp is the reliable row-level marker
// of a reconstructed row; the model fields alone are not, since they are
// stamped best-effort and an observed row may lack them too.
type TapeRow struct {
	// Score is the score state this row records, with the same layout and
	// nil-safe helpers as everywhere else. On ULTRA the model fields
	// ([Score.WinProbabilityP1], [Score.Danger]) are populated per row where
	// the model produced them.
	Score

	// PointWinner is who won the point this row records, 1 or 2 — present
	// only on [SequenceClean] rows, and only where the transition from the
	// previous row is a single attributable point. nil on gaps, torn rows,
	// the first row, and every raw-sequence row (consecutive raw rows are
	// corrections, not points). Derived at read time, never stored or
	// guessed.
	PointWinner *int `json:"point_winner,omitempty"`
}

// SetTiebreak is the final score of one set's tiebreak.
type SetTiebreak struct {
	// P1 is player 1's tiebreak points.
	P1 int `json:"p1"`

	// P2 is player 2's tiebreak points.
	P2 int `json:"p2"`
}

// TapeMeta is the coverage metadata beside a tape.
type TapeMeta struct {
	// MatchID is the match the tape belongs to.
	MatchID int64 `json:"match_id,omitempty"`

	// Rows is how many rows were returned, after any clean-sequence collapse.
	Rows int `json:"rows,omitempty"`

	// Coverage says how the tape came to exist. See [Coverage].
	Coverage Coverage `json:"coverage,omitempty"`

	// PointSource is where the rows came from: "observed" (every row watched
	// live), "reconstructed" (every row expanded after the fact), "mixed" (a
	// reconstructed opening followed by watched rows). Empty on an empty
	// tape. Reported once here and never per row.
	PointSource string `json:"point_source,omitempty"`

	// RawRows is the row count before any collapse; equals Rows for the raw
	// sequence.
	RawRows int `json:"raw_rows,omitempty"`

	// UniqueStates is how many distinct score states the raw tape holds.
	// RawRows minus this is pure repetition.
	UniqueStates int `json:"unique_states,omitempty"`

	// Sequence echoes the requested sequence.
	Sequence Sequence `json:"sequence,omitempty"`

	// FromArchive reports whether the rows were served from the immutable
	// archive rather than the live table. Informational — the content
	// contract is identical.
	FromArchive bool `json:"from_archive,omitempty"`

	// GeneratedAt is when the response was assembled. Zero if absent.
	GeneratedAt Time `json:"generated_at,omitzero"`
}

// MatchTape is the response from [Client.GetMatchTape]: a match header, its
// chronological score sequence, and the coverage metadata that says how much
// of the match the tape actually holds. Check Meta.Coverage before
// backtesting.
type MatchTape struct {
	// Match is the match header.
	Match Match `json:"match"`

	// Tape is the score sequence, chronological. Empty when the match was
	// neither watched nor reconstructable.
	Tape []TapeRow `json:"tape,omitempty"`

	// Tiebreaks holds per-set tiebreak final scores from observed states
	// only, aligned to the sets of the final scoreline: an entry for a 7-6
	// set whose observed maximum tiebreak state is a valid terminal shape,
	// nil per set otherwise — a breaker whose closing point the feed skipped
	// reads nil rather than an under-report. nil entirely when the match has
	// no 7-6 set.
	Tiebreaks []*SetTiebreak `json:"tiebreaks,omitempty"`

	// Profiles holds the model's profiles for the match, oldest first, in the
	// same shape as [Analysis].Profile.
	Profiles []Profile `json:"profiles,omitempty"`

	// Meta is the tape's coverage metadata.
	Meta TapeMeta `json:"meta,omitzero"`
}

// ArchivePlayer is one side of an archive result, as the corpus recorded it
// at the time of the match.
type ArchivePlayer struct {
	// Name is the player's name. Empty when unknown.
	Name string `json:"name,omitempty"`

	// Hand is the playing hand, "R" or "L". Empty when unknown.
	Hand string `json:"hand,omitempty"`

	// Country is the 3-letter code, in the same vocabulary as
	// [Player.Country]. Empty when unknown.
	Country string `json:"country,omitempty"`

	// Rank is the player's rank AT THE TIME of the match, as published. nil
	// when unranked or unrecorded.
	Rank *int `json:"rank,omitempty"`

	// Seed is the draw seed. nil for an unseeded player.
	Seed *int `json:"seed,omitempty"`

	// PlayerID is the corpus person id, which joins
	// [Client.ListArchivePlayers] within the same tour. NOT a roster player
	// id — the archive is a separate id space. nil when unknown.
	PlayerID *int64 `json:"player_id,omitempty"`

	// HeightCm is the height in centimetres. nil when unrecorded.
	HeightCm *int `json:"height_cm,omitempty"`

	// Age is the age at the time of the match, as the corpus records it. nil
	// when unrecorded.
	Age *float64 `json:"age,omitempty"`

	// Entry is the draw entry where recorded — "WC", "Q", "LL", "PR", "SE"
	// and the like. Empty for direct acceptances.
	Entry string `json:"entry,omitempty"`
}

// ArchiveServeStats is one side's per-match serve statistics, where the era
// recorded them.
type ArchiveServeStats struct {
	Aces         *int `json:"aces,omitempty"`
	DoubleFaults *int `json:"double_faults,omitempty"`
	ServePoints  *int `json:"serve_points,omitempty"`
	FirstIn      *int `json:"first_in,omitempty"`
	FirstWon     *int `json:"first_won,omitempty"`
	SecondWon    *int `json:"second_won,omitempty"`
	ServeGames   *int `json:"serve_games,omitempty"`
	BPSaved      *int `json:"bp_saved,omitempty"`
	BPFaced      *int `json:"bp_faced,omitempty"`
}

// ArchiveMatchStats is the per-match statistics block on an archive result.
type ArchiveMatchStats struct {
	Winner *ArchiveServeStats `json:"winner,omitempty"`
	Loser  *ArchiveServeStats `json:"loser,omitempty"`
}

// ArchiveMatch is one result from the deep historical corpus, 1968–2022.
//
// It is winner/loser-shaped because results data is recorded that way at the
// source — the winner is a stored column, never an inference. The archive is
// its own id space, separate from /matches, and ends where the API's own
// point-by-point coverage begins (2023-01), so no match is ever served from
// two datasets.
type ArchiveMatch struct {
	// ID is the archive record's id, usable with [Client.GetArchiveMatch].
	ID int64 `json:"id,omitempty"`

	// SourceID is the stable corpus key.
	SourceID string `json:"source_id,omitempty"`

	// Tour is "atp" or "wta" — the archive covers only those two.
	Tour string `json:"tour,omitempty"`

	// Level is the source tier code: "G" grand slam, "M" masters, "A" tour,
	// "F" finals, "D" Davis Cup, "C" challenger, "O" olympics; futures tiers
	// carry their category codes (e.g. "15", "25") as published.
	Level string `json:"level,omitempty"`

	// Tournament is the event name. Empty when unknown.
	Tournament string `json:"tournament,omitempty"`

	// Surface is the court surface. Empty when unknown.
	Surface string `json:"surface,omitempty"`

	// DrawSize is the draw size. nil when unrecorded.
	DrawSize *int `json:"draw_size,omitempty"`

	// EventDate is the TOURNAMENT START date — per-match dates do not exist
	// in this era's records. Zero if absent.
	EventDate Time `json:"event_date,omitzero"`

	// Round is the round code, e.g. "F", "SF", "R32". Empty when unknown.
	Round string `json:"round,omitempty"`

	// BestOf is 3 or 5. nil when unrecorded.
	BestOf *int `json:"best_of,omitempty"`

	// Minutes is the match duration in minutes. nil when unrecorded.
	Minutes *int `json:"minutes,omitempty"`

	// Winner is the winning side.
	Winner *ArchivePlayer `json:"winner,omitempty"`

	// Loser is the losing side.
	Loser *ArchivePlayer `json:"loser,omitempty"`

	// Score is the final score as published, e.g. "6-4 7-6(5)", "6-3 RET",
	// "W/O". Empty when unknown.
	Score string `json:"score,omitempty"`

	// Outcome is parsed from the score's own vocabulary: "completed",
	// "retired", "walkover", "default" or "abandoned". Empty when
	// unparseable — never guessed.
	Outcome string `json:"outcome,omitempty"`

	// Stats holds both sides' serve statistics where the source recorded
	// them. Populated by [Client.GetArchiveMatch] only, and nil for the
	// (mostly pre-1991) rows the source never recorded statistics for —
	// never synthesised.
	Stats *ArchiveMatchStats `json:"stats,omitempty"`
}

// ArchivePlayerBio is one person of the results archive, in the archive's own
// id space: ID is the corpus person id that archive match rows carry as
// [ArchivePlayer.PlayerID], scoped per tour — never a roster id. Null fields
// are the era's silence, never guessed.
type ArchivePlayerBio struct {
	// ID is the corpus person id.
	ID int64 `json:"id,omitempty"`

	// Tour is "atp" or "wta".
	Tour string `json:"tour,omitempty"`

	// Name is the player's name. Empty when unknown.
	Name string `json:"name,omitempty"`

	// Hand is the playing hand. Empty when unknown.
	Hand string `json:"hand,omitempty"`

	// DOB is the date of birth. Zero when unknown.
	DOB Time `json:"dob,omitzero"`

	// Country is the 3-letter code. Empty when unknown.
	Country string `json:"country,omitempty"`

	// HeightCm is the height in centimetres. nil when unrecorded.
	HeightCm *int `json:"height_cm,omitempty"`

	// CareerHighRank is the best rank reached, computed offline from the
	// corpus's own weekly ranking tables. nil when never ranked or
	// unrecorded.
	CareerHighRank *int `json:"career_high_rank,omitempty"`

	// CareerHighDate is the earliest week the career-high rank was reached.
	// Zero when unknown.
	CareerHighDate Time `json:"career_high_date,omitzero"`
}

// WinLoss is a win-loss split.
type WinLoss struct {
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
}

// ArchiveCareerServe is the summed serve-stat block of an archive career,
// with derived ratios. The corpus records per-match serve statistics from
// 1991 only; MatchesWithStats states the coverage honestly. Ratio fields are
// nil where the denominator is zero.
type ArchiveCareerServe struct {
	MatchesWithStats int      `json:"matches_with_stats,omitempty"`
	Aces             int      `json:"aces,omitempty"`
	DoubleFaults     int      `json:"double_faults,omitempty"`
	ServePoints      int      `json:"serve_points,omitempty"`
	FirstIn          int      `json:"first_in,omitempty"`
	FirstWon         int      `json:"first_won,omitempty"`
	SecondWon        int      `json:"second_won,omitempty"`
	ServeGames       int      `json:"serve_games,omitempty"`
	BPSaved          int      `json:"bp_saved,omitempty"`
	BPFaced          int      `json:"bp_faced,omitempty"`
	FirstInPct       *float64 `json:"first_in_pct,omitempty"`
	FirstWonPct      *float64 `json:"first_won_pct,omitempty"`
	SecondWonPct     *float64 `json:"second_won_pct,omitempty"`
	BPSavedPct       *float64 `json:"bp_saved_pct,omitempty"`
	AcesPerMatch     *float64 `json:"aces_per_match,omitempty"`
}

// ArchiveCareerRecord is the W-L record of an archive career.
type ArchiveCareerRecord struct {
	Wins   int `json:"wins,omitempty"`
	Losses int `json:"losses,omitempty"`

	// Titles is finals won, excluding abandoned finals.
	Titles int `json:"titles,omitempty"`

	// BySurface splits the record by surface name.
	BySurface map[string]WinLoss `json:"by_surface,omitempty"`

	// ByLevel splits the record by source tier code (see
	// [ArchiveMatch.Level]).
	ByLevel map[string]WinLoss `json:"by_level,omitempty"`
}

// ArchiveYearRecord is one season of an archive career.
type ArchiveYearRecord struct {
	Year   int `json:"year,omitempty"`
	Wins   int `json:"wins,omitempty"`
	Losses int `json:"losses,omitempty"`
}

// ArchiveCareer is one player's whole archive career in one response.
// Everything is a sum or a ratio of sums over rows you can fetch individually
// through [Client.ListArchiveMatches] — nothing is modelled.
type ArchiveCareer struct {
	// Player carries the resolved name.
	Player struct {
		Name string `json:"name,omitempty"`
	} `json:"player,omitzero"`

	// Span is the first and last tournament dates of the career.
	Span struct {
		First Time `json:"first,omitzero"`
		Last  Time `json:"last,omitzero"`
	} `json:"span,omitzero"`

	// Record is the W-L record, overall and split.
	Record ArchiveCareerRecord `json:"record,omitzero"`

	// ByYear is the season-by-season record.
	ByYear []ArchiveYearRecord `json:"by_year,omitempty"`

	// Serve is the summed serve-stat block.
	Serve ArchiveCareerServe `json:"serve,omitzero"`
}

// H2HTotals is the headline head-to-head record. Totals count meetings with a
// KNOWN winner; Undecided counts the rest and is never folded into the wins.
type H2HTotals struct {
	P1Wins   int `json:"p1_wins"`
	P2Wins   int `json:"p2_wins"`
	Meetings int `json:"meetings"`

	// Undecided is meetings with no derivable winner.
	Undecided int `json:"undecided"`
}

// H2HSurfaceSplit is one surface's win split in a head-to-head.
type H2HSurfaceSplit struct {
	P1 int `json:"p1"`
	P2 int `json:"p2"`
}

// H2HMeeting is one meeting in a head-to-head, newest first.
//
// Era says which half of the product served the row: "archive" rows carry
// ArchiveMatchID, Level and Score; "current" rows carry MatchID and RoundCode
// and read their score from the match endpoints. Winner is 1 or 2 OF THIS
// HEAD-TO-HEAD — p1 and p2 as requested — not of the original match record.
type H2HMeeting struct {
	// Era is "archive" or "current".
	Era string `json:"era,omitempty"`

	// Date is the meeting's date (for archive rows, the tournament start
	// date). Zero when unknown.
	Date Time `json:"date,omitzero"`

	// Tournament is the event name. Empty when unknown.
	Tournament string `json:"tournament,omitempty"`

	// Level is the archive tier code. Empty on current rows.
	Level string `json:"level,omitempty"`

	// Round is the round label. Empty when unknown.
	Round string `json:"round,omitempty"`

	// RoundCode is the normalised round on current rows. Empty on archive
	// rows.
	RoundCode string `json:"round_code,omitempty"`

	// Surface is the court surface. Empty when unknown.
	Surface string `json:"surface,omitempty"`

	// Score is the final score as published. Empty on current rows, which
	// read their score from the match endpoints.
	Score string `json:"score,omitempty"`

	// Outcome says how the meeting ended ("completed", "retired",
	// "walkover", ...), so walkovers and retirements can be excluded without
	// losing them from the record. Empty when unknown.
	Outcome string `json:"outcome,omitempty"`

	// Winner is 1 or 2 of this head-to-head. nil when underivable.
	Winner *int `json:"winner,omitempty"`

	// ArchiveMatchID joins [Client.GetArchiveMatch] on archive rows. nil on
	// current rows.
	ArchiveMatchID *int64 `json:"archive_match_id,omitempty"`

	// MatchID joins the match endpoints on current rows. nil on archive
	// rows.
	MatchID *int64 `json:"match_id,omitempty"`
}

// HeadToHead is the record between two players, assembled from BOTH halves of
// the product: the results archive (1968–2022), where the winner is a stored
// column, and the API's own completed matches (2023 onward), where the winner
// is derived from the final recorded state.
type HeadToHead struct {
	// Players carries the resolved names as {"p1": {"name": ...}, "p2":
	// {"name": ...}}. nil when no player matches the requested fragments —
	// which also leaves Totals empty.
	Players *struct {
		P1 struct {
			Name string `json:"name,omitempty"`
		} `json:"p1,omitzero"`
		P2 struct {
			Name string `json:"name,omitempty"`
		} `json:"p2,omitzero"`
	} `json:"players,omitempty"`

	// Totals is the headline record.
	Totals H2HTotals `json:"totals,omitzero"`

	// BySurface splits the wins by surface name, plus "unknown".
	BySurface map[string]H2HSurfaceSplit `json:"by_surface,omitempty"`

	// Meetings lists the meetings, newest first, capped at 200.
	Meetings []H2HMeeting `json:"meetings,omitempty"`

	// Stats is the per-player serve/return/break-point aggregate block over
	// the pairing, present on ULTRA only. Carried as raw JSON because the v1
	// schema does not pin its shape; unmarshal it yourself when you need it.
	Stats json.RawMessage `json:"stats,omitempty"`
}

// RankingSystem is a ranking system accepted by [Client.ListRankings].
//
// Systems are never collapsed into a single "rank" — they are not comparable.
// ATP/WTA and the ITF circuits carry rank and points; UTR carries a rating
// with nil rank and points, because it is a rating and has neither.
type RankingSystem string

// The ranking systems.
const (
	RankingATP        RankingSystem = "atp"
	RankingWTA        RankingSystem = "wta"
	RankingITFJuniors RankingSystem = "itf_jt"
	RankingITFMen     RankingSystem = "itf_mt"
	RankingITFWomen   RankingSystem = "itf_wt"
	RankingUTR        RankingSystem = "utr"
)

// RankingRecord is one ranking record in force at the requested instant.
type RankingRecord struct {
	// PlayerID is the roster player id. nil on listing rows for players
	// outside the roster — the published table is served without silent
	// holes, so those rows keep their PlayerName.
	PlayerID *int64 `json:"player_id,omitempty"`

	// PlayerName is the name as the ranking publisher printed it. Present on
	// listing rows; empty on per-player records.
	PlayerName string `json:"player_name,omitempty"`

	// System is the ranking system this record belongs to.
	System RankingSystem `json:"system,omitempty"`

	// Tour is the circuit, where the system implies one. Empty otherwise.
	Tour string `json:"tour,omitempty"`

	// Rank is the published rank. nil for UTR, which has no rank.
	Rank *int `json:"rank,omitempty"`

	// Points is the published points total. nil for UTR.
	Points *int `json:"points,omitempty"`

	// PreviousRank is the rank at the immediately preceding snapshot week.
	// ATP and WTA only; nil when no prior week is held, and always nil for
	// ITF and UTR.
	PreviousRank *int `json:"previous_rank,omitempty"`

	// RankMovement is the circuit's own signed weekly movement. ITF systems
	// only; nil elsewhere.
	RankMovement *int `json:"rank_movement,omitempty"`

	// Rating is the UTR rating. nil for every other system.
	Rating *float64 `json:"rating,omitempty"`

	// EffectiveDate is the publication week this record took effect. For
	// records ingested live rather than from the official weekly publication
	// it is bucketed to the observed week, so it can sit up to six days later
	// than the moment the value took effect. Zero if absent.
	EffectiveDate Time `json:"effective_date,omitzero"`

	// ObservedAt is when the record was observed. Zero if absent.
	ObservedAt Time `json:"observed_at,omitzero"`
}

// RankingsCoverage says what resolved against what was asked. Read it before
// trusting an empty result: ITF and UTR history begins 2026-07-29 and cannot
// be reconstructed earlier, so an as-of before that date correctly returns
// nothing for those systems.
type RankingsCoverage struct {
	// AsOf echoes the requested as-of date. Zero when the latest was asked
	// for.
	AsOf Time `json:"as_of,omitzero"`

	// PlayersRequested and PlayersResolved count the per-player mode's ids.
	PlayersRequested int `json:"players_requested,omitempty"`
	PlayersResolved  int `json:"players_resolved,omitempty"`

	// SystemsRequested and SystemsResolved name the systems asked for and
	// answered.
	SystemsRequested []string `json:"systems_requested,omitempty"`
	SystemsResolved  []string `json:"systems_resolved,omitempty"`

	// OldestAvailable is the earliest effective date held, per requested
	// system. A zero time means the system holds nothing.
	OldestAvailable map[string]Time `json:"oldest_available,omitempty"`
}

// RankingsMeta is the pagination envelope of a rankings response, extended
// with coverage.
type RankingsMeta struct {
	ListMeta

	// Coverage says what resolved against what was asked.
	Coverage RankingsCoverage `json:"coverage,omitzero"`
}

// RankingsPage is one page of [Client.ListRankings].
type RankingsPage struct {
	// Data holds the ranking records.
	Data []RankingRecord `json:"data"`

	// Meta is the pagination envelope with coverage.
	Meta RankingsMeta `json:"meta,omitzero"`
}

// Len returns the number of records on this page.
func (p *RankingsPage) Len() int {
	if p == nil {
		return 0
	}
	return len(p.Data)
}

// StatisticsDescribes is the match state a statistics family describes, per
// upstream: age says when it was fetched, this says WHAT was fetched.
type StatisticsDescribes struct {
	GamesP1    []int `json:"games_p1,omitempty"`
	GamesP2    []int `json:"games_p2,omitempty"`
	TotalGames int   `json:"total_games,omitempty"`
}

// StatisticsFamily is one family's coverage and age.
type StatisticsFamily struct {
	// Coverage is "live", "final" (the closing figures of a completed match,
	// age nil), "stale", "none" or "diverged".
	Coverage string `json:"coverage,omitempty"`

	// AsOf is when the family was last updated. Zero if absent.
	AsOf Time `json:"as_of,omitzero"`

	// AgeSeconds is the family's age. THE TWO FAMILIES USE DIFFERENT CLOCKS
	// and their ages must not be compared: the derived age is measured
	// against the newest score row (between points there is no new score
	// either, so wall-clock age would report staleness that does not exist),
	// while the measured age is wall clock, because those are fetched on a
	// fixed cadence. nil when unknown.
	AgeSeconds *int `json:"age_seconds,omitempty"`

	// Describes is the match state the numbers describe. nil when
	// unavailable.
	Describes *StatisticsDescribes `json:"describes,omitempty"`
}

// MeasuredDivergence says why the measured values were withheld, with both
// match states.
type MeasuredDivergence struct {
	Reason            string `json:"reason,omitempty"`
	GamesInStatistics int    `json:"games_in_statistics,omitempty"`
	GamesInScore      int    `json:"games_in_score,omitempty"`

	// DeltaGames is positive when the statistics are ahead of the score,
	// which staleness cannot cause.
	DeltaGames int    `json:"delta_games,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// StatisticsFreshness is the per-family coverage and age of a statistics
// response. Branch on this rather than on the top-level coverage, which only
// summarises.
type StatisticsFreshness struct {
	// MeasuredDivergence is nil when the families agree; otherwise it says
	// why the measured values were withheld.
	MeasuredDivergence *MeasuredDivergence `json:"measured_divergence,omitempty"`

	Derived  *StatisticsFamily `json:"derived,omitempty"`
	Measured *StatisticsFamily `json:"measured,omitempty"`
}

// StatisticsSide is one player's in-play statistics, in TWO families that are
// deliberately not merged.
//
// The typed fields at this level are DERIVED: rebuilt from the point-by-point
// record. Measured holds counts taken upstream, including the ones no point
// record can yield — aces, double faults, the serve split, winners and
// unforced errors. Both families name some of the same quantities, computed
// two entirely different ways; that is a cross-check, not a duplication to
// collapse.
type StatisticsSide struct {
	// Measured maps measured field names to counts — "aces",
	// "double_faults", "first_serves_in", "winners_total" and the rest of the
	// upstream vocabulary. Coverage is not uniform and an absent field is
	// OMITTED, never zero-filled, so read the keys you are given: a key that
	// is present with 0 is a real measured zero, and a key that is absent was
	// never measured. Aces and double faults are present across every tour;
	// the serve split is absent on ITF singles; winners and unforced errors
	// appear on a minority of main-tour matches. A "_of" suffix is the
	// denominator of its base field and a "_pct" suffix the percentage.
	Measured map[string]int `json:"measured,omitempty"`

	ServiceGamesPlayed int `json:"service_games_played,omitempty"`
	ServiceGamesWon    int `json:"service_games_won,omitempty"`

	// HoldPct is nil when no service game was played — never 0, so a present
	// 0 is a real measured zero. The same rule holds for every other *int
	// percentage here.
	HoldPct *int `json:"hold_pct,omitempty"`

	ReturnGamesPlayed int  `json:"return_games_played,omitempty"`
	ReturnGamesWon    int  `json:"return_games_won,omitempty"`
	BreakPct          *int `json:"break_pct,omitempty"`

	BreakPointsFaced        int  `json:"break_points_faced,omitempty"`
	BreakPointsSaved        int  `json:"break_points_saved,omitempty"`
	BreakPointsSavedPct     *int `json:"break_points_saved_pct,omitempty"`
	BreakPointsPlayed       int  `json:"break_points_played,omitempty"`
	BreakPointsConverted    int  `json:"break_points_converted,omitempty"`
	BreakPointsConvertedPct *int `json:"break_points_converted_pct,omitempty"`

	ServicePointsPlayed int  `json:"service_points_played,omitempty"`
	ServicePointsWon    int  `json:"service_points_won,omitempty"`
	ServicePointsWonPct *int `json:"service_points_won_pct,omitempty"`
	ReturnPointsPlayed  int  `json:"return_points_played,omitempty"`
	ReturnPointsWon     int  `json:"return_points_won,omitempty"`
	ReturnPointsWonPct  *int `json:"return_points_won_pct,omitempty"`

	PointsPlayed int `json:"points_played,omitempty"`
	PointsWon    int `json:"points_won,omitempty"`
}

// MatchStatistics is the in-play statistics for one match. ULTRA.
//
// A coverage of "none" on both families returns a 200 with nil Players, not
// a 404 — the match exists, and holding nothing for it is the honest answer.
type MatchStatistics struct {
	// MatchID is the match these statistics describe.
	MatchID int64 `json:"match_id,omitempty"`

	// Coverage summarises the response: "live", "final", "stale", "none" or
	// "diverged". Branch on Freshness for the per-family truth.
	Coverage string `json:"coverage,omitempty"`

	// AsOf is when the underlying record was last updated. Zero if absent.
	AsOf Time `json:"as_of,omitzero"`

	// AgeSeconds is measured behind the newest SCORE row, not the wall
	// clock. nil when unknown.
	AgeSeconds *int `json:"age_seconds,omitempty"`

	// GamesCounted is how many games the derived family is built from.
	GamesCounted int `json:"games_counted,omitempty"`

	// TiebreakGamesExcluded counts tiebreaks left out of the derived family:
	// the live record collapses a whole tiebreak onto one entry, so most of
	// its points are lost.
	TiebreakGamesExcluded int `json:"tiebreak_games_excluded,omitempty"`

	// InconsistentGamesExcluded counts games whose recorded outcome is
	// neither a legal hold nor a legal break.
	InconsistentGamesExcluded int `json:"inconsistent_games_excluded,omitempty"`

	// SetsCovered names the sets the derived family covers.
	SetsCovered []int `json:"sets_covered,omitempty"`

	// Freshness is the per-family coverage and age.
	Freshness StatisticsFreshness `json:"freshness,omitzero"`

	// Detail explains a coverage of "none". Empty otherwise.
	Detail string `json:"detail,omitempty"`

	// Players holds both sides. nil when coverage is "none".
	Players *struct {
		P1 *StatisticsSide `json:"p1,omitempty"`
		P2 *StatisticsSide `json:"p2,omitempty"`
	} `json:"players,omitempty"`
}

// RallyShot is one stroke of a charted point. Shots are numbered from the
// serve: serve 1, return 2, the server's next ball 3.
type RallyShot struct {
	// Number is the stroke's position in the rally.
	Number int `json:"number,omitempty"`

	// Code is the charter's raw code, e.g. "f".
	Code string `json:"code,omitempty"`

	// Stroke is the parsed stroke type: "serve", "groundstroke", "slice",
	// "volley", "half_volley", "swinging_volley", "overhead", "drop_shot",
	// "lob", "trick" or "unknown". Empty when unparsed.
	Stroke string `json:"stroke,omitempty"`

	// Wing is the side the ball was struck FROM, "forehand" or "backhand".
	// Empty when unknown.
	Wing string `json:"wing,omitempty"`

	// Direction is where the ball was sent: "forehand_side", "middle" or
	// "backhand_side". Empty when unknown.
	Direction string `json:"direction,omitempty"`

	// Depth is "shallow", "mid" or "deep". Empty when unknown.
	Depth string `json:"depth,omitempty"`

	// Position is "approaching", "at_net" or "baseline". Empty when unknown.
	Position string `json:"position,omitempty"`
}

// RallyPoint is one charted point.
//
// Raw is the charter's own string, verbatim, and is always present; the
// parsed fields are the API's reading of it. Parsed is false when the
// notation contained something that could not be read cleanly — the
// recognised part is still returned, and a consumer who wants only
// unambiguous rows filters on Parsed.
type RallyPoint struct {
	// Point is the point's ordinal within the match.
	Point int `json:"point,omitempty"`

	// Set is [sets_p1, sets_p2] at this point; either element may be nil
	// where the charter's score bookkeeping had a hole.
	Set []*int `json:"set,omitempty"`

	// Games is [games_p1, games_p2] at this point, with the same caveat.
	Games []*int `json:"games,omitempty"`

	// Score is the point score as charted, e.g. "30-40". Empty when unknown.
	Score string `json:"score,omitempty"`

	// Game is the game number. nil when unknown.
	Game *int `json:"game,omitempty"`

	// IsTiebreak reports whether the point was played in a tiebreak.
	IsTiebreak bool `json:"is_tiebreak,omitempty"`

	// Server is who served, 1 or 2. nil when unknown.
	Server *int `json:"server,omitempty"`

	// PointWinner is who won the point, 1 or 2. nil when unknown.
	PointWinner *int `json:"point_winner,omitempty"`

	// Raw is the charter's shot string, both serves joined by ";" when the
	// first was a fault.
	Raw string `json:"raw,omitempty"`

	// Parsed reports whether the notation was read cleanly.
	Parsed bool `json:"parsed,omitempty"`

	// ServeNumber is 1 or 2. nil when unknown.
	ServeNumber *int `json:"serve_number,omitempty"`

	// ServeDirection is "wide", "body" or "down_the_t". Empty when unknown.
	ServeDirection string `json:"serve_direction,omitempty"`

	// RallyLength is strokes including the serve: an ace is 1, a double
	// fault 0. nil when unknown.
	RallyLength *int `json:"rally_length,omitempty"`

	// Outcome is "winner", "forced_error", "unforced_error", "error" (the
	// charter recorded a miss without saying whether it was forced — never
	// guessed) or "other". Empty when unknown.
	Outcome string `json:"outcome,omitempty"`

	// ErrorLocation is "net", "wide", "deep" or "wide_and_deep". Empty when
	// the point did not end in an error, or the location went unrecorded.
	ErrorLocation string `json:"error_location,omitempty"`

	// EndingStroke and EndingWing describe the shot that ended the point.
	// Empty when unknown.
	EndingStroke string `json:"ending_stroke,omitempty"`
	EndingWing   string `json:"ending_wing,omitempty"`

	IsAce            bool `json:"is_ace,omitempty"`
	IsDoubleFault    bool `json:"is_double_fault,omitempty"`
	IsServeAndVolley bool `json:"is_serve_and_volley,omitempty"`

	// Shots holds the strokes of the rally, in order.
	Shots []RallyShot `json:"shots,omitempty"`
}

// RallyPlayerRef is one player of a charted match. Charted people are
// identified by name — the charted corpus is its own population, not the
// roster.
type RallyPlayerRef struct {
	// Name is the player's name. Empty when unknown.
	Name string `json:"name,omitempty"`

	// Hand is "R", "L", "U" (unknown) or "A" (ambidextrous). Empty when
	// unrecorded.
	Hand string `json:"hand,omitempty"`
}

// RallyMatch is one charted match with shot-by-shot data. ULTRA.
//
// Rally construction is the layer below the tape: the tape says what the
// score became after each point, this says how the point was played. It has
// its own id space — the charted corpus reaches back decades and concentrates
// on the biggest events, so keying it on the API's own match ids would hide
// most of it.
type RallyMatch struct {
	// RallyMatchID is the id this product is keyed on, usable with
	// [Client.GetRallyMatch].
	RallyMatchID int64 `json:"rally_match_id,omitempty"`

	// SourceID is the stable corpus key.
	SourceID string `json:"source_id,omitempty"`

	// MatchID is the API's own match id, when the charted match is also one
	// it holds. nil otherwise — most charted matches predate the API's own
	// collection.
	MatchID *int64 `json:"match_id,omitempty"`

	// Date is the match date. Zero when unknown.
	Date Time `json:"date,omitzero"`

	// Tournament is the event name. Empty when unknown.
	Tournament string `json:"tournament,omitempty"`

	// Round is the round label. Empty when unknown.
	Round string `json:"round,omitempty"`

	// Surface is the court surface. Empty when unknown.
	Surface string `json:"surface,omitempty"`

	// Gender is "M" or "W". Empty when unknown.
	Gender string `json:"gender,omitempty"`

	// BestOf is 3 or 5. nil when unrecorded.
	BestOf *int `json:"best_of,omitempty"`

	// Players holds both players, in order.
	Players []RallyPlayerRef `json:"players,omitempty"`

	// Points is how many points were charted in this match.
	Points int `json:"points,omitempty"`

	// PointsParsed is how many of them the parser read cleanly — the
	// per-match quality number.
	PointsParsed int `json:"points_parsed,omitempty"`
}

// RallyMatchDetail is one charted match with its points, in play order.
type RallyMatchDetail struct {
	RallyMatch

	// Rally holds the charted points for this page.
	Rally []RallyPoint `json:"rally,omitempty"`

	// Meta paginates the points; Total is the match's full point count.
	Meta ListMeta `json:"meta,omitzero"`
}

// ChartingPlayer is one player's career shot-level charting aggregate, from
// the Match Charting Project: serve placement, return depth and outcomes, net
// and serve-and-volley conversion, clutch serving and returning, winners and
// unforced errors by wing, and rally-length and shot-direction tendencies —
// summed over the player's charted matches.
//
// Coverage is curated, not full-slate: 11,646 charted matches across both
// tours back to the 1960s, concentrated on the majors. MatchesCharted states
// the sample.
type ChartingPlayer struct {
	// Player identifies the resolved player, as raw JSON — the v1 schema
	// does not pin its shape.
	Player json.RawMessage `json:"player,omitempty"`

	// MatchesCharted is how many charted matches the sums cover.
	MatchesCharted int `json:"matches_charted,omitempty"`

	// Coverage describes the sample in prose.
	Coverage string `json:"coverage,omitempty"`

	// Families maps stat-family names to their summed numeric columns, as
	// raw JSON. Every field is a raw SUM over the player's Total rows.
	Families json.RawMessage `json:"families,omitempty"`
}

// ChartingMatch is one charted match with every Match Charting Project stat
// family for both players, with the per-set split (rows "1", "2", "Total")
// exactly as charted. Its id space is the charting product's own, mostly
// matches with no counterpart in the live table.
type ChartingMatch struct {
	// ChartingMatchID is the charting product's id.
	ChartingMatchID int64 `json:"charting_match_id,omitempty"`

	// MCPID is the Match Charting Project's own id string.
	MCPID string `json:"mcp_id,omitempty"`

	// Gender is "M" or "W". Empty when unknown.
	Gender string `json:"gender,omitempty"`

	// Players identifies both players, as raw JSON.
	Players json.RawMessage `json:"players,omitempty"`

	// Families maps stat-family names to per-player, per-set stat rows, as
	// raw JSON — the v1 schema does not pin their shape.
	Families json.RawMessage `json:"families,omitempty"`
}

// PackageKind selects a bulk-package family on [Client.ListHistoryPackages].
type PackageKind string

// The package families. The default is tape, so a tape-only client never
// sees a new kind of row appear.
const (
	// PackageTape is the point-by-point match tapes. PRO and above, or a
	// package subscription.
	PackageTape PackageKind = "tape"

	// PackageRally is the rally-construction packages. ULTRA.
	PackageRally PackageKind = "rally"

	// PackageRankings is the as-of ranking records. ULTRA.
	PackageRankings PackageKind = "rankings"

	// PackageArchive is the results archive (1968–2022) as yearly exports —
	// the period is the bare year "YYYY". Same entitlement as the tape
	// packages, not ULTRA.
	PackageArchive PackageKind = "archive"
)

// PackageFile is one downloadable file of a bulk package.
type PackageFile struct {
	// Format is "jsonl" or "csv". The JSONL file holds one line per match (a
	// whole tape object per line, coverage meta included), not one line per
	// point; the CSV is flattened to one row per point and carries no
	// coverage columns.
	Format   string `json:"format,omitempty"`
	Filename string `json:"filename,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

// HistoryPackage is a published monthly bulk package. Coverage is not a
// contiguous run of months and is still being extended backwards, so treat
// the listing as the authoritative set of months that exist.
type HistoryPackage struct {
	// Period is the month, "YYYY-MM" — or the bare year "YYYY" on the
	// yearly kinds ([PackageRally], [PackageArchive]).
	Period string `json:"period,omitempty"`

	// Status is "ready" — only built months are listed or served.
	Status string `json:"status,omitempty"`

	// MatchCount is the matches in the package — or, on a rankings package,
	// the players covered. nil when unknown.
	MatchCount *int `json:"match_count,omitempty"`

	// RowCount is the tape rows — or ranking records — in the package. nil
	// when unknown.
	RowCount *int `json:"row_count,omitempty"`

	// Files lists the downloadable files.
	Files []PackageFile `json:"files,omitempty"`

	// BuiltAt is when the package was built. Zero if absent.
	BuiltAt Time `json:"built_at,omitzero"`

	// Kind is present only on non-tape packages, so the shape a tape client
	// already parses is unchanged. Empty means tape.
	Kind PackageKind `json:"kind,omitempty"`
}

// HistoryPackagesPage is the response from [Client.ListHistoryPackages].
type HistoryPackagesPage struct {
	// Data holds the packages, newest period first.
	Data []HistoryPackage `json:"data"`

	// Meta carries the count, and echoes Year when one was asked for.
	Meta struct {
		Count int    `json:"count,omitempty"`
		Year  string `json:"year,omitempty"`
	} `json:"meta,omitzero"`
}

// Tournament is one row of the tournament catalogue — the id space
// [Match.TournamentID] joins. Identity is one row per tournament and event
// type, stable across seasons.
type Tournament struct {
	// ID is the stable tournament id that match objects carry.
	ID string `json:"id,omitempty"`

	// Name is the tournament name. Empty when unknown.
	Name string `json:"name,omitempty"`

	// Tour is the circuit, in the filter's own vocabulary (see [Match.Tour]).
	// Empty when the event has no public tour name.
	Tour Tour `json:"tour,omitempty"`

	// Surface is "hard", "clay" or "grass". Empty when unknown.
	Surface string `json:"surface,omitempty"`

	// Indoor reports whether the event is played indoors.
	Indoor bool `json:"indoor,omitempty"`

	// City is the host city, from a curated table. Empty where not curated.
	City string `json:"city,omitempty"`

	// Country is the host country as ISO-3166 alpha-2 — mind that this is a
	// DIFFERENT vocabulary from [Player.Country]'s IOC-style 3-letter codes.
	// Empty where not curated.
	Country string `json:"country,omitempty"`

	// Category is the tournament category where the catalogues agree
	// unambiguously on an exact-name join: "grand_slam", "masters_1000",
	// "tour_finals", "atp_500", "atp_250", "wta_1000", "wta_500", "wta_250",
	// "wta_125", "challenger", "itf" or "juniors". Empty otherwise — never
	// derived from the name, because that would be guesswork.
	Category string `json:"category,omitempty"`
}

// UsageDay is one day of a key's usage history.
type UsageDay struct {
	// Day is the calendar date.
	Day Time `json:"day,omitzero"`

	// Calls is how many requests were made that day.
	Calls int `json:"calls,omitempty"`

	// Errors is how many of them failed.
	Errors int `json:"errors,omitempty"`
}

// Usage is the calling key's own usage against its quota, from
// [Client.GetUsage].
//
// The per-minute window is NOT here — it rides on the X-RateLimit-* headers
// of every response (see [RateLimit]); this is the durable daily picture.
// Usage does not carry the daily reset instant either: that arrives as
// [APIError.ResetsAt] on the daily 429 itself.
type Usage struct {
	// Principal is an opaque reference to your own key.
	Principal string `json:"principal,omitempty"`

	// Tier is the effective tier, lowercase: "free", "basic", "pro" or
	// "ultra". Mind the case — this is the API's own vocabulary here, not
	// the uppercase [Tier] constants this package uses for 403 inference.
	Tier string `json:"tier,omitempty"`

	// BaseTier is the subscription tier; it equals Tier unless a temporary
	// grant is active.
	BaseTier string `json:"base_tier,omitempty"`

	// TierExpiresAt is when a temporary tier grant reverts. Zero when no
	// grant is active.
	TierExpiresAt Time `json:"tier_expires_at,omitzero"`

	// Channel is how the key was issued.
	Channel string `json:"channel,omitempty"`

	// Limits is the quota grid for this key. Either limit is nil when the
	// channel does not enforce it.
	Limits struct {
		PerMinute *int `json:"per_minute,omitempty"`
		PerDay    *int `json:"per_day,omitempty"`
	} `json:"limits,omitzero"`

	// Today is today's usage, current to the second. RemainingDay is nil
	// when no daily limit applies.
	Today struct {
		Calls        int  `json:"calls,omitempty"`
		Errors       int  `json:"errors,omitempty"`
		RemainingDay *int `json:"remaining_day,omitempty"`
	} `json:"today,omitzero"`

	// History is the last 30 days, oldest first.
	History []UsageDay `json:"history,omitempty"`

	// AsOf is when this summary was assembled.
	AsOf Time `json:"as_of,omitzero"`
}

// MatchPricesMeta is the envelope beside a bare price-tick response.
type MatchPricesMeta struct {
	// MatchID echoes the match asked for.
	MatchID int64 `json:"match_id,omitempty"`

	// Count is how many ticks this response holds.
	Count int `json:"count,omitempty"`

	// HasMore reports that the window was clipped at the limit — older ticks
	// exist. There is no offset on this endpoint; raise the limit or narrow
	// the minutes window instead.
	HasMore bool `json:"has_more,omitempty"`

	// Limit echoes the applied limit.
	Limit int `json:"limit,omitempty"`

	// Minutes echoes the lookback window. nil when none was asked for.
	Minutes *int `json:"minutes,omitempty"`
}

// MatchPrices is the bare price ticks of a match's mapped match-winner
// market, newest first, from [Client.ListMatchPrices] — no market wrapper.
type MatchPrices struct {
	// Data holds the ticks, newest first.
	Data []Price `json:"data"`

	// Meta is the window envelope.
	Meta MatchPricesMeta `json:"meta,omitzero"`
}

// WebhookEvent is a frame kind a webhook can subscribe to.
type WebhookEvent string

// The webhook event kinds.
const (
	// WebhookScore delivers a frame on every live score commit. This is the
	// default when a webhook is registered without events.
	WebhookScore WebhookEvent = "score"

	// WebhookBreakPoint delivers break-point frames.
	WebhookBreakPoint WebhookEvent = "break_point"
)

// Webhook is one registered outbound webhook. ULTRA, direct keys only.
//
// The API POSTs the same frames the push WebSocket sends to the registered
// HTTPS endpoint on every matching commit.
type Webhook struct {
	// ID is the webhook's id, usable with [Client.DeleteWebhook].
	ID int64 `json:"id,omitempty"`

	// URL is the delivery endpoint. HTTPS only, publicly routable.
	URL string `json:"url,omitempty"`

	// Events is what the webhook subscribed to.
	Events []WebhookEvent `json:"events,omitempty"`

	// Enabled reports whether deliveries are active.
	Enabled bool `json:"enabled,omitempty"`

	// CreatedAt is when the webhook was registered. Zero if absent.
	CreatedAt Time `json:"created_at,omitzero"`

	// LastDeliveryAt is when the last delivery succeeded. Zero when none has.
	LastDeliveryAt Time `json:"last_delivery_at,omitzero"`

	// ConsecutiveFailures counts deliveries that have failed in a row.
	ConsecutiveFailures int `json:"consecutive_failures,omitempty"`

	// LastError is the most recent delivery error. Empty when none.
	LastError string `json:"last_error,omitempty"`

	// Secret is the signing secret — present ONLY on the registration
	// response from [Client.CreateWebhook], shown exactly once. Store it
	// immediately; it is never returned again, not even by
	// [Client.ListWebhooks].
	Secret string `json:"secret,omitempty"`

	// SecretNote is the API's reminder about the secret's one-time nature.
	SecretNote string `json:"secret_note,omitempty"`
}

// WSChannels is the channel vocabulary of the push WebSocket.
type WSChannels struct {
	// Match is the per-match channel template, "match:{id}".
	Match string `json:"match,omitempty"`

	// Slate is the every-live-score channel, "slate:all".
	Slate string `json:"slate,omitempty"`
}

// WSToken is a short-lived connection token for the high-fan-out push feed,
// from [Client.GetWSToken]. ULTRA.
//
// Frames on the push feed are the same allowlist score objects the polling
// endpoints return. Mint a fresh token on every reconnect — the token
// expires.
type WSToken struct {
	// Token is the signed connection token.
	Token string `json:"token,omitempty"`

	// ExpiresIn is the token's lifetime in seconds.
	ExpiresIn int `json:"expires_in,omitempty"`

	// WSURL is the push WebSocket endpoint to connect to.
	WSURL string `json:"ws_url,omitempty"`

	// Channels is the channel vocabulary.
	Channels WSChannels `json:"channels,omitzero"`
}
