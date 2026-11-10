// Command gcp reads an existing Google secret using Application Default Credentials.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"cloud.google.com/go/secretmanager/apiv1"
	"github.com/goforj/secret/driver/gcpsecret"
)

// main accepts resource identifiers separately from SDK-managed authentication.
func main() {
	parent := flag.String("parent", "", "parent resource, such as projects/my-project")
	id := flag.String("secret", "database-password", "existing secret ID")
	flag.Parse()
	if *parent == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*parent, *id); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// snippet:start
// run owns and closes the SDK client borrowed by the driver.
func run(parent, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	store, err := gcpsecret.New(client, parent, map[string]string{
		"DATABASE_PASSWORD": id,
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
	return nil
}

// snippet:end
