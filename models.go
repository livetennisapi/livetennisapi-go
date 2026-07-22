package livetennisapi

import (
	"encoding/json"
	"strconv"
	"strings"
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

	// Tour is the circuit: "atp", "wta", "challenger", "itf" or "juniors".
	// Empty when unknown.
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

	// Stats is populated by [Client.GetPlayer] only, never by the search
	// endpoint. nil elsewhere.
	Stats *PlayerStats `json:"stats,omitempty"`
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

	// Surface is "hard", "clay" or "grass". Empty when unknown.
	Surface string `json:"surface,omitempty"`

	// Indoor reports whether the court is indoors.
	Indoor bool `json:"indoor,omitempty"`

	// Format is "BO3" or "BO5". Empty when unknown.
	Format string `json:"format,omitempty"`

	// Round is the round name, such as "QF". Empty when unknown.
	Round string `json:"round,omitempty"`

	// Status is the lifecycle state: live, upcoming, completed or cancelled.
	Status MatchStatus `json:"status,omitempty"`

	// EventStatus is the API's finer-grained status string, such as a
	// suspension reason. Empty when unset.
	EventStatus string `json:"event_status,omitempty"`

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
// Fixtures are name-only: the players have not yet been resolved to player
// records, so there are no ids or rankings here. Once a fixture goes live it
// appears through [Client.ListMatches] as a [Match] with full [Player]
// objects.
type Fixture struct {
	// ID is the fixture's identifier.
	ID int64 `json:"id,omitempty"`

	// EventDate is the calendar date of the fixture. Zero if absent.
	EventDate Time `json:"event_date,omitzero"`

	// Tour is the circuit. Empty when unknown.
	Tour string `json:"tour,omitempty"`

	// Tournament is the event name. Empty when unknown.
	Tournament string `json:"tournament,omitempty"`

	// Round is the round name. Empty when unknown.
	Round string `json:"round,omitempty"`

	// Surface is the court surface. Empty when unknown.
	Surface string `json:"surface,omitempty"`

	// Player1Name is player 1's name as printed on the calendar.
	Player1Name string `json:"player1_name,omitempty"`

	// Player2Name is player 2's name as printed on the calendar.
	Player2Name string `json:"player2_name,omitempty"`

	// Status is the fixture's status. Empty when unknown.
	Status string `json:"status,omitempty"`
}
