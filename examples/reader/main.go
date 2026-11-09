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

	_, err := reader.Get(context.Background(), "DATABASE_PASSWORD")
	fmt.Println("loaded:", err == nil)
	// Output: loaded: true

	_, err = reader.Get(context.Background(), "UNKNOWN")
	fmt.Println("missing:", errors.Is(err, secret.ErrNotFound))
	// Output: missing: true
	// snippet:end
}
