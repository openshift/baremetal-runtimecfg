.DEFAULT_GOAL:=help

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./pkg/... ./cmd/...
	git diff --exit-code

.PHONY: test
test: ## Run go test against code
	docker-compose build $@ && docker-compose run --rm $@ make _$@

_test:
	go test -v ./pkg/... ./cmd/...

.PHONY: vet
vet: ## Run go vet against code
	go vet ./pkg/... ./cmd/...
