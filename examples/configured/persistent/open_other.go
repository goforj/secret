//go:build !linux && !darwin

package main

import "os"

// openKeyFile borrows filesystem access under the caller's OS ACL configuration.
func openKeyFile(name string) (*os.File, error) { return os.Open(name) }
