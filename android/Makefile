ANDROID_SDK_ROOT ?= $(HOME)/Android/Sdk
ANDROID_HOME ?= $(ANDROID_SDK_ROOT)
CMDLINE_TOOLS := $(ANDROID_SDK_ROOT)/cmdline-tools/latest
SDKMANAGER := $(CMDLINE_TOOLS)/bin/sdkmanager
AVDMANAGER := $(CMDLINE_TOOLS)/bin/avdmanager
EMULATOR := $(ANDROID_SDK_ROOT)/emulator/emulator
ADB := $(ANDROID_SDK_ROOT)/platform-tools/adb
EMULATOR_FLAGS ?= -gpu host -no-snapshot

PRESET ?=
AVD_NAME ?= lingon-aosp-35
SYSTEM_IMAGE ?= system-images;android-35;default;x86_64
DEVICE_PROFILE ?= small_phone
APP_ID := systems.pkt.lingon
UI_TEST_FLAGS ?= --rerun-tasks --no-build-cache

PRESET_LC := $(shell printf '%s' '$(PRESET)' | tr '[:upper:]' '[:lower:]')

ifneq ($(strip $(PRESET_LC)),)
  ifeq ($(PRESET_LC),small)
    PRESET_AVD_NAME := lingon-small
    PRESET_DEVICE_PROFILE := small_phone
  else ifeq ($(PRESET_LC),medium)
    PRESET_AVD_NAME := lingon-medium
    PRESET_DEVICE_PROFILE := medium_phone
  else ifeq ($(PRESET_LC),pixel7)
    PRESET_AVD_NAME := lingon-pixel7
    PRESET_DEVICE_PROFILE := pixel_7
  else ifeq ($(PRESET_LC),pixel9)
    PRESET_AVD_NAME := lingon-pixel9
    PRESET_DEVICE_PROFILE := pixel_9
  else
    $(error Unknown PRESET '$(PRESET)'. Use small, medium, pixel7, or pixel9.)
  endif
  ifeq ($(origin AVD_NAME),file)
    AVD_NAME := $(PRESET_AVD_NAME)
  endif
  ifeq ($(origin DEVICE_PROFILE),file)
    DEVICE_PROFILE := $(PRESET_DEVICE_PROFILE)
  endif
endif

.DEFAULT_GOAL := help

.PHONY: help sdk licenses avd avd-clean-snapshots emulator build release install run clean local-properties list-presets cleanup-managed-devices cleanup-offline-emulators adb-install adb-push integration-test
VERSION_CMD := cd .. && go run -buildvcs=true ./cmd/lingon version --version

help:
	@echo "lingon android make targets:"
	@echo "  make sdk          - install Android SDK + emulator (API 35 target + API 36 compile)"
	@echo "  make licenses     - accept Android SDK licenses"
	@echo "  make avd          - create AVD $(AVD_NAME) under ~/.android/avd (perf tune: GPU/RAM/cores; hardware keyboard enabled)"
	@echo "  make emulator     - start emulator for $(AVD_NAME) (-gpu host -no-snapshot + adb reverse + debug endpoint)"
	@echo "                      also enforces host emulator shortcuts/pinch defaults (Ctrl+wheel/gesture zoom)"
	@echo "                      auto-pushes $$HOME/.lingon/tls/ca.pem to Downloads when present (set AUTO_PUSH_CA_PEM=0 to disable)"
	@echo "  make avd-clean-snapshots - remove stale Quick Boot snapshot state for $(AVD_NAME)"
	@echo "  make build        - assemble debug APK"
	@echo "  make release      - assemble release APK (auto-generates a local dev keystore if needed)"
	@echo "  make install      - install debug APK"
	@echo "  make adb-install  - install release APK via adb (uses APK=... and ADB_SERIAL=... overrides)"
	@echo "  make adb-push     - push FILE to emulator Downloads (defaults to $$HOME/.lingon/tls/ca.pem)"
	@echo "  make integration-test - run Android integration tests (harness + emulator, PRESET=medium)"
	@echo "  make run          - launch the app on a device/emulator"
	@echo "  make ui-test      - run UI tests on a managed API 35 emulator"
	@echo "  make clean        - clean Gradle outputs"
	@echo "  make list-presets - list emulator presets"
	@echo "  make cleanup-managed-devices - stop stray Gradle managed device emulators"
	@echo "  make cleanup-offline-emulators - stop adb offline emulator devices"
	@echo ""
	@echo "Presets:"
	@echo "  make avd PRESET=small    - use small phone profile"
	@echo "  make avd PRESET=medium   - use medium phone profile"
	@echo "  make avd PRESET=pixel7   - use Pixel 7 profile"
	@echo "  make avd PRESET=pixel9   - use Pixel 9 profile"
	@echo ""
	@echo "Overrides:"
	@echo "  make avd AVD_NAME=... DEVICE_PROFILE=... SYSTEM_IMAGE=..."
	@echo "  make emulator PRESET=medium AVD_RAM_MB=2048   - lower emulator RAM when needed"
	@echo "  make adb-install ADB_SERIAL=<device-serial> APK=/path/to/app.apk"
	@echo "  make integration-test LINGON_IT_CPU_QUOTA_PERCENT=400 LINGON_IT_MEMORY_MAX=6G"
	@echo "                      (default: systemd cgroup, CPUQuota=min(half of online CPUs, 200%), MemoryMax=7G, fresh managed emulator)"
	@echo "                      set LINGON_IT_REUSE_DEVICE=1 only when deliberately using an existing device/emulator"

list-presets:
	@echo "Available PRESET values:"
	@echo "  small"
	@echo "  medium"
	@echo "  pixel7"
	@echo "  pixel9"
	@echo ""
	@echo "Example:"
	@echo "  make PRESET=medium emulator"

cleanup-managed-devices:
	@./scripts/cleanup-managed-devices.sh

cleanup-offline-emulators:
	@ADB=$(ADB) ./scripts/cleanup-offline-emulators.sh

sdk:
	ANDROID_SDK_ROOT=$(ANDROID_SDK_ROOT) ./scripts/install-android-sdk.sh

licenses:
	yes | $(SDKMANAGER) --sdk_root=$(ANDROID_SDK_ROOT) --licenses

local-properties:
	@printf "sdk.dir=%s\n" "$(ANDROID_SDK_ROOT)" > local.properties

avd:
	@if [ -d "$(HOME)/.android/avd/$(AVD_NAME).avd" ]; then \
		echo "AVD $(AVD_NAME) already exists."; \
	else \
		mkdir -p "$(HOME)/.android/avd"; \
		echo "no" | $(AVDMANAGER) create avd -n "$(AVD_NAME)" -k "$(SYSTEM_IMAGE)" -d "$(DEVICE_PROFILE)"; \
	fi
	@AVD_NAME=$(AVD_NAME) AVD_RAM_MB="$${AVD_RAM_MB:-}" ./scripts/configure-avd.sh

avd-clean-snapshots:
	@AVD_NAME=$(AVD_NAME) ./scripts/cleanup-avd-snapshots.sh

emulator:
	@./scripts/cleanup-managed-devices.sh
	@ADB=$(ADB) ./scripts/cleanup-offline-emulators.sh
	@$(ADB) kill-server >/dev/null 2>&1 || true
	@$(ADB) start-server >/dev/null 2>&1 || true
	@./scripts/configure-emulator-host.sh
	@AVD_NAME=$(AVD_NAME) AVD_RAM_MB="$${AVD_RAM_MB:-}" ./scripts/configure-avd.sh
	@AVD_NAME=$(AVD_NAME) ./scripts/cleanup-avd-snapshots.sh
	@EMU_PORT="$${EMU_PORT:-$$(ADB=$(ADB) ./scripts/find-emu-port.sh)}"; \
	echo "Starting emulator $(AVD_NAME) on port $${EMU_PORT}"; \
	ADB=$(ADB) ADB_SERIAL="emulator-$${EMU_PORT}" ./scripts/ensure-emu-keyboard.sh & \
	$(EMULATOR) -avd $(AVD_NAME) -port "$${EMU_PORT}" $(EMULATOR_FLAGS)

build: local-properties
	@VERSION="$$($(VERSION_CMD))"; \
	sed -i.bak -E "s/^([[:space:]]*versionName[[:space:]]*=[[:space:]]*\").*(\".*)/\\1$${VERSION}\\2/" app/build.gradle.kts; \
	rm -f app/build.gradle.kts.bak; \
	ANDROID_SDK_ROOT=$(ANDROID_SDK_ROOT) ANDROID_HOME=$(ANDROID_HOME) ./gradlew :app:assembleDebug
	@APK_PATH="$(CURDIR)/app/build/outputs/apk/debug/app-debug.apk"; \
	if [ -f "$$APK_PATH" ]; then \
		echo "APK built: $$APK_PATH"; \
	else \
		echo "Error: APK not found at $$APK_PATH" >&2; \
		exit 1; \
	fi

release: local-properties
	@VERSION="$$($(VERSION_CMD))"; \
	sed -i.bak -E "s/^([[:space:]]*versionName[[:space:]]*=[[:space:]]*\").*(\".*)/\\1$${VERSION}\\2/" app/build.gradle.kts; \
	rm -f app/build.gradle.kts.bak
	@./scripts/generate-release-keystore.sh
	ANDROID_SDK_ROOT=$(ANDROID_SDK_ROOT) ANDROID_HOME=$(ANDROID_HOME) ./gradlew :app:assembleRelease
	@APK_PATH="$(CURDIR)/app/build/outputs/apk/release/app-release.apk"; \
	if [ -f "$$APK_PATH" ]; then \
		echo "APK built: $$APK_PATH"; \
	else \
		echo "Error: APK not found at $$APK_PATH" >&2; \
		exit 1; \
	fi; \
	APK_SIGNER="$$(ls $(ANDROID_SDK_ROOT)/build-tools/*/apksigner 2>/dev/null | sort -V | tail -n 1)"; \
	if [ -z "$$APK_SIGNER" ]; then \
		echo "Error: apksigner not found under $(ANDROID_SDK_ROOT)/build-tools. Cannot verify release signer." >&2; \
		exit 1; \
	fi; \
	APKSIGNER_BIN="$$APK_SIGNER" EXPECTED_CERT_PATH="$(CURDIR)/lingon-release-cert.pem" ./scripts/verify-release-cert.sh "$$APK_PATH"

install: local-properties
	ANDROID_SDK_ROOT=$(ANDROID_SDK_ROOT) ANDROID_HOME=$(ANDROID_HOME) ./gradlew :app:installDebug

adb-install:
	@set -e; \
	ADB_BIN="$(ADB)"; \
	if [ ! -x "$$ADB_BIN" ]; then \
		if command -v adb >/dev/null 2>&1; then \
			ADB_BIN="$$(command -v adb)"; \
		else \
			echo "Error: adb not found. Set ANDROID_SDK_ROOT/ANDROID_HOME or install platform-tools." >&2; \
			exit 1; \
		fi; \
	fi; \
	APK_PATH="$${APK:-$(CURDIR)/app/build/outputs/apk/release/app-release.apk}"; \
	if [ ! -f "$$APK_PATH" ]; then \
		echo "Error: APK not found at $$APK_PATH. Run 'make release' first." >&2; \
		exit 1; \
	fi; \
	echo "Checking connected devices..."; \
	DEVICE_TABLE="$$( "$$ADB_BIN" devices -l | awk 'NR>1 && $$1 != "" {print}' )"; \
	if [ -n "$$DEVICE_TABLE" ]; then \
		echo "Available adb devices:"; \
		printf '%s\n' "$$DEVICE_TABLE" | sed 's/^/  - /'; \
	else \
		echo "Available adb devices: (none)"; \
	fi; \
	DEVICES="$$( printf '%s\n' "$$DEVICE_TABLE" | awk '$$2 == "device" {print $$1}' )"; \
	if [ -z "$$DEVICES" ]; then \
		echo "Error: no devices in 'device' state. Check 'adb devices'." >&2; \
		exit 1; \
	fi; \
	TARGET_SERIAL="$${ADB_SERIAL:-}"; \
	if [ -z "$$TARGET_SERIAL" ]; then \
		DEVICE_COUNT="$$(printf '%s\n' "$$DEVICES" | sed '/^$$/d' | wc -l | tr -d ' ')"; \
		if [ "$$DEVICE_COUNT" -gt 1 ]; then \
			echo "Error: multiple devices detected. Set ADB_SERIAL=<device-serial>." >&2; \
			echo "Example: make adb-install ADB_SERIAL=$$(printf '%s\n' "$$DEVICES" | head -n 1)" >&2; \
			exit 1; \
		fi; \
		TARGET_SERIAL="$$(printf '%s\n' "$$DEVICES" | head -n 1)"; \
	fi; \
	if ! printf '%s\n' "$$DEVICES" | grep -Fx "$$TARGET_SERIAL" >/dev/null 2>&1; then \
		echo "Error: ADB_SERIAL '$$TARGET_SERIAL' is not in 'device' state." >&2; \
		exit 1; \
	fi; \
	echo "Installing $$APK_PATH to $$TARGET_SERIAL"; \
	"$$ADB_BIN" -s "$$TARGET_SERIAL" install -r "$$APK_PATH"

adb-push:
	@set -e; \
	ADB_BIN="$(ADB)"; \
	if [ ! -x "$$ADB_BIN" ]; then \
		if command -v adb >/dev/null 2>&1; then \
			ADB_BIN="$$(command -v adb)"; \
		else \
			echo "Error: adb not found. Set ANDROID_SDK_ROOT/ANDROID_HOME or install platform-tools." >&2; \
			exit 1; \
		fi; \
	fi; \
	SRC_FILE="$${FILE:-$${HOME}/.lingon/tls/ca.pem}"; \
	if [ ! -f "$$SRC_FILE" ]; then \
		echo "Error: file not found: $$SRC_FILE" >&2; \
		echo "Tip: run 'make adb-push FILE=/path/to/ca.pem'" >&2; \
		exit 1; \
	fi; \
	DEST_NAME="$$(basename "$$SRC_FILE")"; \
	echo "Pushing $$SRC_FILE to /sdcard/Download/$$DEST_NAME"; \
	"$$ADB_BIN" push "$$SRC_FILE" "/sdcard/Download/$$DEST_NAME"

integration-test:
	@./scripts/run-integration-tests.sh

run: install
	@ADB=$(ADB) ./scripts/ensure-emu-keyboard.sh
	$(ADB) shell am start -n $(APP_ID)/.MainActivity

ui-test: local-properties
	@set -e; \
	trap './scripts/cleanup-managed-devices.sh' EXIT; \
	ANDROID_SDK_ROOT=$(ANDROID_SDK_ROOT) ANDROID_HOME=$(ANDROID_HOME) ./gradlew $(UI_TEST_FLAGS) :app:phoneApi35DebugAndroidTest

clean: local-properties
	ANDROID_SDK_ROOT=$(ANDROID_SDK_ROOT) ANDROID_HOME=$(ANDROID_HOME) ./gradlew clean
