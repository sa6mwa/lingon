.NOTPARALLEL:
.PHONY: help generate build test test-short test-webui test-last-fail test-android test-all lint fmt vet golangci-lint install release prerelease release-matrix package-verify publish print-release-version clean container push-container dev-up dev-down dev-ps dev-logs dev-reset \
	android-clean android-sdk android-avd android-emulator android-build android-release android-install android-install-release android-integration-test

GO ?= go
CGO_ENABLED ?= 0
BIN ?= bin/lingon
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
INSTALL ?= install
INSTALL_MODE ?= 0755
VERSION ?=
RELEASE_MESSAGE ?=
export LINGON_RELEASE_VERSION := $(VERSION)
export LINGON_RELEASE_MESSAGE := $(RELEASE_MESSAGE)
LIFECYCLE = env GOMAXPROCS=2 GOFLAGS=-p=1 $(GO) run ./cmd/lingon-lifecycle

GOFLAGS ?=
BUILD_FLAGS ?= -trimpath
LD_FLAGS ?=
TEST_LOG ?= build/test.log
WEBUI_TEST_PKGS ?= ./integration/webui

ANDROID_DIR ?= android
APK_PATH ?= $(ANDROID_DIR)/app/build/outputs/apk/release/app-release.apk

.DEFAULT_GOAL := help

help:
	@echo "Targets:"
	@echo "  make                # show this help"
	@echo "  make generate       # run: go generate ./..."
	@echo "  make build          # build the lingon binary"
	@echo "  make test           # run tests with coverage"
	@echo "  make test-short     # run uncached short Go tests with coverage"
	@echo "  make test-short     # run short tests with coverage"
	@echo "  make test-webui     # run web UI integration tests"
	@echo "  make test-last-fail # show last failing test from $(TEST_LOG)"
	@echo "  make test-android   # run Android integration tests"
	@echo "  make test-all       # run all tests (phone tests are not headless!)"
	@echo "  make lint           # go vet + golint + golangci-lint"
	@echo "  make install        # install binary"
	@echo "  make prerelease     # serial full proof + six-platform matrix + APK + Podman rehearsal"
	@echo "  make release VERSION=X.Y.Z RELEASE_MESSAGE='feat(scope): change'"
	@echo "                      # candidate proof, squash to trunk, lightweight tag, clean final proof/artifacts"
	@echo "  make release-matrix # incremental artifact rehearsal, never sufficient to publish"
	@echo "  make package-verify # inspect existing receipt, checksums, binary/APK/image metadata"
	@echo "  make publish        # push verified trunk/tag, publish GitHub assets, push Docker Hub last"
	@echo "  make container      # verify the image produced by release-matrix"
	@echo "  make push-container # retry only the final Docker Hub push; never rebuild"
	@echo "  make print-release-version # exact lightweight vX.Y.Z on HEAD, otherwise 0.0.0"
	@echo "  make dev-up LINGON_DEV_IMAGE=docker.io/pktsystems/lingon:vX.Y.Z"
	@echo "  make dev-down dev-ps dev-logs dev-reset # rootless Podman Kube, state under build/devenv"
	@echo "  Release contract: Linux/FreeBSD/Darwin amd64+arm64, CGO=0; Linux/BSD static."
	@echo "  Release tests run on Linux; BSD/Darwin are cross-built and inspected."
	@echo "  GitHub and Docker Hub authentication must already be configured."
	@echo ""
	@echo "Android targets:"
	@echo "  make android-sdk"
	@echo "  make android-avd"
	@echo "  make android-emulator"
	@echo "  make android-build"
	@echo "  make android-release"
	@echo "  make android-install          # install debug APK on a device/emulator"
	@echo "  make android-install-release  # build and install release APK on a connected phone"
	@echo "  make android-integration-test"
	@echo ""
	@echo "Variables:"
	@echo "  GO=$(GO)"
	@echo "  CGO_ENABLED=$(CGO_ENABLED)"
	@echo "  BIN=$(BIN)"
	@echo "  PREFIX=$(PREFIX)"
	@echo "  BINDIR=$(BINDIR)"
	@echo "  INSTALL=$(INSTALL)"
	@echo "  INSTALL_MODE=$(INSTALL_MODE)"
	@echo "  GOFLAGS=$(GOFLAGS)"
	@echo "  BUILD_FLAGS=$(BUILD_FLAGS)"
	@echo "  LD_FLAGS=$(LD_FLAGS)"
	@echo "  WEBUI_TEST_PKGS=$(WEBUI_TEST_PKGS)"
	@echo "  ANDROID_DIR=$(ANDROID_DIR)"
	@echo "  APK_PATH=$(APK_PATH)"

generate:
	$(GO) generate ./...

build:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -buildvcs=true $(GOFLAGS) $(BUILD_FLAGS) -ldflags="$(LD_FLAGS)" -o $(BIN) ./cmd/lingon

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

golangci-lint:
	golangci-lint run ./...

lint: vet
	golint ./...
	$(MAKE) golangci-lint

test:
	@mkdir -p $(dir $(TEST_LOG))
	@bash -lc 'set -o pipefail; $(GO) test -count=1 -cover -json ./... | tee "$(TEST_LOG)"'

test-short:
	@mkdir -p $(dir $(TEST_LOG))
	@bash -lc 'set -o pipefail; $(GO) test -short -count=1 -cover -json ./... | tee "$(TEST_LOG)"'

test-webui:
	@mkdir -p $(dir $(TEST_LOG))
	@bash -lc 'set -o pipefail; $(GO) test -count=1 -tags integration -json $(WEBUI_TEST_PKGS) | tee -a "$(TEST_LOG)"'

test-last-fail:
	@bash -lc 'if [ ! -f "$(TEST_LOG)" ]; then echo "missing $(TEST_LOG); run make test first"; exit 1; fi; \
		if ! command -v jq >/dev/null 2>&1; then echo "jq is required for test-last-fail"; exit 1; fi; \
		test_row=$$(jq -r '\''select(.Action=="fail" and .Test != null) | [.Package, .Test, (.Elapsed // 0)] | @tsv'\'' "$(TEST_LOG)" | tail -n 1); \
		if [ -n "$$test_row" ]; then \
			pkg=$$(printf "%s" "$$test_row" | cut -f1); \
			test_name=$$(printf "%s" "$$test_row" | cut -f2); \
			elapsed=$$(printf "%s" "$$test_row" | cut -f3); \
			echo "last failing test: package=$$pkg test=$$test_name elapsed=$$elapsed"; \
			echo "--- recent test output ---"; \
			jq -r --arg p "$$pkg" --arg t "$$test_name" '\''select(.Package==$$p and .Test==$$t and .Action=="output") | .Output'\'' "$(TEST_LOG)" | tail -n 120; \
			exit 0; \
		fi; \
		pkg_row=$$(jq -r '\''select(.Action=="fail" and .Test == null) | [.Package, (.Elapsed // 0)] | @tsv'\'' "$(TEST_LOG)" | tail -n 1); \
		if [ -z "$$pkg_row" ]; then echo "no failing tests in $(TEST_LOG)"; exit 0; fi; \
		pkg=$$(printf "%s" "$$pkg_row" | cut -f1); \
		elapsed=$$(printf "%s" "$$pkg_row" | cut -f2); \
		echo "last failing package: package=$$pkg elapsed=$$elapsed"; \
		echo "--- failed tests in package (recent) ---"; \
		jq -r --arg p "$$pkg" '\''select(.Package==$$p and .Action=="fail" and .Test != null) | .Test'\'' "$(TEST_LOG)" | tail -n 10; \
		echo "--- recent package output ---"; \
		jq -r --arg p "$$pkg" '\''select(.Package==$$p and .Action=="output") | .Output'\'' "$(TEST_LOG)" | tail -n 120'

test-android:
	$(MAKE) -C $(ANDROID_DIR) integration-test

test-all:
	$(LIFECYCLE) prerelease </dev/null

install: build
	$(INSTALL) -m $(INSTALL_MODE) $(BIN) $(BINDIR)/lingon
	rm -f $(BINDIR)/lingonx
	ln -s lingon $(BINDIR)/lingonx

release prerelease release-matrix package-verify publish container push-container dev-up dev-down dev-ps dev-logs dev-reset:
	$(LIFECYCLE) $@ </dev/null

print-release-version:
	@$(LIFECYCLE) version </dev/null

clean:
	rm -rf $(dir $(BIN))
	$(GO) clean ./...

android-clean:
	$(MAKE) -C $(ANDROID_DIR) clean

android-sdk:
	$(MAKE) -C $(ANDROID_DIR) sdk

android-avd:
	$(MAKE) -C $(ANDROID_DIR) avd

android-emulator:
	$(MAKE) -C $(ANDROID_DIR) emulator

android-build:
	$(MAKE) -C $(ANDROID_DIR) build

android-release:
	$(MAKE) -C $(ANDROID_DIR) release

android-install:
	$(MAKE) -C $(ANDROID_DIR) install

android-install-release:
	$(MAKE) -C $(ANDROID_DIR) install-release

android-integration-test:
	$(MAKE) -C $(ANDROID_DIR) integration-test
