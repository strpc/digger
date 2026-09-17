package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestCRTShProviderParsesNamesAndQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "%.example.com" || r.URL.Query().Get("output") != "json" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `[{"name_value":"a.example.com\nb.example.com"}]`)
	}))
	defer server.Close()

	provider := &crtshProvider{client: server.Client(), endpoint: server.URL, state: newProviderState(time.Now)}
	var names []string
	err := provider.Discover(context.Background(), "example.com", func(name string) bool {
		names = append(names, name)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"a.example.com", "b.example.com"}) {
		t.Fatalf("names = %v", names)
	}
}

func TestSubMDNotFoundIsSuccessfulEmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	provider := &subMDProvider{client: server.Client(), endpoint: server.URL, state: newProviderState(time.Now)}
	if err := provider.Discover(context.Background(), "example.com", func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
}

func TestHackerTargetParsesRowsAndCoolsDownOnQuotaMessage(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			fmt.Fprintln(w, "a.example.com,192.0.2.1")
			return
		}
		fmt.Fprintln(w, "API count exceeded - Increase Quota with Membership")
	}))
	defer server.Close()

	state := newProviderState(time.Now)
	provider := &hackerTargetProvider{client: server.Client(), endpoint: server.URL, state: state}
	var names []string
	if err := provider.Discover(context.Background(), "example.com", func(name string) bool {
		names = append(names, name)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"a.example.com"}) {
		t.Fatalf("names = %v", names)
	}
	if err := provider.Discover(context.Background(), "example.com", func(string) bool { return true }); err == nil {
		t.Fatal("quota response was accepted")
	}
	if err := provider.Discover(context.Background(), "example.com", func(string) bool { return true }); !errors.Is(err, errCooldown) {
		t.Fatalf("provider did not enter cooldown: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("HTTP calls = %d", calls.Load())
	}
}

func TestRetryAfterDuration(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	if got := retryAfterDuration("15", now); got != 15*time.Second {
		t.Fatalf("seconds duration = %s", got)
	}
	if got := retryAfterDuration(now.Add(time.Minute).Format(http.TimeFormat), now); got != time.Minute {
		t.Fatalf("date duration = %s", got)
	}
	if got := retryAfterDuration("invalid", now); got != 0 {
		t.Fatalf("invalid duration = %s", got)
	}
}

type stubProvider struct {
	name   string
	values []string
	err    error
}

type waitingProvider struct{ name string }

func (p waitingProvider) Name() string { return p.name }
func (p waitingProvider) Discover(ctx context.Context, _ string, _ func(string) bool) error {
	<-ctx.Done()
	return ctx.Err()
}

func (p stubProvider) Name() string { return p.name }
func (p stubProvider) Discover(_ context.Context, _ string, emit func(string) bool) error {
	for _, value := range p.values {
		if !emit(value) {
			return context.Canceled
		}
	}
	return p.err
}

func TestAggregatorTimesOutOnlySlowProvider(t *testing.T) {
	aggregator := newAggregator([]Provider{
		stubProvider{name: "good", values: []string{"a.example.com"}},
		waitingProvider{name: "slow"},
	}, 10*time.Millisecond)
	var names []string
	err := aggregator.Discover(context.Background(), "example.com", func(name string) bool {
		names = append(names, name)
		return true
	})
	if err == nil {
		t.Fatal("slow provider timeout was hidden")
	}
	if !slices.Equal(names, []string{"a.example.com"}) {
		t.Fatalf("names = %v", names)
	}
}

func TestAggregatorKeepsResultsWhenProviderFails(t *testing.T) {
	aggregator := newAggregator([]Provider{
		stubProvider{name: "good", values: []string{"a.example.com"}},
		stubProvider{name: "bad", err: errors.New("unavailable")},
	}, time.Second)
	var names []string
	err := aggregator.Discover(context.Background(), "example.com", func(name string) bool {
		names = append(names, name)
		return true
	})
	if err == nil {
		t.Fatal("provider error was hidden")
	}
	if !slices.Equal(names, []string{"a.example.com"}) {
		t.Fatalf("names = %v", names)
	}
}
