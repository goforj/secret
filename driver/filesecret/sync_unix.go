//go:build linux || darwin

package filesecret

import "os"

// syncDirectory persists rename and removal entries on supported Unix filesystems.
func syncDirectory(directory *os.File) error { return directory.Sync() }
