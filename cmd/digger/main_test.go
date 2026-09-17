package main

import (
	"testing"
	"time"
)

func TestPositiveIntEnv(t *testing.T) {
	t.Setenv("SUBDOMAIN_LIMIT", "25")
	value, err := positiveIntEnv("SUBDOMAIN_LIMIT", "500")
	if err != nil || value != 25 {
		t.Fatalf("value = %d, err = %v", value, err)
	}

	for _, invalid := range []string{"0", "-1", "nope"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv("SUBDOMAIN_LIMIT", invalid)
			if _, err := positiveIntEnv("SUBDOMAIN_LIMIT", "500"); err == nil {
				t.Fatalf("accepted invalid value %q", invalid)
			}
		})
	}
}

func TestPositiveDurationEnv(t *testing.T) {
	t.Setenv("SUBDOMAIN_TIMEOUT", "25")
	value, err := positiveDurationEnv("SUBDOMAIN_TIMEOUT", "20")
	if err != nil || value != 25*time.Second {
		t.Fatalf("value = %s, err = %v", value, err)
	}

	for _, invalid := range []string{"0", "-1", "1.5", "nope", "9223372037"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv("SUBDOMAIN_TIMEOUT", invalid)
			if _, err := positiveDurationEnv("SUBDOMAIN_TIMEOUT", "20"); err == nil {
				t.Fatalf("accepted invalid value %q", invalid)
			}
		})
	}
}
