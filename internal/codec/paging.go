package codec

import (
	"context"
	"fmt"
	"iter"

	"github.com/langgenius/dify-go-sdk/internal/kernel"
)

// MaxWalk is how many items Page.All walks before it stops and says so.
//
// A walk is a loop the server controls: it ends when Dify stops saying
// has_more. A server that never stops — a bug, a proxy, a listing growing
// faster than it is read — would otherwise be an unbounded number of requests
// inside what reads like an ordinary for loop. Reaching this yields an error
// rather than quietly truncating, because a walk that stops early without
// saying so is the bug All exists to fix.
const MaxWalk = 10_000

// PageLimitError is yielded by Page.All when a listing keeps going past its
// ceiling. Everything before it was yielded normally.
type PageLimitError struct {
	Limit int
}

func (e *PageLimitError) Error() string {
	return fmt.Sprintf("dify: walked %d items and Dify still says there are more; "+
		"pass a larger ceiling to All if the listing really is this long, or narrow it with the listing's own filters", e.Limit)
}

// Page is one page of a listing, and how to get the rest.
//
// Dify pages some listings by number and others by cursor (last_id or
// first_id), and answers a few with a bare array. The difference stays
// visible in each listing's parameters; what a caller does with the result
// does not depend on it.
//
//	page, err := app.Chat.Conversations.List(ctx, nil)
//	for _, c := range page.Items { ... }           // this page
//	for c, err := range page.All(ctx) { ... }      // every page, fetched as it goes
type Page[T any] struct {
	Items []T
	// HasMore is whether Dify said there is another page. False is not "this
	// page was short": a short last page and a short only page look the same
	// without it.
	HasMore bool
	Limit   int
	// Total is what Dify reported, or nil when this listing does not report
	// one.
	Total *int

	next func(ctx context.Context) (*Page[T], error)
}

// NextPage fetches the page after this one, or returns nil when there is not
// one — including when the listing cannot be continued at all.
func (p *Page[T]) NextPage(ctx context.Context) (*Page[T], error) {
	if p == nil || !p.HasMore || p.next == nil {
		return nil, nil
	}
	return p.next(ctx)
}

// All yields every item across every page, fetching as it goes, stopping at
// MaxWalk items — or at ceiling, when one is given — with a *PageLimitError.
// Raising the ceiling is a decision; there is no "no limit".
//
// An error ends the walk after it is yielded.
func (p *Page[T]) All(ctx context.Context, ceiling ...int) iter.Seq2[T, error] {
	limit := MaxWalk
	if len(ceiling) > 0 && ceiling[0] > 0 {
		limit = ceiling[0]
	}
	return func(yield func(T, error) bool) {
		var zero T
		walked := 0
		page := p
		for page != nil {
			for _, item := range page.Items {
				if walked >= limit && page.HasMore {
					yield(zero, &PageLimitError{Limit: limit})
					return
				}
				walked++
				if !yield(item, nil) {
					return
				}
			}
			if page.HasMore && len(page.Items) == 0 {
				// Dify says there is more and sends nothing: stop rather than
				// ask forever.
				return
			}
			next, err := page.NextPage(ctx)
			if err != nil {
				yield(zero, err)
				return
			}
			page = next
		}
	}
}

// Collect walks every page into one slice. It is All with the loop written
// out, for the common case of wanting the lot.
func (p *Page[T]) Collect(ctx context.Context, ceiling ...int) ([]T, error) {
	var out []T
	for item, err := range p.All(ctx, ceiling...) {
		if err != nil {
			return out, err
		}
		out = append(out, item)
	}
	return out, nil
}

// envelope reads the four things Dify's list envelopes carry.
func envelope(payload kernel.Object) (items []kernel.Object, hasMore bool, limit int, total *int) {
	items = payload.Objs("data")
	hasMore = payload.Bool("has_more")
	limit = payload.Int("limit")
	if payload.Has("total") {
		n := payload.Int("total")
		total = &n
	}
	return
}

func buildAll[T any](items []kernel.Object, build func(kernel.Object) T) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		out = append(out, build(item))
	}
	return out
}

// byPage is a page of a numbered listing, able to fetch page number+1.
func byPage[T any](payload kernel.Object, build func(kernel.Object) T, fetch func(ctx context.Context, number int) (kernel.Object, error), number int) *Page[T] {
	items, hasMore, limit, total := envelope(payload)
	return &Page[T]{
		Items:   buildAll(items, build),
		HasMore: hasMore,
		Limit:   limit,
		Total:   total,
		next: func(ctx context.Context) (*Page[T], error) {
			next, err := fetch(ctx, number+1)
			if err != nil {
				return nil, err
			}
			return byPage(next, build, fetch, number+1), nil
		},
	}
}

// FetchByPage fetches the first page and builds it.
func FetchByPage[T any](ctx context.Context, build func(kernel.Object) T, fetch func(ctx context.Context, number int) (kernel.Object, error), number int) (*Page[T], error) {
	if number < 1 {
		number = 1
	}
	first, err := fetch(ctx, number)
	if err != nil {
		return nil, err
	}
	return byPage(first, build, fetch, number), nil
}

// cursorEnd says which end of a page continues a cursor listing, because it
// is not the same for every one: conversations come Newest-first and continue
// from the last id, while a conversation's messages come Oldest-first and
// continue from the first, since the next page is older still. Taking the
// last item either way re-fetched most of the page it had just read.
type cursorEnd func(items []kernel.Object) string

// Newest continues from the last item — Dify's last_id.
func Newest(items []kernel.Object) string {
	if len(items) == 0 {
		return ""
	}
	return items[len(items)-1].Str("id")
}

// Oldest continues from the first item — Dify's first_id.
func Oldest(items []kernel.Object) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].Str("id")
}

// byCursor is a page of a cursor listing, able to fetch the page after it.
func byCursor[T any](payload kernel.Object, build func(kernel.Object) T, fetch func(ctx context.Context, cursor string) (kernel.Object, error), end cursorEnd, previous string) *Page[T] {
	items, hasMore, limit, total := envelope(payload)
	cursor := end(items)
	page := &Page[T]{Items: buildAll(items, build), HasMore: hasMore, Limit: limit, Total: total}
	// A cursor that comes back unchanged is a listing that is not advancing:
	// asking again returns this page again, and All would fetch it forever —
	// a hang rather than an error — so such a page cannot be continued.
	if cursor != "" && cursor != previous {
		page.next = func(ctx context.Context) (*Page[T], error) {
			next, err := fetch(ctx, cursor)
			if err != nil {
				return nil, err
			}
			return byCursor(next, build, fetch, end, cursor), nil
		}
	}
	return page
}

func FetchByCursor[T any](ctx context.Context, build func(kernel.Object) T, fetch func(ctx context.Context, cursor string) (kernel.Object, error), end cursorEnd) (*Page[T], error) {
	first, err := fetch(ctx, "")
	if err != nil {
		return nil, err
	}
	return byCursor(first, build, fetch, end, ""), nil
}

// Unpaged is everything, from a listing Dify does not page. Still a Page, so
// a caller does not have to know which listings page and which do not.
func Unpaged[T any](items []T) *Page[T] {
	n := len(items)
	return &Page[T]{Items: items, Limit: n, Total: &n}
}
