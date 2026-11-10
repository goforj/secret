// Command reader adapts application-owned secret retrieval without provider dependencies.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/goforj/secret"
)

// main demonstrates an adapter and classification without printing secret values.
func main() {
	// snippet:start
	reader := secret.Func(func(ctx context.Context, name string) (string, error) {
		if name != "DATABASE_PASSWORD" {
			return "", secret.ErrNotFound
		}
		return "public-example-password", nil
	})

	ctx := context.Background()
	if _, err := reader.Get(ctx, "DATABASE_PASSWORD"); err != nil {
		panic(err)
	}
	fmt.Println("credential loaded")
	// Output: credential loaded

	if _, err := reader.Get(ctx, "UNKNOWN"); !errors.Is(err, secret.ErrNotFound) {
		panic("expected an unknown name to return ErrNotFound")
	}
	fmt.Println("unknown name rejected")
	// Output: unknown name rejected
	// snippet:end
}
