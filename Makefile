.DEFAULT_GOAL := help

.PHONY: help docs-install docs-dev docs-build docs-preview

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

docs-install: ## Install documentation site dependencies
	npm --prefix docs ci

docs-dev: ## Start the documentation development server
	npm --prefix docs run dev

docs-build: docs-install ## Build the static documentation site
	npm --prefix docs run build

docs-preview: ## Preview the production documentation build
	npm --prefix docs run preview
