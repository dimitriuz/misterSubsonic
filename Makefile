GO      ?= go
ZIG     ?= zig
BIN     := bin
MISTER  ?= mister.local
DEVDIR  := /media/fat/mistersubsonic/dev
REL     := $(BIN)/release
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
ARM_ENV := GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
           CC="$(ZIG) cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"


.PHONY: build test launcher-test vet e2e viewer mister mister-test release db deploy deploy-dev vendor-check clean

build:
	$(GO) build -o $(BIN)/ ./cmd/... ./tools/...

test: launcher-test
	$(GO) test -race ./...

# The Scripts-menu launcher, against stand-ins for BGM, SAM and the app.
launcher-test:
	./scripts/test-launcher.sh

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

# The release: the SD card tree (Scripts/ and mistersubsonic/) in bin/release/sdcard,
# zipped as bin/release/MiSTer_Subsonic-$(VERSION).zip.
release:
	rm -rf $(REL)
	mkdir -p $(REL)/sdcard/Scripts $(REL)/sdcard/mistersubsonic
	$(ARM_ENV) $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
		-o $(REL)/sdcard/mistersubsonic/mistersubsonic ./cmd/mistersubsonic
	./scripts/check-glibc.sh $(REL)/sdcard/mistersubsonic/mistersubsonic
	cp sdcard/Scripts/MiSTer_Subsonic.sh $(REL)/sdcard/Scripts/
	cp sdcard/mistersubsonic/config.example.toml LICENSE $(REL)/sdcard/mistersubsonic/
	{ echo "MiSTer Subsonic includes this third-party software."; \
	  echo; echo "== speexdsp resampler (BSD) =="; cat third_party/speexdsp/COPYING; \
	  echo; echo "== Noto Sans fonts (SIL Open Font License 1.1) =="; cat internal/gfx/fonts/OFL.txt; \
	  echo; echo "== miniaudio (public domain or MIT No Attribution): https://miniaud.io =="; \
	  echo; echo "== Mozilla CA certificate bundle (MPL-2.0): https://curl.se/docs/caextract.html =="; \
	} > $(REL)/sdcard/mistersubsonic/THIRD_PARTY.txt
	cd $(REL)/sdcard && zip -qrX ../MiSTer_Subsonic-$(VERSION).zip Scripts mistersubsonic

# The MiSTer Downloader database for a release whose files are downloaded from
# BASE_URL (the GitHub release's assets): bin/release/db.json.
db:
	$(GO) run ./tools/mkdb -dir $(REL)/sdcard -base-url $(BASE_URL) -o $(REL)/db.json

# Installs the release on the MiSTer over ssh (default root password "1").
deploy: release
	ssh root@$(MISTER) mkdir -p /media/fat/mistersubsonic
	scp $(REL)/sdcard/Scripts/MiSTer_Subsonic.sh root@$(MISTER):/media/fat/Scripts/
	scp $(REL)/sdcard/mistersubsonic/* root@$(MISTER):/media/fat/mistersubsonic/

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
