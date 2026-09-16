package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/strpc/digger/internal/cache"
	"github.com/strpc/digger/internal/resolver"
	"github.com/strpc/digger/internal/server"
	"github.com/strpc/digger/internal/service"
	"github.com/strpc/digger/internal/subfinder"
)

var (
	version    = "unknown"
	commitHash = "unknown"
)

func main() {
	port := getenv("PORT", "8080")

	ttlSecs, err := strconv.Atoi(getenv("CACHE_TTL", "300"))
	if err != nil {
		ttlSecs = 300
	}
	subdomainLimit, err := positiveIntEnv("SUBDOMAIN_LIMIT", "500")
	if err != nil {
		log.Fatal(err)
	}

	c := cache.New(time.Duration(ttlSecs) * time.Second)
	discoverer, err := subfinder.New(subdomainLimit)
	if err != nil {
		log.Fatal(err)
	}
	resolveService := service.NewResolver(c, resolver.New(), discoverer, subdomainLimit)
	s := server.New(resolveService, version, commitHash)

	addr := ":" + port
	log.Printf("digger %s (%s) listening on %s (default TTL %ds)", version, commitHash, addr, ttlSecs)
	log.Fatal(http.ListenAndServe(addr, s))
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func positiveIntEnv(key, def string) (int, error) {
	value := getenv(key, def)
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}
