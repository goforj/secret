//go:build !race

package examples

// raceEnabled avoids requiring a race toolchain for ordinary example runs.
func raceEnabled() bool { return false }
