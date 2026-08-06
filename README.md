<div align="center">

<img src="https://raw.githubusercontent.com/livetennisapi/.github/main/profile/banner.jpg" alt="Live Tennis API" width="640">

# livetennisapi-go

**Official Go client for the [Live Tennis API](https://livetennisapi.com).**

Real-time tennis scores, players, rankings, match-winner market prices and model
win-probability — for ATP, WTA, Challenger, ITF and juniors.

[![Go Reference](https://pkg.go.dev/badge/github.com/livetennisapi/livetennisapi-go.svg)](https://pkg.go.dev/github.com/livetennisapi/livetennisapi-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/livetennisapi/livetennisapi-go)](https://goreportcard.com/report/github.com/livetennisapi/livetennisapi-go)
[![license](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

[**Documentation**](https://docs.livetennisapi.com) · [**Get a free API key**](https://livetennisapi.com/subscribe/free)

</div>

---

## Install

```bash
go get github.com/livetennisapi/livetennisapi-go
```

**Zero dependencies.** Standard library only — `net/http` and `encoding/json`.
Requires Go 1.25 or later.

## Use

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/livetennisapi/livetennisapi-go"
)

func main() {
	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))

	page, err := client.ListMatches(context.Background(), livetennisapi.ListMatchesParams{
		Status:     livetennisapi.StatusLive,
		ListParams: livetennisapi.ListParams{Limit: 10},
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, match := range page.Data {
		fmt.Printf("%-28s %s vs %s — %s\n",
			match.Tournament,
			match.Players.P1.Name,
			match.Players.P2.Name,
			match.Score, // nil-safe: prints "-" before a match starts
		)
	}
}
```

```console
$ LIVETENNISAPI_KEY=twjp_… go run .
M15 Kursumlijska Banja 10    Vlado Jankanj vs Alessandro Bellifemine — 7-5 6-5 (30-15)
```

The key travels as a Bearer token by default, matching the Python and JS clients.
`WithAuthMethod(livetennisapi.AuthAPIKey)` switches it to the `X-API-Key` header,
which the API accepts equally.

## Filtering by tour

`ListMatches` and `ListFixtures` take an optional tour, covering that circuit's
singles and doubles draws alike.

```go
page, err := client.ListMatches(ctx, livetennisapi.ListMatchesParams{
	Status: livetennisapi.StatusLive,
	Tour:   livetennisapi.TourWTA,
})
```

`TourATP`, `TourWTA`, `TourChallenger`, `TourITF`, `TourJuniors`. An unrecognised
tour is a 400, never a silent pass-through, and the accepted list comes back on
the error rather than buried in the body:

```go
_, err := client.ListMatches(ctx, livetennisapi.ListMatchesParams{Tour: "atpp"})

var apiErr *livetennisapi.APIError
if errors.As(err, &apiErr) && apiErr.Code == "bad_tour" {
	fmt.Println(apiErr.AllowedValues) // [atp challenger itf juniors wta]
}
```

> **The filter vocabulary is narrower than the response vocabulary.** Filtering by
> `TourJuniors` returns records whose own `Tour` field reads `"juniors_boys"` or
> `"juniors_girls"`. That is why `Player.Tour` and `Fixture.Tour` are plain
> strings — comparing one to a `Tour` constant will silently fail to match.

## Endpoints

Every method takes a `context.Context` first and returns a typed value and an error.

| Method | Endpoint | Tier |
|---|---|---|
| `Health` | `/health` | none |
| `ListMatches` | `/matches` | FREE¹ |
| `GetMatch` | `/matches/{id}` | FREE |
| `GetMatchScore` | `/matches/{id}/score` | FREE |
| `SearchPlayers` | `/players` | FREE |
| `GetPlayer` | `/players/{id}` | FREE |
| `ListFixtures` | `/fixtures` | FREE |
| `ListCompletedMatches` | `/history/matches` | BASIC |
| `ListMatchEvents` | `/matches/{id}/events` | PRO |
| `ListMarkets` | `/markets` | PRO |
| `GetMarketPrices` | `/markets/{id}/prices` | PRO |
| `GetMatchAnalysis` | `/matches/{id}/analysis` | ULTRA |

¹ `ListMatches` is FREE for `StatusLive` and `StatusUpcoming`. Since 2026-07-25,
`StatusCompleted` returns `ErrUpgradeRequired` on a FREE key — completed-match
listings need the BASIC tier or any History plan. `GetMatch` on a completed
match stays FREE.

`GetMatch` additionally embeds `Market` from PRO and `Analysis` from ULTRA, and
`GetMatchScore` populates `WinProbabilityP1` and `Danger` on ULTRA.

## Options

```go
client := livetennisapi.New(key,
	livetennisapi.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	livetennisapi.WithBaseURL("https://api.livetennisapi.com/api/public/v1"),
	livetennisapi.WithUserAgent("my-app/1.0"),
	livetennisapi.WithAuthMethod(livetennisapi.AuthAPIKey), // default is Bearer
	livetennisapi.WithMaxRetries(2),
	livetennisapi.WithRateLimitObserver(func(rl livetennisapi.RateLimit) {
		log.Printf("%d of %d requests left", rl.RemainingOr(0), rl.LimitOr(0))
	}),
)
```

A `Client` is safe for concurrent use. Create one and share it.

## Errors

A tier wall is not an authentication failure — a 403 proves your key works.
Branch with `errors.Is`, and reach for the detail with `errors.As`.

```go
analysis, err := client.GetMatchAnalysis(ctx, matchID)
switch {
case errors.Is(err, livetennisapi.ErrUpgradeRequired):
	// valid key, plan too low
case errors.Is(err, livetennisapi.ErrUnauthorized):
	// the key itself was rejected
case errors.Is(err, livetennisapi.ErrNotFound):
	// no analysis for this match yet — not a failure
case err != nil:
	return err
}
```

| Sentinel | Meaning |
|---|---|
| `ErrBadRequest` | 400 — a query parameter was malformed |
| `ErrUnauthorized` | 401 — key missing, unknown or disabled |
| `ErrUpgradeRequired` | 403 — your tier does not unlock this endpoint |
| `ErrNotFound` | 404 — no such resource, or no data yet |
| `ErrRateLimited` | 429 — the window was exceeded |
| `ErrServerError` | any 5xx |
| `ErrServiceUnavailable` | 503 — also matches `ErrServerError` |
| `ErrConnection` | no response at all (DNS, TLS, refused, cancelled) |
| `ErrTimeout` | a deadline — also matches `ErrConnection` |
| `ErrAPI` | any error from this package |

The concrete `*APIError` carries `StatusCode`, `Code`, `Message`, `RequiredTier`,
the raw `Body`, and the `RateLimit` budget observed on that response:

```go
var apiErr *livetennisapi.APIError
if errors.As(err, &apiErr) {
	log.Printf("%d %s (needs %s), %d requests left",
		apiErr.StatusCode, apiErr.Code, apiErr.RequiredTier,
		apiErr.RateLimit.RemainingOr(-1))
}
```

## Rate limits

The API reports `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`
and `Retry-After` on every response. The FREE tier is **30 requests/minute**,
100/day.

`RateLimit.Reset` is an absolute instant, not a delay, and every field is a
pointer or zero-testable value so that "no header" stays distinct from "zero
budget left". Note that `Retry-After` appears on successful responses too, where
it merely describes the window — only `ErrRateLimited` means you were throttled.

429 and 5xx are retried automatically (twice by default, honouring `Retry-After`).
Nothing else is: a bad key, an unentitled tier or a missing id cannot start
working, and retrying only burns rate-limit budget.

## Pagination

`Paginate` walks a whole collection, fetching each page only as the loop asks for it.

```go
players := livetennisapi.Paginate(ctx,
	func(ctx context.Context, p livetennisapi.ListParams) (*livetennisapi.Page[livetennisapi.Player], error) {
		return client.SearchPlayers(ctx, livetennisapi.SearchPlayersParams{Search: "nadal", ListParams: p})
	}, 0)

for player, err := range players {
	if err != nil {
		return err
	}
	fmt.Println(player.Name)
}
```

## Payload gotchas

These trip people up against this API in every language, so they are worth stating plainly.

- **`Match.Score` is nil for an upcoming match.** Always check it. The `Score`
  methods (`String`, `GamesForSet`, `NumSets`) are nil-safe; reading a field is not.
- **`Score.Games` is player-major**: `[games_p1, games_p2]`, each a per-set list.
  `[[6,3,2],[4,6,1]]` reads 6-4, 3-6, 2-1. Use `GamesForSet` rather than indexing.
- **`Score.Points` are strings** — `"0"`, `"15"`, `"30"`, `"40"`, `"AD"`. Not integers.
- **Nullable numbers are pointers.** `Player.Ranking` is `*int` because an unranked
  player is not ranked 0. Nullable strings are plain strings, where `""` is
  unambiguous; nullable timestamps are `Time`, whose zero value means absent.
- **`Match.Winner` can be nil on a completed match.** The API omits the key when the
  result is indeterminate — seen in a real recording, not a hypothetical.
- **The tour filter and the tour field are different vocabularies.** See above.
- **`Player.DataCompleteness` tells you what is missing** — `{Known, Of, Missing}` —
  so you can distinguish "not in the feed" from "not fetched" without probing.
  Lower tours carry far less biography than main tour. `Known`/`Of` are `*int`
  because a **doubles team** has them as `null` with an explanatory `Note`: check
  `Applicable()` first, since null there means "does not apply", not zero.
- **A tour filter returns doubles draws too**, as `Player` records with
  `IsDoublesTeam` set, both names in `Name`, and no individual ranking.
- **Unknown fields are ignored, never rejected.** The API ships additive changes
  within v1, so treat every field as optional.
- **`meta.count` describes the page, not the collection.** End-of-data is a short page.

## Development

```bash
go test ./...     # httptest + recorded fixtures, no network
go vet ./...
gofmt -l .
```

Tests never touch the network. `testdata/` holds responses recorded verbatim from
the production API; `testdata/synthetic/` holds hand-written PRO and ULTRA payloads
that a FREE key cannot reach. See [testdata/README.md](testdata/README.md).

## Related

- [livetennisapi-python](https://github.com/livetennisapi/livetennisapi-python) — official Python client
- [livetennisapi-js](https://github.com/livetennisapi/livetennisapi-js) — official JavaScript / TypeScript client
- [openapi](https://github.com/livetennisapi/openapi) — the OpenAPI 3.1 specification

## Licence

MIT — see [LICENSE](LICENSE).

## Affiliate program

Know developers who need tennis data? The [affiliate program](https://affiliates.livetennisapi.com/program) pays 51% recurring commission for the life of every referred subscription — 30-day cookie, and the people you refer get 10% off.
