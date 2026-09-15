VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/cyokozai/pvectl/internal/cmd.Version=$(VERSION) \
	-X github.com/cyokozai/pvectl/internal/cmd.GitCommit=$(GIT_COMMIT) \
	-X github.com/cyokozai/pvectl/internal/cmd.BuildDate=$(BUILD_DATE)

# All targets run inside the dev container (compose service "dev") so
# the host stays clean. Use HOST=1 to run on the host toolchain instead.
ifdef HOST
RUN :=
else
RUN := docker compose exec dev
endif

.PHONY: build test race lint fmt vet check clean dev-up dev-down

build:
	$(RUN) go build -ldflags "$(LDFLAGS)" -o pvectl ./cmd/pvectl

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

dev-up:
	docker compose up -d --build

dev-down:
	docker compose down
