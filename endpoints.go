package livetennisapi

import (
	"context"
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

// ListMatchesParams filters [Client.ListMatches].
type ListMatchesParams struct {
	// Status selects the lifecycle stage: [StatusLive], [StatusUpcoming] or
	// [StatusCompleted]. Empty means the API's default, which is live.
	Status MatchStatus

	// Tour restricts results to one circuit, its singles and doubles draws
	// alike. Empty means every tour. An unrecognised value is rejected with
	// [ErrBadRequest] rather than quietly ignored.
	Tour Tour

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
// [Match.Winner] derived from the final sets. BASIC — below that it returns
// [ErrUpgradeRequired].
func (c *Client) ListCompletedMatches(ctx context.Context, params ListParams) (*Page[Match], error) {
	q := url.Values{}
	params.apply(q)

	var out Page[Match]
	if err := c.get(ctx, "/history/matches", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListFixtures returns upcoming scheduled fixtures, earliest first. FREE.
//
// Fixtures are name-only — the players are not yet resolved to player records,
// so use [Client.ListMatches] with [StatusUpcoming] when you need ids.
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
