package livetennisapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// Version is this client's version, reported in the User-Agent.
	Version = "1.2.1"

	// DefaultBaseURL is the production API root.
	DefaultBaseURL = "https://api.livetennisapi.com/api/public/v1"

	// DefaultTimeout is the per-request timeout of the default HTTP client.
	DefaultTimeout = 30 * time.Second

	// DefaultMaxRetries is how many times a retryable failure is retried.
	DefaultMaxRetries = 2

	// MaxLimit is the largest page size the API accepts.
	MaxLimit = 200

	// MaxPriceTicks is the largest tick count [Client.ListMatchPrices]
	// accepts — that endpoint has its own, higher cap in place of pagination.
	MaxPriceTicks = 500

	// maxErrorBody caps how much of an error response is buffered, so a
	// misrouted request that returns a large HTML page cannot balloon memory.
	maxErrorBody = 1 << 20
)

// AuthMethod selects the header that carries the API key.
type AuthMethod int

const (
	// AuthBearer sends the key as "Authorization: Bearer <key>". This is the
	// default, matching the official Python and JS clients.
	AuthBearer AuthMethod = iota

	// AuthAPIKey sends the key as "X-API-Key: <key>" instead. The API accepts
	// either; use this when an intermediary strips or rewrites Authorization
	// headers.
	AuthAPIKey
)

// Client is a Live Tennis API client.
//
// Create one with [New] and share it: it is safe for concurrent use, holds no
// per-request state, and reuses connections through its HTTP client.
type Client struct {
	apiKey     string
	baseURL    string
	userAgent  string
	auth       AuthMethod
	httpClient *http.Client
	maxRetries int
	observe    func(RateLimit)

	// backoff is a field rather than a method so tests can collapse the delay
	// without sleeping through a real retry schedule.
	backoff func(attempt int, retryAfter time.Duration) time.Duration
}

// Option configures a [Client]. Options are applied in order by [New].
type Option func(*Client)

// WithHTTPClient sets the HTTP client used for every request.
//
// This replaces the default client outright, including its 30-second timeout,
// so set a Timeout on the client you pass unless your transport imposes one or
// you rely on per-call context deadlines.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithBaseURL overrides the API root, for a proxy or a test server. A trailing
// slash is trimmed. An empty value is ignored.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/"); baseURL != "" {
			c.baseURL = baseURL
		}
	}
}

// WithUserAgent overrides the User-Agent header.
//
// Please keep a token identifying your application; it is how API support can
// tell your traffic apart when you ask about it.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua = strings.TrimSpace(ua); ua != "" {
			c.userAgent = ua
		}
	}
}

// WithAuthMethod selects which header carries the API key. The default is
// [AuthBearer]; pass [AuthAPIKey] for the X-API-Key header.
func WithAuthMethod(m AuthMethod) Option {
	return func(c *Client) { c.auth = m }
}

// WithMaxRetries sets how many times a retryable failure is retried, on top of
// the initial attempt. Zero disables retrying. Negative values are clamped to
// zero.
//
// Only 429 and 5xx are retried, plus transport failures. Every other 4xx is a
// client-side mistake — a bad key, an unentitled tier, a missing id — that
// cannot start working, and retrying it only burns rate-limit budget.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = max(0, n) }
}

// WithRateLimitObserver registers a callback invoked with the rate-limit
// budget reported on every response, successful or not. It is the only way to
// read the budget on a successful call, since a successful call returns no
// error to carry it.
//
// The callback runs synchronously on the calling goroutine before the method
// returns, and is called once per HTTP attempt — so a retried request calls it
// more than once. Keep it fast and make it safe for concurrent use.
func WithRateLimitObserver(fn func(RateLimit)) Option {
	return func(c *Client) { c.observe = fn }
}

// New returns a client authenticating with apiKey.
//
// Get a free key at https://livetennisapi.com/subscribe/free. The key is read
// only from this argument: pull it from the environment yourself so your
// configuration stays explicit.
//
//	client := livetennisapi.New(os.Getenv("LIVETENNISAPI_KEY"))
//
// An empty key is allowed, because [Client.Health] needs none. Every other
// endpoint will then fail with [ErrUnauthorized].
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     strings.TrimSpace(apiKey),
		baseURL:    DefaultBaseURL,
		userAgent:  "livetennisapi-go/" + Version,
		auth:       AuthBearer,
		httpClient: &http.Client{Timeout: DefaultTimeout},
		maxRetries: DefaultMaxRetries,
		backoff:    defaultBackoff,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// defaultBackoff waits as long as the server asked, and otherwise backs off
// exponentially with jitter so concurrent clients do not retry in lockstep.
func defaultBackoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, time.Minute)
	}
	// Clamped before shifting: an unbounded attempt count would overflow the
	// duration and wrap negative, turning a backoff into no wait at all.
	return min(500*time.Millisecond<<min(attempt, 8)+time.Duration(rand.N(250))*time.Millisecond, 10*time.Second)
}

// shouldRetry reports whether a status is worth retrying. 429 and 5xx are
// transient; nothing else is.
func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

// get performs an authenticated GET and decodes the JSON body into out, which
// may be nil to discard it.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.call(ctx, http.MethodGet, path, query, nil, out)
}

// call performs an authenticated request and decodes the JSON body into out,
// which may be nil to discard it. payload, when non-nil, is marshalled once
// and sent as the JSON request body.
//
// Only GET requests are retried. A POST that timed out may nonetheless have
// been applied — re-sending it could register a duplicate webhook — and a
// re-sent DELETE would answer 404 for work that actually succeeded, so a
// mutation gets exactly one attempt and surfaces its failure honestly.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, payload, out any) error {
	if ctx == nil {
		return errors.New("livetennisapi: nil context")
	}

	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return fmt.Errorf("livetennisapi: encoding request: %w", err)
		}
	}

	endpoint := c.baseURL + "/" + strings.TrimLeft(path, "/")
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	maxRetries := c.maxRetries
	if method != http.MethodGet {
		maxRetries = 0
	}

	for attempt := 0; ; attempt++ {
		resp, err := c.do(ctx, method, endpoint, body)
		if err != nil {
			// A cancelled or expired caller context is final: the caller has
			// stopped waiting, so retrying serves nobody.
			if ctxErr := ctx.Err(); ctxErr != nil || attempt >= maxRetries {
				return connectionError(endpoint, err, ctxErr)
			}
			if waitErr := sleep(ctx, c.backoff(attempt, 0)); waitErr != nil {
				return connectionError(endpoint, err, waitErr)
			}
			continue
		}

		rl := parseRateLimit(resp.Header)
		if c.observe != nil {
			c.observe(rl)
		}

		if shouldRetry(resp.StatusCode) && attempt < maxRetries {
			// Drain before closing so the connection returns to the pool
			// instead of being torn down.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
			resp.Body.Close()
			if waitErr := sleep(ctx, c.backoff(attempt, rl.RetryAfterOr(0))); waitErr != nil {
				return connectionError(endpoint, waitErr, ctx.Err())
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := c.apiError(resp, path, query, endpoint, rl)
			resp.Body.Close()
			return err
		}

		err = decode(resp.Body, out)
		resp.Body.Close()
		return err
	}
}

// do issues a single attempt. A fresh request is built each time because a
// request that has been sent must not be reused.
func (c *Client) do(ctx context.Context, method, endpoint string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		if c.auth == AuthBearer {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		} else {
			req.Header.Set("X-API-Key", c.apiKey)
		}
	}

	return c.httpClient.Do(req)
}

// decode reads a JSON body into out, tolerating a nil out.
func decode(body io.Reader, out any) error {
	if out == nil {
		_, _ = io.Copy(io.Discard, body)
		return nil
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("livetennisapi: decoding response: %w", err)
	}
	return nil
}

// apiError builds the error for a non-2xx response.
func (c *Client) apiError(resp *http.Response, path string, query url.Values, endpoint string, rl RateLimit) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	// Only a string code is usable. An {"error": null} body, or a body that is
	// not JSON at all, must fall through to the status text rather than
	// surface as "null" — the same rule the Python and JS clients apply.
	var payload struct {
		Error      string   `json:"error"`
		Detail     string   `json:"detail"`
		Allowed    []string `json:"allowed"`
		Candidates []string `json:"candidates"`
		Scope      string   `json:"scope"`
		LimitDay   *int     `json:"limit_per_day"`
		ResetsAt   string   `json:"resets_at"`
		RetryEpoch *int64   `json:"retry_at_epoch"`
	}
	_ = json.Unmarshal(body, &payload)

	message := payload.Error
	if message == "" {
		// HTTP/2 carries no status text, so derive it from the code.
		message = http.StatusText(resp.StatusCode)
	}
	if message == "" {
		message = "request failed"
	}

	apiErr := &APIError{
		StatusCode:    resp.StatusCode,
		Code:          payload.Error,
		Message:       message,
		Detail:        payload.Detail,
		RateLimit:     rl,
		URL:           endpoint,
		Header:        resp.Header,
		Body:          body,
		AllowedValues: payload.Allowed,
		Candidates:    payload.Candidates,
	}
	if resp.StatusCode == http.StatusForbidden {
		apiErr.RequiredTier = requiredTierFor(path, query)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		// The three 429 shapes: the per-minute window (bare rate_limited),
		// the daily quota (scope "day" with an absolute resets_at instant,
		// derived from the account's local midnight), and the abuse throttle
		// (retry_at_epoch, a ~24h block on chronically over-cap clients).
		apiErr.Scope = payload.Scope
		apiErr.LimitPerDay = payload.LimitDay
		if payload.ResetsAt != "" {
			var at Time
			if err := at.UnmarshalJSON([]byte(strconv.Quote(payload.ResetsAt))); err == nil && !at.IsZero() {
				apiErr.ResetsAt = at.Time
			}
		}
		if payload.RetryEpoch != nil && *payload.RetryEpoch > 0 {
			apiErr.RetryAt = time.Unix(*payload.RetryEpoch, 0).UTC()
		}
	}
	return apiErr
}

// connectionError wraps a transport failure, preferring the context's own
// error as the cause so [context.Canceled] stays visible to [errors.Is].
func connectionError(endpoint string, err, ctxErr error) error {
	cause := err
	if ctxErr != nil {
		cause = ctxErr
	}

	timeout := errors.Is(cause, context.DeadlineExceeded)
	if !timeout {
		var netErr net.Error
		timeout = errors.As(cause, &netErr) && netErr.Timeout()
	}

	return &ConnectionError{URL: endpoint, Timeout: timeout, Err: cause}
}

// sleep waits for d, or returns early if the context ends first.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
