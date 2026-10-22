package livetennisapi

import (
	"context"
	"net/url"
	"strconv"
)

// StatsSurface is a surface accepted by [PlayerStatsParams.Surface].
//
// [StatsSurfaceHard], [StatsSurfaceClay] and [StatsSurfaceGrass] read the
// match surface. [StatsSurfaceIndoor] keeps matches whose venue is stated
// indoor, on any surface.
type StatsSurface string

// The surfaces [Client.GetPlayerStats] accepts.
const (
	StatsSurfaceHard   StatsSurface = "hard"
	StatsSurfaceClay   StatsSurface = "clay"
	StatsSurfaceGrass  StatsSurface = "grass"
	StatsSurfaceIndoor StatsSurface = "indoor"
)

// PlayerStatsParams filters [Client.GetPlayerStats]. A zero field is omitted
// and the API's own default applies.
type PlayerStatsParams struct {
	// From and To bound the window by scheduled start: "YYYY-MM-DD" or an
	// ISO-8601 UTC datetime. A bare From is the start of that day and a bare
	// To includes the whole day. The default is the 365 days ending now. An
	// unparseable value is rejected with [ErrBadRequest] and code "bad_date".
	From string
	To   string

	// Surface restricts the window to one surface. Empty means every surface.
	Surface StatsSurface

	// Level restricts the window to one match tour, the same vocabulary as
	// the tour filter. A match with no stated tour matches no level. Empty
	// means every level.
	Level Tour

	// Last is how many recent matches [PlayerRecords.Recent] lists, newest
	// first, 1 to 50. Zero means the API default, 20.
	Last int
}

func (p PlayerStatsParams) query() url.Values {
	q := url.Values{}
	if p.From != "" {
		q.Set("from", p.From)
	}
	if p.To != "" {
		q.Set("to", p.To)
	}
	if p.Surface != "" {
		q.Set("surface", string(p.Surface))
	}
	if p.Level != "" {
		q.Set("level", string(p.Level))
	}
	if p.Last > 0 {
		q.Set("last", strconv.Itoa(p.Last))
	}
	return q
}

// GetPlayerStats returns one player's record over our own completed singles
// matches, 2023 onward. PRO — below that it returns [ErrUpgradeRequired].
//
// PRO carries [PlayerStatsReport.Records]: W/L splits, the set-based record,
// withdrawals and the last N matches. ULTRA adds
// [PlayerStatsReport.ServeReturn], the per-match statistics of
// [Client.GetMatchStatistics] summed over the window; below ULTRA it is nil.
//
// A doubles-team id is rejected with [ErrBadRequest] and code
// "doubles_not_supported". For 1968–2022 use [Client.GetArchiveCareer].
func (c *Client) GetPlayerStats(ctx context.Context, playerID int64, params PlayerStatsParams) (*PlayerStatsReport, error) {
	var out PlayerStatsReport
	path := "/players/" + strconv.FormatInt(playerID, 10) + "/stats"
	if err := c.get(ctx, path, params.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PlayerStatsReport is the response of [Client.GetPlayerStats].
type PlayerStatsReport struct {
	// Player identifies whose record this is.
	Player struct {
		ID   int64  `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"player"`

	// Window is the window applied, UTC.
	Window struct {
		From Time `json:"from,omitzero"`
		To   Time `json:"to,omitzero"`
	} `json:"window"`

	// Filters echoes the filters applied. Surface and Level are empty when
	// not filtered.
	Filters struct {
		Surface string `json:"surface,omitempty"`
		Level   string `json:"level,omitempty"`
		Last    int    `json:"last,omitempty"`
	} `json:"filters"`

	// Records is the results-based record (PRO).
	Records *PlayerRecords `json:"records,omitempty"`

	// ServeReturn is the summed serve and return statistics. ULTRA only; nil
	// below ULTRA, where the API omits the key.
	ServeReturn *PlayerServeReturn `json:"serve_return,omitempty"`

	// Meta carries when the response was computed (it is cached up to 60
	// seconds) and a note on what it is computed from.
	Meta struct {
		GeneratedAt Time   `json:"generated_at,omitzero"`
		Note        string `json:"note,omitempty"`
	} `json:"meta"`
}

// PlayerRecords is the results-based record of [PlayerStatsReport]. W/L
// splits count matches with a published winner and an outcome of completed,
// retired, walkover or default.
type PlayerRecords struct {
	Overall      PlayerWinLoss `json:"overall"`
	Last12Months PlayerWinLoss `json:"last_12_months"`

	// BySurface is keyed hard, clay, grass or unknown.
	BySurface map[string]PlayerWinLoss `json:"by_surface,omitempty"`

	// ByVenue is keyed indoor, outdoor or unknown.
	ByVenue map[string]PlayerWinLoss `json:"by_venue,omitempty"`

	// ByLevel is keyed by match tour (atp, wta, challenger, itf, juniors) or
	// unknown.
	ByLevel map[string]PlayerWinLoss `json:"by_level,omitempty"`

	// BySeason is keyed by the UTC calendar year of the scheduled start.
	BySeason map[string]PlayerWinLoss `json:"by_season,omitempty"`

	// Sets is the set-based record, over completed matches whose final score
	// is complete and legal.
	Sets PlayerSetRecord `json:"sets"`

	// Withdrawals counts, per outcome, matches this player did not finish
	// (Given) and matches the opponent did not finish (Received).
	Withdrawals struct {
		Retired  PlayerWithdrawalCount `json:"retired"`
		Walkover PlayerWithdrawalCount `json:"walkover"`
		Default  PlayerWithdrawalCount `json:"default"`
	} `json:"withdrawals"`

	// Excluded counts the matches in the window left out of every record, by
	// reason.
	Excluded struct {
		NotSingles        int `json:"not_singles"`
		Unresolved        int `json:"unresolved"`
		Abandoned         int `json:"abandoned"`
		NoPublishedWinner int `json:"no_published_winner"`
	} `json:"excluded"`

	// Recent is the last N matches in the window, newest first.
	Recent []PlayerRecentMatch `json:"recent,omitempty"`
}

// PlayerSetRecord is the set-based part of [PlayerRecords].
type PlayerSetRecord struct {
	// Sample is the matches the set record is computed over, and the matches
	// counted out by reason.
	Sample struct {
		Matches  int `json:"matches"`
		Excluded struct {
			Retired         int `json:"retired"`
			Walkover        int `json:"walkover"`
			Default         int `json:"default"`
			ScoreIncomplete int `json:"score_incomplete"`
		} `json:"excluded"`
	} `json:"sample"`

	Tiebreaks            PlayerPlayedRecord `json:"tiebreaks"`
	DecidingSets         PlayerPlayedRecord `json:"deciding_sets"`
	AfterWinningFirstSet PlayerSplitRecord  `json:"after_winning_first_set"`
	AfterLosingFirstSet  PlayerSplitRecord  `json:"after_losing_first_set"`

	// Comebacks counts matches in which the player lost at least one set, and
	// how many of them they won.
	Comebacks struct {
		MatchesLostASet int      `json:"matches_lost_a_set"`
		Won             int      `json:"won"`
		WinPct          *float64 `json:"win_pct"`
	} `json:"comebacks"`

	// Sets is sets won and lost.
	Sets struct {
		Won    int      `json:"won"`
		Lost   int      `json:"lost"`
		WinPct *float64 `json:"win_pct"`
	} `json:"sets"`
}

// PlayerWinLoss is a W/L split with its sample size. WinPct is a percent with
// one decimal, nil (never 0) when Matches is 0.
type PlayerWinLoss struct {
	Matches int      `json:"matches"`
	Wins    int      `json:"wins"`
	Losses  int      `json:"losses"`
	WinPct  *float64 `json:"win_pct"`
}

// PlayerPlayedRecord is a record over units played, such as tiebreaks.
// WinPct is nil when Played is 0.
type PlayerPlayedRecord struct {
	Played int      `json:"played"`
	Won    int      `json:"won"`
	Lost   int      `json:"lost"`
	WinPct *float64 `json:"win_pct"`
}

// PlayerSplitRecord is a record over the matches that met a condition, such
// as winning the first set. WinPct is nil when Matches is 0.
type PlayerSplitRecord struct {
	Matches int      `json:"matches"`
	Won     int      `json:"won"`
	Lost    int      `json:"lost"`
	WinPct  *float64 `json:"win_pct"`
}

// PlayerWithdrawalCount is one withdrawal outcome, given and received.
type PlayerWithdrawalCount struct {
	Given    int `json:"given"`
	Received int `json:"received"`
}

// PlayerRecentMatch is one row of [PlayerRecords.Recent].
type PlayerRecentMatch struct {
	MatchID       int64  `json:"match_id"`
	Date          Time   `json:"date,omitzero"`
	ScheduledTime Time   `json:"scheduled_time,omitzero"`
	TournamentID  string `json:"tournament_id,omitempty"`
	Tournament    string `json:"tournament,omitempty"`
	Round         string `json:"round,omitempty"`
	RoundCode     string `json:"round_code,omitempty"`
	Surface       string `json:"surface,omitempty"`
	Indoor        *bool  `json:"indoor,omitempty"`
	Level         string `json:"level,omitempty"`

	Opponent struct {
		ID   int64  `json:"id"`
		Name string `json:"name,omitempty"`
	} `json:"opponent"`

	// Won reports whether this player won the match.
	Won bool `json:"won"`

	// Outcome is completed, retired, walkover or default.
	Outcome string `json:"outcome"`

	// Score is the final score turned round to this player, or nil when no
	// score row is held.
	Score *PlayerRecentScore `json:"score,omitempty"`
}

// PlayerRecentScore is a final score from the player's side.
type PlayerRecentScore struct {
	// Sets is [player, opponent]; an element is nil when not stated.
	Sets []*int `json:"sets,omitempty"`

	// Games is [[player per set], [opponent per set]].
	Games [][]int `json:"games,omitempty"`
}

// PlayerServeReturn is the ULTRA serve and return block of
// [PlayerStatsReport]: measured per-match statistics summed over the
// player's completed matches in the window. Each field carries the matches it
// was summed over.
type PlayerServeReturn struct {
	Matches          int `json:"matches"`
	MatchesWithStats int `json:"matches_with_stats"`

	Serve struct {
		Aces                 PlayerStatCount `json:"aces"`
		DoubleFaults         PlayerStatCount `json:"double_faults"`
		FirstServeIn         PlayerStatRatio `json:"first_serve_in"`
		FirstServePointsWon  PlayerStatRatio `json:"first_serve_points_won"`
		SecondServePointsWon PlayerStatRatio `json:"second_serve_points_won"`
		ServiceGamesWon      PlayerStatRatio `json:"service_games_won"`
		BreakPointsSaved     PlayerStatRatio `json:"break_points_saved"`
	} `json:"serve"`

	Return struct {
		FirstServeReturnPointsWon  PlayerStatRatio `json:"first_serve_return_points_won"`
		SecondServeReturnPointsWon PlayerStatRatio `json:"second_serve_return_points_won"`
		ReturnGamesWon             PlayerStatRatio `json:"return_games_won"`
		BreakPointsConverted       PlayerStatRatio `json:"break_points_converted"`
	} `json:"return"`
}

// PlayerStatRatio is a sum of numerators over a sum of denominators. All
// three values are nil, with Matches 0, when no match carried the field.
type PlayerStatRatio struct {
	Numerator   *int     `json:"numerator"`
	Denominator *int     `json:"denominator"`
	Pct         *float64 `json:"pct"`
	Matches     int      `json:"matches"`
}

// PlayerStatCount is a summed count. Total and PerMatch are nil, with
// Matches 0, when no match carried the field.
type PlayerStatCount struct {
	Total    *int     `json:"total"`
	Matches  int      `json:"matches"`
	PerMatch *float64 `json:"per_match"`
}

// DoublesRanking is [Player.DoublesRanking]: the current position on the
// official ATP/WTA doubles table.
type DoublesRanking struct {
	Rank int `json:"rank"`

	// Points is nil when the table states none.
	Points *int `json:"points,omitempty"`

	// ObservedOn is the UTC date our weekly refresh last found the player on
	// the table.
	ObservedOn Time `json:"observed_on,omitzero"`
}
