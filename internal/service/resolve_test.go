package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/strpc/digger/internal/cache"
)

type fakeDiscoverer struct {
	mu      sync.Mutex
	values  map[string][]string
	err     error
	calls   int
	stopped bool
}

func (f *fakeDiscoverer) Discover(ctx context.Context, root string, emit func(string) bool) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	for _, value := range f.values[root] {
		if !emit(value) {
			f.mu.Lock()
			f.stopped = true
			f.mu.Unlock()
			return ctx.Err()
		}
	}
	return f.err
}

type fakeDNS struct {
	mu    sync.Mutex
	hosts [][]string
	ips   []string
}

func (f *fakeDNS) Resolve(_ context.Context, hosts []string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts = append(f.hosts, append([]string(nil), hosts...))
	return append([]string(nil), f.ips...)
}

func TestResolveWithoutSubdomainsPreservesOriginalBehavior(t *testing.T) {
	discoverer := &fakeDiscoverer{}
	dns := &fakeDNS{ips: []string{"1.1.1.1"}}
	svc := NewResolver(cache.New(time.Minute), dns, discoverer, 2)

	result, err := svc.Resolve(context.Background(), []string{"example.com"}, false, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.IPs, []string{"1.1.1.1"}) || result.CacheStatus != "MISS" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if discoverer.calls != 0 {
		t.Fatalf("discovery called %d times", discoverer.calls)
	}
}

func TestDiscoveryNormalizesFiltersAndLimitsGlobally(t *testing.T) {
	discoverer := &fakeDiscoverer{values: map[string][]string{
		"example.com": {
			"EXAMPLE.COM.", "*.example.com", "bad_name.example.com",
			"outside.test", "c.example.com.", "b.example.com", "a.example.com",
		},
	}}
	dns := &fakeDNS{ips: []string{"2.2.2.2"}}
	svc := NewResolver(cache.New(time.Minute), dns, discoverer, 2)

	result, err := svc.Resolve(context.Background(), []string{"example.com"}, true, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if result.SubdomainsStatus != SubdomainsLimited || result.SubdomainsSource != SubdomainsSource {
		t.Fatalf("unexpected metadata: %+v", result)
	}
	if !discoverer.stopped {
		t.Fatal("discoverer was not cancelled after the extra result")
	}
	wantHosts := []string{"example.com", "a.example.com", "b.example.com"}
	if !slices.Equal(dns.hosts[0], wantHosts) {
		t.Fatalf("resolved hosts = %v, want %v", dns.hosts[0], wantHosts)
	}
}

func TestDiscoveryErrorDiscardsNamesAndDoesNotCache(t *testing.T) {
	discoverer := &fakeDiscoverer{
		values: map[string][]string{"example.com": {"a.example.com"}},
		err:    errors.New("provider failed"),
	}
	dns := &fakeDNS{ips: []string{"3.3.3.3"}}
	svc := NewResolver(cache.New(time.Minute), dns, discoverer, 10)

	for range 2 {
		result, err := svc.Resolve(context.Background(), []string{"example.com"}, true, true, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if result.SubdomainsStatus != SubdomainsDegraded || result.SubdomainsSource != NoSource {
			t.Fatalf("unexpected metadata: %+v", result)
		}
	}
	if discoverer.calls != 2 {
		t.Fatalf("degraded result was cached: calls = %d", discoverer.calls)
	}
	for _, hosts := range dns.hosts {
		if !slices.Equal(hosts, []string{"example.com"}) {
			t.Fatalf("discovered names were not discarded: %v", hosts)
		}
	}
}

func TestCacheSeparatesModesAndRestoresMetadata(t *testing.T) {
	discoverer := &fakeDiscoverer{values: map[string][]string{"example.com": {"a.example.com"}}}
	dns := &fakeDNS{ips: []string{"4.4.4.4"}}
	svc := NewResolver(cache.New(time.Minute), dns, discoverer, 10)
	hosts := []string{"example.com"}

	if _, err := svc.Resolve(context.Background(), hosts, false, true, time.Minute); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Resolve(context.Background(), hosts, true, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Resolve(context.Background(), hosts, true, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.CacheStatus != "MISS" || second.CacheStatus != "HIT" {
		t.Fatalf("cache statuses = %q, %q", first.CacheStatus, second.CacheStatus)
	}
	if second.SubdomainsStatus != SubdomainsComplete || second.SubdomainsSource != SubdomainsSource {
		t.Fatalf("cached metadata not restored: %+v", second)
	}
	if len(dns.hosts) != 2 {
		t.Fatalf("mode keys collided or cache missed: DNS calls = %d", len(dns.hosts))
	}
}

func TestCacheFalseBypassesReadButWritesResult(t *testing.T) {
	discoverer := &fakeDiscoverer{}
	dns := &fakeDNS{ips: []string{"5.5.5.5"}}
	svc := NewResolver(cache.New(time.Minute), dns, discoverer, 10)
	hosts := []string{"example.com"}

	first, err := svc.Resolve(context.Background(), hosts, false, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Resolve(context.Background(), hosts, false, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.CacheStatus != "MISS" || second.CacheStatus != "HIT" || len(dns.hosts) != 1 {
		t.Fatalf("results = %+v, %+v; DNS calls = %d", first, second, len(dns.hosts))
	}
}

func TestIPOnlySkipsDiscovery(t *testing.T) {
	discoverer := &fakeDiscoverer{}
	dns := &fakeDNS{ips: []string{"192.0.2.1"}}
	svc := NewResolver(cache.New(time.Minute), dns, discoverer, 10)

	result, err := svc.Resolve(context.Background(), []string{"192.0.2.1"}, true, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if result.SubdomainsStatus != SubdomainsComplete || result.SubdomainsSource != NoSource {
		t.Fatalf("unexpected IP-only metadata: %+v", result)
	}
	if discoverer.calls != 0 {
		t.Fatal("IP literal triggered discovery")
	}
}
