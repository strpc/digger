package subfinder

import (
	"slices"
	"testing"

	"github.com/projectdiscovery/subfinder/v2/pkg/passive"
	"github.com/projectdiscovery/subfinder/v2/pkg/subscraping"
)

func TestNoKeySourcesAndRunnerOptions(t *testing.T) {
	sources := noKeySourceNames()
	if len(sources) == 0 || !slices.IsSorted(sources) {
		t.Fatalf("sources must be non-empty and sorted: %v", sources)
	}
	allowed := make(map[string]bool, len(sources))
	for _, source := range passive.AllSources {
		allowed[source.Name()] = source.KeyRequirement() == subscraping.NoKey
	}
	for _, source := range sources {
		if !allowed[source] {
			t.Fatalf("source %q requires a key", source)
		}
	}

	options := runnerOptions(sources, 500)
	if options.All || options.Threads != 4 || options.Timeout != 10 || options.MaxEnumerationTime != 1 ||
		options.MaxResults != 501 || options.MaxResponseBodySize != 5*1024*1024 ||
		!options.DisableUpdateCheck || options.RemoveWildcard {
		t.Fatalf("unexpected runner options: %+v", options)
	}
	if !slices.Equal([]string(options.Sources), sources) {
		t.Fatalf("configured sources = %v, want %v", options.Sources, sources)
	}
}
