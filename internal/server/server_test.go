package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/strpc/digger/internal/cache"
	"github.com/strpc/digger/internal/service"
)

type testDNS struct{}

func (testDNS) Resolve(context.Context, []string) []string { return []string{"192.0.2.2"} }

type testDiscoverer struct {
	calls int
	err   error
}

func (d *testDiscoverer) Discover(_ context.Context, _ string, emit func(string) bool) error {
	d.calls++
	emit("www.example.com")
	return d.err
}

func TestResolveReturnsAndCachesPartialMetadata(t *testing.T) {
	discoverer := &testDiscoverer{err: errors.New("provider unavailable")}
	srv := testServer(discoverer)
	for index := range 2 {
		response := httptest.NewRecorder()
		srv.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resolve?host=example.com&subdomains=true", nil))
		if response.Header().Get("X-Subdomains-Status") != service.SubdomainsPartial || response.Header().Get("X-Subdomains-Source") != service.SubdomainsSource {
			t.Fatalf("unexpected headers: %v", response.Header())
		}
		wantCache := "MISS"
		if index == 1 {
			wantCache = "HIT"
		}
		if response.Header().Get("X-Cache") != wantCache {
			t.Fatalf("X-Cache = %q, want %q", response.Header().Get("X-Cache"), wantCache)
		}
	}
	if discoverer.calls != 1 {
		t.Fatalf("partial result was not cached: calls = %d", discoverer.calls)
	}
}

func testServer(discoverer *testDiscoverer) *Server {
	svc := service.NewResolver(cache.New(time.Minute), testDNS{}, discoverer, 10, time.Minute)
	return New(svc, "test", "test")
}

func TestResolveOldAPIHasNoSubdomainHeaders(t *testing.T) {
	discoverer := &testDiscoverer{}
	response := httptest.NewRecorder()
	testServer(discoverer).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resolve?host=example.com", nil))

	if response.Code != http.StatusOK || response.Body.String() != "192.0.2.2\n" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Subdomains-Status") != "" || response.Header().Get("X-Subdomains-Source") != "" {
		t.Fatal("opt-out response contained subdomain headers")
	}
	if discoverer.calls != 0 {
		t.Fatal("opt-out request triggered discovery")
	}
}

func TestResolveParsesSubdomainsBool(t *testing.T) {
	for _, value := range []string{"not-bool", ""} {
		discoverer := &testDiscoverer{}
		response := httptest.NewRecorder()
		testServer(discoverer).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resolve?host=example.com&subdomains="+value, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("subdomains=%q status = %d", value, response.Code)
		}
	}

	discoverer := &testDiscoverer{}
	response := httptest.NewRecorder()
	testServer(discoverer).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resolve?host=example.com&subdomains=true", nil))
	if response.Header().Get("X-Subdomains-Status") != service.SubdomainsComplete || response.Header().Get("X-Subdomains-Source") != service.SubdomainsSource {
		t.Fatalf("unexpected headers: %v", response.Header())
	}
}
