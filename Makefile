GO      ?= go
ZIG     ?= zig
BIN     := bin
MISTER  ?= mister.local
DEVDIR  := /media/fat/mistersubsonic/dev
ARM_ENV := GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
           CC="$(ZIG) cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"


.PHONY: build test vet e2e viewer mister mister-test deploy-dev vendor-check clean

build:
	$(GO) build -o $(BIN)/ ./cmd/... ./tools/...

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

# Silent end-to-end run (mock server + CLI on the null audio device).
e2e:
	./scripts/e2e-smoke.sh
	./scripts/e2e-ui.sh

# Run the app on this PC in the browser viewer (starts at -30 dB).
viewer:
	$(GO) run ./cmd/mistersubsonic -config $(or $(CONFIG),config.toml) -display viewer

# Cross-compiled binaries for the MiSTer (ARMv7, glibc <= 2.31).
mister:
	$(ARM_ENV) $(GO) build -trimpath -ldflags "-s -w" -o $(BIN)/arm/mss-cli ./cmd/mss-cli
	$(ARM_ENV) $(GO) build -trimpath -ldflags "-s -w" -o $(BIN)/arm/mistersubsonic ./cmd/mistersubsonic
	./scripts/check-glibc.sh $(BIN)/arm/mss-cli
	./scripts/check-glibc.sh $(BIN)/arm/mistersubsonic

# Audio test binary for on-device checks and benchmarks.
mister-test:
	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/audio.test ./internal/audio
	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/ui.test ./internal/ui
	./scripts/check-glibc.sh $(BIN)/arm/audio.test
	./scripts/check-glibc.sh $(BIN)/arm/ui.test

# Copies the dev tools to the MiSTer (default root password is "1").
deploy-dev: mister mister-test
	ssh root@$(MISTER) mkdir -p $(DEVDIR)/testdata
	scp $(BIN)/arm/mss-cli $(BIN)/arm/mistersubsonic $(BIN)/arm/audio.test $(BIN)/arm/ui.test root@$(MISTER):$(DEVDIR)/
	scp internal/audio/testdata/*.flac internal/audio/testdata/*.wav internal/audio/testdata/*.mp3 root@$(MISTER):$(DEVDIR)/testdata/

vendor-check:
	./third_party/fetch.sh

clean:
	rm -rf $(BIN)
