SHELL=/bin/bash

COMMIT_HASH = $(shell git rev-parse --short HEAD)
VERSION     = $(shell git tag --points-at HEAD)
APP_NAME    = digger

.PHONY: build
build:
	go build \
		-ldflags=" \
			-X 'main.version=$(VERSION)' \
			-X 'main.commitHash=$(COMMIT_HASH)' \
		" \
		-o ./bin/$(APP_NAME) ./cmd/$(APP_NAME)

.PHONY: app
app: build
	./bin/$(APP_NAME)

.PHONY: docker-build
docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_HASH=$(COMMIT_HASH) \
		--build-arg PROJECT_PATH=cmd/$(APP_NAME) \
		-t $(APP_NAME):$(VERSION) \
		-t $(APP_NAME):latest \
		.
