GO       ?= go
PREFIX   ?= $(HOME)/.local
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -X main.version=$(VERSION)

JAVA_HOME_17 ?= /opt/homebrew/opt/openjdk@17
ANDROID_SDK  ?= $(HOME)/Android/sdk
APK          := android/app/build/outputs/apk/release/app-release.apk
GHOSTALERT_HOME ?= $(HOME)/.config/ghostalert

.PHONY: all build test vet install apk install-apk clean

all: build

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o ghostalert ./cmd/ghostalert

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# Installs the daemon/CLI and the hook helper.
install: build
	install -d $(PREFIX)/bin
	install -m 0755 ghostalert $(PREFIX)/bin/ghostalert
	install -m 0755 hooks/agents/ghostalert-hook $(PREFIX)/bin/ghostalert-hook
	@echo "installed to $(PREFIX)/bin"

apk:
	@echo "sdk.dir=$(ANDROID_SDK)" > android/local.properties
	cd android && JAVA_HOME=$(JAVA_HOME_17) ANDROID_HOME=$(ANDROID_SDK) ./gradlew assembleRelease
	@mkdir -p $(GHOSTALERT_HOME)
	@cp $(APK) $(GHOSTALERT_HOME)/app.apk
	@echo "built $(APK), served at /app.apk"

install-apk: apk
	adb install -r $(APK)

clean:
	rm -f ghostalert
	rm -rf android/app/build android/build android/.gradle
