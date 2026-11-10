// Command rotation migrates encrypted files while keeping previous keys readable.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"

	"github.com/goforj/secret/driver/filesecret"
)

// main uses disposable keys and prepares an old-key fixture before demonstrating rotation.
func main() {
	directory, err := os.MkdirTemp("", "secret-rotation-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	root, err := os.OpenRoot(directory)
	if err != nil {
		panic(err)
	}
	defer root.Close()
	currentKey, previousKey := make([]byte, 32), make([]byte, 32)
	if _, err := rand.Read(currentKey); err != nil {
		panic(err)
	}
	if _, err := rand.Read(previousKey); err != nil {
		panic(err)
	}
	bindings := map[string]string{"DATABASE_PASSWORD": "database-password.enc"}
	old, err := filesecret.NewStore(root, previousKey, bindings)
	if err != nil {
		panic(err)
	}
	if err := old.Set(context.Background(), "DATABASE_PASSWORD", "public-example-password"); err != nil {
		panic(err)
	}
	// snippet:start
	store, err := filesecret.NewStoreWithKeys(root, currentKey, bindings, previousKey)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	if _, err := store.Get(ctx, "DATABASE_PASSWORD"); err != nil {
		panic(err)
	}
	fmt.Println("old-key credential readable")
	// Output: old-key credential readable

	if err := store.Rotate(ctx); err != nil {
		panic(err)
	}
	fmt.Println("files rotated")
	// Output: files rotated

	currentOnly, err := filesecret.NewStore(root, currentKey, bindings)
	if err != nil {
		panic(err)
	}
	if _, err := currentOnly.Get(ctx, "DATABASE_PASSWORD"); err != nil {
		panic(err)
	}
	fmt.Println("credential readable without previous key")
	// Output: credential readable without previous key
	// snippet:end
}
