package livetennisapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

// pagedPlayers serves a synthetic collection of `total` players, honouring
// limit and offset the way the API does.
func pagedPlayers(total int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 50
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

		end := min(offset+limit, total)
		items := make([]string, 0, limit)
		for i := offset; i < end; i++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"name":"Player %d"}`, i, i))
		}

		fmt.Fprintf(w, `{"data":[%s],"meta":{"limit":%d,"offset":%d,"count":%d}}`,
			join(items), limit, offset, len(items))
	}
}

func join(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += item
	}
	return out
}

func TestPaginateWalksEveryPage(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		pageSize int
		wantReqs int
	}{
		// 25 items in pages of 10: two full pages then a short one ends it.
		{name: "short final page", total: 25, pageSize: 10, wantReqs: 3},
		// An exact multiple needs one extra request to see the empty page.
		{name: "exact multiple", total: 20, pageSize: 10, wantReqs: 3},
		{name: "single short page", total: 4, pageSize: 10, wantReqs: 1},
		{name: "empty collection", total: 0, pageSize: 10, wantReqs: 1},
		{name: "page size is clamped to MaxLimit", total: 5, pageSize: 100000, wantReqs: 1},
		{name: "zero page size means the maximum", total: 5, pageSize: 0, wantReqs: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				pagedPlayers(tc.total)(w, r)
			}))

			seq := Paginate(t.Context(),
				func(ctx context.Context, p ListParams) (*Page[Player], error) {
					return client.SearchPlayers(ctx, SearchPlayersParams{ListParams: p})
				}, tc.pageSize)

			var seen []int64
			for player, err := range seq {
				if err != nil {
					t.Fatalf("iteration error: %v", err)
				}
				seen = append(seen, player.ID)
			}

			if len(seen) != tc.total {
				t.Errorf("yielded %d players, want %d", len(seen), tc.total)
			}
			for i, id := range seen {
				if id != int64(i) {
					t.Fatalf("item %d has id %d: pages are out of order or repeated", i, id)
				}
			}
			if requests != tc.wantReqs {
				t.Errorf("made %d requests, want %d", requests, tc.wantReqs)
			}
		})
	}
}

// The coverage-filtered history listing cuts the page BEFORE the filter
// runs, so a page can come back short — even empty — while later pages still
// hold matches. When the API says has_more, Paginate must believe it over
// the short-page heuristic, and advance the offset by the limit rather than
// by the rows received.
func TestPaginateHonoursHasMore(t *testing.T) {
	// Three pre-filter pages of 10; the filter keeps 2, 0 and 1 rows.
	pages := []struct {
		kept    int
		hasMore bool
	}{
		{kept: 2, hasMore: true},
		{kept: 0, hasMore: true},
		{kept: 1, hasMore: false},
	}

	var requests int
	var offsets []int
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		offsets = append(offsets, offset)
		page := pages[requests]
		requests++

		items := make([]string, 0, page.kept)
		for i := range page.kept {
			items = append(items, fmt.Sprintf(`{"id":%d}`, offset+i))
		}
		fmt.Fprintf(w, `{"data":[%s],"meta":{"limit":10,"offset":%d,"count":%d,"has_more":%v}}`,
			join(items), offset, page.kept, page.hasMore)
	}))

	seq := Paginate(t.Context(),
		func(ctx context.Context, p ListParams) (*Page[Match], error) {
			return client.ListHistoryMatches(ctx, HistoryMatchesParams{
				Coverage:   CoverageFromStart,
				ListParams: p,
			})
		}, 10)

	items := 0
	for _, err := range seq {
		if err != nil {
			t.Fatalf("iteration error: %v", err)
		}
		items++
	}

	if items != 3 {
		t.Errorf("yielded %d items, want 3", items)
	}
	if requests != 3 {
		t.Errorf("made %d requests, want 3: short filtered pages must not end iteration while has_more is true", requests)
	}
	// The offset space is the pre-filter one.
	want := []int{0, 10, 20}
	for i, offset := range offsets {
		if offset != want[i] {
			t.Errorf("request %d used offset %d, want %d", i, offset, want[i])
		}
	}
}

func TestPaginateStopsEarlyWhenTheLoopBreaks(t *testing.T) {
	var requests int
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		pagedPlayers(1000)(w, r)
	}))

	seq := Paginate(t.Context(),
		func(ctx context.Context, p ListParams) (*Page[Player], error) {
			return client.SearchPlayers(ctx, SearchPlayersParams{ListParams: p})
		}, 10)

	count := 0
	for range seq {
		count++
		if count == 3 {
			break
		}
	}

	if count != 3 {
		t.Errorf("consumed %d items, want 3", count)
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1: breaking must not prefetch", requests)
	}
}

// An error must be yielded once and end iteration, never be swallowed and
// never loop.
func TestPaginateYieldsErrorAndStops(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"upgrade_required"}`))
	}), WithMaxRetries(0))

	seq := Paginate(t.Context(),
		func(ctx context.Context, p ListParams) (*Page[Match], error) {
			return client.ListCompletedMatches(ctx, p)
		}, 10)

	var errs []error
	items := 0
	for match, err := range seq {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		_ = match
		items++
	}

	if items != 0 {
		t.Errorf("yielded %d items alongside the error, want 0", items)
	}
	if len(errs) != 1 {
		t.Fatalf("yielded %d errors, want exactly 1", len(errs))
	}
	if !errors.Is(errs[0], ErrUpgradeRequired) {
		t.Errorf("error = %v, want ErrUpgradeRequired", errs[0])
	}
}

func TestPaginateHonoursContextCancellation(t *testing.T) {
	client := newTestClient(t, pagedPlayers(1000))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	seq := Paginate(ctx, func(ctx context.Context, p ListParams) (*Page[Player], error) {
		return client.SearchPlayers(ctx, SearchPlayersParams{ListParams: p})
	}, 10)

	var got error
	for _, err := range seq {
		got = err
		break
	}
	if !errors.Is(got, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", got)
	}
}

func TestPaginateWithNilFetchIsEmpty(t *testing.T) {
	for range Paginate[Player](t.Context(), nil, 10) {
		t.Fatal("a nil fetch func should yield nothing")
	}
}
