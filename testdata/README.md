# Test fixtures

## `testdata/*.json` — real recordings

Captured verbatim from the production API (`https://api.livetennisapi.com/api/public/v1`)
on 2026-07-22 with a FREE-tier key. Bodies are stored exactly as received: not
reformatted, not trimmed, nothing redacted. No API key appears in any of them —
the key travels in a request header, and only response bodies are stored here.

| File | Request | Status |
|---|---|---|
| `health.json` | `/health` (no auth) | 200 |
| `matches_live.json` | `/matches?status=live&limit=3` | 200 |
| `matches_upcoming.json` | `/matches?status=upcoming&limit=3` | 200 |
| `matches_completed.json` | `/matches?status=completed&limit=2` | 200 — see note below |
| `matches_tour_wta.json` | `/matches?tour=wta&limit=2` | 200 |
| `match_detail.json` | `/matches/21635` | 200 |
| `score.json` | `/matches/21635/score` | 200 |
| `players_search.json` | `/players?search=alcaraz&limit=3` | 200 |
| `player.json` | `/players/2317` | 200 |
| `fixtures.json` | `/fixtures?limit=3` | 200 |
| `fixtures_tour_juniors.json` | `/fixtures?tour=juniors&limit=3` | 200 |
| `error_401.json` | `/matches` with no credentials | 401 |
| `error_bad_tour.json` | `/matches?tour=bogus` | 400 |
| `error_403_upgrade_required.json` | `/matches/21635/analysis` (ULTRA) | 403 |
| `error_403_history.json` | `/history/matches?limit=2` (BASIC) | 403 |

**Note on `matches_completed.json`:** the 200 above was real when recorded, but
the recording predates the 2026-07-25 gating change. Since that date,
`/matches?status=completed` returns `403 {"error":"upgrade_required"}` on a
FREE-tier key — completed-match listings need the BASIC tier or any History
plan. The fixture still exercises decoding of a completed-match page (the body
shape is unchanged for entitled keys); it no longer represents what a FREE key
receives from production. Single-match fetches of completed matches
(`/matches/{id}`) remain FREE.

## `testdata/synthetic/*.json` — hand-written, NOT recordings

These model BASIC, PRO and ULTRA payloads. They could not be recorded: the key
used for the capture is FREE tier, and every one of these endpoints answers it
with `403 {"error":"upgrade_required"}` — which is itself recorded above, in
`error_403_upgrade_required.json` and `error_403_history.json`.

Their **shapes** follow `components/schemas` in the OpenAPI spec, but their
**values are invented**. Re-record them against an entitled key before
trusting them as a contract.

| File | Models | Tier of the real endpoint |
|---|---|---|
| `markets.json`, `market_prices.json` | `Market`, `Price` | PRO |
| `events.json` | `Event` | PRO |
| `analysis.json`, `analysis_uncovered.json` | `Analysis`, `Thesis`, `Profile` | ULTRA |
| `tape.json` | `MatchTape`, `TapeRow`, `SetTiebreak`, `TapeMeta` | BASIC |
| `h2h.json` | `HeadToHead`, `H2HMeeting` | BASIC (stats block ULTRA) |
| `archive_matches.json`, `archive_match.json` | `ArchiveMatch`, `ArchivePlayer`, `ArchiveMatchStats` | BASIC |
| `archive_players.json` | `ArchivePlayerBio` | BASIC |
| `archive_career.json` | `ArchiveCareer` | BASIC |
| `rankings_listing.json` | `RankingRecord`, `RankingsMeta` (listing mode) | PRO |
| `rankings_players.json` | `RankingRecord`, `RankingsMeta` (per-player mode) | ULTRA |
| `statistics.json` | `MatchStatistics`, `StatisticsSide` | ULTRA |
| `rally_matches.json`, `rally_match.json` | `RallyMatch`, `RallyPoint`, `RallyShot` | ULTRA |
| `charting_player.json`, `charting_match.json` | `ChartingPlayer`, `ChartingMatch` | ULTRA |
| `history_packages.json`, `package_manifest.json` | `HistoryPackage`, `PackageFile` | PRO |
| `ws_token.json` | `WSToken`, `WSChannels` | ULTRA |
| `tournaments.json`, `tournament.json` | `Tournament` | FREE (not recorded: capture key retired) |
| `usage.json` | `Usage`, `UsageDay` | any (not recorded: capture key retired) |
| `match_prices.json` | `MatchPrices`, `Price` (source/synthetic tags) | PRO |
| `webhook_created.json`, `webhooks.json` | `Webhook` | ULTRA, direct keys |
