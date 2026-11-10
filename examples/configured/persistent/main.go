// Command persistent reads encrypted files with a separately managed, persistent key file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/goforj/secret/driver/filesecret"
)

// main requires explicit paths and never provisions, replaces, or prints keys or secrets.
func main() {
	directory := flag.String("directory", "", "existing encrypted secret directory")
	keyFile := flag.String("key-file", "", "separately managed file containing exactly 32 raw key bytes")
	flag.Parse()
	if *directory == "" || *keyFile == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*directory, *keyFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run reloads the existing key on every process start without creating a replacement.
func run(directory, keyFile string) error {
	// snippet:start
	key, err := loadKey(keyFile)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return errors.New("encrypted secret directory unavailable")
	}
	defer root.Close()
	store, err := filesecret.NewStore(root, key, map[string]string{
		"DATABASE_PASSWORD": "database-password.enc",
	})
	if err != nil {
		return err
	}
	password, err := store.Get(context.Background(), "DATABASE_PASSWORD")
	if err != nil {
		return err
	}
	// Pass password to your database client instead of logging it.
	_ = password
	fmt.Println("credential loaded from persistent key")
	// Output: credential loaded from persistent key
	// snippet:end
	return nil
}

// loadKey accepts one private regular file and bounds reads before allocating key material.
func loadKey(name string) ([]byte, error) {
	file, err := openKeyFile(name)
	if err != nil {
		return nil, errors.New("encryption key file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("encryption key must come from a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("encryption key file must not be accessible to group or other users")
	}
	key, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil || len(key) != 32 {
		return nil, errors.New("encryption key file must contain exactly 32 raw bytes")
	}
	return key, nil
}
