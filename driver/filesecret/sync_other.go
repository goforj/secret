//go:build !linux && !darwin

package filesecret

import "os"

// syncDirectory leaves directory durability to the OS where Go cannot flush it.
// In particular, Windows does not support syncing directory handles this way.
func syncDirectory(directory *os.File) error { return nil }
