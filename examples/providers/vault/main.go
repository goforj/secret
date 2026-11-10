// Command vault reads an existing scalar document from a Vault KV v2 mount.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/goforj/secret/driver/vaultsecret"
	"github.com/hashicorp/vault/api"
)

// main leaves server configuration and token authentication in the environment.
func main() {
	mount := flag.String("mount", "secret", "KV v2 mount")
	path := flag.String("path", "production/database-password", "existing scalar document path")
	flag.Parse()
	if err := run(*mount, *path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run borrows a KV v2 client without taking over authentication or token renewal.
func run(mount, path string) error {
	// snippet:start
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := api.NewClient(nil)
	if err != nil {
		return err
	}
	store, err := vaultsecret.New(client.KVv2(mount), map[string]string{
		"DATABASE_PASSWORD": path,
	})
	if err != nil {
		return err
	}
	password, err := store.Get(ctx, "DATABASE_PASSWORD")
	if err != nil {
		return err
	}
	// Pass password to your database client instead of logging it.
	_ = password
	fmt.Println("credential loaded")
	// Output: credential loaded
	// snippet:end
	return nil
}
