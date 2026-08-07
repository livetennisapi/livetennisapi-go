package livetennisapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Tier is a subscription level. A call above your tier is refused with 403.
type Tier string

// The subscription tiers, cheapest first.
const (
	TierFree  Tier = "FREE"
	TierBasic Tier = "BASIC"
	TierPro   Tier = "PRO"
	TierUltra Tier = "ULTRA"
)

// Sentinel errors for the cases worth branching on. Compare with [errors.Is]
// rather than by value, since the concrete error is always an [*APIError] or
// a [*ConnectionError] that reports itself as matching these:
//
//	if errors.Is(err, livetennisapi.ErrUpgradeRequired) { ... }
//
// ErrServerError matches any 5xx, so a 503 satisfies both it and
// ErrServiceUnavailable — the same containment the Python and JS clients get
// from subclassing.
var (
	// ErrAPI matches every error this package returns, transport failures
	// included. Useful to tell "the tennis API failed" from an unrelated error
	// further up your own stack.
	ErrAPI = errors.New("livetennisapi: request failed")

	// ErrBadRequest is a 400: a query parameter was malformed.
	ErrBadRequest = errors.New("livetennisapi: bad request")

	// ErrUnauthorized is a 401: the key is missing, unknown, or disabled.
	// This is a credential problem, never a plan problem.
	ErrUnauthorized = errors.New("livetennisapi: unauthorized")

	// ErrUpgradeRequired is a 403: the endpoint exists and your key is valid,
	// but your tier does not unlock it. Treating this as an auth failure is
	// the classic mistake — a 403 proves the key works.
	ErrUpgradeRequired = errors.New("livetennisapi: upgrade required")

	// ErrNotFound is a 404: no such resource, or no data for it yet. Analysis
	// and market endpoints return it for a match the model has not covered.
	ErrNotFound = errors.New("livetennisapi: not found")

	// ErrRateLimited is a 429: the tier's rate-limit window was exceeded.
	// [APIError.RateLimit] carries how long to wait.
	ErrRateLimited = errors.New("livetennisapi: rate limited")

	// ErrServerError is any 5xx: the API failed to serve the request.
	ErrServerError = errors.New("livetennisapi: server error")

	// ErrServiceUnavailable is a 503: the public surface is disabled or down.
	ErrServiceUnavailable = errors.New("livetennisapi: service unavailable")

	// ErrConnection means the request never produced a response — DNS, TLS,
	// connection refused, or a cancelled context.
	ErrConnection = errors.New("livetennisapi: connection failed")

	// ErrTimeout means the request exceeded its deadline. It also matches
	// ErrConnection, since no response was produced either way.
	ErrTimeout = errors.New("livetennisapi: request timed out")
)

// pricingURL is named in the 403 message so the fix is one click away.
const pricingURL = "https://livetennisapi.com/#pricing"

// tierRequirements maps a path fragment to the lowest tier that unlocks it.
// The API answers a tier wall with a bare {"error":"upgrade_required"} and
// does not say which tier is needed, so the client infers it from the endpoint
// it called. First match wins, so the more specific markers sit above the
// broad "/history" one. Kept aligned with the Python and JS clients.
var tierRequirements = []struct {
	marker string
	tier   Tier
}{
	{"/analysis", TierUltra},
	{"/statistics", TierUltra},
	{"/rally", TierUltra},
	{"/charting", TierUltra},
	{"/ws-token", TierUltra},
	{"/events", TierPro},
	{"/markets", TierPro},
	{"/history/packages", TierPro},
	{"/h2h", TierBasic},
	{"/history", TierBasic},
}

// requiredTierFor infers the tier an endpoint needs, or "" when the endpoint
// is on the FREE floor and a 403 therefore has no obvious upgrade to name.
// The query matters only for /rankings, whose two modes are gated apart:
// the rank-ordered listing is PRO, per-player as-of records (?player=) are
// ULTRA.
func requiredTierFor(path string, query url.Values) Tier {
	if strings.Contains(path, "/rankings") {
		if len(query["player"]) > 0 {
			return TierUltra
		}
		return TierPro
	}
	for _, req := range tierRequirements {
		if strings.Contains(path, req.marker) {
			return req.tier
		}
	}
	return ""
}

// APIError is a non-2xx response from the API.
//
// Branch on it with [errors.Is] against the package sentinels for the common
// cases, and pull it out with [errors.As] when you need the detail:
//
//	var apiErr *livetennisapi.APIError
//	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
//		// no data for that match yet
//	}
type APIError struct {
	// StatusCode is the HTTP status.
	StatusCode int

	// Code is the API's machine-readable error code, taken from the response
	// body's "error" field — for example "unauthorized" or "upgrade_required".
	// Empty when the body carried no usable code.
	Code string

	// Message is a human-readable summary: the API's code when it sent one,
	// otherwise the HTTP status text.
	Message string

	// Detail is the API's human-readable explanation, from the body's
	// "detail" field, when one adds anything. Empty otherwise.
	Detail string

	// RateLimit is the budget the API reported on this response.
	RateLimit RateLimit

	// ResetsAt is when the daily quota resets, from the "resets_at" field of
	// a daily 429 (Code "rate_limited" with Scope "day"). It is an absolute
	// instant derived from the account's local midnight — do not assume any
	// fixed UTC hour. Zero on every other error.
	ResetsAt time.Time

	// Scope is the rate-limit window that was exhausted on a 429: "day" for
	// the daily quota, empty for the per-minute window (which carries no
	// scope) and for every non-429 error.
	Scope string

	// LimitPerDay is the daily quota that was exhausted, from the
	// "limit_per_day" field of a daily 429. nil elsewhere.
	LimitPerDay *int

	// RetryAt is when an abuse throttle lifts, from the "retry_at_epoch"
	// field of a 429 with Code "abuse_throttled" — the block the API places
	// on chronically over-cap clients for around 24 hours. If you see it,
	// fix the retry loop that earned it rather than waiting it out. Zero on
	// every other error.
	RetryAt time.Time

	// RequiredTier is the lowest tier that unlocks the endpoint, inferred from
	// the path on a 403. Empty on every other status, and on a 403 from an
	// endpoint that needs no upgrade.
	RequiredTier Tier

	// URL is the request URL, with the API key never included (the key travels
	// in a header, not the query string).
	URL string

	// Header is the full response header, for anything this struct does not
	// model. May be nil.
	Header http.Header

	// Body is the raw response body, kept verbatim so a payload this package
	// failed to model is still available. May be nil.
	Body []byte

	// AllowedValues lists the values the API would have accepted, when it says
	// so. Rejecting a tour answers 400 with
	// {"error":"bad_tour","allowed":["atp","challenger","itf","juniors","wta"]}
	// and this is that list. nil when the response named no alternatives.
	AllowedValues []string

	// Candidates lists the players an ambiguous name fragment matched, from
	// the "candidates" field of a 400 with Code "ambiguous_name" on the
	// head-to-head and archive-career endpoints — the API refuses to sum two
	// people into one record. nil elsewhere.
	Candidates []string
}

// Error implements error.
func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "livetennisapi: %d %s", e.StatusCode, e.Message)
	if e.URL != "" {
		fmt.Fprintf(&b, " (%s)", e.URL)
	}
	if e.RequiredTier != "" {
		fmt.Fprintf(&b, ": this endpoint requires the %s tier, see %s", e.RequiredTier, pricingURL)
	}
	if len(e.AllowedValues) > 0 {
		fmt.Fprintf(&b, ": allowed values are %s", strings.Join(e.AllowedValues, ", "))
	}
	if len(e.Candidates) > 0 {
		fmt.Fprintf(&b, ": candidates are %s", strings.Join(e.Candidates, ", "))
	}
	if !e.RetryAt.IsZero() {
		fmt.Fprintf(&b, ": abuse-throttled until %s — fix the retry loop that earned this", e.RetryAt.UTC().Format(time.RFC3339))
	}
	if !e.ResetsAt.IsZero() {
		fmt.Fprintf(&b, ": daily quota resets at %s", e.ResetsAt.UTC().Format(time.RFC3339))
	}
	if d := e.RateLimit.RetryAfter; d != nil && e.StatusCode == http.StatusTooManyRequests {
		fmt.Fprintf(&b, ": retry after %s", *d)
	}
	return b.String()
}

// Is reports whether this error matches one of the package sentinels, so that
// [errors.Is] works without the caller inspecting StatusCode by hand.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrAPI:
		return true
	case ErrBadRequest:
		return e.StatusCode == http.StatusBadRequest
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrUpgradeRequired:
		return e.StatusCode == http.StatusForbidden
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrServiceUnavailable:
		return e.StatusCode == http.StatusServiceUnavailable
	case ErrServerError:
		return e.StatusCode >= http.StatusInternalServerError
	}
	return false
}

// ConnectionError means the request never produced a response.
//
// It matches [ErrConnection], and additionally [ErrTimeout] when the cause was
// a deadline. The underlying cause is available through [errors.Unwrap], so
// [context.Canceled] and [context.DeadlineExceeded] stay detectable.
type ConnectionError struct {
	// URL is the request URL that could not be reached.
	URL string

	// Timeout reports whether the failure was a deadline rather than a refusal.
	Timeout bool

	// Err is the underlying transport error.
	Err error
}

// Error implements error.
func (e *ConnectionError) Error() string {
	what := "could not reach"
	if e.Timeout {
		what = "timed out calling"
	}
	return fmt.Sprintf("livetennisapi: %s %s: %v", what, e.URL, e.Err)
}

// Unwrap returns the underlying transport error.
func (e *ConnectionError) Unwrap() error { return e.Err }

// Is matches [ErrConnection] always, and [ErrTimeout] for a deadline.
func (e *ConnectionError) Is(target error) bool {
	switch target {
	case ErrAPI, ErrConnection:
		return true
	case ErrTimeout:
		return e.Timeout
	}
	return false
}
