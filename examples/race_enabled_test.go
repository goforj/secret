//go:build race

package examples

// raceEnabled carries parent race instrumentation into child example programs.
func raceEnabled() bool { return true }
