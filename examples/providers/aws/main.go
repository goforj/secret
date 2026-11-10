// Command aws reads an existing AWS Secrets Manager secret using the default credential chain.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/goforj/secret/driver/awssecret"
)

// main leaves authentication and region selection to the AWS SDK.
func main() {
	arn := flag.String("arn", "", "full ARN of an existing text secret")
	flag.Parse()
	if *arn == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*arn); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run binds an application name without exposing the retrieved credential.
func run(arn string) error {
	// snippet:start
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	client := secretsmanager.NewFromConfig(cfg)
	store, err := awssecret.New(client, map[string]awssecret.Binding{
		"DATABASE_PASSWORD": {ARN: arn, Encoding: awssecret.Text},
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
