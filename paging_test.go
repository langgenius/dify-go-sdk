package dify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

func TestHistoryContinuesFromTheFirstIDOnEachPage(t *testing.T) {
	// Pages arrive oldest-first and the next one is older, so taking the last
	// id re-read most of the page each time.
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("first_id") {
		case "":
			writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "m3", "query": "q3"}, map[string]any{"id": "m4"}}, "has_more": true, "limit": 2})
		case "m3":
			writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "m1"}, map[string]any{"id": "m2"}}, "has_more": false, "limit": 2})
		default:
			t.Errorf("continued from the wrong end: %s", r.URL.RawQuery)
			writeJSON(w, 200, map[string]any{"data": []any{}})
		}
	})
	page, err := f.app(t).Chat.Messages.List(context.Background(), "conv", &HistoryParams{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Query != "q3" {
		t.Error("history keeps the query each answer replied to")
	}
	all, err := page.Collect(context.Background())
	if err != nil || len(all) != 4 {
		t.Errorf("got %d items, %v", len(all), err)
	}
}

func TestConversationsContinueFromTheLastID(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("last_id") == "" {
			writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "c1"}, map[string]any{"id": "c2"}}, "has_more": true})
			return
		}
		if got := r.URL.Query().Get("last_id"); got != "c2" {
			t.Errorf("want last_id=c2, got %s", got)
		}
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "c3"}}, "has_more": false})
	})
	page, _ := f.app(t).Chat.Conversations.List(context.Background(), nil)
	all, err := page.Collect(context.Background())
	if err != nil || len(all) != 3 || all[2].ID != "c3" {
		t.Errorf("got %+v %v", all, err)
	}
}

func TestACursorThatDoesNotAdvanceEndsTheWalk(t *testing.T) {
	calls := 0
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		// A server that always answers the same page and says there is more.
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "same"}}, "has_more": true})
	})
	page, _ := f.app(t).Chat.Conversations.List(context.Background(), nil)
	all, err := page.Collect(context.Background())
	if err != nil || calls != 2 || len(all) != 2 {
		t.Errorf("asking again returns the same page, so the walk should stop: %d calls, %d items, %v", calls, len(all), err)
	}
}

func TestAWalkThatNeverEndsStopsAtItsCeiling(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("page"))
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": fmt.Sprint(n)}}, "has_more": true, "page": n})
	})
	page, _ := f.app(t).Annotations.List(context.Background(), nil)
	all, err := page.Collect(context.Background(), 5)
	var limit *PageLimitError
	if !errors.As(err, &limit) || len(all) != 5 {
		t.Errorf("a walk that quietly stops early is the bug All exists to fix: %d items, %v", len(all), err)
	}
}

func TestAPageClaimingMoreButEmptyEndsTheWalk(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{}, "has_more": true})
	})
	page, _ := f.app(t).Annotations.List(context.Background(), nil)
	if all, err := page.Collect(context.Background()); err != nil || len(all) != 0 || len(f.seen()) != 1 {
		t.Errorf("got %v %v after %d requests", all, err, len(f.seen()))
	}
}

func TestNumberedListingsAskForTheNextNumber(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("page"))
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": fmt.Sprint(n)}}, "has_more": n < 3, "total": 3, "limit": 1})
	})
	page, _ := f.app(t).Workflows.Runs.Logs(context.Background(), &LogParams{Limit: 1, Status: "failed"})
	if page.Total == nil || *page.Total != 3 {
		t.Errorf("total is what Dify said: %v", page.Total)
	}
	all, err := page.Collect(context.Background())
	if err != nil || len(all) != 3 || all[2]["id"] != "3" {
		t.Errorf("got %v %v", all, err)
	}
	if f.seen()[0].Query["status"][0] != "failed" {
		t.Error("filters are sent")
	}
}

func TestAWalkCanBeAbandonedEarly(t *testing.T) {
	f := newFakeDify(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}, "has_more": true})
	})
	page, _ := f.app(t).Annotations.List(context.Background(), nil)
	for range page.All(context.Background()) {
		break
	}
	if len(f.seen()) != 1 {
		t.Errorf("breaking out should fetch nothing more, got %d requests", len(f.seen()))
	}
}
