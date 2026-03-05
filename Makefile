.PHONY: help generate build test test-short test-webui test-last-fail test-android lint fmt vet golangci-lint install release clean container push-container \
	android-sdk android-avd android-emulator android-build android-release android-install android-integration-test

GO ?= go
CGO_ENABLED ?= 0
BIN ?= bin/lingon
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
INSTALL ?= install
INSTALL_MODE ?= 0755
CONTAINER_BUILDER ?= $(shell command -v podman || command -v nerdctl || command -v docker)
IMAGE ?= docker.io/pktsystems/lingon
LINGON_VERSION ?= $(shell $(GO) run ./cmd/lingon version --version)
LINGON_SEMVER ?= $(shell $(GO) run ./cmd/lingon version --semver)

GOFLAGS ?=
BUILD_FLAGS ?= -trimpath
LD_FLAGS ?= -s -w
TEST_LOG ?= test.log

ANDROID_DIR ?= android
APK_PATH ?= $(ANDROID_DIR)/app/build/outputs/apk/release/app-release.apk
APK_OUT ?= $(dir $(BIN))lingon.apk
GIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
ZIP_OUT ?= $(dir $(BIN))lingon-$(shell $(GO) env GOOS)-$(shell $(GO) env GOARCH)-$(LINGON_SEMVER).zip

.DEFAULT_GOAL := help

help:
	@echo "Targets:"
	@echo "  make                # show this help"
	@echo "  make generate       # run: go generate ./..."
	@echo "  make build          # build the lingon binary"
	@echo "  make test           # run tests with coverage"
	@echo "  make test-short     # run short tests with coverage"
	@echo "  make test-webui     # run webui-tagged tests"
	@echo "  make test-last-fail # show last failing test from $(TEST_LOG)"
	@echo "  make test-android   # run Android integration tests"
	@echo "  make test-all       # run all tests (phone tests are not headless!)"
	@echo "  make lint           # go vet + golint + golangci-lint"
	@echo "  make install        # install binary"
	@echo "  make container      # build container image (podman/nerdctl/docker)"
	@echo "  make push-container # push version + latest container tags"
	@echo "  make release        # build binary + Android APK + zip"
	@echo ""
	@echo "Android targets:"
	@echo "  make android-sdk"
	@echo "  make android-avd"
	@echo "  make android-emulator"
	@echo "  make android-build"
	@echo "  make android-release"
	@echo "  make android-install"
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
	@echo "  CONTAINER_BUILDER=$(CONTAINER_BUILDER)"
	@echo "  IMAGE=$(IMAGE)"
	@echo "  LINGON_VERSION=$(LINGON_VERSION)"
	@echo "  LINGON_SEMVER=$(LINGON_SEMVER)"
	@echo "  GOFLAGS=$(GOFLAGS)"
	@echo "  BUILD_FLAGS=$(BUILD_FLAGS)"
	@echo "  LD_FLAGS=$(LD_FLAGS)"
	@echo "  ANDROID_DIR=$(ANDROID_DIR)"
	@echo "  APK_PATH=$(APK_PATH)"
	@echo "  APK_OUT=$(APK_OUT)"
	@echo "  ZIP_OUT=$(ZIP_OUT)"

generate:
	$(GO) generate ./...

$(BIN):
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(BUILD_FLAGS) -ldflags="$(LD_FLAGS)" -o $(BIN) ./cmd/lingon

build: $(BIN)

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
	@bash -lc 'set -o pipefail; $(GO) test -count=1 -cover -json ./... | tee "$(TEST_LOG)"'

test-webui:
	@bash -lc 'set -o pipefail; $(GO) test -count=1 -tags webui -json ./... | tee -a "$(TEST_LOG)"'

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
	time $(MAKE) test test-webui test-android

install: $(BIN)
	$(INSTALL) -m $(INSTALL_MODE) $(BIN) $(BINDIR)/lingon
	rm -f $(BINDIR)/lingonx
	ln -s lingon $(BINDIR)/lingonx

release: build android-release
	@mkdir -p $(dir $(BIN))
	@if [ -f "$(APK_PATH)" ]; then \
		cp $(APK_PATH) $(APK_OUT); \
	else \
		echo "Error: APK not found at $(APK_PATH). Run 'make android-release' first." >&2; \
		exit 1; \
	fi
	zip -j $(ZIP_OUT) $(BIN) $(APK_OUT)

podman.yaml:
	envsubst < podman.yaml.template > podman.yaml

container: $(BIN) podman.yaml
	@if [ -z "$(CONTAINER_BUILDER)" ]; then \
		echo "Error: no container builder found (podman, nerdctl, docker)." >&2; \
		exit 1; \
	fi
	$(CONTAINER_BUILDER) build -f Containerfile --build-arg LINGON_BIN=$(BIN) -t $(IMAGE):$(LINGON_VERSION) .
	$(CONTAINER_BUILDER) tag $(IMAGE):$(LINGON_VERSION) $(IMAGE):latest

push-container:
	$(CONTAINER_BUILDER) push $(IMAGE):$(LINGON_VERSION)
	$(CONTAINER_BUILDER) push $(IMAGE):latest

clean:
	rm -rf $(dir $(BIN))
	$(GO) clean ./...

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

android-integration-test:
	$(MAKE) -C $(ANDROID_DIR) integration-test
