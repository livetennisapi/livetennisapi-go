# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.2.0] — 2026-08-07

**Full API parity**: every path in the public OpenAPI spec now has a typed
method. Undocumented gateway aliases and non-API surfaces (HTML views, static
assets) are deliberately out of scope.

### Added

- **Tournaments**: `ListTournaments` (search + tour filters) and
  `GetTournament` — the catalogue `Match.TournamentID` joins, with curated
  city/country (ISO-3166 alpha-2 — a different vocabulary from
  `Player.Country`) and `Category` (never derived from the name).
- **Usage**: `GetUsage` (`/usage`, any tier, quota-exempt) — effective vs
  base tier with grant expiry, limits, today's calls and the 30-day history.
  The daily reset instant is not in this response; it arrives on the daily
  429 as `APIError.ResetsAt`.
- **Bare prices**: `ListMatchPrices` (`/matches/{id}/prices`, PRO) — ticks
  without the market wrapper, its own 500 cap (`MaxPriceTicks`) and `Minutes`
  lookback in place of pagination. `Price` gains `PriceSource` and
  `Synthetic`, so an estimated quote is never mistaken for a live book.
- **Package manifest & download**: `GetHistoryPackage`
  (`/history/packages/{period}`) and `DownloadHistoryPackage` (streaming
  `io.ReadCloser` for the jsonl/csv file; verify against the manifest's
  SHA-256).
- **Webhooks** (ULTRA, direct keys only): `CreateWebhook` (secret returned
  exactly once), `ListWebhooks` (never the secret), `DeleteWebhook`. Up to 3
  per key — the 4th registration is `ErrWebhookLimit` (409). A marketplace
  key is refused with 403 `direct_key_required`.
- HTTP core generalised for mutations: POST/DELETE are attempted exactly
  once, never retried — a timed-out POST may still have been applied, and
  re-sending it could register a duplicate webhook.

## [1.1.0] — 2026-08-07

### Added

- **History & archive coverage**: `GetMatchTape` (`/history/matches/{id}`,
  point-by-point tape with `?sequence=raw|clean`, per-set `Tiebreaks` and the
  clean-sequence `PointWinner`), `ListHistoryMatches` (full filter set:
  `From`/`To`, `Coverage`, `Tour`, `Player`, `Country`), `ListArchiveMatches`,
  `GetArchiveMatch`, `ListArchivePlayers` and `GetArchiveCareer` over the
  1968–2022 results archive, and `GetHeadToHead` (`/h2h`) joining both eras.
  `ListCompletedMatches` is unchanged and now delegates to
  `ListHistoryMatches`.
- **Rankings**: `ListRankings` (`/rankings`) with both modes typed and
  documented — the rank-ordered listing (PRO) and per-player as-of records
  (ULTRA) — plus `RankingRecord.PreviousRank` and the coverage meta
  (`oldest_available` per system).
- **In-play statistics**: `GetMatchStatistics` (`/matches/{id}/statistics`,
  ULTRA) with the derived and measured families kept apart, per-family
  freshness, and divergence reporting.
- **Rally & charting**: `ListRallyMatches`, `GetRallyMatch`, `GetMatchRally`
  (`404 not_charted` documented), `GetChartingPlayer` and `GetChartingMatch`
  (ULTRA, Match Charting Project data).
- **Bulk packages**: `ListHistoryPackages` (`/history/packages`) with
  `Kind` (`tape` default, `rally`/`rankings` ULTRA) and the `Year` archive
  listing.
- **Push feed**: `GetWSToken` (`/ws-token`, ULTRA) returning the typed
  `WSToken` with `ws_url` and the channel vocabulary, including `slate:all`.
- **Match fields**: `Tour` (typed — the filter's own vocabulary),
  `TournamentID`, `RoundCode`, `Withdrew` and the history-listing `Tape`
  summary, alongside the existing `EventStatus` and `IsDoubles`.
- **Fixture fields**: `StartTime`, `Player1ID`/`Player2ID`, `RoundCode`.
- **List filters**: `ListMatchesParams` gains `Player` (repeatable, ≤50),
  `Country`, `From` and `To`.
- **Envelope**: `ListMeta.Total` and `ListMeta.HasMore`; `Paginate` now
  honours `has_more` when present and advances the offset by the limit, so
  coverage-filtered history pages (cut before the filter runs) paginate
  correctly instead of ending on the first short page.
- **Errors**: `APIError` now surfaces the daily-quota 429 (`Scope`,
  `LimitPerDay`, `ResetsAt` — an absolute local-midnight-derived instant) and
  the `abuse_throttled` 429 (`RetryAt` from `retry_at_epoch`), plus `Detail`
  and the `ambiguous_name` `Candidates` list. Tier inference covers the new
  endpoints, including the two `/rankings` modes.
- CI truth-pin step (`scripts/truthcheck.sh`) guarding quota and URL copy.

### Changed

- README documents the full endpoint surface with tier gates and the current
  quota grid (2026-08-06 change: FREE 100/day, BASIC 1,000/day, PRO
  10,000/day, ULTRA 500,000/day — reflected in the 1.0.x README on
  2026-08-06), plus free-key polling guidance (poll ≥15 min; always-on
  dashboards on BASIC).

## [1.0.0] — 2026-08-02

### Added

- Initial release: `/health`, `/matches` (live/upcoming/completed, tour
  filter), `/matches/{id}` (+score, events, analysis), `/players` and
  `/players/{id}`, `/markets` and `/markets/{id}/prices`, `/history/matches`,
  `/fixtures`.
- Typed errors with tier inference, automatic retries honouring
  `Retry-After`, rate-limit observation, `Paginate`, and recorded-fixture
  tests. Zero dependencies.
