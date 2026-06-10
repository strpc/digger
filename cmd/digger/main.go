package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/strpc/digger/internal/cache"
	"github.com/strpc/digger/internal/server"
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

	c := cache.New(time.Duration(ttlSecs) * time.Second)
	s := server.New(c, version, commitHash)

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
