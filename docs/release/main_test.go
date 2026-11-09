package main

import "testing"

// TestValidVersion keeps release plans compatible with Go's canonical module versions.
func TestValidVersion(t *testing.T) {
	for _, version := range []string{"v0.0.0", "v0.1.0", "v1.0.0", "v1.2.3-rc.1", "v0.1.0-0.20261109120000-abcdef012345", "v1.0.0-0", "v1.0.0-01a"} {
		if !validVersion(version) {
			t.Errorf("valid version rejected: %s", version)
		}
	}
	for _, version := range []string{"", "1.0.0", "v1", "v1.0", "v01.0.0", "v1.01.0", "v1.0.01", "v2.0.0", "v0.1.0+meta", "v1.0.0-", "v1.0.0-a..b", "v1.0.0-01", "v1.0.0-rc.01", "v1.0.0/other", "v1.0.0\n", "v1.0.0-a_b"} {
		if validVersion(version) {
			t.Errorf("invalid version accepted: %q", version)
		}
	}
}
