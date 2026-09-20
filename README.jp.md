<div align="center">
  <img src="images/logo.png" alt="pvectl" width="200"/>

# pvectl

**Proxmox VE のための kubectl ライクな CLI。** · [English](README.md)
仮想マシンを YAML/JSON のマニフェストで宣言し、冪等に apply する。

</div>

## なぜ存在するか

pvectl は、state ファイルを持たない宣言的収束と、命令的な運用動詞を、手元で
実行する 1 つのバイナリにまとめたものである。中心原理は **「マニフェストに書いて
いないフィールドは、pvectl にとって存在しない」** — これによって、サーバー側の
既定値やテンプレートから継承した値が差分として現れることがない。
論拠と非目標の全文: [ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md)。

## インストール

[最新リリース](https://github.com/cyokozai/pvectl/releases/latest)から自分の
プラットフォーム（linux/darwin × amd64/arm64）の tar.gz を取得し、隣に置かれた
`SHA256SUMS` で検証して、`pvectl` を `PATH` に置く。

```bash
sha256sum --ignore-missing -c SHA256SUMS   # macOS: shasum -a 256 --ignore-missing -c
tar -xzf pvectl_v0.1.0_linux_amd64.tar.gz
```

Go ツールチェインがある場合は次でもよい。

```bash
go install github.com/cyokozai/pvectl/cmd/pvectl@latest
```

どちらも使えない場合は、コンテナでクロスビルドするか、コンテナ内で実行する — [quick-start](docs/quick-start/README.jp.md) を参照。

## 動作例

```yaml
# vm.yaml
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata: { name: web-server }
spec:
  targetNode: pve-node1
  resources: { cpu: { cores: 2 }, memory: 2048 }
  disks:
    - { name: scsi0, size: 32G, storage: local-lvm }
  networks:
    - { name: net0, bridge: vmbr0 }
```

```bash
pvectl apply -f vm.yaml            # 作成または更新。再実行しても書き込まない
pvectl diff -f vm.yaml             # exit 0 は差分なし、1 は差分あり
pvectl get vm web-server -o yaml   # そのまま apply に戻せる
```

## ドキュメント

| | |
|---|---|
| [quick-start](docs/quick-start/README.jp.md) | インストール、コンテキスト設定、最初の VM、全動詞とフラグ |
| [quick-start/manifests](docs/quick-start/manifests.jp.md) | `spec` 参照、`apply` の意味論、電源状態、秘密情報、エスケープハッチ |
| [quick-start/development](docs/quick-start/development.md) | 開発・テスト・インストール（コンテナ完結） |
| [examples/](examples/) | 注釈付きマニフェスト: 直接作成、クローン、マルチドキュメント |
| [docs/adr/](docs/adr/) | アーキテクチャ決定記録 |
| [docs/prd.md](docs/prd.md) | プロダクト要件とマイルストーン |

## ロードマップ

| 版 | マイルストーン | スコープ |
|---|---|---|
| `v0.1.0`（次回） | M1 + M1.5 | `VirtualMachine`: 冪等 apply、diff、dry-run、clone、cloud-init、lifecycle、`exec` / `migrate`、`spec.raw`、`spec.runStrategy`、`cloudInit.passwordFrom` |
| `v0.2.0` | M2 | LXC コンテナ（`kind: Container`） |
| `v0.3.0` | M3 | ストレージ、ネットワーク、スナップショット |
| `v0.4.*` | M4 | プール、ユーザー、ACL、HA |
| **`v1.0.0`** | — | M4 が完成し、テストとフィードバック反映を経て、マニフェストのスキーマが安定し `v1alpha1` を抜ける時点 |

開発は `dev` で進む。**`main` はリリース済みバージョンを指す** — リリースごとに `dev` を `main` へマージし、タグを打つ。`v0.x` のリリースはスキーマの破壊的変更を含みうる。新しい kind はリソースレジストリを通じて同じ動詞に接続される — 新しいコマンドは増えない。

## コントリビュート

機能を書く前に issue で議論してほしい — pvectl のスコープは狭く、スコープ外の
提案は意図的に受け入れない（[非目標](docs/adr/ADR-005-purpose-and-non-goals.md)）。
コミットには DCO のサインオフ（`git commit -s`）が必要。メンテナは 1 名のため、
レビューはベストエフォートである。詳細:
[CONTRIBUTING.jp.md](CONTRIBUTING.jp.md)、
[CODE_OF_CONDUCT.jp.md](CODE_OF_CONDUCT.jp.md)、[SECURITY.jp.md](SECURITY.jp.md)。

## 開発

`make dev-up && make check` が vet・lint・race テストをコンテナ内で実行する。
テストはインメモリのフェイク Proxmox VE API（[test/pvefake](test/pvefake)）に
対して走るため、実クラスタは不要。詳細は [quick-start/development](docs/quick-start/development.md)。

## ライセンス

[MIT](LICENSE)
