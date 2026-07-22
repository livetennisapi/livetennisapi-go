package livetennisapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimit is the rate-limit budget the API reported on a single response.
//
// The API sets X-RateLimit-Limit, X-RateLimit-Remaining and X-RateLimit-Reset
// on every response, and exposes them to browsers via CORS. Each field is a
// pointer or a zero-testable value because "the header was absent" and "the
// value was zero" are genuinely different: Remaining of 0 means the budget is
// spent, while an absent header means nothing was reported at all.
//
// Observe the budget on a successful call with [WithRateLimitObserver]; on a
// failed one it is carried by [APIError.RateLimit].
type RateLimit struct {
	// Limit is X-RateLimit-Limit, the ceiling for the current window. nil if
	// the header was absent. The FREE tier is 30 requests per minute.
	Limit *int

	// Remaining is X-RateLimit-Remaining, the requests left in the current
	// window. nil if the header was absent.
	Remaining *int

	// Reset is X-RateLimit-Reset, the instant the current window rolls over.
	// The API emits it as unix epoch seconds, not as a delay. The zero value
	// means the header was absent; test with Reset.IsZero.
	Reset time.Time

	// RetryAfter is the Retry-After header as a duration. nil if absent.
	//
	// Do not read this as proof of throttling: the API sets Retry-After on
	// ordinary 2xx responses too, where it merely describes the window. Only
	// a 429 (see [ErrRateLimited]) means you were actually limited.
	RetryAfter *time.Duration
}

// Known reports whether the response carried any rate-limit information at all.
func (r RateLimit) Known() bool {
	return r.Limit != nil || r.Remaining != nil || !r.Reset.IsZero() || r.RetryAfter != nil
}

// LimitOr returns the reported limit, or def if the header was absent.
func (r RateLimit) LimitOr(def int) int {
	if r.Limit == nil {
		return def
	}
	return *r.Limit
}

// RemainingOr returns the reported remaining budget, or def if the header was
// absent. Pass a negative default to keep "unknown" distinct from "exhausted".
func (r RateLimit) RemainingOr(def int) int {
	if r.Remaining == nil {
		return def
	}
	return *r.Remaining
}

// RetryAfterOr returns the reported Retry-After delay, or def if absent.
func (r RateLimit) RetryAfterOr(def time.Duration) time.Duration {
	if r.RetryAfter == nil {
		return def
	}
	return *r.RetryAfter
}

// parseRateLimit reads the rate-limit headers off a response. It never fails:
// a header that is missing or unparseable is simply left unset, because a
// malformed budget is not a reason to fail a request that otherwise succeeded.
func parseRateLimit(h http.Header) RateLimit {
	var rl RateLimit

	if n, ok := parseHeaderInt(h.Get("X-RateLimit-Limit")); ok {
		rl.Limit = &n
	}
	if n, ok := parseHeaderInt(h.Get("X-RateLimit-Remaining")); ok {
		rl.Remaining = &n
	}
	// Unix epoch seconds. A non-positive value carries no information, so it
	// is treated as absent rather than decoded into 1970.
	if n, ok := parseHeaderInt(h.Get("X-RateLimit-Reset")); ok && n > 0 {
		rl.Reset = time.Unix(int64(n), 0).UTC()
	}
	if d, ok := parseRetryAfter(h.Get("Retry-After")); ok {
		rl.RetryAfter = &d
	}

	return rl
}

func parseHeaderInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseRetryAfter handles both forms RFC 9110 allows: delta-seconds, which is
// what this API emits, and an HTTP-date, accepted so a proxy that rewrites the
// header cannot break retry timing. A date already in the past yields zero
// rather than a negative delay.
func parseRetryAfter(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}

	if secs, err := strconv.ParseFloat(raw, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs * float64(time.Second)), true
	}

	if when, err := http.ParseTime(raw); err == nil {
		if d := time.Until(when); d > 0 {
			return d, true
		}
		return 0, true
	}

	return 0, false
}
