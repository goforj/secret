module github.com/goforj/secret/driver/azuresecret

go 1.26.0

require (
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.13.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/internal v1.2.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.23.3
	github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets v1.5.1
	github.com/goforj/secret v0.1.0
)

replace github.com/goforj/secret => ../..
