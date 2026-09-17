# マニフェストと apply の意味論

[English](manifests.md)

`VirtualMachine` のマニフェストと、`apply` がそれに対して実際に行うことの参照
資料。まだ VM を作ったことがない場合は
[クイックスタート](README.jp.md) から読むこと。

## 形

すべてのマニフェストは `{apiVersion, kind, metadata, spec}` であり、kubectl と
同じ形である。YAML が使えるところでは JSON も使え、1 つのファイルに `---` で
区切った複数のドキュメントを置ける。

```yaml
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server          # spec.vmid でピンしない限り、apply が解決に使う同一性
  labels:
    app: nginx
spec:
  targetNode: pve-node1     # 必須、immutable
  ...
```

[examples/vm-full.yaml](../../examples/vm-full.yaml) に `spec` の全フィールドが
注釈付きで並んでいる。ほかの [examples](../../examples/) は典型的な形（直接作成、
クローン、マルチドキュメント）を示す。`pvectl get vm NAME -o yaml` は同じ形を出力し、
その出力を apply すると `unchanged` を報告する。

## ほかのすべてを説明する規則

**マニフェストに書いていないフィールドは、pvectl にとって存在しない。** 比較と
更新の対象は、マニフェストが宣言したキーちょうどそれだけである。サーバー側の
既定値と生成値（`vmgenid`、`digest`、`smbios1`、`boot`、…）は恒久的に無視される。
state ファイルが要らないのも、クローン由来のマニフェストの再 apply が差分ノイズ
だらけの計画ではなく no-op になるのも、この規則のためである。

### フィールドを消しても元の値は戻らない

これは 2-way merge である。pvectl は kubectl の `last-applied-configuration` を
持たないため、「利用者がこのフィールドを消した」と「利用者が最初から設定して
いない」を区別できない。

> マニフェストからフィールドを消すことは **管理をやめる** ことを意味する。
> ライブ値はそのまま残る。

**これは不具合ではなく仕様である**（[ADR-005
§2](../adr/ADR-005-purpose-and-non-goals.md)）。設定を取り消したいときは、行を
消すのではなく望む値に書き換えること。あるいは Web UI や `qm` で、pvectl の外から
変更すること。

## apply の動き

`apply` は `spec.vmid`（ピンされている場合）または `metadata.name` で VM を解決し、
そのうえで:

- **見つからない** → 作成する。直接作成するか、`spec.clone` の後にクローン後の
  設定パスを走らせて、宣言した resources・networks・cloud-init・tags が
  テンプレートの値に優先するようにする
- **見つかった** → マニフェストが宣言したフィールドだけをライブ設定と比較し、
  サーバー由来のノイズ（ディスクのボリューム名、生成された MAC、tag の順序、
  ssh 鍵のエンコード、`cputype` の flags）を正規化して、変わったキーだけを `PUT`
  する。ディスクの拡大は resize 呼び出しになる
- **変化なし** → `unchanged`、書き込みゼロ

dry run は手前で止まる。`--dry-run=client` は API に触らずにデコードと
バリデーションだけを行い `validated` を報告する。`--dry-run=server` はライブ状態を
読み、書き込まずに `created` / `configured` / `unchanged` のいずれになるかを報告
する。`pvectl diff -f` は変わることになる宣言キーを表示し、0（差分なし）・
1（差分あり）・>1（エラー）で終了する。

### 電源状態

設定のパスの後、apply は電源状態を `spec.runStrategy` へ収束させる。

| `runStrategy` | `onboot` | apply |
|---|---|---|
| `Manual`（既定） | 非管理 — キーを生成しない | 電源状態に一切触らない |
| `Always` | `1` | 停止していれば VM を起動する |
| `Halted` | `0` | 起動していれば VM を停止する |

電源の遷移も変化として数えるので、ほかが同一のマニフェストでも VM を動かすときは
`configured` を報告する。`unchanged` は設定 *と* 電源状態の両方が既に一致している
ことを意味する。`--dry-run=server` は遷移を実行せずに報告する。

`get -o yaml` はライブの `onboot=1` を `Always` に、それ以外を `Manual` に写す —
決して `Halted` には写さない。出力を再生した apply が、停止を宣言していない所有者の
VM を停止させないためである。命令的な
[`start` / `stop`](README.jp.md#5-命令的動詞) 動詞は従来どおり使えて、`spec` に
痕跡を残さない。

### ガードレール

検出できる不一致はエラーにする。pvectl はマニフェストを満たすために VM を削除して
作り直すことを決してしない。

| | |
|---|---|
| `spec.vmid`、`spec.targetNode` | immutable。不一致はエラーであり、黙って再作成やマイグレーションをすることはない |
| ディスク | 縮小できない。`storage` と `format` を in-place で変更できない |
| `spec.clone` + `spec.disks` | 排他 — クローンはソースからディスクを継承する |
| `spec.pool` | 作成時のみ。ライブのプールとの不一致はエラーであり、黙った no-op にはしない |
| `spec.clone`、`spec.fullClone` | 作成時のみ有効だが、更新時も警告なしで受理する。Proxmox は出自を記録しないため、突き合わせるライブ値が存在しない。マニフェストに残り続ける出自の宣言として扱うこと |
| `cloudInit.passwordFrom` | write-only。作成時に設定し、diff も update もしない |

## 秘密情報

マニフェストは cloud-init のパスワードを持つのではなく参照するので、コミットしても
安全なままである。

```yaml
spec:
  cloudInit:
    passwordFrom: env:PVE_VM_PASSWORD     # または file:/run/secrets/vmpw
```

`file:` の参照は末尾の改行を切り落とす。参照は apply の実行時に解決される。
`--dry-run=client` は構文を検査し、参照先が無いときは失敗ではなく警告にする。
クライアント dry-run が検証するのはマニフェストであって、それを実行している
マシンではないからである。

値がマニフェストに現れないので比較すべきものが存在しない — これがまさに、
`cipassword` に専用の diff 規則が要らない理由である。長命な認証情報を持つのは
pvectl の設定ファイルだけであり、モード 0600 で保存され、`config view` が伏せる。

## モデル化されていない API フィールド

`spec.raw` は、pvectl にまだ型付きフィールドが無い Proxmox API の設定キーを、
フラットなままそのまま通す。

```yaml
spec:
  raw:
    hookscript: "local:snippets/hook.pl"
    args: "-cpu host,+vmx"
    bios: ovmf
    machine: q35
```

値は検証されない — 誤ったキーは Proxmox API のエラーとして返ってくる。宣言キーだけを
管理する規則はここでも成り立つ。`raw` の下に列挙したキーだけが比較・更新の対象に
なる。pvectl が型付きフィールドから既に生成するキー（`cores`、`scsi0`、`net0`、…）を
宣言するのはエラーである。

## 背景

- [ADR-001](../adr/ADR-001-resource-model.md) — リソースモデルとレジストリ
- [ADR-003](../adr/ADR-003-apply-strategy.md) — 宣言キーの diff、正規化規則、immutable 方針
- [ADR-005](../adr/ADR-005-purpose-and-non-goals.md) — 宣言キーのみの原理、2-way merge のトレードオフ、非目標
- [ADR-006](../adr/ADR-006-api-groups-and-schema.md) — API グループ、スキーマ、`raw` エスケープハッチ
