// Command errors demonstrates reconciliation after a provider may have accepted a write.
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/goforj/secret"
)

// uncertainWriter simulates a lost acknowledgement without contacting a provider.
type uncertainWriter struct{}

// Set models a write whose response was lost when its deadline expired.
func (uncertainWriter) Set(context.Context, string, string) error {
	return errors.Join(secret.ErrIndeterminate, context.DeadlineExceeded)
}

// main checks uncertainty first because caller cancellation cannot prove a write did not commit.
func main() {
	var writer secret.Writer = uncertainWriter{}
	// snippet:start
	err := writer.Set(context.Background(), "DATABASE_PASSWORD", "public-replacement")
	if errors.Is(err, secret.ErrIndeterminate) {
		// Read back or reconcile through provider tooling before attempting another write.
		fmt.Println("check provider state before retrying")
		// Output: check provider state before retrying
	} else if err != nil {
		panic(err)
	}
	fmt.Println("deadline still detectable:", errors.Is(err, context.DeadlineExceeded))
	// Output: deadline still detectable: true
	// snippet:end
}
