package examples

import (
	"context"
	"errors"
	"fmt"
	"github.com/goforj/secret"
	"github.com/goforj/secret/secrettest"
	"testing"
)

// TestStore demonstrates deterministic application tests using the same narrow capabilities.
func TestStore(t *testing.T) {
	// snippet:start
	store := secrettest.New(t, map[string]string{
		"DATABASE_PASSWORD": "public-test-password",
	})
	ctx := context.Background()

	if err := store.Set(ctx, "DATABASE_PASSWORD", "public-replacement"); err != nil {
		t.Fatal(err)
	}
	_, err := store.Get(ctx, "DATABASE_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("replacement readable:", err == nil)
	// Output: replacement readable: true

	if err := store.Delete(ctx, "DATABASE_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	_, err = store.Get(ctx, "DATABASE_PASSWORD")
	if !errors.Is(err, secret.ErrNotFound) {
		t.Fatal("deleted fixture remains readable")
	}
	fmt.Println("removed:", errors.Is(err, secret.ErrNotFound))
	// Output: removed: true
	// snippet:end
}
