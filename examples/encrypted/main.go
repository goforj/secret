// Command encrypted stores a secret without writing its plaintext to disk.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"

	"github.com/goforj/secret/driver/filesecret"
)

// main uses a temporary key and directory solely for this disposable example.
func main() {
	directory, err := os.MkdirTemp("", "secret-encrypted-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	// Real applications load a persistent key from a separate key manager.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	// snippet:start
	root, err := os.OpenRoot(directory)
	if err != nil {
		panic(err)
	}
	defer root.Close()

	store, err := filesecret.NewStore(root, key, map[string]string{
		"DATABASE_PASSWORD": "database-password.enc",
	})
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	if err := store.Set(ctx, "DATABASE_PASSWORD", "public-example-password"); err != nil {
		panic(err)
	}
	password, err := store.Get(ctx, "DATABASE_PASSWORD")
	if err != nil {
		panic(err)
	}
	// Pass password to your database client instead of logging it.
	_ = password
	fmt.Println("credential loaded")
	// Output: credential loaded

	if err := store.Delete(ctx, "DATABASE_PASSWORD"); err != nil {
		panic(err)
	}
	fmt.Println("credential deleted")
	// Output: credential deleted
	// snippet:end
}
