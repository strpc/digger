package resolver

import (
	"net"
	"sort"
	"sync"
)

// IPv4 resolves a hostname and returns only its IPv4 addresses.
func IPv4(host string) []string {
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil
	}
	var ips []string
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil && ip.To4() != nil {
			ips = append(ips, addr)
		}
	}
	return ips
}

// All resolves all hosts concurrently, deduplicates, and returns sorted IPv4s.
func All(hosts []string) []string {
	results := make([][]string, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func(idx int, h string) {
			defer wg.Done()
			results[idx] = IPv4(h)
		}(i, host)
	}
	wg.Wait()

	seen := make(map[string]struct{})
	var all []string
	for _, ips := range results {
		for _, ip := range ips {
			if _, ok := seen[ip]; !ok {
				seen[ip] = struct{}{}
				all = append(all, ip)
			}
		}
	}
	sort.Strings(all)
	return all
}
