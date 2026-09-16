package resolver

import (
	"context"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	maxConcurrentLookups = 32
	lookupTimeout        = 3 * time.Second
)

var lookupSlots = make(chan struct{}, maxConcurrentLookups)

// Resolver performs bounded, context-aware IPv4 DNS lookups.
type Resolver struct {
	lookupHost func(context.Context, string) ([]string, error)
}

func New() *Resolver {
	return &Resolver{lookupHost: net.DefaultResolver.LookupHost}
}

// Resolve resolves all hosts with a bounded worker pool and returns sorted,
// unique IPv4 addresses. Individual lookup failures are ignored.
func (r *Resolver) Resolve(ctx context.Context, hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}

	jobs := make(chan string)
	results := make(chan []string)
	workers := min(len(hosts), maxConcurrentLookups)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for host := range jobs {
				results <- r.resolveOne(ctx, host)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, host := range hosts {
			select {
			case jobs <- host:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	seen := make(map[string]struct{})
	for addresses := range results {
		for _, address := range addresses {
			seen[address] = struct{}{}
		}
	}

	ips := make([]string, 0, len(seen))
	for ip := range seen {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips
}

func (r *Resolver) resolveOne(ctx context.Context, host string) []string {
	if parsed := net.ParseIP(host); parsed != nil {
		if ipv4 := parsed.To4(); ipv4 != nil {
			return []string{ipv4.String()}
		}
		return nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	select {
	case lookupSlots <- struct{}{}:
		defer func() { <-lookupSlots }()
	case <-lookupCtx.Done():
		return nil
	}

	addresses, err := r.lookupHost(lookupCtx, host)
	if err != nil {
		return nil
	}

	ips := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
			ips = append(ips, ip.String())
		}
	}
	return ips
}

// IPv4 preserves the original package API while using the bounded resolver.
func IPv4(host string) []string {
	return New().Resolve(context.Background(), []string{host})
}

// All preserves the original package API while using the bounded resolver.
func All(hosts []string) []string {
	return New().Resolve(context.Background(), hosts)
}
