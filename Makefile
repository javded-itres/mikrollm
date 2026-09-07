GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: test run build-arm64 tar-ros tidy

tidy:
	$(GO) mod tidy

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/mikrollm -listen :4000 -data ./data -admin-password admin

build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/mikrollm ./cmd/mikrollm

tar-ros: build-arm64
	mkdir -p dist
	docker buildx build --platform linux/arm64 --output type=docker,dest=dist/mikrollm-ros.tar -t mikrollm:arm64 .
	python3 scripts/oci_to_legacy_docker.py dist/mikrollm-ros.tar dist/mikrollm-ros-legacy.tar
	ls -lh dist/mikrollm-ros-legacy.tar dist/mikrollm
