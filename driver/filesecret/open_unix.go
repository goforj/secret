//go:build linux || darwin

package filesecret

import (
	"os"
	"syscall"
)

// open prevents a FIFO open from blocking before the descriptor's type is checked.
func open(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
