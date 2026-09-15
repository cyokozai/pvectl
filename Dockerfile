# ===== Development =====
FROM golang:1.26-alpine AS dev

RUN apk add --no-cache git curl bash build-base && \
    go install golang.org/x/tools/cmd/goimports@latest && \
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b /go/bin v2.12.2

WORKDIR /workspace

CMD ["sleep", "infinity"]

# ===== Build =====
FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG VERSION=dev
ARG GIT_COMMIT=none
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w \
      -X github.com/cyokozai/pvectl/internal/cmd.Version=${VERSION} \
      -X github.com/cyokozai/pvectl/internal/cmd.GitCommit=${GIT_COMMIT} \
      -X github.com/cyokozai/pvectl/internal/cmd.BuildDate=${BUILD_DATE}" \
    -o /out/pvectl ./cmd/pvectl

# ===== Runtime =====
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/pvectl /usr/local/bin/pvectl

ENTRYPOINT ["pvectl"]
