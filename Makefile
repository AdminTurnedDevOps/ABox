MODULE := github.com/AdminTurnedDevOps/ABox
BIN := bin
ENTITLEMENTS := assets/entitlements.plist
IMAGE ?= $(HOME)/.abox/images/abox-guest.raw

.PHONY: all build guest vmm abox image test fmt tidy sign

all: build

build: abox vmm guest

abox:
	mkdir -p $(BIN)
	go build -o $(BIN)/abox ./cmd/abox

vmm:
	mkdir -p $(BIN)
	go build -o $(BIN)/abox-vmm ./cmd/abox-vmm
	$(MAKE) sign

guest:
	mkdir -p $(BIN)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(BIN)/abox-guest-linux-arm64 ./cmd/abox-guest

sign:
	codesign --entitlements $(ENTITLEMENTS) --force -s - $(BIN)/abox-vmm

image: guest
	ABOX_IMAGE=$(IMAGE) sh images/build-guest.sh

image-update: guest
	ABOX_IMAGE=$(IMAGE) sh images/update-guest-bin.sh

test:
	go test ./protocol ./internal/...

fmt:
	gofmt -w ./cmd ./internal ./protocol

tidy:
	go mod tidy
