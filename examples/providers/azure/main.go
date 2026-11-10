// Command azure reads an existing Key Vault secret using DefaultAzureCredential.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/goforj/secret/driver/azuresecret"
)

// main accepts resource identifiers separately from SDK-managed authentication.
func main() {
	vault := flag.String("vault", "", "vault URL, such as https://my-vault.vault.azure.net/")
	name := flag.String("secret", "database-password", "existing secret name")
	flag.Parse()
	if *vault == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*vault, *name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run lets the SDK select a configured development or workload identity.
func run(vault, name string) error {
	// snippet:start
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return err
	}
	client, err := azsecrets.NewClient(vault, credential, nil)
	if err != nil {
		return err
	}
	store, err := azuresecret.New(client, map[string]azuresecret.Binding{
		"DATABASE_PASSWORD": {Name: name},
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
