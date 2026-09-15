VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/cyokozai/pvectl/internal/cmd.Version=$(VERSION) \
	-X github.com/cyokozai/pvectl/internal/cmd.GitCommit=$(GIT_COMMIT) \
	-X github.com/cyokozai/pvectl/internal/cmd.BuildDate=$(BUILD_DATE)

# Targets that the release artifacts and CI cross-build must both cover.
PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
DIST      ?= dist

# All targets run inside the dev container (compose service "dev") so
# the host stays clean. Use HOST=1 to run on the host toolchain instead.
ifdef HOST
RUN :=
else
RUN := docker compose exec dev
endif

.PHONY: build cross-build test race lint fmt vet check clean dev-up dev-down

build:
	$(RUN) go build -ldflags "$(LDFLAGS)" -o pvectl ./cmd/pvectl

# Cross-compile every supported platform with the same LDFLAGS as `build`.
# CGO is off so a single runner can produce all of them.
cross-build:
	@set -e; \
	for platform in $(PLATFORMS); do \
		goos=$${platform%/*}; goarch=$${platform#*/}; \
		echo "==> $$goos/$$goarch"; \
		$(RUN) env CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch \
			go build -ldflags "$(LDFLAGS)" \
			-o $(DIST)/pvectl_$${goos}_$${goarch} ./cmd/pvectl; \
	done

test:
	$(RUN) go test ./...

race:
	$(RUN) env CGO_ENABLED=1 go test -race ./...

lint:
	$(RUN) golangci-lint run ./...

fmt:
	$(RUN) gofmt -w cmd/ internal/ test/

vet:
	$(RUN) go vet ./...

check: vet lint race

clean:
	rm -f pvectl coverage.out
	rm -rf $(DIST)

dev-up:
	docker compose up -d --build

dev-down:
	docker compose down
