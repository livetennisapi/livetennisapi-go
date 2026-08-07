<div align="center">

<img src="https://raw.githubusercontent.com/livetennisapi/.github/main/profile/banner.jpg" alt="Live Tennis API" width="640">

# livetennisapi-go

**Official Go client for the [Live Tennis API](https://livetennisapi.com).**

Real-time tennis scores, players, point-by-point tapes, a 1968–2022 results
archive, head-to-head records, point-in-time rankings, shot-level charting,
match-winner market prices and model win-probability — for ATP, WTA,
Challenger, ITF and juniors.

[![ci](https://github.com/livetennisapi/livetennisapi-go/actions/workflows/ci.yml/badge.svg)](https://github.com/livetennisapi/livetennisapi-go/actions/workflows/ci.yml)
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
| `ListTournaments` | `/tournaments` | FREE |
| `GetTournament` | `/tournaments/{id}` | FREE |
| `GetUsage` | `/usage` | any (quota-exempt) |
| `ListCompletedMatches` / `ListHistoryMatches` | `/history/matches` | BASIC² |
| `GetMatchTape` | `/history/matches/{id}` | BASIC² |
| `GetHeadToHead` | `/h2h` | BASIC² |
| `ListArchiveMatches` | `/history/archive/matches` | BASIC² |
| `GetArchiveMatch` | `/history/archive/matches/{id}` | BASIC² |
| `ListArchivePlayers` | `/history/archive/players` | BASIC² |
| `GetArchiveCareer` | `/history/archive/career` | BASIC² |
| `ListMatchEvents` | `/matches/{id}/events` | PRO |
| `ListMarkets` | `/markets` | PRO |
| `GetMarketPrices` | `/markets/{id}/prices` | PRO |
| `ListMatchPrices` | `/matches/{id}/prices` | PRO |
| `ListHistoryPackages` | `/history/packages` | PRO³ |
| `GetHistoryPackage` / `DownloadHistoryPackage` | `/history/packages/{period}` | PRO³ |
| `ListRankings` | `/rankings` | PRO / ULTRA⁴ |
| `GetMatchAnalysis` | `/matches/{id}/analysis` | ULTRA |
| `GetMatchStatistics` | `/matches/{id}/statistics` | ULTRA |
| `ListRallyMatches` | `/rally/matches` | ULTRA |
| `GetRallyMatch` | `/rally/matches/{id}` | ULTRA |
| `GetMatchRally` | `/history/matches/{id}/rally` | ULTRA |
| `GetChartingPlayer` | `/charting/players` | ULTRA |
| `GetChartingMatch` | `/charting/matches/{id}` | ULTRA |
| `GetWSToken` | `/ws-token` | ULTRA |
| `CreateWebhook` | `POST /webhooks` | ULTRA⁵ |
| `ListWebhooks` | `/webhooks` | ULTRA⁵ |
| `DeleteWebhook` | `DELETE /webhooks/{id}` | ULTRA⁵ |

¹ `ListMatches` is FREE for `StatusLive` and `StatusUpcoming`. Since 2026-07-25,
`StatusCompleted` returns `ErrUpgradeRequired` on a FREE key — completed-match
listings need the BASIC tier or any History plan. `GetMatch` on a completed
match stays FREE.

² BASIC, **or any History plan** — a History grant unlocks these even on a
FREE core key.

³ The tape kind's floor. `PackageRally` and `PackageRankings` kinds, and the
`Year` archive listing, need ULTRA (or the matching History product).

⁴ `/rankings` has two modes gated apart: the rank-ordered **listing** (one
system, no player ids) is **PRO**; **per-player** point-in-time records
(`Player` ids, up to 50) are **ULTRA**. The client infers the right tier on a
403 from which mode you called.

⁵ **Direct keys only** — a marketplace key is refused with a 403 carrying
code `direct_key_required`. Up to 3 webhooks per key: the 4th registration is
`ErrWebhookLimit` (409). The signing secret is returned exactly once, on
registration. Webhook mutations are never retried automatically — a timed-out
POST may still have been applied.

`GetMatch` additionally embeds `Market` from PRO and `Analysis` from ULTRA, and
`GetMatchScore` populates `WinProbabilityP1` and `Danger` on ULTRA.

This is the API's **complete public surface** — every path in the OpenAPI
spec has a method above. Undocumented gateway aliases and non-API surfaces
(HTML views, static assets/fonts) are deliberately not covered.

## History, archive and head-to-head

The point-by-point **tape** works on live matches too — it is the sequence of
states where `GetMatchScore` is one state. Ask for the clean sequence to get
one row per point, each with `PointWinner`, plus per-set tiebreak scores:

```go
tape, err := client.GetMatchTape(ctx, matchID, livetennisapi.TapeParams{
	Sequence: livetennisapi.SequenceClean,
})
if err != nil {
	return err
}
// Not every tape covers the whole match — check before backtesting.
if tape.Meta.Coverage == livetennisapi.CoverageReconstructedPartial {
	// known-incomplete: rows are real, the match is not fully covered
}
for _, row := range tape.Tape {
	if row.Timestamp.IsZero() {
		continue // reconstructed row: no wall clock, no model fields
	}
	fmt.Println(row.Score.String(), row.PointWinner)
}
```

The **archive** holds 1,485,752 results from 1968 through 2022 — its own id
space, keyed by name, ending exactly where the API's own coverage begins.
**Head-to-head** joins both halves:

```go
h2h, err := client.GetHeadToHead(ctx, "nadal", "djokovic")
if err != nil {
	return err
}
fmt.Printf("%d–%d over %d meetings (%d undecided)\n",
	h2h.Totals.P1Wins, h2h.Totals.P2Wins, h2h.Totals.Meetings, h2h.Totals.Undecided)
```

An ambiguous name fragment is refused with the candidates on
`APIError.Candidates` rather than silently summing two people into one record.

## Rankings

`ListRankings` is the point-in-time answer — every other ranking field in the
API is the player's *current* value joined at read time. The listing mode
(PRO) returns the full published table for one system; the per-player mode
(ULTRA) returns as-of records for up to 50 ids:

```go
page, err := client.ListRankings(ctx, livetennisapi.RankingsParams{
	System: []livetennisapi.RankingSystem{livetennisapi.RankingATP},
	AsOf:   "2026-08-03",
})
```

Rows carry `PreviousRank` (ATP/WTA), and UTR rows carry a `Rating` with nil
`Rank` and `Points` — it is a rating, not a ranking, and the systems are
never collapsed into one number.

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
| `ErrWebhookLimit` | 409 — the key already holds 3 webhooks |
| `ErrRateLimited` | 429 — the window was exceeded |
| `ErrServerError` | any 5xx |
| `ErrServiceUnavailable` | 503 — also matches `ErrServerError` |
| `ErrConnection` | no response at all (DNS, TLS, refused, cancelled) |
| `ErrTimeout` | a deadline — also matches `ErrConnection` |
| `ErrAPI` | any error from this package |

The concrete `*APIError` carries `StatusCode`, `Code`, `Message`, `Detail`,
`RequiredTier`, the raw `Body`, the `RateLimit` budget observed on that
response, and — where the response provided them — `AllowedValues`,
`Candidates` (ambiguous names), `Scope`/`LimitPerDay`/`ResetsAt` (daily 429)
and `RetryAt` (abuse throttle):

```go
var apiErr *livetennisapi.APIError
if errors.As(err, &apiErr) {
	log.Printf("%d %s (needs %s), %d requests left",
		apiErr.StatusCode, apiErr.Code, apiErr.RequiredTier,
		apiErr.RateLimit.RemainingOr(-1))
}
```

## Rate limits & quotas

| Tier | Requests/min | Requests/day | Price |
|---|---|---|---|
| FREE | 30 | 100 | $0 |
| BASIC | 60 | 1,000 | $9.99/mo |
| PRO | 300 | 10,000 | $29.99/mo |
| ULTRA | 600 | 500,000 | $99.99/mo |

At 100/day, poll no faster than every **15 minutes** on a FREE key. An
always-on dashboard should run on BASIC or above.

The API reports `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`
and `Retry-After` on every response. `RateLimit.Reset` is an absolute instant,
not a delay, and every field is a pointer or zero-testable value so that "no
header" stays distinct from "zero budget left". Note that `Retry-After` appears
on successful responses too, where it merely describes the window — only
`ErrRateLimited` means you were throttled.

A 429 comes in three shapes, and the error carries each one's recovery
information:

- **Per-minute window** — wait `RateLimit.RetryAfter` and continue.
- **Daily quota** — `APIError.Scope` is `"day"` and `APIError.ResetsAt` is the
  absolute instant the quota resets (derived from the account's local
  midnight — do not assume any fixed UTC hour). `APIError.LimitPerDay` names
  the cap.
- **Abuse throttle** — `APIError.Code` is `"abuse_throttled"` and
  `APIError.RetryAt` says when the ~24h block lifts. It is placed on
  chronically over-cap clients: fix the retry loop that earned it rather than
  waiting it out.

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
- **`meta.count` describes the page, not the collection.** Use `meta.has_more`
  (or a short page where the endpoint predates it) for end-of-data — `Paginate`
  does both. `meta.total` is `nil` when the set cannot be counted cheaply,
  which is not zero results.
- **`Match.Tour` is typed; `Player.Tour` and `Fixture.Tour` are not.** The
  match field shares the filter's own vocabulary and is safe to compare against
  the `Tour` constants; the other two use the wider response vocabulary
  (`"juniors_boys"`, `"juniors_girls"`).
- **A zero `TapeRow.Timestamp` marks a reconstructed row** — no wall clock and
  no model output ever existed for it, and nothing is synthesised. Check
  `TapeMeta.Coverage` before backtesting a tape.

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

## Links

- **Docs**: https://docs.livetennisapi.com
- **Free API key**: https://livetennisapi.com/subscribe/free
- **Discord**: https://discord.gg/f8WUZHgDm6
- **GitHub org**: https://github.com/livetennisapi

## Licence

MIT — see [LICENSE](LICENSE).

## Affiliate program

Know developers who need tennis data? The [affiliate program](https://affiliates.livetennisapi.com/program) pays 51% recurring commission for the life of every referred subscription — 30-day cookie, and the people you refer get 10% off.
