package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/strpc/digger/internal/cache"
	"github.com/strpc/digger/internal/resolver"
)

type Server struct {
	cache      *cache.Cache
	version    string
	commitHash string
}

func New(c *cache.Cache, version, commitHash string) *Server {
	return &Server{cache: c, version: version, commitHash: commitHash}
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

	ttl := s.cache.DefaultTTL
	if v := q.Get("ttl"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			ttl = time.Duration(secs) * time.Second
		}
	}

	key := cache.Key(hosts)

	if useCache {
		if ips, ok := s.cache.Get(key, ttl); ok {
			writeIPs(w, ips, "HIT")
			return
		}
	}

	ips := resolver.All(hosts)
	s.cache.Set(key, ips)
	writeIPs(w, ips, "MISS")
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "version=%s\ncommit=%s\ncache_entries=%d\n", s.version, s.commitHash, s.cache.Count())
}

func (s *Server) cacheClear(w http.ResponseWriter, _ *http.Request) {
	s.cache.Clear()
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, "cache cleared")
}

func writeIPs(w http.ResponseWriter, ips []string, cacheStatus string) {
	w.Header().Set("X-Cache", cacheStatus)
	w.Header().Set("Content-Type", "text/plain")
	if len(ips) > 0 {
		fmt.Fprintln(w, strings.Join(ips, "\n"))
	}
}
