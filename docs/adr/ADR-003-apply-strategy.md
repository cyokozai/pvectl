# ADR-003: apply 戦略 — managed-keys diff-and-patch、immutable はエラー

- **状態**: 採用（2026-08-12）
- **決定**: apply は「マニフェストが宣言したキーだけ」を正規化済みライブ設定と比較し、変更キーのみ PUT する。immutable な変更はエラーにし、再作成は絶対にしない

## 決定内容

### 同一性の解決

1. `spec.vmid` があれば vmid で解決（名前変更 = rename として扱う）
2. なければ `metadata.name` で解決。同名 VM が複数ノードにある場合は `ErrAmbiguousName` →「spec.vmid を指定せよ」
3. 見つからなければ create、見つかれば update

### managed-keys 比較（kubectl の managed fields の簡易版）

- 比較対象は desired（マニフェスト由来のフラット param map）に存在するキーのみ。サーバー側キー（`vmgenid`, `digest`, `smbios1`, `boot`...）は無視
- 正規化ルール（`resource/vm/normalize.go`）:
  - **ディスク** `local-lvm:vm-100-disk-0,size=32G,...`: storage + size（バイト正規化）+ マニフェストが明示した opt のみ比較。ボリューム名・サーバー付与 opt は無視
  - **NIC**: サーバー生成 MAC は無視（マニフェストがピンした場合のみ比較）
  - **tags**: ソートして比較（順序不感）
  - **sshkeys**: URL デコードして比較（%XX / + の揺れを吸収）
  - **cipassword**: API が `**********` を返すため **write-only**（作成時のみ設定、diff/update 対象外）
  - **cpu**: `cputype=host,flags=...` → ベース型のみ比較

### 更新の実行

- 変更キーのみ `PUT /config`。ディスクは特別扱い:
  - サイズ拡大 → `PUT /resize`（config PUT には含めない）
  - サイズ縮小 → エラー（PVE 非対応）
  - storage / format 変更 → エラー（in-place 不可能）
  - opt 変更（cache 等）→ 既存ボリューム値 + 新 opt で再構成して PUT
  - 新しいスロット → allocation 構文で PUT（PVE が新規割当）

### immutable ポリシー: エラー、再作成しない

- `vmid` = 同一性そのもの。name 一致 + vmid 不一致 → エラー
- `targetNode` 不一致 → エラー（apply は migrate しない）
- `clone` / `fullClone` / `pool` は作成時のみ有効。更新時は警告して無視
- 根拠: VM の暗黙削除→再作成はデータ喪失リスクがあり、CLI が勝手にやってよい操作ではない

### clone + 再設定

クローン作成は `clone` → 完了待機 → マニフェストの resources/networks/cloudInit/tags/onboot を post-clone PUT。ディスクはソース継承（宣言されていれば警告）。

### dry-run / diff

- `--dry-run=client`: デコード+バリデーションのみ、API 呼び出しゼロ → `validated`
- `--dry-run=server`: 読み取り+diff まで、書き込みゼロ → `created`/`configured`/`unchanged`
- `pvectl diff -f`: exit 0 = 差分なし / 1 = 差分あり / >1 = エラー（kubectl diff 準拠）

## 却下した代替案

- **全フィールド比較**: サーバー側デフォルトとの差分ノイズで常に configured になる
- **Terraform 型 state ファイル**: サーバーが唯一の真実であるべき。state drift 問題を持ち込まない
- **immutable 変更時の自動再作成**: データ喪失リスク。ユーザーの明示操作に委ねる

## 追補（2026-09-16、ADR-005/006 により）

本文は当時の決定として残し、以下の 2 点を上書きする。

### apply は電源状態を変えうる（ADR-005 §3）

`spec.startOnBoot: bool` を `spec.runStrategy`（`Halted` / `Always` / `Manual`）に
置き換えた。onboot フラグの写しから「望ましい電源状態」の宣言へ格上げした
ことにより、**apply の意味論が「設定の収束」から「設定 + 電源状態の収束」へ
広がる**。

| `runStrategy` | `onboot` | apply の電源操作 |
|---|---|---|
| `Manual`（未指定時の既定） | 非管理（キーを生成しない） | しない。本文の挙動と同一 |
| `Always` | `1` | 停止していれば起動する |
| `Halted` | `0` | 起動していれば停止する |

- 電源遷移も望ましい状態の一部なので、**設定の差分が空でも遷移が必要なら
  `configured` を返す**。`unchanged` は「設定も電源状態も一致」を意味する
- `--dry-run=server` は遷移の要否を読み取って `configured` を報告するが、
  電源には触れない
- ライブ設定からの復元（`get -o yaml` / `export`）は `onboot=1` を `Always`、
  それ以外を **`Manual`** に写す。`Halted` には決して写さない。export の出力を
  apply したときに、利用者が停止を宣言していない VM を停止させないため
- 命令的な `start` / `stop` は従来どおり spec に痕跡を残さない（ADR-005 §3）

### clone 系の create-only は警告ではなくエラー（ADR-006 §6）

本文「`clone` / `fullClone` / `pool` は作成時のみ有効。更新時は警告して無視」を
次のように改める。「宣言したのに黙って効かない」が最も分かりにくい失敗である
ため、**検出できる不一致はエラーにする**。

- `spec.clone` があるとき `spec.disks` は宣言できない（バリデーションエラー）。
  クローンはソースからディスクを継承するため、宣言しても無視するしかない
- 更新時、`spec.pool` がライブのプールと異なる場合は**エラー**。警告して無視は
  やめる
- `spec.clone` / `spec.fullClone` は**更新時も受理し、警告も出さない**。Proxmox は
  出自を記録しないため突き合わせるライブ値が存在せず、「異なる」を判定できない。
  ADR-006 の整理どおり `spec.clone` は命令ではなく「この VM はこのテンプレート
  由来である」という出自の宣言として扱う。クローンで作った VM のマニフェストは
  以後も `clone` を持ち続け、再 apply は no-op になる
