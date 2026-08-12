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
