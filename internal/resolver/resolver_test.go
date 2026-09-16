package resolver

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveBoundsConcurrentLookups(t *testing.T) {
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	r := &Resolver{lookupHost: func(ctx context.Context, _ string) ([]string, error) {
		current := active.Add(1)
		for current > maximum.Load() && !maximum.CompareAndSwap(maximum.Load(), current) {
		}
		defer active.Add(-1)
		select {
		case <-release:
			return []string{"192.0.2.3"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}

	hosts := make([]string, 64)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("host-%d.example", i)
	}
	done := make(chan struct{})
	go func() {
		r.Resolve(context.Background(), hosts)
		close(done)
	}()

	deadline := time.After(time.Second)
	for maximum.Load() < maxConcurrentLookups {
		select {
		case <-deadline:
			t.Fatalf("only %d lookups started", maximum.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(release)
	<-done
	if maximum.Load() > maxConcurrentLookups {
		t.Fatalf("maximum concurrency = %d", maximum.Load())
	}
}
