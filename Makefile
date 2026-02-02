# Makefile for itch-setup
# Matches the build process from release/build.js

# Configuration
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Version info
VERSION ?= head
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo "no-commit")
BUILT_AT ?= $(shell date +%s)

# Output binary name
ifeq ($(GOOS),windows)
	BINARY = itch-setup.exe
else
	BINARY = itch-setup
endif

# Base ldflags
LDFLAGS = -X main.version=$(VERSION) -X main.builtAt=$(BUILT_AT) -X main.commit=$(COMMIT) -w -s

# Windows-specific ldflags
ifeq ($(GOOS),windows)
	LDFLAGS += -H windowsgui -extldflags=-static
endif

# Linux-specific build tags (GTK compatibility)
ifeq ($(GOOS),linux)
	GO_TAGS = -tags "pango_1_42 gtk_3_22 glib_2_58 gdk_pixbuf_2_38"
endif

# macOS-specific CGO flags
ifeq ($(GOOS),darwin)
	ifeq ($(GOARCH),arm64)
		export CGO_CFLAGS = -mmacosx-version-min=11.0
		export CGO_LDFLAGS = -mmacosx-version-min=11.0
	else
		export CGO_CFLAGS = -mmacosx-version-min=10.10
		export CGO_LDFLAGS = -mmacosx-version-min=10.10
	endif
endif

.PHONY: build clean

build: export CGO_ENABLED = 1
build:
ifeq ($(GOOS),windows)
	windres -o itch-setup.syso itch-setup.rc
endif
	go build -a -ldflags "$(LDFLAGS)" $(GO_TAGS) -o $(BINARY)
	@echo "Built $(BINARY)"

clean:
	rm -f itch-setup itch-setup.exe itch-setup.syso
