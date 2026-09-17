package discovery

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	providerTimeout = 8 * time.Second
	defaultCooldown = time.Hour
	maxBodySize     = 5 * 1024 * 1024
	maxScannerToken = 1024 * 1024
)

var errCooldown = errors.New("provider is cooling down")

// Provider emits passive subdomain names for one root domain.
type Provider interface {
	Name() string
	Discover(ctx context.Context, root string, emit func(string) bool) error
}

// Aggregator runs independent passive providers concurrently.
type Aggregator struct {
	providers []Provider
	timeout   time.Duration
}

func New() *Aggregator {
	client := &http.Client{}
	state := newProviderState(time.Now)
	return newAggregator([]Provider{
		&crtshProvider{client: client, endpoint: "https://crt.sh/", state: state},
		&subMDProvider{client: client, endpoint: "https://api.sub.md/v1/search", state: state},
		&hackerTargetProvider{client: client, endpoint: "https://api.hackertarget.com/hostsearch/", state: state},
	}, providerTimeout)
}

func newAggregator(providers []Provider, timeout time.Duration) *Aggregator {
	return &Aggregator{providers: providers, timeout: timeout}
}

func (a *Aggregator) Discover(ctx context.Context, root string, emit func(string) bool) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type providerResult struct{ err error }
	results := make(chan providerResult, len(a.providers))
	var emitMu sync.Mutex
	stopped := false

	for _, provider := range a.providers {
		provider := provider
		go func() {
			providerCtx, cancelProvider := context.WithTimeout(runCtx, a.timeout)
			defer cancelProvider()
			err := provider.Discover(providerCtx, root, func(name string) bool {
				emitMu.Lock()
				defer emitMu.Unlock()
				if stopped {
					return false
				}
				if !emit(name) {
					stopped = true
					cancel()
					return false
				}
				return true
			})
			results <- providerResult{err: err}
		}()
	}

	var providerErrors []error
	for range a.providers {
		result := <-results
		if result.err != nil {
			providerErrors = append(providerErrors, result.err)
		}
	}
	if stopped {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(providerErrors...)
}

type providerState struct {
	mu            sync.Mutex
	now           func() time.Time
	cooldownUntil map[string]time.Time
	nextSubMD     time.Time
}

func newProviderState(now func() time.Time) *providerState {
	return &providerState{now: now, cooldownUntil: make(map[string]time.Time)}
}

func (s *providerState) check(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.now().Before(s.cooldownUntil[name]) {
		return fmt.Errorf("%s: %w", name, errCooldown)
	}
	return nil
}

func (s *providerState) coolDown(name, retryAfter string) {
	now := s.now()
	duration := retryAfterDuration(retryAfter, now)
	if duration <= 0 {
		duration = defaultCooldown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	until := now.Add(duration)
	if until.After(s.cooldownUntil[name]) {
		s.cooldownUntil[name] = until
	}
}

func (s *providerState) waitSubMD(ctx context.Context) error {
	s.mu.Lock()
	now := s.now()
	start := now
	if s.nextSubMD.After(start) {
		start = s.nextSubMD
	}
	s.nextSubMD = start.Add(time.Second)
	s.mu.Unlock()

	delay := start.Sub(now)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryAfterDuration(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil && when.After(now) {
		return when.Sub(now)
	}
	return 0
}

type limitedBody struct {
	reader *io.LimitedReader
}

func newLimitedBody(body io.Reader) *limitedBody {
	return &limitedBody{reader: &io.LimitedReader{R: body, N: maxBodySize + 1}}
}

func (b *limitedBody) exceeded() bool { return b.reader.N <= 0 }

func newRequest(ctx context.Context, endpoint string, query url.Values) (*http.Request, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	values := u.Query()
	for key, entries := range query {
		for _, entry := range entries {
			values.Add(key, entry)
		}
	}
	u.RawQuery = values.Encode()
	return http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
}

func responseError(name string, response *http.Response) error {
	return fmt.Errorf("%s: unexpected HTTP status %d", name, response.StatusCode)
}

type crtshProvider struct {
	client   *http.Client
	endpoint string
	state    *providerState
}

func (p *crtshProvider) Name() string { return "crtsh" }

func (p *crtshProvider) Discover(ctx context.Context, root string, emit func(string) bool) error {
	if err := p.state.check(p.Name()); err != nil {
		return err
	}
	request, err := newRequest(ctx, p.endpoint, url.Values{"q": {"%." + root}, "output": {"json"}})
	if err != nil {
		return err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
			p.state.coolDown(p.Name(), retryAfter)
		}
		return responseError(p.Name(), response)
	}

	body := newLimitedBody(response.Body)
	decoder := json.NewDecoder(body.reader)
	token, err := decoder.Token()
	if err != nil {
		if body.exceeded() {
			return errors.New("crtsh: response body exceeds 5 MiB")
		}
		return fmt.Errorf("crtsh: decode response: %w", err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '[' {
		return errors.New("crtsh: expected JSON array")
	}
	for decoder.More() {
		var entry struct {
			NameValue string `json:"name_value"`
		}
		if err := decoder.Decode(&entry); err != nil {
			if body.exceeded() {
				return errors.New("crtsh: response body exceeds 5 MiB")
			}
			return fmt.Errorf("crtsh: decode entry: %w", err)
		}
		for _, name := range strings.Split(entry.NameValue, "\n") {
			if name = strings.TrimSpace(name); name != "" && !emit(name) {
				return context.Canceled
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		if body.exceeded() {
			return errors.New("crtsh: response body exceeds 5 MiB")
		}
		return fmt.Errorf("crtsh: decode response end: %w", err)
	}
	if _, err := io.Copy(io.Discard, body.reader); err != nil {
		return fmt.Errorf("crtsh: read response end: %w", err)
	}
	if body.exceeded() {
		return errors.New("crtsh: response body exceeds 5 MiB")
	}
	return nil
}

type subMDProvider struct {
	client   *http.Client
	endpoint string
	state    *providerState
}

func (p *subMDProvider) Name() string { return "submd" }

func (p *subMDProvider) Discover(ctx context.Context, root string, emit func(string) bool) error {
	if err := p.state.check(p.Name()); err != nil {
		return err
	}
	if err := p.state.waitSubMD(ctx); err != nil {
		return err
	}
	if err := p.state.check(p.Name()); err != nil {
		return err
	}
	request, err := newRequest(ctx, p.endpoint, url.Values{"apex": {root}})
	if err != nil {
		return err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			p.state.coolDown(p.Name(), response.Header.Get("Retry-After"))
		}
		return responseError(p.Name(), response)
	}
	return scanLines(response.Body, "submd", func(line string) error {
		if !emit(line) {
			return context.Canceled
		}
		return nil
	})
}

type hackerTargetProvider struct {
	client   *http.Client
	endpoint string
	state    *providerState
}

func (p *hackerTargetProvider) Name() string { return "hackertarget" }

func (p *hackerTargetProvider) Discover(ctx context.Context, root string, emit func(string) bool) error {
	if err := p.state.check(p.Name()); err != nil {
		return err
	}
	request, err := newRequest(ctx, p.endpoint, url.Values{"q": {root}})
	if err != nil {
		return err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusTooManyRequests {
			p.state.coolDown(p.Name(), response.Header.Get("Retry-After"))
		}
		return responseError(p.Name(), response)
	}

	quotaExceeded := false
	err = scanLines(response.Body, "hackertarget", func(line string) error {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "api count exceeded") || strings.Contains(lower, "quota") && strings.Contains(lower, "exceed") {
			quotaExceeded = true
			return errors.New("hackertarget: anonymous quota exceeded")
		}
		if lower == "no records found" || lower == "no records found." {
			return nil
		}
		record, parseErr := csv.NewReader(strings.NewReader(line)).Read()
		if parseErr != nil || len(record) != 2 || net.ParseIP(strings.TrimSpace(record[1])) == nil {
			return fmt.Errorf("hackertarget: unexpected response line %q", line)
		}
		if !emit(strings.TrimSpace(record[0])) {
			return context.Canceled
		}
		return nil
	})
	if quotaExceeded {
		p.state.coolDown(p.Name(), "")
	}
	return err
}

func scanLines(reader io.Reader, provider string, consume func(string) error) error {
	body := newLimitedBody(reader)
	scanner := bufio.NewScanner(body.reader)
	scanner.Buffer(make([]byte, 64*1024), maxScannerToken)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := consume(line); err != nil {
			return err
		}
	}
	if body.exceeded() {
		return fmt.Errorf("%s: response body exceeds 5 MiB", provider)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s: read response: %w", provider, err)
	}
	return nil
}
