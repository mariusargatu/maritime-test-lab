// Package env reads configuration from the process environment. Tiny on
// purpose: config structs own validation, this only does lookup-with-default.
package env

import (
	"os"
	"strings"
)

// Or returns the value of the environment variable named key, or def when the
// variable is unset or empty.
func Or(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// List reads key (falling back to def), splits it on commas, trims each item,
// and drops empties — e.g. a "host1:9092, host2:9092" broker list.
func List(key, def string) []string {
	parts := strings.Split(Or(key, def), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
