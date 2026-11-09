module github.com/goforj/secret/driver/awssecret

go 1.24.4

require (
	github.com/aws/aws-sdk-go-v2 v1.47.8 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.11 // indirect
)

require (
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.50.11
	github.com/aws/smithy-go v1.28.8
	github.com/goforj/secret v0.1.0
)

replace github.com/goforj/secret => ../..
