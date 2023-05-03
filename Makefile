.DEFAULT_GOAL:=help

IMAGE_REGISTRY ?= quay.io
IMAGE_REPO ?= $(USER)
IMAGE_NAME ?= origin-baremetal-runtimecfg
IMAGE_BUILDER ?= podman
IMAGE_DOCKERFILE ?= Dockerfile

SHELL := /bin/bash
GO_VERSION = $(shell hack/go-version.sh)

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./pkg/... ./cmd/...
	git diff --exit-code

.PHONY: test
test: ## Run go test against code
	go test -v ./pkg/... ./cmd/... -ginkgo.focus=${FOCUS} -ginkgo.v

.PHONY: docker_test
docker_test: ## Run test target on docker
	-docker-compose down
	docker-compose build test && docker-compose run --rm test make test

.PHONY: vet
vet: ## Run go vet against code
	go vet ./pkg/... ./cmd/...

.PHONY: image
image: ## Build a local image
	$(IMAGE_BUILDER) build -t $(IMAGE_REGISTRY)/$(IMAGE_REPO)/$(IMAGE_NAME) -f $(IMAGE_DOCKERFILE) .

.PHONY: push
push: image ## Push the image to the registry. Will attempt to build the image first
	$(IMAGE_BUILDER) push $(IMAGE_REGISTRY)/$(IMAGE_REPO)/$(IMAGE_NAME)

.PHONY: vendor
vendor:
	go mod tidy -compat=$(GO_VERSION)
	go mod vendor
