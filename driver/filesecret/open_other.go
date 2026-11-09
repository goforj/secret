//go:build !linux && !darwin

package filesecret

import "os"

// open uses Windows file-open semantics; New rejects other unverified platforms.
func open(root *os.Root, path string) (*os.File, error) { return root.OpenFile(path, os.O_RDONLY, 0) }
