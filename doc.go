// Package livetennisapi is the official Go client for the Live Tennis API:
// real-time tennis scores, player data, match-winner market prices, and
// model-driven match analysis. The API is read-only.
//
// # Getting started
//
// Create a client with your key and call it with a [context.Context]:
//
//	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))
//
//	page, err := client.ListMatches(ctx, livetennisapi.ListMatchesParams{
//		Status: livetennisapi.StatusLive,
//		Limit:  10,
//	})
//	if err != nil {
//		return err
//	}
//	for _, m := range page.Data {
//		fmt.Println(m.Tournament, m.Score)
//	}
//
// A free key is self-serve at https://livetennisapi.com/subscribe/free.
//
// # Tiers
//
// Access is tiered FREE, BASIC, PRO, ULTRA. FREE covers live and upcoming
// matches, scores, players and fixtures; historical results need BASIC; match
// events and market prices need PRO; model analysis and the live model fields
// ([Score.WinProbabilityP1], [Score.Danger]) need ULTRA. Calling above your
// tier returns 403, which this package surfaces as [ErrUpgradeRequired]:
//
//	analysis, err := client.GetMatchAnalysis(ctx, matchID)
//	if errors.Is(err, livetennisapi.ErrUpgradeRequired) {
//		// Valid key, insufficient plan — not an authentication failure.
//	}
//
// That distinction matters: a tier wall proves the key works. Use
// [ErrUnauthorized] for a key problem and [ErrUpgradeRequired] for a plan
// problem, and never treat one as the other.
//
// # Errors
//
// Every non-2xx response becomes an [*APIError] carrying the status, the API's
// machine-readable code, and the rate-limit budget observed on that response.
// Match the common cases with [errors.Is] against the package sentinels
// ([ErrBadRequest], [ErrUnauthorized], [ErrUpgradeRequired], [ErrNotFound],
// [ErrRateLimited], [ErrServerError], [ErrServiceUnavailable]), and reach for
// the concrete type with [errors.As] when you need the detail:
//
//	var apiErr *livetennisapi.APIError
//	if errors.As(err, &apiErr) {
//		log.Printf("%d %s, %d requests left", apiErr.StatusCode, apiErr.Code,
//			apiErr.RateLimit.RemainingOr(-1))
//	}
//
// A request that never produced a response — DNS, TLS, refused, timed out,
// context cancelled — becomes a [*ConnectionError] instead, matching
// [ErrConnection] and, for a deadline, [ErrTimeout].
//
// # Nullability
//
// The API distinguishes "absent" from zero, and so does this package, because
// a rank of 0 or a price of 0 looks meaningful and is not. The rule is:
//
//   - Nullable numbers and booleans are pointers ([Player.Ranking],
//     [Score.Server], [Match.Winner], [Price.Bid]). nil means the API sent
//     null or omitted the field.
//   - Nullable strings are plain strings, where "" already means absent
//     unambiguously ([Player.Country], [Match.Surface]).
//   - Nullable timestamps are [Time], whose zero value means absent; test it
//     with IsZero.
//   - A whole absent object is a nil pointer. In particular [Match.Score] is
//     nil for an upcoming match that has not started, so always check it
//     before dereferencing.
//
// # Forward compatibility
//
// The API ships additive changes within v1. Fields this package does not know
// about are ignored rather than rejected, so a server-side addition never
// breaks an older client. Treat every field as optional.
//
// # Concurrency
//
// A [Client] is safe for concurrent use by multiple goroutines, and is meant
// to be created once and shared. It holds no per-request state.
package livetennisapi
