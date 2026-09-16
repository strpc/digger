package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/strpc/digger/internal/service"
)

type Server struct {
	resolver   *service.Resolver
	version    string
	commitHash string
}

func New(resolver *service.Resolver, version, commitHash string) *Server {
	return &Server{resolver: resolver, version: version, commitHash: commitHash}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/resolve":
		s.resolve(w, r)
	case "/health":
		s.health(w, r)
	case "/cache/clear":
		s.cacheClear(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hosts := q["host"]
	if len(hosts) == 0 {
		http.Error(w, "missing host parameter", http.StatusBadRequest)
		return
	}

	useCache := q.Get("cache") != "false"
	subdomains := false
	if q.Has("subdomains") {
		var err error
		subdomains, err = strconv.ParseBool(q.Get("subdomains"))
		if err != nil {
			http.Error(w, "invalid subdomains parameter", http.StatusBadRequest)
			return
		}
	}

	ttl := s.resolver.DefaultTTL()
	if v := q.Get("ttl"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			ttl = time.Duration(secs) * time.Second
		}
	}

	result, err := s.resolver.Resolve(r.Context(), hosts, subdomains, useCache, ttl)
	if err != nil {
		return
	}
	writeIPs(w, result, subdomains)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "version=%s\ncommit=%s\ncache_entries=%d\n", s.version, s.commitHash, s.resolver.CacheCount())
}

func (s *Server) cacheClear(w http.ResponseWriter, _ *http.Request) {
	s.resolver.ClearCache()
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, "cache cleared")
}

func writeIPs(w http.ResponseWriter, result service.Result, subdomains bool) {
	w.Header().Set("X-Cache", result.CacheStatus)
	if subdomains {
		w.Header().Set("X-Subdomains-Status", result.SubdomainsStatus)
		w.Header().Set("X-Subdomains-Source", result.SubdomainsSource)
	}
	w.Header().Set("Content-Type", "text/plain")
	if len(result.IPs) > 0 {
		fmt.Fprintln(w, strings.Join(result.IPs, "\n"))
	}
}
