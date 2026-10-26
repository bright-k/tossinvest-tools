package toss

import (
	"context"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"net/http"
	"testing"
	"time"
)

func TestActualRateGroupsAndDynamicHeaders(t *testing.T) {
	c := contract.Load()
	for _, op := range c.Ops {
		if op.RateGroup == "" {
			t.Fatalf("missing official group %s", op.ID)
		}
	}
	a, _ := c.Find("getPrices")
	b, _ := c.Find("getOrderbook")
	if rateGroup(a) != rateGroup(b) || rateGroup(a) != "MARKET_DATA" {
		t.Fatal("group isolation")
	}
	r := NewRESTForTransport(c, nil, "http://127.0.0.1:1", http.DefaultTransport)
	now := time.Now()
	r.observe("ACCOUNT", http.Header{"X-Ratelimit-Limit": []string{"1"}, "X-Ratelimit-Remaining": []string{"0"}, "X-Ratelimit-Reset": []string{"0.25"}, "Retry-After": []string{"2"}}, 429)
	if r.next["ACCOUNT"].Before(now.Add(1900 * time.Millisecond)) {
		t.Fatal("Retry-After was shortened by reset")
	}
	if r.intervals["ACCOUNT"] != time.Second {
		t.Fatal("dynamic burst capacity ignored")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if r.pace(ctx, "ACCOUNT") == nil {
		t.Fatal("limit wait ignored cancellation")
	}
	if e := r.pace(context.Background(), "OTHER_GROUP"); e != nil {
		t.Fatal(e)
	}
	// Already waiting work must re-evaluate a newer server backoff instead of using an obsolete reservation.
	r.next["queued"] = time.Now().Add(30 * time.Millisecond)
	done := make(chan time.Time)
	go func() { _ = r.pace(context.Background(), "queued"); done <- time.Now() }()
	time.Sleep(10 * time.Millisecond)
	until := time.Now().Add(80 * time.Millisecond)
	r.rateMu.Lock()
	r.next["queued"] = until
	r.rateMu.Unlock()
	if at := <-done; at.Before(until) {
		t.Fatal("queued request bypassed observed backoff")
	}
}
