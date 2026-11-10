//go:build linux || darwin

package main

import (
	"os"
	"syscall"
)

// openKeyFile prevents a mistaken FIFO path from blocking before the regular-file check.
func openKeyFile(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
