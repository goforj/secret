// Command files reads mounted credentials without trimming their bytes or printing their values.
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
	// These public fixtures stand in for Docker or Kubernetes mounted secrets.
	if err := os.WriteFile(filepath.Join(directory, "database-username"), []byte("public-example-user"), 0600); err != nil {
		panic(err)
	}
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
		"DATABASE_USERNAME": "database-username",
		"DATABASE_PASSWORD": "database-password",
	})
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	username, err := reader.Get(ctx, "DATABASE_USERNAME")
	if err != nil {
		panic(err)
	}
	password, err := reader.Get(ctx, "DATABASE_PASSWORD")
	if err != nil {
		panic(err)
	}
	// Pass both values directly to your database client. Keep them out of logs.
	_, _ = username, password
	fmt.Println("database credentials loaded")
	// Output: database credentials loaded
	// snippet:end
}
