.DEFAULT_GOAL := help
VERSION ?= v0.1.0

.PHONY: help test test-race vet tidy modules-check test-integration test-coverage generate check-generated security release-plan release-check release-published

help: ## Show development commands.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Test every module and execute canonical example programs.
	@scripts/test-modules.sh test

test-race: ## Race-test every module, including child example programs.
	@scripts/test-modules.sh race

vet: ## Vet every independent module.
	@scripts/test-modules.sh vet

tidy: ## Tidy every module while preserving development replacements.
	@scripts/test-modules.sh tidy

modules-check: ## Check the complete module inventory.
	@GOWORK=off go -C docs run ./modules

test-integration: ## Verify managed providers against pinned disposable services.
	@scripts/test-integration.sh

test-coverage: ## Enforce exact 100 percent unit statement coverage for all runtime modules.
	@scripts/test-unit.sh

generate: ## Generate README examples, tables, and evidence badges.
	@GOWORK=off go -C docs run ./readme

check-generated: ## Verify two generations leave the README and badges unchanged.
	@scripts/check-generated.sh

security: ## Scan every module using the installed govulncheck.
	@scripts/security.sh

release-plan: ## Print independent runtime tags, without creating them.
	@GOWORK=off go -C docs run ./release -mode plan -version $(VERSION)

release-check: ## Verify exact committed candidate archives in isolated consumers.
	@GOWORK=off go -C docs run ./release -mode archive -version $(VERSION)

release-published: ## Verify published modules through the public proxy and checksums.
	@GOWORK=off go -C docs run ./release -mode published -version $(VERSION)
