package subfinder

import (
	"context"
	"errors"
	"io"
	"sort"

	"github.com/projectdiscovery/goflags"
	"github.com/projectdiscovery/subfinder/v2/pkg/passive"
	"github.com/projectdiscovery/subfinder/v2/pkg/resolve"
	"github.com/projectdiscovery/subfinder/v2/pkg/runner"
	"github.com/projectdiscovery/subfinder/v2/pkg/subscraping"
)

const maxResponseBodySize = 5 * 1024 * 1024

var discoverySlot = make(chan struct{}, 1)

type Adapter struct {
	sources []string
	limit   int
}

func New(limit int) (*Adapter, error) {
	sources := noKeySourceNames()
	if len(sources) == 0 {
		return nil, errors.New("subfinder has no no-key passive sources")
	}
	return &Adapter{sources: sources, limit: limit}, nil
}

func (a *Adapter) Discover(ctx context.Context, root string, emit func(string) bool) error {
	select {
	case discoverySlot <- struct{}{}:
		defer func() { <-discoverySlot }()
	case <-ctx.Done():
		return ctx.Err()
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	options := runnerOptions(a.sources, a.limit)
	options.ResultCallback = func(result *resolve.HostEntry) {
		if !emit(result.Host) {
			cancel()
		}
	}

	r, err := runner.NewRunner(options)
	if err != nil {
		return err
	}
	_, err = r.EnumerateSingleDomainWithCtx(runCtx, root, []io.Writer{io.Discard})
	if err != nil {
		return err
	}
	return runCtx.Err()
}

func noKeySourceNames() []string {
	sources := make([]string, 0, len(passive.AllSources))
	for _, source := range passive.AllSources {
		if source.KeyRequirement() == subscraping.NoKey {
			sources = append(sources, source.Name())
		}
	}
	sort.Strings(sources)
	return sources
}

func runnerOptions(sources []string, limit int) *runner.Options {
	return &runner.Options{
		All:                 false,
		Sources:             goflags.StringSlice(sources),
		Threads:             4,
		Timeout:             10,
		MaxEnumerationTime:  1,
		MaxResults:          limit + 1,
		MaxResponseBodySize: maxResponseBodySize,
		DisableUpdateCheck:  true,
		RemoveWildcard:      false,
		Silent:              true,
	}
}
