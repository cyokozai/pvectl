# ADR-004: config/context — kubeconfig 型ファイル + Factory 一元化、viper 廃止

- **状態**: 採用（2026-08-12）
- **決定**: 認証・接続情報は kubeconfig 型の `~/.pvectl/config` に置き、フラグ→実体（設定・コンテキスト・クライアント）の解決は `cliopt.Factory` ただ一箇所で行う。viper は削除

## 文脈

旧実装は viper を初期化しながら誰も読まず、`--config`/`--context` フラグが受理されるのに**完全に無視される**バグがあった。原因はフラグと実体化コードの間に配線が存在しなかったこと。

## 決定内容

### 設定ファイル

```yaml
apiVersion: v1
kind: Config
users:    [{name, user: {token | username+password}}]
nodes:    [{name, node: {server, insecureSkipTLSVerify}}]
contexts: [{name, context: {user, node}}]
current-context: <name>
```

- kubeconfig の `clusters` に相当するものは `nodes`（Proxmox の語彙に合わせる）
- 認証は token **XOR** username+password（両方/どちらも無しはバリデーションエラー）
- `config.Validate()` が参照整合性（context→user/node、URL 形式、重複名）を一括検査
- 保存は 0600 / ディレクトリ 0700

### Factory（kubectl cmdutil.Factory 相当）

```go
type Factory struct { ConfigPath, ContextName, Output string; Timeout time.Duration; NewClient func(...) }
func (f *Factory) Config() (*config.Config, error)          // load + validate、メモ化
func (f *Factory) CurrentContext() (*config.ResolvedContext, error)
func (f *Factory) Client(ctx) (api.Client, error)           // メモ化
```

- root コマンドが 1 個の Factory を生成し、全サブコマンドはコンストラクタ注入（`newGetCmd(f, reg)`）
- **クライアント生成経路が `Factory.Client()` 以外に存在しない**ため、「フラグ無視」バグは構造的に再発不能
- `NewClient` フィールドはテストシーム（フェイク注入）

### 優先順位

すべて「フラグ > 環境変数 > ファイル既定」:

| 項目 | フラグ | env | 既定 |
|---|---|---|---|
| 設定パス | `--config` | `PVECTL_CONFIG` | `~/.pvectl/config` |
| コンテキスト | `--context` | `PVECTL_CONTEXT` | `current-context` |

### viper 削除

env override は上記 2 変数を Factory で明示処理（各 2 行）。viper が提供する残りの機能（多形式設定・ホットリロード）は不要。依存 18 個が go.mod から消えた。

## 却下した代替案

- **viper を正しく配線**: 設定スキーマが構造体で固定されており、viper の動的キー解決は不整合の温床。明示コードの方が短い
- **フラット単一ターゲット設定（旧 setting.yaml）**: マルチクラスタ切替という要件に合わない
