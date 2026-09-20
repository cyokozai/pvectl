# ADR-006: API グループ・版管理・kind 粒度

- **状態**: 採用（2026-09-14）
- **決定**: グループは当面 `pve.io/v1alpha1` 単一のまま維持し、分割は M3（SDN）で行う。ただし Registry の内部表現は今すぐ GVK キーに変更する。kind は Proxmox の型ごとに分割せず discriminator で束ねる。Handler の `Apply` / `Delete` はオプショナル能力に降格する

## 文脈

先行例の粒度を調査した（2026-09）。

| プロジェクト | グループ | 粒度 |
|---|---|---|
| KubeVirt | **多グループ**。`kubevirt.io/v1` を core に、`subresources` / `instancetype` / `snapshot` / `export` / `clone` / `pool` / `migrations` を機能ごとに分離。版もバラバラ（core は v1、snapshot/clone は beta、pool は alpha） | 機能と成熟度で分割 |
| bpg/proxmox | フラット（Terraform にグループ概念なし） | 約 55 リソース。SDN zone とストレージを**型ごとに**分割 |
| Telmate/proxmox | フラット | 5 リソースのみ |
| Kubemox | **単一グループ** `proxmox.alperen.cloud` | 9 kind |
| community.proxmox (Ansible) | — | 約 65 モジュール。1 モジュール = 1 操作で、`proxmox_disk` / `proxmox_nic` も VM と別 |

多グループ化しているのは KubeVirt だけであり、その動機は **GA 済みの `kubevirt.io/v1` を壊さずに新機能を alpha で出すこと** にある。pvectl は全体が `v1alpha1` であり、その動機はまだ発生していない。

## 決定内容

### 1. グループ分割は M3 まで行わない

M1〜M2 は `pve.io/v1alpha1` 単一で進める。分割の基準は **「書き込みの意味論が core と異なるか」** とし、最初の分割は SDN（pending → reload の二段階コミット）で発生する。

将来のグループ（M3 以降に確定する予定）

| グループ | kind | 書き込みの性質 |
|---|---|---|
| `pve.io/v1alpha1`（core） | VirtualMachine, Container | ゲスト単位の PUT |
| `sdn.pve.io/v1alpha1` | Zone, VNet, Subnet | pending + reload の二段階 |
| `access.pve.io/v1alpha1` | User, Token, Role, ACL, Pool | 集合の調停（差分ではなく set） |
| `storage.pve.io/v1alpha1` | Storage | クラスタ全体設定 |

分割の唯一の実利は、core が `v1` に到達した後も未成熟なグループを alpha のまま壊せることである。逆に言えば、その必要が生じるまで分割しない。

### 2. Registry は今すぐ GVK キーにする

分割を M3 に遅らせても、Registry の内部表現だけは先に直す。後から変えると全 kind のテストに波及するためである。

- マニフェスト解決: `(group, version, kind)` の完全一致
- コマンドライン解決: 短縮名 → GVK の別マップ。複数 GVK に衝突したら曖昧エラー
- 現在の `ForObject` における apiVersion の文字列比較を GVK 比較に置き換える

### 3. kind は Proxmox の型ごとに分割しない

bpg は `storage_nfs` / `storage_lvm` / `sdn_zone_vlan` / `sdn_zone_evpn` … と型ごとに別リソースを切っているが、これは Terraform のスキーマが union type を表現しづらいことに起因する実装都合である。

pvectl は YAML の discriminator で 1 kind に束ねる。

```yaml
kind: Storage
spec:
  type: nfs
  nfs:
    server: 10.0.0.10
    export: /export/pve
```

`spec.type` に対応するサブ構造以外が存在した場合はバリデーションエラーとする。これにより kind 数が抑えられ、`pvectl get` のヘルプ出力が読める規模に保たれる。

### 4. オブジェクト粒度は 1 リソース = 1 ドキュメント

`kind: Node` が配下の VM を抱えるような大きな単位にはしない。apply / diff / delete がオブジェクト単位で動くため、粒度を上げると冪等性が壊れる。まとめたい需要はマルチドキュメントファイルとディレクトリ apply で満たす（実装済み）。

Ansible が `proxmox_disk` / `proxmox_nic` を VM と別モジュールにしているのは命令的・タスク単位のモデルゆえであり、宣言的な pvectl では disk / network は VM spec の内側に置く（現状維持）。

### 5. Handler の Apply / Delete はオプショナル能力に降格

`kind: Node` は宣言して作れない。現在の `resource.Handler` は `Apply` / `Diff` / `Delete` を必須にしているため、読み取り専用 kind を表現できない。`Starter` / `Stopper` と同じ能力インターフェース方式に揃える。

- **必須**: `Kind` / `GVK` / `Aliases` / `Columns` / `Get` / `List` / `Describe`
- **オプショナル**: `Applier`（`Apply` + `Diff`）、`Deleter`（`Delete`）、`Starter`、`Stopper`

未実装の動詞は「この kind は apply に対応していません」と明示的に失敗する。

### 6. clone と完全宣言は排他にする

先行例はいずれも「同じ型に clone と完全宣言を同居させられない」と判断している。

| プロジェクト | 手法 |
|---|---|
| KubeVirt | 別 kind `VirtualMachineClone` |
| bpg | 別リソース `proxmox_cloned_vm`（EXPERIMENTAL） |
| Kubemox | 同一 kind 内で CEL による型レベル排他（`has(self.template) \|\| has(self.vmSpec)` かつ両立不可） |
| pvectl（現状） | 同居 + create-only + 更新時は警告して無視 |

pvectl は **Kubemox 方式**を採る。YAML では最も安く、kind を増やさずに済む。

- `spec.clone` があるとき `spec.disks` は宣言できない（クローン元から継承するため）
- `spec.clone` / `spec.fullClone` / `spec.pool` が作成時のみ有効である点は維持する。ただし **更新時に警告して無視するのをやめ、live と値が異なる場合はエラーにする**。「宣言したのに黙って効かない」は最も分かりにくい失敗であるため

### 7. モデル化していない API フィールドのエスケープハッチ

Kubemox の `additionalConfig` に倣い、`spec.raw` を置く。

```yaml
spec:
  raw:
    hookscript: "local:snippets/hook.pl"
    args: "-cpu host,+vmx"
    hotplug: "disk,network,usb"
```

- `spec.raw` のキーは Proxmox API のフラットな設定キーそのもの。pvectl は検証せず素通しする
- 宣言キーのみ管理の原理（ADR-005）はそのまま適用され、`raw` に書いたキーだけが比較・更新の対象になる
- 型付きフィールドと同じ API キーを `raw` に書いた場合はエラー（二重管理の防止）
- これにより「`qm` / API の全フィールドを網羅する」作業が必須ではなくなる。利用頻度の高い項目を順次、型付きフィールドへ昇格させる

### 8. status は spec と非対称に扱う

k8s の status サブリソースに倣い、status はサーバー所有とする。

| 動詞 | status |
|---|---|
| `get -o yaml` | 含む（観測用） |
| `export` | 落とす（適用用） |
| `apply` | マニフェスト中の `status` を無視する（エラーにしない） |

## 結果

- ✅ M1〜M2 の実装コストが増えない（グループ分割の移行コストを M3 まで繰り延べる）
- ✅ 読み取り専用 kind（Node）と、apply 可能な kind を同じレジストリに載せられる
- ✅ `spec.raw` により、API フィールドの網羅が新 kind 追加の前提条件でなくなる
- ⚠️ `spec.raw` は型検証を迂回するため、誤ったキーは Proxmox API のエラーとして返る。エラーメッセージの読みにくさは許容する
- ⚠️ clone 更新時の「警告して無視」→「エラー」は破壊的変更。v1alpha1 のうちに実施する

## 却下した代替案

- **最初から 4〜5 グループに分割**: 分割の実利（成熟度の異なる版を独立に進める）は core が `v1` に達するまで発生しない。M1 時点の分割は移行コストだけを先払いすることになる
- **bpg 型の「型ごと kind 分割」**: kind 数が 20 を超え、動詞のヘルプ出力が読めなくなる。YAML の discriminator で十分に表現できる
- **全 API フィールドの型付きモデル化**: Proxmox API の設定キーは版ごとに増え続けるため終わらない。エスケープハッチ + 昇格のほうが現実的
- **clone を別 kind に分離（KubeVirt / bpg 方式）**: テンプレートからの VM 量産という主用途に対し、1 台につき 2 オブジェクトは冗長。`spec.clone` は命令ではなく「この VM はこのテンプレート由来である」という出自の宣言として扱う
