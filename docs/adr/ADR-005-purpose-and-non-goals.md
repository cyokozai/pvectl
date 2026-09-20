# ADR-005: pvectl の存在理由と非目標 — 宣言キーのみを管理する

- **状態**: 採用（2026-09-14）
- **決定**: pvectl を「state ファイルを持たない宣言的収束と、命令的な運用動詞を、1 つのバイナリで手元から実行する CLI」と定義する。中心原理は **「マニフェストに書いていないフィールドは、pvectl にとって存在しない」**。この原理から外れる機能は、有用であっても採用しない

## 文脈

### なぜ REST API があるのに CLI を作るのか

REST API は転送路であって UX ではない。Kubernetes API も REST だが kubectl が存在するのと同じ構図であり、pvectl が API の上に足すのは次の 4 層に限られる。

| 層 | API が提供しないもの |
|---|---|
| 認証・接続 | コンテキスト切替、トークン管理、0600 保存（ADR-004） |
| **収束** | 望ましい状態 → 差分 → 変更キーのみ PUT。**`resource/vm/normalize.go` がこの製品の本体** |
| タスク | UPID 待機と exit code 化（ADR-002） |
| 検証 | 型付きマニフェストの事前バリデーション（ADR-001） |

この 4 層以外を pvectl が担うことはない。API のラッパを増やすこと自体は目的ではない。

### Terraform Provider の構造的限界（2026-09 調査）

bpg/terraform-provider-proxmox（v0.113.1、2026-09-11）は約 55 リソースを持つ最も成熟した実装だが、クローン運用の冪等性に構造的な問題を抱える。原因は provider 自身のドキュメントに記されている。

> Many attributes are marked as **optional** _and_ **computed** in the schema, hence you may seem added to the plan with "(known after apply)" status, even if they are not set in the configuration. This is done to support the `clone` operation.
> — `proxmox_vm` リソース（Plugin Framework 版 PoC、"DO NOT USE" と明記）

Terraform のスキーマは「ユーザーが宣言したか否か」と「値が空か否か」を区別できない。クローン元から継承した値を表現するには全属性を optional かつ computed にするほかなく、結果として plan にノイズが乗り、意図しない書き戻しが起きる。

bpg はこれを認識しており、`proxmox_cloned_vm`（EXPERIMENTAL）を追加している。説明は "explicit opt-in management: only configuration blocks and devices explicitly listed in your Terraform code are managed" であり、pvectl の managed-keys（ADR-003）とほぼ同じ思想である。ただし、

- EXPERIMENTAL であり、本番利用が推奨されていない
- BIOS / machine / boot order、EFI ディスク、secure boot、TPM、**cloud-init**、guest agent、PCI/USB パススルー、serial/audio/watchdog/VirtioFS が管理対象外として明示的に除外されている
- 既存 `proxmox_virtual_environment_vm` の意味論を壊せないため、別リソースとして切り出すしかなかった
- tfstate は依然として必要

**pvectl の優位は偶然ではなく構造的である。** 宣言キーの集合をそのまま管理対象とするモデルは Terraform のスキーマでは表現できず、別リソース + experimental で近似するしかない。pvectl ではこれが既定であり、全フィールドに一様に適用される。

加えて SDN の二段階コミット（pending → reload）も Terraform では表現できず、bpg は `PUT /cluster/sdn` を叩くだけのダミーリソース `proxmox_sdn_applier` を `replace_triggered_by` と組み合わせる回避策をとっている。書き込みの意味論が CRUD に収まらない領域では、専用 CLI のほうが素直に書ける。

## 決定内容

### 1. 一文の定義

> **pvectl は、state ファイルを持たない宣言的収束と、命令的な運用動詞を、1 つのバイナリで手元から実行する CLI である。**

| | 宣言的 | 手元から実行 | state 不要 | 観測動詞 |
|---|---|---|---|---|
| Web UI | ✗ | ✓ | ✓ | ✓ |
| `qm` / `pct` | ✗ | ✗（SSH 必須） | ✓ | ✓ |
| `pvesh` | ✗ | ✗（ノード上） | ✓ | △ |
| Terraform | ✓ | ✓ | **✗** | ✗ |
| **pvectl** | ✓ | ✓ | ✓ | ✓ |

### 2. 中心原理 — 宣言キーのみを管理する

ADR-003 の managed-keys 比較を、VM 固有の実装方針から **全 kind に適用される原理** へ昇格させる。

- 比較・更新の対象はマニフェストが宣言したキーだけ
- マニフェストから消したフィールドは「管理をやめる」を意味し、ライブ値を元に戻さない
- サーバー側の既定値・生成値との差分は恒久的に無視される

**受け入れるトレードオフ**: これは 2-way merge であり、kubectl の 3-way merge（`last-applied-configuration`）は持たない。「フィールドを消したのに元に戻らない」は不具合ではなく仕様であり、ドキュメントに明記する。

将来 3-way が必要になった場合の逃げ道は Proxmox の `description` に last-applied を埋め込むことだが、`description` は利用者の表示領域でもあるため現時点では採用しない。採用を検討する条件は「消したフィールドが戻らないことに起因する不具合報告が実際に発生したとき」とする。

### 3. 命令的動詞は spec に書かない

KubeVirt は start / stop / restart / pause / migrate / console を VM の spec ではなく `subresources.kubevirt.io` という独立 API グループのサブリソースとして提供し、`virtctl` という別バイナリから叩く。同時に宣言側には `spec.runStrategy` を持つ。宣言と命令の両方を持ちつつ、層を分けている。

pvectl もこれに倣い、採用基準を **「望ましい状態として持続するか」** とする。

| | 実装 |
|---|---|
| 持続する（宣言） | `spec.runStrategy`: `Halted` / `Always` / `Manual`。apply は起動状態まで収束させる |
| 1 回限り（命令） | `pvectl start` / `stop` / `exec` / `migrate` / `unlock`。spec に痕跡を残さない |

現在の `spec.startOnBoot: bool` は Proxmox の `onboot` フラグを写しただけで「apply したら起動していてほしい」を表現できない。`runStrategy` に置き換える（`Always` が `onboot=1` にマップされる）。

### 4. 秘密情報をマニフェストに書かせない

`cloudInit.password` は API が `**********` を返すため write-only の特例になっているが、これは設計の匂いである。KubeVirt は `userDataSecretRef` で Secret を参照し、bpg は `proxmox_virtual_environment_file` を参照する。

pvectl は外部参照を導入する。

```yaml
cloudInit:
  passwordFrom: env:PVE_VM_PASSWORD    # または file:/run/secrets/vmpw
```

外部参照であれば比較すべき値がマニフェストに存在せず、write-only 特例そのものが不要になる。GitOps に載せる前提では、平文パスワードがリポジトリに入る現状は採用障害である。

### 5. live → マニフェストのエクスポートを一級機能とする

2026-09 時点で bpg / Telmate / Kubemox のいずれも、ライブ状態からマニフェストや HCL を生成する一級機能を持たない。Terraform 側の導入経路は `terraform import`（state に取り込むだけ）であり、既存クラスタの取り込みが最大の障害になっている。

pvectl は `export` を一級動詞として持ち、**「export の出力をそのまま apply して全件 `unchanged`」** を正しさの機械的な定義とする。これは e2e テストで保証する。

### 非目標

- tfstate 相当のローカル状態ファイル（サーバーが唯一の真実。ADR-003 で却下済み）
- 常駐リコンサイル / watch（apply は 1 回限りの収束）
- `--prune`（所有権表明の設計が固まる M3 以降まで扱わない）
- apply によるライブマイグレーションの暗黙実行（`migrate` 動詞としてのみ提供）
- Proxmox クラスタ自体の構築・ノード追加
- **`qm` のコマンド体系の網羅**（ADR-006 のエスケープハッチで代替する）
- ノードローカルの対話操作（`qm terminal` / `monitor` / `sendkey`）

## 結果

- ✅ 機能追加の可否を「宣言キーのみの原理に収まるか」「持続する状態か」の 2 問で判定できる
- ✅ Terraform に対する優位が、実装品質ではなくモデルの差として説明できる
- ✅ `export` という空いた領域を差別化軸として確保できる
- ⚠️ 2-way merge の制約はドキュメントで周知し続ける必要がある。理解されないと「消しても戻らない」が不具合として報告される
- ⚠️ `runStrategy` と `passwordFrom` は `startOnBoot` / `password` の破壊的変更を伴う。v1alpha1 のうちに実施する

## 却下した代替案

- **Terraform Provider に貢献して直す**: 問題は provider の実装品質ではなくスキーマの表現力にあり、provider 側では解けない。bpg 自身が別リソース + experimental という形でしか近似できていないことがその証拠
- **KubeVirt 型の VirtualMachine / VirtualMachineInstance 分離**: KubeVirt が分離したのは k8s 側に Pod という既存の実行単位があったため。Proxmox では config が停止中も同じ vmid に残り、実行実体を別 ID で参照しないため、`status` で足りる
- **`qm` ラッパとして再構築**: GUI 不可 / `qm` 可の差はその大半が「GUI 未実装だが API には存在する」ものであり、`qm` 自体が API 層の薄いラッパである。網羅を目指すと SSH 前提に戻り、手元から実行できるという最大の利点を失う

## 追補（2026-09-16、v0.1.0 リリース前の整理）

本文（§1〜§5 と非目標）は当時の決定として残す。以下は 1 件が決定の撤回、
もう 1 件が実装の遅れであり、性質が異なる。

### `unlock` を実装しないことにした（§3 の決定を撤回）

§3 の表で命令的動詞として挙げた `unlock` を取り下げる。根拠は 2 点。

- `qm unlock` に対応する REST エンドポイントが存在しない。等価な操作は
  `PUT /nodes/{node}/qemu/{vmid}/config` に `delete=lock` と `skiplock=1` を
  渡す形になる
- その `skiplock` は qm(1) の全コマンドで一貫して *"Ignore locks - only root is
  allowed to use this option."* と規定されている。つまり **`root@pam` 専用**で、
  README が推奨する API トークン認証では主経路が成立しない

**「認証方式によって可否が変わる動詞をどう扱うか」は未決の設計論点である。**
トークン認証時にエラーで落とすのか、ヘルプにも出さないのか — 挙動の約束を
先に決めるまで実装しない。再検討する条件は次のいずれか。

- 「`root@pam` のパスワード認証時のみ有効な動詞」という設計に合意できたとき
- Proxmox 側が lock 解除の専用エンドポイントを追加したとき

当面のロック解除はノード上の `qm unlock` か Web UI に委ねる。

### `export` は決定を維持したまま未実装（§5）

§5 の決定 — `export` を一級動詞として持ち、**「export の出力をそのまま apply
して全件 `unchanged`」** を正しさの機械的な定義とする — は**有効のままであり、
撤回しない**。ただし **v0.1.0 時点では未実装**である。

- `internal/cmd/root.go` に登録されている動詞は get / describe / apply / diff /
  delete / start / stop / exec / migrate / config / version のみ
- 現状これに最も近いのは `get -o yaml` の出力をそのまま apply する round-trip
  で、`internal/resource/vm/apply_test.go` の `TestGetYAMLRoundTrip` が
  **単体テストとして**押さえている
- §5 が求める e2e レベルの保証（export → apply が全件 `unchanged`）は**まだ無い**

差別化軸としての位置づけは変えない。未定なのは実装時期だけである。
