# クイックスタート

[English](README.md)

このページを上から順に読めば、pvectl のインストール、コンテキストの設定、
マニフェストからの最初の VM 作成までが終わる。

| | |
|---|---|
| [manifests.jp.md](manifests.jp.md) | マニフェストのフィールド、`apply` の挙動、秘密情報、エスケープハッチ |
| [development.md](development.md) | 開発・テスト・インストール（コンテナ完結） |

## 1. インストール

```bash
go install github.com/cyokozai/pvectl/cmd/pvectl@latest
```

ビルド済みバイナリはまだ配布していない。goreleaser は M2 の検討項目である
（[dev-process.md §6](../dev-process.md)）。ホストに Go ツールチェインを入れずに
バイナリを得るには、dev コンテナ内でクロスビルドする — 手順の全文は
[development.md](development.md) にある。

```bash
make dev-up
docker compose exec dev sh -c 'GOOS=darwin GOARCH=arm64 go build -o pvectl-darwin ./cmd/pvectl'
install -m 0755 pvectl-darwin ~/.local/bin/pvectl && rm pvectl-darwin
```

[Dockerfile](../../Dockerfile) は、pvectl 自身を entrypoint とする distroless
イメージもビルドする。

```bash
docker build -t pvectl .
docker run --rm -v ~/.pvectl/config:/config:ro pvectl --config /config get vm
```

インストールの確認は `pvectl version`。シェル補完スクリプトは
`pvectl completion bash|zsh|fish|powershell` で生成する
（zsh への組み込みは [development.md](development.md) を参照）。

## 2. コンテキストを設定する

pvectl は `~/.pvectl/config` を読む（`--config` または `PVECTL_CONFIG` で上書き
可能）。ファイルは kubeconfig と同じ形で、`users` が認証情報、`nodes` が接続先を
持ち、`context` がその 1 組を対にする。

```yaml
apiVersion: v1
kind: Config
users:
  - name: admin@pam
    user:
      token: admin@pam!pvectl=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
nodes:
  - name: prod-pve
    node:
      server: https://pve.example.com:8006
      # insecureSkipTLSVerify: true    # 自己署名証明書の場合
contexts:
  - name: production
    context:
      user: admin@pam
      node: prod-pve
current-context: production
```

各ユーザーは `token`（`user@realm!tokenid=secret`）**または** `username` +
`password` のどちらかを持つ — 両者はちょうど一方のみ。
[examples/config.yaml](../../examples/config.yaml) に両方の形と 2 つのコンテキストが
載っている。pvectl はこのファイルをモード 0600 で書き、`config view` は出力前に
すべての秘密情報を伏せる。

```bash
pvectl config view                    # 秘密情報を REDACTED にした設定
pvectl config get-contexts            # 全コンテキスト（現在のものに印）
pvectl config current-context
pvectl config use-context development # 既定を切り替える
pvectl get vm --context production    # または 1 コマンドだけ上書きする
```

## 3. 最初の VM を apply する

```yaml
# vm.yaml
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server
spec:
  targetNode: pve-node1
  resources:
    cpu:
      cores: 2
    memory: 2048
  disks:
    - name: scsi0
      size: 32G
      storage: local-lvm
  networks:
    - name: net0
      bridge: vmbr0
```

```bash
pvectl apply -f vm.yaml --dry-run=client   # マニフェストを検証。API 呼び出しゼロ
pvectl apply -f vm.yaml --dry-run=server   # ライブ状態を読み、行われる操作を報告
pvectl apply -f vm.yaml                    # 収束させる
pvectl get vm web-server
pvectl describe vm web-server
```

`apply` は冪等である。もう一度実行すると、何も書き込まずに `unchanged` を報告
する。何を比較するのか、どのフィールドが immutable なのか、`spec.runStrategy` が
電源状態をどう制御するのかは [manifests.jp.md](manifests.jp.md) にある。

## 4. 動詞

| 動詞 | |
|---|---|
| `get TYPE [NAME...]` | リソースの一覧または表示 |
| `describe TYPE NAME` | ライブ状態の詳細 |
| `apply -f FILE` | マニフェストから作成または更新（冪等） |
| `diff -f FILE` | 宣言キーの差分。exit 0 差分なし、1 差分あり、>1 エラー |
| `delete TYPE NAME` / `delete -f FILE` | 名前またはマニフェストで削除 |
| `start TYPE NAME` / `stop TYPE NAME` | ゲストの電源を入れる／切る |
| `exec TYPE NAME -- CMD` | QEMU guest agent 経由でゲスト内でコマンドを実行 |
| `migrate TYPE NAME --to NODE` | ゲストを別ノードへ移動 |
| `config SUBCOMMAND` | `view` / `get-contexts` / `current-context` / `use-context` |
| `completion SHELL` | `bash` / `zsh` / `fish` / `powershell` |
| `version` | バージョン、コミット、ビルド日時 |

`TYPE` は kind かその別名である。`vm`、`vms`、`virtualmachine`、
`virtualmachines` はいずれも `VirtualMachine` を指す。新しい kind はリソース
レジストリを通じて参加するので、新しいコマンドなしに上記すべての動詞を得る。

すべての動詞が受け付けるグローバルフラグ:

| フラグ | |
|---|---|
| `--config` | 設定ファイルのパス（環境変数 `PVECTL_CONFIG`、既定 `~/.pvectl/config`） |
| `--context` | 使うコンテキスト（環境変数 `PVECTL_CONTEXT`、既定 `current-context`） |
| `-o`, `--output` | `table` \| `wide` \| `yaml` \| `json` \| `name` |
| `--timeout` | Proxmox タスクを待つ長さ。既定 `5m` |

すべての mutation は Proxmox タスク（UPID）の完了を待ち、失敗したタスクを非 0 の
exit code に変える。そのため、パイプラインの中の `apply` が、サーバーに拒否された
作業を成功として報告することはない。

`-f` は繰り返し指定でき、ファイル・ディレクトリ・`-`（標準入力）を受け付ける。

```bash
pvectl get vms -o wide
pvectl get vm web-server -o yaml      # 往復する: これを apply すると unchanged
pvectl apply -f a.yaml -f b.yaml
pvectl apply -f manifests/            # ディレクトリ内の全 *.yaml / *.yml / *.json
cat vm.yaml | pvectl apply -f -
pvectl delete -f vm.yaml
```

## 5. 命令的動詞

`start`、`stop`、`exec`、`migrate` は 1 回限りの操作である。これらはどのマニフェストにも
痕跡を残さない。「一度これを実行した」は収束すべき望ましい状態ではないので、
`spec` に属するものが何もない。電源状態についての宣言的な対応物は
[`spec.runStrategy`](manifests.jp.md#電源状態) である。

`exec` は SSH を使わず、QEMU guest agent 経由でゲスト内でコマンドを実行し、
ゲスト側の stdout・stderr・exit code を自分のものとして引き受ける。

```bash
pvectl exec vm web-server -- systemctl is-active nginx
pvectl exec vm web-server -- /bin/sh -c "df -h / | tail -1"
pvectl exec vm web-server --exec-timeout 5m -- apt-get -y dist-upgrade
```

`--` の後ろはすべてそのままゲストに届く。ゲストには設定の `agent: 1` と稼働中の
`qemu-guest-agent` が必要。待機は `--exec-timeout`（既定 60s）で上限を切る。これは
グローバルな `--timeout` とは意図的に分けてある。Proxmox タスクを待つことと、
誰かがいま打ったコマンドを待つことは、種類の違う待機である。

`migrate` はゲストをノード間で移動する唯一の動詞で、`--online` は稼働中のゲストを
ライブマイグレーションする。

```bash
pvectl migrate vm web-server --to pve2
pvectl migrate vm web-server --to pve2 --online
```

`apply` は決してマイグレーションしない。`spec.targetNode` がゲストが実際に稼働して
いるノードと食い違う場合は、移動させるのではなくエラーを報告する。

## 次に読むもの

- [manifests.jp.md](manifests.jp.md) — `spec` の全フィールドと `apply` の正確な意味論
- [examples/](../../examples/) — 直接作成・クローン・マルチドキュメントの注釈付きマニフェスト
- [../adr/](../adr/) — pvectl がこう動く理由
