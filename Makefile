GO ?= go
GOBIN ?= $(shell $(GO) env GOPATH)/bin
# Last x/mobile with go 1.23 (later commits bump 1.24+ / 1.26).
MOBILE_VER ?= v0.0.0-20250808145247-395d808d53cd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: test run build-arm64 build-keenetic tar-ros ios tidy

tidy:
	$(GO) mod tidy

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/mikrollm -listen :4000 -data ./data -admin-password admin

build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/mikrollm ./cmd/mikrollm

# Keenetic Entware: linux/arm64 (Peak / Ultra KN-1811 / Giga KN-1012 / Hopper KN-3811).
# MIPS Entware is the same installer; modernc SQLite has no linux/mips(le) in this module set.
build-keenetic: build-arm64
	mkdir -p dist
	cp dist/mikrollm dist/mikrollm-linux-arm64
	ls -lh dist/mikrollm-linux-arm64

# iOS: gomobile xcframework + XcodeGen project (first slice: localhost server + WKWebView).
# Pin x/mobile to MOBILE_VER so `go get` does not bump the module past Go 1.23.
ios:
	$(GO) install golang.org/x/mobile/cmd/gomobile@$(MOBILE_VER)
	$(GO) install golang.org/x/mobile/cmd/gobind@$(MOBILE_VER)
	PATH="$(GOBIN):$$PATH" $(GOBIN)/gomobile init || true
	mkdir -p ios
	PATH="$(GOBIN):$$PATH" $(GOBIN)/gomobile bind -target=ios/arm64,iossimulator/arm64 -iosversion=16.0 -o ios/Mobile.xcframework ./mobile
	@if command -v xcodegen >/dev/null 2>&1; then cd ios && xcodegen generate; echo "Open ios/MikroLLM.xcodeproj"; else echo "Install xcodegen (brew install xcodegen) then: cd ios && xcodegen generate"; fi

# GitHub Releases: English notes from CHANGELOG.md — see docs/releasing.md
tar-ros: build-arm64
	mkdir -p dist
	docker buildx build --platform linux/arm64 --output type=docker,dest=dist/mikrollm-ros.tar -t mikrollm:arm64 .
	python3 scripts/oci_to_legacy_docker.py dist/mikrollm-ros.tar dist/mikrollm-ros-legacy.tar
	ls -lh dist/mikrollm-ros-legacy.tar dist/mikrollm
