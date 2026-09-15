# USAGE — セットアップと動作検証ガイド

pvectl の開発環境構築から動作確認、ローカルへのバイナリインストールまでの手順。
**ビルド・テストはすべて Docker コンテナ内で実行する**（ローカル環境を汚さない）。

## 前提

- Docker（Docker Desktop / OrbStack など、`docker compose` が使えること）
- make
- （ローカルインストール検証のみ）PATH の通ったディレクトリ（例: `~/.local/bin`）

## 1. セットアップ

```bash
git clone https://github.com/cyokozai/pvectl.git
cd pvectl

# 開発コンテナを起動（golang:1.26-alpine + goimports + golangci-lint v2）
make dev-up
```

起動確認:

```bash
docker compose ps                       # pvectl-dev が Up
docker compose exec dev go version      # go1.26.x
```

ソースはバインドマウントされるため、ホストで編集 → コンテナに即反映される。
go mod / build キャッシュは名前付きボリュームに置かれ、ホストには何も残らない。

## 2. コンテナ内での動作確認

### 2-1. テストスイート（実クラスタ不要）

```bash
make check    # go vet + golangci-lint + go test -race ./...
```

テストは httptest 製のフェイク Proxmox VE API（`test/pvefake`）に対して走るため、
実クラスタなしで apply → diff → update → delete の e2e まで検証される。

### 2-2. バイナリのスモークテスト

```bash
make build                              # コンテナ内でビルド → ./pvectl (linux)
docker compose exec dev ./pvectl version
docker compose exec dev ./pvectl --help
```

### 2-3. クラスタ不要のコマンド確認

```bash
# 設定ファイルの表示（examples の設定で確認、シークレットは REDACTED）
docker compose exec dev ./pvectl config view --config examples/config.yaml
docker compose exec dev ./pvectl config get-contexts --config examples/config.yaml

# マニフェストのクライアント検証（API 非接続でバリデーションのみ）
docker compose exec dev ./pvectl apply -f examples/vm.yaml --dry-run=client
docker compose exec dev ./pvectl apply -f examples/multi.yaml -f examples/vm-clone.yaml --dry-run=client

# 注: `-f <ディレクトリ>` はディレクトリ内の全 *.yaml/*.yml/*.json を読む。
#     examples/ には pvectl 設定サンプル（config.yaml）が同居しているため、
#     ディレクトリ apply はマニフェスト専用ディレクトリに対して使うこと。
```

### 2-4. 実クラスタへの接続確認（任意）

`~/.pvectl/config` を用意し（形式は [examples/config.yaml](examples/config.yaml)）、
コンテナにマウントして読み取り系から確認する:

```bash
docker compose run --rm -v ~/.pvectl:/root/.pvectl:ro dev ./pvectl get vm
docker compose run --rm -v ~/.pvectl:/root/.pvectl:ro dev ./pvectl apply -f examples/vm.yaml --dry-run=server
docker compose run --rm -v ~/.pvectl:/root/.pvectl:ro dev ./pvectl diff -f examples/vm.yaml
```

書き込み系（`apply`／`delete`／`start`／`stop`）は dry-run と diff で内容を確認してから実行すること。

## 3. ローカルへのインストール

### 方法 A: コンテナでクロスビルドして配置（ホストに Go 不要・推奨）

```bash
# macOS (Apple Silicon) 向けバイナリをコンテナ内でビルド
docker compose exec dev sh -c 'GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 \
  go build -ldflags "-s -w -X github.com/cyokozai/pvectl/internal/cmd.Version=$(git describe --tags --always 2>/dev/null || echo dev)" \
  -o pvectl-darwin ./cmd/pvectl'

# PATH の通ったディレクトリへ配置
install -m 0755 pvectl-darwin ~/.local/bin/pvectl && rm pvectl-darwin
```

Intel Mac は `GOARCH=amd64`、Linux は `GOOS=linux`。

### 方法 B: ホストの Go でインストール

```bash
go install github.com/cyokozai/pvectl/cmd/pvectl@latest   # 公開後
# または手元のソースから: go install ./cmd/pvectl  (→ $GOPATH/bin)
```

### インストール確認（zsh）

```bash
which pvectl
pvectl version
pvectl config get-contexts        # ~/.pvectl/config を読む
pvectl get vm                     # 実クラスタに接続できる場合
```

### シェル補完（zsh）

```bash
mkdir -p ~/.zsh/completions
pvectl completion zsh > ~/.zsh/completions/_pvectl
# ~/.zshrc に（未設定なら）:
#   fpath=(~/.zsh/completions $fpath)
#   autoload -Uz compinit && compinit
exec zsh
pvectl <TAB>                      # サブコマンドが補完される
```

## 4. アンインストール / 後片付け

```bash
rm ~/.local/bin/pvectl            # ローカルバイナリ
make dev-down                     # コンテナ停止
docker compose down -v            # キャッシュボリュームも削除する場合
```

## トラブルシュート

| 症状 | 対処 |
|---|---|
| `service "dev" is not running` | `make dev-up` を再実行 |
| `no current context set` | `pvectl config use-context <name>` または `--context` を指定 |
| `context "x" not found (available: ...)` | `pvectl config get-contexts` で名前を確認 |
| 実クラスタに繋がらない | `server:` の URL / ネットワーク到達性（VPN 等）を確認。自己署名証明書は `insecureSkipTLSVerify: true` |
| タスクが終わらない | `--timeout`（既定 5m）を延長。Proxmox 側のタスクログを確認 |
| `zsh: command not found: pvectl` | 配置先が PATH に含まれるか確認（`echo $PATH`）、`hash -r` で再スキャン |
