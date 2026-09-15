> [English](SECURITY.md)

# セキュリティポリシー

## サポート対象バージョン

pvectl の版は [docs/prd.md](docs/prd.md) のマイルストーンに対応する。

| バージョン | マイルストーン | 状態 |
|---|---|---|
| `v0.1.x` | M1 — `VirtualMachine`（qemu） | **サポート対象** |
| `v0.2.0` | M2 — `Container`（LXC） | 予定 |
| `v0.3.0` | M3 — `Storage` / `Network` / `Snapshot` | 予定 |
| `v0.4.*` | M4 — `Pool` / `User` / `ACL` / `HA` | 予定 |
| `v1.0.0` | M4 完成後 | 予定 |

**現在セキュリティ修正を受けるのは `v0.1.x` のみである。** 修正は最新の `v0.1.x`
パッチリリースに入り、それより前のパッチ版へのバックポートは行わない。マニフェストの
API グループは `pve.io/v1alpha1` であり、`v1.0.0` までは破壊的なスキーマ変更が起こる。
報告の前に、最新リリースでも再現するか確認してほしい。

## 脆弱性の報告方法

**セキュリティ上の問題を public issue / Pull Request / Discussions に書かないこと。**

GitHub の **Private vulnerability reporting** を使う。

1. <https://github.com/cyokozai/pvectl/security/advisories/new> を開く
2. 問題の内容、影響を受けるバージョン（`pvectl version` の出力）、影響範囲を記載する
3. 再現手順があれば添える（マニフェストとコマンド行があれば十分）。
   **実物のトークン・パスワード・ホスト名は必ず削除してから貼ること。**

これにより報告者とメンテナだけが見える非公開のアドバイザリが作られ、公開前に修正の
準備と CVE の申請ができる。

何らかの理由で非公開報告が使えない場合は、メンテナの GitHub プロフィール
<https://github.com/cyokozai> に記載された連絡先から連絡してほしい。最後の手段として、
技術的な詳細を**一切書かず**「セキュリティ上の報告がある」とだけ記した public issue を
立て、連絡を待つこともできる。

pvectl のメンテナは 1 名で、余暇に作業している。応答期限は保証できないが、脆弱性報告は
機能開発より先に扱う。公開するまでに修正のための妥当な期間を置いてほしい。

## 対象となるもの

- pvectl が預かっている認証情報（設定ファイルの内容、API トークン、解決済みの
  cloud-init パスワード）が、ログ・標準出力・標準エラー出力・エラーメッセージ・
  緩い権限のファイルに漏れること
- 認証情報を意図しないホストへ送ること、また拒否すべきサーバー同一性を受け入れること
- マニフェストの入力によって、マニフェストが宣言していないものが書き込まれること
- pvectl 自身のファイル取り扱い（設定ファイルのパス、一時ファイル）を経由した権限昇格

## 対象にならないもの

- Proxmox VE 自体。これは
  [Proxmox のセキュリティ窓口](https://www.proxmox.com/en/about/security)へ報告すること
- pvectl が素通しするだけの Proxmox API の挙動。特に `spec.raw` 経由で渡したものは、
  設計上 API へ**検証せず**転送される（[ADR-006 §7](docs/adr/ADR-006-api-groups-and-schema.md)）
- pvectl が「置かないでほしい」と案内している場所に、利用者が自ら平文の秘密を置いた場合
- プロジェクトが明示的に採用しないと決めた堅牢化。
  [ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md) の非目標を参照

## pvectl における秘密の取り扱い

以下は pvectl が保とうとしている性質である。いずれかが崩れているなら、報告に値する
脆弱性である。

### 設定ファイルは API 認証情報を持つ

`~/.pvectl/config` は API トークンやパスワードを含む接続コンテキストを保存する。
`kubeconfig` と同程度に機微なファイルである。

- pvectl はこれを **`0600`**（所有者のみ読み書き）で書き出す（`internal/config/loader.go`）
- パスは `--config` または `PVECTL_CONFIG` で上書きできる。優先順位は
  `--config` > `PVECTL_CONFIG` > `~/.pvectl/config`
  （[ADR-004](docs/adr/ADR-004-config-context.md)）
- **このファイルをコミットしないこと。信頼できないコードを走らせるコンテナへ
  マウントしないこと。** dev コンテナはホストの秘密を意図的にマウントしない
  （[docs/dev-process.md §2](docs/dev-process.md)）

### `pvectl config view` は秘密を REDACTED する

`pvectl config view` は、出力する前に各 user エントリの `token` と `password` を
文字列 `REDACTED` に置き換える。バグ報告に貼るならこのコマンドの出力を使うこと。
`cat ~/.pvectl/config` は貼ってはいけない。

REDACTED されるのは `config view` の出力である。他のコマンドの生の出力を貼るときは、
自分で内容を確認すること。

### パスワードをマニフェストに書かせない: `cloudInit.passwordFrom`

マニフェストはバージョン管理に載せる前提であるため、pvectl には
**cloud-init のパスワードを直接書くフィールドが存在しない**。以前の
`spec.cloudInit.password` は削除され、外部参照に置き換えられた。

```yaml
spec:
  cloudInit:
    user: admin
    passwordFrom: env:PVE_VM_PASSWORD        # 環境変数
    # passwordFrom: file:/run/secrets/vmpw   # またはファイルの内容
```

- 受け付けるのは `env:NAME` と `file:/path` のみ。それ以外はバリデーションエラー
- 参照は `apply` 実行時に解決され、解決した値は非公開フィールドに保持されるため、
  YAML や JSON に書き出されることはない
- 参照の**構文**は環境を読まずに検証されるため、秘密を持たないマシンでも
  `--dry-run=client` が動く
- 値は write-only である。Proxmox API が `cipassword` をマスクするため、pvectl は
  作成時に設定するだけで、差分計算も更新も行わない

`passwordFrom` の文字列はコミットしても issue に貼っても安全である。それが指す値は
安全ではない。

### コミットするマニフェストから秘密を排除する

想定されている運用どおりマニフェストを git に入れるなら、次を守ること。

- パスワードを直接書かない。`env:` または `file:` を使う `passwordFrom` を用いる
- `sshKeys` に入れるのは**公開**鍵であり、コミットして問題ない。秘密鍵は pvectl の
  入力になることがない
- `pvectl get <kind> <name> -o yaml` の出力は round-trip 可能でコミットしてよいが、
  無条件に信頼せず、コミット前に目を通すこと
- `file:` の参照先はリポジトリの外（`/run/secrets/…` など）にしておく。そうすれば
  `.gitignore` の記述漏れで値が漏れることがない
- `spec.raw` は人間が目で確認すること。キーは検証されずに Proxmox API へ渡されるため、
  ここに置かれた秘密は pvectl に検出も REDACTED もされない
- root のパスワードより、マニフェストに必要な最小権限の **API トークン**を使うこと

## 依存関係

依存の更新は Dependabot（gomod / github-actions / docker、週次）で届く。Proxmox の SDK は
semver を持たないため、自動マージせず Pull Request ごとに検証して取り込む
（[ADR-002](docs/adr/ADR-002-api-client.md)）。pvectl から到達可能な依存の脆弱性は対象に
含まれる。アップグレードとアドバイザリを同時に公開できるよう、上記の手順で非公開に
報告してほしい。
