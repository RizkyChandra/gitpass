# gitpass — task runner
#
#   just            list recipes
#   just test       run the suite
#   just tui        build and open the TUI against a scratch vault

set shell := ["bash", "-uc"]

ndk_version := "30.0.15729638"
android_api := "24"
demo_dir := justfile_directory() / ".demo"

# List available recipes.
default:
    @just --list --unsorted

# Build the CLI into ./gitpass.
build:
    go build -o gitpass ./cmd/gitpass

# Run the whole test suite.
test:
    go test ./...

# Run tests with the race detector and coverage.
test-full:
    go test -race -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out | tail -1

# Format, vet, and verify the module is tidy.
check: fmt
    go vet ./...
    go mod tidy -diff

# Format all Go source.
fmt:
    gofmt -w .

# Install gitpass into $GOBIN (or ~/go/bin).
install:
    go install ./cmd/gitpass

# Open the TUI against your real vault.
tui: build
    ./gitpass

# Create a throwaway vault in .demo and open the TUI on it. Safe to delete.
demo: build
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f "{{ demo_dir }}/identity.age" ]; then
      mkdir -p "{{ demo_dir }}"
      printf 'demo-passphrase-not-secret\ndemo-passphrase-not-secret\n' \
        | GITPASS_DIR="{{ demo_dir }}" ./gitpass init
      GITPASS_DIR="{{ demo_dir }}" GITPASS_PASSPHRASE=demo-passphrase-not-secret ./gitpass add <<'JSON'
    [{"name":"github.com","username":"alice","password":"hunter2",
      "totp":"otpauth://totp/GitHub:alice?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=GitHub",
      "tags":["work"]},
     {"name":"bank.example","email":"a@b.c","password":"s3cret","notes":"account 12345"}]
    JSON
    fi
    GITPASS_DIR="{{ demo_dir }}" GITPASS_PASSPHRASE=demo-passphrase-not-secret ./gitpass

# Build the Go core as an Android .aar into the app. Needs the SDK, NDK and a JDK.
aar:
    #!/usr/bin/env bash
    set -euo pipefail
    export ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
    export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/{{ ndk_version }}}"
    command -v javac >/dev/null || { echo "javac not found — install a JDK (gomobile shells out to it)"; exit 1; }
    command -v gomobile >/dev/null || go install golang.org/x/mobile/cmd/gomobile@latest
    command -v gobind >/dev/null || go install golang.org/x/mobile/cmd/gobind@latest
    # -androidapi 24: gomobile rejects its own default of 16. The two ABIs here
    # must match the abiFilters in android/app/build.gradle.kts.
    gomobile bind -target=android/arm64,android/amd64 -androidapi {{ android_api }} \
        -o android/app/libs/gitpass.aar ./mobile
    echo "wrote android/app/libs/gitpass.aar"

# Build the Android app (debug APK) and run its unit tests.
apk: aar
    cd android && ./gradlew assembleDebug testDebugUnitTest

# Run the Android instrumented tests. Needs a booted emulator or attached device.
android-test: aar
    cd android && ./gradlew connectedDebugAndroidTest

# Install the debug APK on the attached device.
android-install: apk
    cd android && ./gradlew installDebug

# Check the goreleaser config and build a local snapshot into ./dist.
release-snapshot:
    goreleaser release --snapshot --clean

# Remove build artifacts and the demo vault.
clean:
    rm -rf gitpass gitpass.aar gitpass-sources.jar dist coverage.out "{{ demo_dir }}" android/app/libs/gitpass.aar android/app/build android/build
