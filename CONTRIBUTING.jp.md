> [English](CONTRIBUTING.md)

# pvectl への貢献

pvectl に関心を持ってくれてありがとう。この文書では、プロジェクトが受け入れる変更、
レビューまでの進め方、そして最も重要な貢献経路である「新しい resource kind の追加」を
説明する。

pvectl のメンテナは現在 **1 名**（[@cyokozai](https://github.com/cyokozai)）であり、
すべての Pull Request をこの 1 名がレビューしマージする。これがこのプロジェクトの
制約であり、以下の規則の多くは、手続きを増やすためではなくレビューを安く保つために
存在する。

参加することで、[行動規範](CODE_OF_CONDUCT.jp.md)に同意したものとみなす。

## 1. コードを書く前に: その変更はスコープ内か

pvectl は意図的に狭い。大きめの提案をする前に
[ADR-005: pvectl の存在理由と非目標](docs/adr/ADR-005-purpose-and-non-goals.md)を
読んでほしい。変更を断るとき、メンテナが根拠として示すのはこの文書である。

### 中心原理

> **マニフェストに書いていないフィールドは、pvectl にとって存在しない。**

比較と更新の対象は、マニフェストが宣言したキーだけである。フィールドを消すことは
「管理をやめる」を意味し、「サーバーの既定値に戻す」ではない。サーバー側の既定値や
生成値との差分は恒久的に無視される。これは 2-way merge であり、pvectl は
`last-applied-configuration` も state ファイルも持たない。

**この原理に反する提案は、有用であっても受け入れない。**
「消したフィールドが元に戻らない」は仕様であり、不具合ではない。

### pvectl が Proxmox VE REST API の上に足すもの

次の 4 層だけである（[ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md)）。

| 層 | 生の API が提供しないもの |
|---|---|
| 認証・接続 | コンテキスト切替、トークン管理、`0600` 保存（[ADR-004](docs/adr/ADR-004-config-context.md)） |
| 収束 | 望ましい状態 → 差分 → 変更キーのみ PUT。`internal/resource/vm/normalize.go` がこの製品の本体 |
| タスク | UPID 待機と exit code 化（[ADR-002](docs/adr/ADR-002-api-client.md)） |
| 検証 | 書き込み前の、型付きマニフェストの厳格なバリデーション（[ADR-001](docs/adr/ADR-001-resource-model.md)） |

API のラッパを薄くすることも厚くすることも、それ自体は目的ではない。

### 明示された非目標

以下は既に採用しないと決まっている。再検討には Pull Request ではなく新しい ADR が要る。

- `tfstate` 相当のローカル state ファイル — サーバーが唯一の真実
- 常駐リコンサイル / `watch` — `apply` は 1 回限りの収束
- `--prune` — 所有権表明の設計が固まるまで扱わない（M3 以降）
- `apply` によるライブマイグレーションの暗黙実行 — `migrate` 動詞としてのみ提供
- Proxmox クラスタ自体の構築・ノード追加
- `qm` のコマンド体系の網羅 — `spec.raw` がエスケープハッチである
  （[ADR-006 §7](docs/adr/ADR-006-api-groups-and-schema.md)）
- ノードローカルの対話操作（`qm terminal` / `monitor` / `sendkey`）

### 機能が通すべき 2 問

1. 「宣言キーのみを管理する」に収まるか
2. **持続する望ましい状態**（`spec` に書く）か、**1 回限りの操作**（命令的動詞として
   提供し `spec` に痕跡を残さない）か

`spec.runStrategy`（`Halted` / `Always` / `Manual`）が宣言側、
`pvectl start` / `stop` / `exec` / `migrate` が命令側である。

## 2. 機能を書く前に issue を立てる

誤字や明らかなバグ修正を超える変更では、**先に issue を立てて合意を取ってから
コードを書くこと。** メンテナが 1 名の状況では、スコープ外だと分かった大きな
Pull Request は、誰よりも提案者本人の時間を無駄にする。

issue テンプレートを使う。

- [バグ報告](.github/ISSUE_TEMPLATE/bug-report.yml)
- [機能提案](.github/ISSUE_TEMPLATE/feature-request.yml) — ADR-005 の非目標に
  触れていないことの確認を求める

小さく自明な修正（誤字、リンク切れ、失敗しているテスト、明らかに誤ったエラー
メッセージ）は、そのまま Pull Request にしてよい。

### `help wanted` と `good first issue` は緩い

issue に **`help wanted`** または **`good first issue`** が付いている場合、
スコープの判定は既に済んでいる。メンテナが「この変更は欲しい」と判断したという意味
である。作業の重複を避けるためにコメントで着手を宣言し、そのまま Pull Request を
出してほしい。設計の議論は不要で、issue で改めて必要性を論じる必要もない。

貢献したいが自分固有の課題は特に無い、という場合はここが入口である。

## 3. 開発環境

**すべてコンテナ内で行う。** pvectl の開発のためにホストへ Go や linter を
インストールしないこと。ビルド・テスト・lint はすべて dev コンテナ内で実行する。

環境構築、make のターゲット、テストの層、リリースチェックリストは
**[docs/dev-process.md](docs/dev-process.md)** に記載されている。ローカルに独自の
ツールチェーンを組むのではなく、あの文書に従うこと。開発フローの単一の情報源は
あちらであり、この文書では意図的に再掲しない。

## 4. テストは必須

**pvectl の開発とテストに実クラスタは不要である。**
[`test/pvefake`](test/pvefake/pvefake.go) は `httptest` 製のフェイク PVE API で、
ticket ログイン・token 認証・タスク(UPID)ポーリングまで模倣するため、スタック全体を
オフラインで動かせる。

挙動を変える変更にはテストが必要であり、バグ修正は失敗する再現テストから始める。
変更した箇所に対応する層を選ぶこと。層と道具の対応表は
[docs/dev-process.md §1](docs/dev-process.md) にある。要約すると次のとおり。

- 純関数（convert / normalize / validate / diff）→ テーブルテスト
- 出力の整形 → golden file（`go test ./internal/printer -update` で再生成）
- リソースハンドラ → `internal/api/apitest` のフェイククライアント
- e2e → `test/pvefake` に対する `test/e2e`

[`examples/`](examples/) 以下のマニフェストはテストからもパースされるため、スキーマを
変えたらこちらも更新しないとテストが落ちる。これは意図的で、ドキュメントの腐敗を
防ぐためである。

レビュー開始の前提として CI（lint / test / build）が green であること。まずコンテナ内
で検査を回すこと（[docs/dev-process.md §2](docs/dev-process.md)）。

## 5. コミットメッセージ

pvectl は **Conventional Commits** を使う。本文は日本語でも英語でもよい。
[`.gitmessage`](.gitmessage) が正式なテンプレートである。中身を読み、自動で
使われるように設定しておくこと。

```bash
git config commit.template .gitmessage
```

書式:

```
<type>(<scope>): <description>

何を変えたか、なぜ必要だったかを説明する。
```

- type: `fix` / `feat` / `docs` / `style` / `refactor` / `perf` / `test` / `chore`
- scope は触ったパッケージや領域（`vm`、`cmd`、`api`、`resource`、`docs` など）
- 破壊的変更には `!` を付ける: `feat(vm)!: startOnBoot を runStrategy に置き換える`
- 既存の履歴に合わせること。`git log --oneline` を見て、そこにある形に従う。
  なお `.gitmessage` の絵文字の表は参考の一覧であり、実際の履歴では絵文字を
  使っていない。追加しないこと

## 6. 署名（任意）

pvectl は Developer Certificate of Origin の署名を**必須にしない**。CLA も**採用
しない**。MIT の個人 OSS に CLA は過剰であり、全コミットに DCO を強制するには
このプロジェクトが運用していない bot が必要になるためである。

出自を明示しておきたい場合、`Signed-off-by` 行は歓迎する。

```bash
git commit -s -m "feat(vm): ballooning に対応する"
```

これで次の行が追加される。

```
Signed-off-by: Your Name <your.email@example.com>
```

これは要求ではなく提案である。署名が無いことを理由に Pull Request を止めることも
遅らせることもないし、後から署名を足すために履歴を書き換えるよう求めることもない。
この行の意味に関心があれば、原文は
[developercertificate.org](https://developercertificate.org/) にある。

## 7. Pull Request の粒度

**1 Pull Request = 1 つの独立した変更。** マージ衝突しない単位で分け、
リファクタリングと挙動の変更は分離すること。

- 変更ごとに開発ブランチから短い説明的な名前のブランチを切る
- 無関係な整理を機能の PR に混ぜないこと。差分がレビュー不能になり、
  全体を受けるか全体を断るかの二択を強いることになる
- 触っていないファイルを整形し直さないこと
- API グループが alpha のうちは `v1alpha1` マニフェストスキーマの破壊的変更を
  受け入れるが、PR の説明に明記すること
- [Pull Request テンプレート](.github/pull_request_template.md)を埋めること。
  どうテストしたかも書く

## 8. AI を使った貢献

AI コーディングアシスタントの使用は**禁止しない**。pvectl 自体も AI を使って
開発されている。レビューの対象は、変更がどういう状態で届いたかであり、それを作った
道具ではない。使用したことを開示する義務もない（書いてくれるのは歓迎する）。

どちらにせよ要求されるのは次の点である。

- **差分を理解していること。** 各行がなぜそこにあり、消すと何が壊れるかを説明できる
  必要がある。「モデルが書いた」はレビューの質問への回答にならず、内容について
  答えられない Pull Request は close する
- **テストを実際に走らせたこと。** 「テストは通るはず」ではなく、コンテナ内で
  [`test/pvefake`](test/pvefake/pvefake.go) に対して実行し、通ることを自分で見たこと。
  実行したコマンドを Pull Request の説明に書くこと。実クラスタが不要なのだから、
  テストしていない変更に言い分は無い
- **事実をこのコードベースで確認したこと。** モデルが pvectl について記憶している
  内容ではなく、コードで裏を取ること。スキーマは最近変わっており、生成された回答は
  古いことが多い。`spec.startOnBoot` は既に無い（`spec.runStrategy` である）、
  `spec.cloudInit.password` も既に無い（`cloudInit.passwordFrom` である）、
  `spec.raw` は**ある**、`exec` と `migrate` は**実装済み**、`unlock` は**未実装**。
  もっともらしい要約を信じる前に
  [`internal/resource/vm/types.go`](internal/resource/vm/types.go) と
  [`internal/cmd/root.go`](internal/cmd/root.go) を読むこと
- **存在しない API・フラグ・出典を書かないこと。** 作業対象のツリーに存在しない
  メソッド・フィールド・ADR の節を参照しないこと。ADR が言っていないことを
  ADR の引用として書くのは、引用しないより悪い
- **生成された文章もコードと同じ厳しさで見る。** コードが持っていない挙動を
  説明したドキュメントは不具合である。`examples/` のマニフェストがテストから
  パースされるのは、まさにこれを検出するためである

これは AI に反対しているのではない。手書きのコードと同じ基準を明示しているだけ
である。アシスタントは、自信に満ちたもっともらしい誤ったパッチを異常に作りやすい。

## 9. 新しい resource kind を足す

これが pvectl で最も価値の高い貢献経路であり、アーキテクチャはそのために作られて
いる。汎用動詞（`get` / `describe` / `apply` / `diff` / `delete` / `start` / `stop` /
`exec` / `migrate`）はレジストリ経由でディスパッチされるため、**新しい kind に必要な
のはハンドラ 1 つと登録 1 行で、コマンド層の変更はゼロ**である
（[ADR-001](docs/adr/ADR-001-resource-model.md)）。

### 手順 1 — 必須の読み取り側を実装する

`internal/resource/<kind>/` を作り、
[`resource.Handler`](internal/resource/interfaces.go) を実装する。必須は 6 メソッド
だけである。

```go
type Handler interface {
	GVK() runtime.GVK                   // {Group: "pve.io", Version: "v1alpha1", Kind: "Storage"}
	Aliases() []string                  // コマンドライン名（すべて小文字）: "storage", "storages", "sto"
	Columns(wide bool) []printer.Column // テーブル出力。printer は kind 非依存のまま

	Get(ctx context.Context, c api.Client, name string) (printer.Object, error)
	List(ctx context.Context, c api.Client) ([]printer.Object, error)
	Describe(ctx context.Context, c api.Client, name string, w io.Writer) error
}
```

読み取り専用の kind（ノードはマニフェストが宣言するかどうかに関わらず存在する）は、
これだけを実装し、それ以上は実装しない。

### 手順 2 — 対応できる書き込み動詞にだけ手を挙げる

`Apply` と `Delete` は `Handler` の一部**ではない**。type assertion で解決される
任意の能力インターフェースである
（[ADR-006 §5](docs/adr/ADR-006-api-groups-and-schema.md)）。自分の kind が
責任を持てるものだけを実装すること。

| インターフェース | メソッド | 有効になる動詞 |
|---|---|---|
| `Applier` | `Apply` + `Diff` | `pvectl apply`、`pvectl diff` |
| `Deleter` | `Delete` | `pvectl delete` |
| `Starter` | `Start` | `pvectl start <kind> <name>` |
| `Stopper` | `Stop` | `pvectl stop <kind> <name>` |

ライフサイクルを持たない kind（たとえば Storage）は `Starter` / `Stopper` を単に
省略すればよく、動詞は panic も黙った無処理もせず「この kind は … に対応して
いません」と明示的に失敗する。`nil` を返すスタブを置かないこと。

### 手順 3 — 登録する

[`internal/cmd/root.go`](internal/cmd/root.go) の `DefaultRegistry()` に 1 行足す。

```go
func DefaultRegistry() *resource.Registry {
	reg := resource.NewRegistry()
	reg.Register(vm.NewHandler())
	reg.Register(storage.NewHandler())  // ← 追加する kind
	return reg
}
```

マニフェストは `(group, version, kind)` の完全一致で解決される。コマンドラインの
短縮名は別の別名マップで解決され、2 つの kind が同じ別名を主張した場合は曖昧エラー
になる。

### 手順 4 — スキーマの規約に従う

[`internal/resource/vm`](internal/resource/vm) が参照実装である。ファイル分割も
これに合わせること。`types.go`（スキーマ）/ `decode.go`（strict デコード）/
`validate.go` / `convert.go`（マニフェスト → フラットな API パラメータ）/
`normalize.go`（ライブ状態 → 比較可能な形）/ `apply.go` / `handler.go`。

守るべき規約:

- **宣言キーのみ。** diff と更新が触るのはマニフェストに存在するキーだけである
  （[ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md)）。これは kind ごとの方針
  ではなく、全 kind に適用される
- **strict デコード。** `spec` の未知フィールドはエラーにする。黙って捨てるのではなく
  typo として検出するため
- **1 リソース = 1 ドキュメント。** 他のオブジェクトを抱える kind を作らないこと。
  apply / diff / delete はオブジェクト単位で動くため、粒度を上げると冪等性が壊れる
  （[ADR-006 §4](docs/adr/ADR-006-api-groups-and-schema.md)）
- **union 型は kind を分けずに discriminator で表す。** `spec.type: nfs` と
  `spec.nfs:` のサブ構造を置き、`spec.type` に対応しないサブ構造が存在したら
  バリデーションエラーにする
  （[ADR-006 §3](docs/adr/ADR-006-api-groups-and-schema.md)）
- **`status` はサーバー所有。** `get -o yaml` には含め、`apply` は入力中の `status` を
  エラーにせず無視する（[ADR-006 §8](docs/adr/ADR-006-api-groups-and-schema.md)）
- **round-trip が成立すること。** `pvectl get <kind> <name> -o yaml` の出力を
  `pvectl apply -f -` に流したら `unchanged` になること。テストで固定する
- **新しい API グループは作らない。** M3 まではすべて `pve.io/v1alpha1` に置く
  （[ADR-006 §1](docs/adr/ADR-006-api-groups-and-schema.md)）
- API キーを最初から全部モデル化するより `spec.raw` を使うこと。利用頻度の高い
  キーは後から型付きフィールドへ昇格させる
  （[ADR-006 §7](docs/adr/ADR-006-api-groups-and-schema.md)）

### 手順 5 — テストする

ハンドラのテストには `internal/api/apitest` のフェイクを使う。加えて
`test/pvefake` を通る e2e テストを `test/e2e` に置き、`examples/` に例となる
マニフェストを追加する（テストからパースされる）。テストの無いハンドラは差し戻す。

## 10. レビューとマージ

- メンテナがすべての Pull Request をレビューし、マージするのはこの 1 名だけである
- レビュー開始の前提として **CI が green** であること（lint / test / build すべて通る）
- 質問はまずスコープについて、次に実装について来ると思ってほしい。
  [ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md) に合わない変更は、コードの
  品質に関わらず断られる
- レビューには時間がかかることがある。1 名のメンテナが余暇で作業しているため応答
  期限は約束できない。1 週間ほど経ってからの催促は、失礼ではなく歓迎する
- マージはリリース可能な main ブランチへの squash merge である

これはメンテナが 2 名以上になれば変わる。現在の体制を文書化しているのは、これを
不意打ちではなく明示された方針にしておくためである。

## 11. セキュリティ上の問題の報告

**脆弱性を public issue に書かないこと。** GitHub の Private vulnerability reporting を
使う。手順は [SECURITY.jp.md](SECURITY.jp.md) にある。API トークン・設定ファイルの
権限・マニフェストから秘密を排除する方法もあわせて記載している。

## ライセンス

pvectl は MIT ライセンス（[LICENSE](LICENSE)）である。Pull Request を出すことで、
その貢献を同じライセンスで提供すること、およびそうする権利を持っていることを
表明したものとみなす。
