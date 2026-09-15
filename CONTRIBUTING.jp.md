> [English](CONTRIBUTING.md)

# pvectl への貢献

pvectl に関心を持ってくれてありがとう。参加することで
[行動規範](CODE_OF_CONDUCT.jp.md)に同意したものとみなす。セキュリティ上の問題は
public issue ではなく [SECURITY.jp.md](SECURITY.jp.md) の手順で報告すること。

## 1. スコープ — コードを書く前に読むこと

pvectl は意図的に狭い。中心原理は次のとおりである。

> **マニフェストに書いていないフィールドは、pvectl にとって存在しない。**

比較と更新の対象は、マニフェストが宣言したキーだけである。フィールドを消すことは
「管理をやめる」を意味し、「サーバーの既定値に戻す」ではない。これは 2-way merge で
あり、`last-applied-configuration` も state ファイルも持たない。したがって
「消したフィールドが元に戻らない」は仕様であり、不具合ではない。

**この原理に反する提案は、有用であっても受け入れない。** pvectl が Proxmox VE
REST API の上に足すのは 4 層（認証・接続、収束、タスク待機、マニフェストの検証）に
限られる。API のラッパを厚くすること自体は目的ではない。理由は
[ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md) にある。

### 既に採用しないと決まっているもの

以下の再検討には、Pull Request ではなく新しい ADR が要る。

- `tfstate` 相当のローカル state ファイル — サーバーが唯一の真実
- 常駐リコンサイル / `watch` — `apply` は 1 回限りの収束
- `--prune` — 所有権表明の設計が固まるまで扱わない（M3 以降）
- `apply` によるライブマイグレーションの暗黙実行 — `migrate` 動詞としてのみ提供
- Proxmox クラスタ自体の構築・ノード追加
- `qm` のコマンド体系の網羅 — `spec.raw` がエスケープハッチである
- ノードローカルの対話操作（`qm terminal` / `monitor` / `sendkey`）

### 機能が通すべき 2 問

1. 「宣言キーのみを管理する」に収まるか
2. **持続する望ましい状態**（`spec` に書く）か、**1 回限りの操作**（命令的動詞として
   提供し `spec` に痕跡を残さない）か

`spec.runStrategy`（`Halted` / `Always` / `Manual`）が宣言側、
`pvectl start` / `stop` / `exec` / `migrate` が命令側である。

### 先に issue を立てる

誤字や明らかなバグ修正を超える変更では、**先に issue を立てて合意を取ってから
コードを書くこと。** スコープ外だと分かった大きな Pull Request は、誰よりも提案者
本人の時間を無駄にする。小さく自明な修正はそのまま Pull Request にしてよい。

**`help wanted`** または **`good first issue`** が付いた issue は例外である。
スコープの判定は済んでいるので、コメントで着手を宣言してそのまま Pull Request を
出してほしい。

## 2. 開発環境

すべてコンテナ内で行う。ホストへ Go や linter をインストールしないこと。環境構築、
make のターゲット、テストの層、リリースチェックリストは
**[docs/dev-process.md](docs/dev-process.md)** にある。あの文書に従うこと。開発フローの
単一の情報源はあちらであり、ここでは意図的に再掲しない。

## 3. テスト

**実クラスタは不要である。** [`test/pvefake`](test/pvefake/pvefake.go) は `httptest`
製のフェイク PVE API で、ticket ログイン・token 認証・タスク(UPID)ポーリングまで
模倣するため、スタック全体がオフラインで動く。

挙動を変える変更にはテストが必要であり、バグ修正は失敗する再現テストから始める。
変更した層に対応する道具を選ぶこと（対応表の全体は
[docs/dev-process.md §1](docs/dev-process.md)）。

- 純関数（convert / normalize / validate / diff）→ テーブルテスト
- 出力の整形 → golden file（`go test ./internal/printer -update` で再生成）
- リソースハンドラ → `internal/api/apitest` のフェイククライアント
- e2e → `test/pvefake` に対する `test/e2e`

[`examples/`](examples/) 以下のマニフェストはテストからもパースされるため、スキーマを
変えたらこちらも更新する必要がある。これは意図的で、ドキュメントの腐敗を防ぐため
である。

## 4. コミットと Pull Request

**1 Pull Request = 1 つの独立した変更。** リファクタリングと挙動の変更は分離し、
触っていないファイルを整形し直さないこと。
[Pull Request テンプレート](.github/PULL_REQUEST_TEMPLATE.md)を埋めること。
API グループが alpha のうちは `v1alpha1` マニフェストスキーマの破壊的変更を
受け入れるが、説明に明記すること。

コミットメッセージは **Conventional Commits** を使う。本文は日本語でも英語でもよい。
[`.gitmessage`](.gitmessage) がテンプレートである
（`git config commit.template .gitmessage`）。

```
<type>(<scope>): <description>
```

- type: `fix` / `feat` / `docs` / `style` / `refactor` / `perf` / `test` / `chore`
- scope は触ったパッケージや領域（`vm`、`cmd`、`api`、`resource`、`docs` など）
- 破壊的変更には `!` を付ける: `feat(vm)!: startOnBoot を runStrategy に置き換える`
- `.gitmessage` の絵文字の表は参考の一覧である。実際の履歴では絵文字を使っていない
  ので追加しないこと。`git log --oneline` を見てそこにある形に従う
- **署名も CLA も求めない。** 出自を明示したい場合 `git commit -s` は歓迎するが、
  無いことを理由に何かを止めることはない

## 5. AI を使った貢献

AI コーディングアシスタントの使用は**禁止しない**。pvectl 自体も AI を使って開発
されている。レビューの対象は、変更がどういう状態で届いたかであり、それを作った道具
ではない。使用したことを開示する義務もない。どちらにせよ要求されるのは次の点である。

- **差分を理解していること。** 各行がなぜそこにあり、消すと何が壊れるかを説明できる
  こと。「モデルが書いた」はレビューの質問への回答にならず、内容について答えられない
  Pull Request は close する
- **テストを実際に走らせたこと。** 「通るはず」ではなく、コンテナ内で
  [`test/pvefake`](test/pvefake/pvefake.go) に対して実行し、通ることを自分で見て、
  実行したコマンドを書くこと。実クラスタが不要なのだから、テストしていない変更に
  言い分は無い
- **事実をこのコードベースで確認したこと。** 生成された回答は古いことが多い。
  `spec.startOnBoot` は既に無い（`spec.runStrategy` である）、
  `spec.cloudInit.password` も既に無い（`cloudInit.passwordFrom` である）、
  `spec.raw` は**ある**、`exec` と `migrate` は**実装済み**、`unlock` は**未実装**。
  [`internal/resource/vm/types.go`](internal/resource/vm/types.go) と
  [`internal/cmd/root.go`](internal/cmd/root.go) を読むこと
- **存在しない API・フラグ・出典を書かないこと。** ADR が言っていないことを ADR の
  引用として書くのは、引用しないより悪い
- **生成された文章もコードと同じ厳しさで見る。** コードが持っていない挙動を説明した
  ドキュメントは不具合である

これは手書きのコードと同じ基準を明示しているだけである。アシスタントは、自信に満ちた
もっともらしい誤ったパッチを異常に作りやすい。

## 6. 新しい resource kind を足す

これが最も価値の高い貢献経路であり、アーキテクチャはそのために作られている。汎用動詞
はレジストリ経由でディスパッチされるため、**新しい kind に必要なのはハンドラ 1 つと
登録 1 行で、コマンド層の変更はゼロ**である
（[ADR-001](docs/adr/ADR-001-resource-model.md)）。

1. **読み取り側を実装する。** `internal/resource/<kind>/` に
   [`resource.Handler`](internal/resource/interfaces.go) を実装する。必須は 6 メソッド
   だけである: `GVK` / `Aliases` / `Columns` / `Get` / `List` / `Describe`。
   読み取り専用の kind（ノードはマニフェストが宣言するかどうかに関わらず存在する）は、
   これだけを実装し、それ以上は実装しない。
2. **対応できる書き込み動詞にだけ手を挙げる。** `Apply` と `Delete` は `Handler` の
   一部**ではない**。type assertion で解決される任意の能力インターフェースである
   （[ADR-006 §5](docs/adr/ADR-006-api-groups-and-schema.md)）:
   `Applier`（`Apply` + `Diff`）/ `Deleter` / `Starter` / `Stopper`。自分の kind が
   責任を持てるものだけを実装すること。省略した動詞は「対応していません」と明示的に
   失敗し、それが望ましい挙動である。`nil` を返すスタブを置かないこと。
3. **登録する。** [`internal/cmd/root.go`](internal/cmd/root.go) の
   `DefaultRegistry()` に 1 行足す。マニフェストは `(group, version, kind)` の完全
   一致で解決され、コマンドラインの短縮名は別の別名マップで解決される。2 つの kind が
   同じ別名を主張した場合は曖昧エラーになる。
4. **規約に従う。** [`internal/resource/vm`](internal/resource/vm) が参照実装である。
   読んで、そのファイル分割（`types.go` / `decode.go` / `validate.go` / `convert.go` /
   `normalize.go` / `apply.go` / `handler.go`）に合わせること。自明でない規則:
   `spec` の未知フィールドはエラー（strict デコード）、1 リソース = 1 ドキュメント、
   union 型は kind を分けず `spec.type` の discriminator で表す
   （[ADR-006 §3](docs/adr/ADR-006-api-groups-and-schema.md)）、`status` はサーバー
   所有で `apply` は無視する、当面すべて `pve.io/v1alpha1` に置く、API キーを最初から
   全部モデル化するより `spec.raw` を使う。
5. **テストする。** ハンドラのテストには `internal/api/apitest` のフェイクを使い、
   加えて `test/pvefake` を通る e2e テストを `test/e2e` に、例となるマニフェストを
   `examples/` に置く。round-trip も固定すること —
   `get <kind> <name> -o yaml` の出力を `apply -f -` に流したら `unchanged` になること。
   テストの無いハンドラは差し戻す。

## 7. レビューとマージ

**pvectl のメンテナは [@cyokozai](https://github.com/cyokozai) 1 名であり、すべての
Pull Request をこの 1 名がレビューし、この 1 名がマージする。** 他の誰もマージ権限を
持たない。

- レビュー開始の前提として **CI が green** であること（lint / test / build すべて通る）
- 質問はまずスコープについて、次に実装について来る。
  [ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md) に合わない変更はコードの品質に
  関わらず断られる。だから §1 が何よりも先に置かれている
- 1 名が余暇で作業しているため、レビューには時間がかかることがある。1 週間ほど経って
  からの催促は、失礼ではなく歓迎する
- マージは squash merge である

## ライセンス

pvectl は MIT ライセンス（[LICENSE](LICENSE)）である。Pull Request を出すことで、
その貢献を同じライセンスで提供すること、およびそうする権利を持っていることを
表明したものとみなす。
