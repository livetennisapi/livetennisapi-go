package livetennisapi

import (
	"context"
	"iter"
)

// Paginate walks every page of a list endpoint and yields the items one at a
// time, fetching the next page only as the range loop asks for it.
//
// fetch is called with the pagination to apply, which lets it close over
// whatever other filters the endpoint takes:
//
//	seq := livetennisapi.Paginate(ctx,
//		func(ctx context.Context, p livetennisapi.ListParams) (*livetennisapi.Page[livetennisapi.Player], error) {
//			return client.SearchPlayers(ctx, livetennisapi.SearchPlayersParams{Search: "nadal", ListParams: p})
//		}, 0)
//
//	for player, err := range seq {
//		if err != nil {
//			return err
//		}
//		fmt.Println(player.Name)
//	}
//
// An error is yielded once, with the zero item, and then iteration stops — so
// a loop that ignores err would spin on nothing rather than forever. Always
// check it.
//
// pageSize is clamped to 1..[MaxLimit]; pass 0 for the largest page allowed,
// which minimises requests against your rate limit. Iteration ends on the
// first short page, which is the only reliable end-of-data signal the API
// gives: [ListMeta.Count] describes the page, not the collection.
func Paginate[T any](
	ctx context.Context,
	fetch func(context.Context, ListParams) (*Page[T], error),
	pageSize int,
) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T

		if fetch == nil {
			return
		}

		limit := MaxLimit
		if pageSize > 0 {
			limit = min(pageSize, MaxLimit)
		}

		for offset := 0; ; {
			if err := ctx.Err(); err != nil {
				yield(zero, err)
				return
			}

			page, err := fetch(ctx, ListParams{Limit: limit, Offset: offset})
			if err != nil {
				yield(zero, err)
				return
			}
			if page == nil {
				return
			}

			for _, item := range page.Data {
				if !yield(item, nil) {
					return
				}
			}

			// A page shorter than requested is the last one. An empty page
			// also ends iteration, which additionally guards against an
			// endpoint that ignores offset and would otherwise loop forever.
			if len(page.Data) < limit {
				return
			}
			offset += len(page.Data)
		}
	}
}
