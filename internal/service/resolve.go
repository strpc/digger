package service

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/strpc/digger/internal/cache"
)

const (
	SubdomainsComplete = "complete"
	SubdomainsLimited  = "limited"
	SubdomainsPartial  = "partial"
	SubdomainsDegraded = "degraded"
	SubdomainsSource   = "direct"
	NoSource           = "none"
)

// Discoverer emits discovered names for one search boundary. Returning false
// from emit asks the implementation to cancel its operation.
type Discoverer interface {
	Discover(ctx context.Context, root string, emit func(string) bool) error
}

type DNSResolver interface {
	Resolve(ctx context.Context, hosts []string) []string
}

type Result struct {
	IPs              []string
	CacheStatus      string
	SubdomainsStatus string
	SubdomainsSource string
}

type Resolver struct {
	cache      *cache.Cache
	dns        DNSResolver
	discoverer Discoverer
	limit      int
	timeout    time.Duration
}

func NewResolver(c *cache.Cache, dns DNSResolver, discoverer Discoverer, limit int, timeout time.Duration) *Resolver {
	return &Resolver{cache: c, dns: dns, discoverer: discoverer, limit: limit, timeout: timeout}
}

func (s *Resolver) Resolve(ctx context.Context, hosts []string, subdomains, useCache bool, ttl time.Duration) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	key := cache.Key(hosts) + "\x00subdomains=" + boolString(subdomains)
	if useCache {
		if cached, ok := s.cache.GetResult(key, ttl); ok {
			return Result{
				IPs:              cached.IPs,
				CacheStatus:      "HIT",
				SubdomainsStatus: cached.SubdomainsStatus,
				SubdomainsSource: cached.SubdomainsSource,
			}, nil
		}
	}

	result := Result{CacheStatus: "MISS"}
	resolveHosts := hosts
	cacheable := true
	if subdomains {
		var err error
		resolveHosts, result.SubdomainsStatus, result.SubdomainsSource, cacheable, err = s.expand(ctx, hosts)
		if err != nil {
			return Result{}, err
		}
	}

	result.IPs = s.dns.Resolve(ctx, resolveHosts)
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if cacheable {
		s.cache.SetResult(key, cache.Result{
			IPs:              result.IPs,
			SubdomainsStatus: result.SubdomainsStatus,
			SubdomainsSource: result.SubdomainsSource,
		})
	}
	return result, nil
}

func (s *Resolver) expand(parent context.Context, hosts []string) ([]string, string, string, bool, error) {
	allIP := true
	for _, host := range hosts {
		if net.ParseIP(host) == nil {
			allIP = false
			break
		}
	}
	if allIP {
		return hosts, SubdomainsComplete, NoSource, true, nil
	}

	discoveryCtx, cancelDiscovery := context.WithTimeout(parent, s.timeout)
	defer cancelDiscovery()

	originals := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if normalized, ok := normalizeName(host); ok {
			originals[normalized] = struct{}{}
		}
	}

	discovered := make(map[string]struct{}, s.limit+1)
	limited := false
	incomplete := false
	var discoveredMu sync.Mutex
	for _, host := range hosts {
		if net.ParseIP(host) != nil {
			continue
		}
		root, ok := normalizeName(host)
		if !ok {
			incomplete = true
			continue
		}

		callCtx, cancelCall := context.WithCancel(discoveryCtx)
		err := s.discoverer.Discover(callCtx, root, func(candidate string) bool {
			discoveredMu.Lock()
			defer discoveredMu.Unlock()
			if limited {
				return false
			}
			name, valid := normalizeName(candidate)
			if !valid || (name != root && !strings.HasSuffix(name, "."+root)) {
				return true
			}
			if _, original := originals[name]; original {
				return true
			}
			if _, duplicate := discovered[name]; duplicate {
				return true
			}
			discovered[name] = struct{}{}
			if len(discovered) > s.limit {
				limited = true
				cancelCall()
				return false
			}
			return true
		})
		cancelCall()
		discoveredMu.Lock()
		limitReached := limited
		discoveredMu.Unlock()

		if err != nil && !limitReached {
			if parent.Err() != nil {
				return nil, "", "", false, parent.Err()
			}
			incomplete = true
			if discoveryCtx.Err() != nil {
				break
			}
			continue
		}
		if discoveryCtx.Err() != nil && !limitReached {
			if parent.Err() != nil {
				return nil, "", "", false, parent.Err()
			}
			incomplete = true
			break
		}
		if limitReached {
			break
		}
	}

	if parent.Err() != nil {
		return nil, "", "", false, parent.Err()
	}
	names := make([]string, 0, len(discovered))
	for name := range discovered {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > s.limit {
		names = names[:s.limit]
	}
	resolved := append(append([]string(nil), hosts...), names...)
	status := SubdomainsComplete
	cacheable := true
	source := NoSource
	if len(names) > 0 {
		source = SubdomainsSource
	}
	if limited {
		status = SubdomainsLimited
	} else if incomplete && len(names) > 0 {
		status = SubdomainsPartial
	} else if incomplete {
		status = SubdomainsDegraded
		cacheable = false
	}
	return resolved, status, source, cacheable, nil
}

func (s *Resolver) CacheCount() int { return s.cache.Count() }
func (s *Resolver) ClearCache()     { s.cache.Clear() }
func (s *Resolver) DefaultTTL() time.Duration {
	return s.cache.DefaultTTL
}

func normalizeName(value string) (string, bool) {
	name := strings.ToLower(strings.TrimSuffix(value, "."))
	if name == "" || len(name) > 253 || strings.Contains(name, "*") || !isASCII(name) {
		return "", false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return "", false
			}
		}
	}
	return name, true
}

func isASCII(value string) bool {
	for _, char := range value {
		if char > 127 {
			return false
		}
	}
	return true
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
