package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/strpc/digger/internal/cache"
	"github.com/strpc/digger/internal/discovery"
	"github.com/strpc/digger/internal/resolver"
	"github.com/strpc/digger/internal/server"
	"github.com/strpc/digger/internal/service"
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
	subdomainTimeout, err := positiveDurationEnv("SUBDOMAIN_TIMEOUT", "20")
	if err != nil {
		log.Fatal(err)
	}

	c := cache.New(time.Duration(ttlSecs) * time.Second)
	discoverer := discovery.New(discovery.Config{
		SubMDAPIKey:        os.Getenv("SUBMD_API_KEY"),
		HackerTargetAPIKey: os.Getenv("HACKERTARGET_API_KEY"),
	})
	resolveService := service.NewResolver(c, resolver.New(), discoverer, subdomainLimit, subdomainTimeout)
	s := server.New(resolveService, version, commitHash)

	addr := ":" + port
	log.Printf("digger %s (%s) listening on %s (default TTL %ds)", version, commitHash, addr, ttlSecs)
	log.Fatal(http.ListenAndServe(addr, s))
}

func positiveDurationEnv(key, def string) (time.Duration, error) {
	value := getenv(key, def)
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds <= 0 || seconds > int64((1<<63-1)/time.Second) {
		return 0, fmt.Errorf("%s must be a positive integer number of seconds", key)
	}
	return time.Duration(seconds) * time.Second, nil
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
