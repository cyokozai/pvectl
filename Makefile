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

# help の色付け。NO_COLOR が設定されていれば無効化する（https://no-color.org/）
ifeq ($(origin NO_COLOR), undefined)
HELP_COLOR := \033[36m
HELP_RESET := \033[0m
else
HELP_COLOR :=
HELP_RESET :=
endif

# 引数なしの `make` はヘルプを出す。何ができるか分からないことが
# make の最大の弱点なので、既定動作をその解消に充てる。
.DEFAULT_GOAL := help

.PHONY: help build test race coverage lint fmt vet check clean dev-up dev-down require-dev

# ターゲット名はハードコードせず、`## 説明` が付いた行を走査して一覧にする。
# 新しいターゲットは `## 説明` を書けば自動で載る（書き忘れると載らない）。
# `##@ 見出し` の行はセクション見出しになる。
help: ## このヘルプを表示する
	@printf 'pvectl — 開発用 make ターゲット\n'
	@printf '\n使い方: make <target> [HOST=1]\n\n'
	@awk 'BEGIN {FS = ":.*?## "} \
		/^##@/ {printf "\n%s\n", substr($$0, 5); next} \
		/^[a-zA-Z0-9_-]+:.*?## / {printf "  $(HELP_COLOR)%-16s$(HELP_RESET) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf '\n変数:\n'
	@printf '  %-16s %s\n' 'HOST=1'   '開発コンテナを使わず、ホストのツールチェーンで実行する'
	@printf '  %-16s %s\n' 'VERSION'  '埋め込むバージョン (既定: git describe / $(VERSION))'
	@printf '  %-16s %s\n' 'NO_COLOR' 'このヘルプの色付けを無効化する'

##@ ビルド

build: require-dev ## pvectl をリポジトリ直下にビルドする
	$(RUN) go build -ldflags "$(LDFLAGS)" -o pvectl ./cmd/pvectl

##@ テスト・検査

test: require-dev ## テストを実行する
	$(RUN) go test ./...

race: require-dev ## データ競合検出付きでテストを実行する
	$(RUN) env CGO_ENABLED=1 go test -race ./...

# CI (.github/workflows/ci.yaml) の test ジョブと同じ計測を手元で再現する。
# 数字が食い違うと CI のカバレッジ低下を手元で追えないため、オプションを揃える。
coverage: require-dev ## カバレッジを取得して合計を表示する (coverage.out)
	$(RUN) env CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
	$(RUN) go tool cover -func=coverage.out | tail -1

lint: require-dev ## golangci-lint を実行する
	$(RUN) golangci-lint run ./...

fmt: require-dev ## gofmt でソースを整形する
	$(RUN) gofmt -w cmd/ internal/ test/

vet: require-dev ## go vet を実行する
	$(RUN) go vet ./...

check: vet lint race ## vet / lint / race をまとめて実行する

##@ 後片付け

clean: ## ビルド生成物を削除する
	rm -f pvectl coverage.out

##@ 開発コンテナ

dev-up: ## 開発コンテナを起動する（イメージのビルドを含む）
	docker compose up -d --build

dev-down: ## 開発コンテナを停止・破棄する
	docker compose down

# 開発コンテナの存在を事前に確認する。素の `docker compose exec` は
# `service "dev" is not running` という原因の分かりにくいエラーを出すため、
# ここで次に打つべきコマンドまで案内する。HOST=1 のときは何もしない。
require-dev:
ifndef HOST
	@command -v docker >/dev/null 2>&1 || { \
		printf 'error: docker が見つかりません。\n' >&2; \
		printf '  Docker を導入するか、ホストのツールチェーンで実行してください: make $(MAKECMDGOALS) HOST=1\n' >&2; \
		exit 1; }
	@docker compose ps --services --status running 2>/dev/null | grep -qx dev || { \
		printf 'error: 開発コンテナ (compose service "dev") が起動していません。\n' >&2; \
		printf '  先に `make dev-up` を実行してください。\n' >&2; \
		printf '  ホストのツールチェーンで実行する場合は: make $(MAKECMDGOALS) HOST=1\n' >&2; \
		exit 1; }
endif
