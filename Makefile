GO      ?= go
ZIG     ?= zig
BIN     := bin
MISTER  ?= mister.local
DEVDIR  := /media/fat/mistersubsonic/dev
ARM_ENV := GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
           CC="$(ZIG) cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"

.PHONY: test vet mister-test deploy-dev vendor-check clean

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

# Audio test binary for on-device checks and benchmarks.
mister-test:
	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/audio.test ./internal/audio
	./scripts/check-glibc.sh $(BIN)/arm/audio.test

# Copies the audio test binary and fixtures to the MiSTer (default root password is "1").
deploy-dev: mister-test
	ssh root@$(MISTER) mkdir -p $(DEVDIR)/testdata
	scp $(BIN)/arm/audio.test root@$(MISTER):$(DEVDIR)/
	scp internal/audio/testdata/*.flac internal/audio/testdata/*.wav internal/audio/testdata/*.mp3 root@$(MISTER):$(DEVDIR)/testdata/

vendor-check:
	./third_party/fetch.sh

clean:
	rm -rf $(BIN)
