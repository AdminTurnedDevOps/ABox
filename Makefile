MODULE := github.com/AdminTurnedDevOps/ABox
BIN := bin
ENTITLEMENTS := assets/entitlements.plist
PLATFORM ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)
GUEST_ARCH ?= $(ARCH)
IMAGE_ID ?= abox-guest-dev
CGO_ENABLED ?= $(shell go env CGO_ENABLED)
GUEST_BIN := $(BIN)/abox-guest-linux-$(GUEST_ARCH)
IMAGE ?= $(HOME)/.abox/images/abox-guest-linux-$(GUEST_ARCH).raw

.PHONY: all build guest vmm vmm-preflight abox image image-update test test-freeze-tags fmt tidy sign

all: build

build: abox vmm guest

abox:
	mkdir -p $(BIN)
	go build -o $(BIN)/abox ./cmd/abox

vmm:
	mkdir -p $(BIN)
	$(MAKE) vmm-preflight
	CGO_ENABLED=$(CGO_ENABLED) go build -o $(BIN)/abox-vmm ./cmd/abox-vmm
	$(if $(filter darwin,$(PLATFORM)),$(MAKE) sign,)

vmm-preflight:
	@if [ "$(PLATFORM)" = linux ] && [ "$(CGO_ENABLED)" = 0 ]; then \
		echo "CGO_ENABLED=0: building the diagnostic abox-vmm stub; it cannot start a VM" >&2; \
	elif [ "$(PLATFORM)" = linux ]; then \
		if ! command -v pkg-config >/dev/null 2>&1 || \
		   ! pkg-config --atleast-version=1.19.0 libkrun >/dev/null 2>&1 || \
		   ! pkg-config --max-version=1.19.99 libkrun >/dev/null 2>&1; then \
			echo "Linux VMM builds require libkrun 1.19.x and its pkg-config metadata." >&2; \
			echo "Install pkgconf + libkrun (Arch), pkgconf-pkg-config + libkrun-devel (Fedora)," >&2; \
			echo "or set PKG_CONFIG_PATH for a pinned source install (often /usr/local/lib64/pkgconfig)." >&2; \
			exit 1; \
		fi; \
	fi

guest:
	@if [ "$(GUEST_ARCH)" != amd64 ] && [ "$(GUEST_ARCH)" != arm64 ]; then \
		echo "unsupported GUEST_ARCH=$(GUEST_ARCH); expected amd64 or arm64" >&2; \
		exit 1; \
	fi
	@case "$(IMAGE_ID)" in ''|*[!A-Za-z0-9._-]*) echo "invalid IMAGE_ID=$(IMAGE_ID)" >&2; exit 1;; esac
	mkdir -p $(BIN)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GUEST_ARCH) go build -tags abox_guest -ldflags "-X main.imageID=$(IMAGE_ID)" -o $(GUEST_BIN) ./cmd/abox-guest

sign:
	@if [ "$(PLATFORM)" = darwin ]; then \
		codesign --entitlements $(ENTITLEMENTS) --force -s - $(BIN)/abox-vmm; \
	else \
		echo "codesign skipped on $(PLATFORM)"; \
	fi

image: guest
	ABOX_GUEST_ARCH=$(GUEST_ARCH) ABOX_IMAGE=$(IMAGE) ABOX_IMAGE_ID=$(IMAGE_ID) sh images/build-guest.sh

image-update: guest
	ABOX_GUEST_ARCH=$(GUEST_ARCH) ABOX_IMAGE=$(IMAGE) ABOX_IMAGE_ID=$(IMAGE_ID) sh images/update-guest-bin.sh

test: test-freeze-tags
	go test ./protocol ./internal/... ./pkg/...

test-freeze-tags:
	@host_files=$$(go list -f '{{join .GoFiles " "}}' ./internal/guest/tools); \
	case " $$host_files " in *" freeze_stub.go "*) ;; *) echo "host tools build did not select freeze_stub.go" >&2; exit 1;; esac; \
	case " $$host_files " in *" freeze_linux.go "*) echo "host tools build selected freeze_linux.go" >&2; exit 1;; esac
	@guest_files=$$(CGO_ENABLED=0 GOOS=linux GOARCH=$(GUEST_ARCH) go list -tags abox_guest -f '{{join .GoFiles " "}}' ./internal/guest/tools); \
	case " $$guest_files " in *" freeze_linux.go "*) ;; *) echo "tagged guest build did not select freeze_linux.go" >&2; exit 1;; esac; \
	case " $$guest_files " in *" freeze_stub.go "*) echo "tagged guest build selected freeze_stub.go" >&2; exit 1;; esac

fmt:
	gofmt -w ./cmd ./internal ./protocol

tidy:
	go mod tidy
