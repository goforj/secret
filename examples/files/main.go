// Command files reads a mounted credential without trimming its bytes or printing its value.
package main

import (
	"context"
	"fmt"
	"github.com/goforj/secret/driver/filesecret"
	"os"
	"path/filepath"
)

// main owns the temporary directory and root; filesecret only borrows the root.
func main() {
	directory, err := os.MkdirTemp("", "secret-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	// This public fixture stands in for a Docker or Kubernetes mounted secret.
	if err := os.WriteFile(filepath.Join(directory, "database-password"), []byte("public-example-password\n"), 0600); err != nil {
		panic(err)
	}
	// snippet:start
	root, err := os.OpenRoot(directory)
	if err != nil {
		panic(err)
	}
	defer root.Close()

	reader, err := filesecret.New(root, map[string]string{
		"DATABASE_PASSWORD": "database-password",
	})
	if err != nil {
		panic(err)
	}

	password, err := reader.Get(context.Background(), "DATABASE_PASSWORD")
	if err != nil {
		panic(err)
	}
	// Pass password directly to your database client. Keep it out of logs.
	_ = password
	fmt.Println("credential loaded")
	// Output: credential loaded
	// snippet:end
}
