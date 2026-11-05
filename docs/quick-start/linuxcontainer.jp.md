# LinuxContainer（LXC）

[English](linuxcontainer.md)

`LinuxContainer` のマニフェストと、`apply` がそれに対して行うことの参照資料。
宣言したキーだけを扱う規則、`diff`、dry-run、`spec.runStrategy`、秘密情報、
`spec.raw` について [manifests.jp.md](manifests.jp.md) に書いたことは、ここでも
そのまま成り立つ。このページはコンテナで違う点を扱う。設計の記録は
[ADR-010](../adr/ADR-010-linux-container.md) にある。

コマンドラインの `TYPE` は `ct`・`lxc`・`linuxcontainer`・`linuxcontainers` の
いずれでもよい。

```bash
pvectl get ct
pvectl get ct web-01 -o yaml
pvectl describe ct web-01
pvectl start ct web-01
pvectl delete ct web-01
```

## 最初のコンテナ

```yaml
# ct.yaml
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata:
  name: web-01              # コンテナのホスト名
spec:
  targetNode: pve1
  unprivileged: true        # 必須
  osTemplate:
    volid: local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst
  resources:
    cores: 1
    memory: 512
  rootfs:
    storage: local-lvm
    size: 8G
  networks:
    - name: net0
      bridge: vmbr0
      ip: dhcp
```

```bash
pvectl apply -f ct.yaml --dry-run=server   # テンプレートがストレージに在るかを報告する
pvectl apply -f ct.yaml
```

すべてのフィールドを注釈付きで並べた例は
[examples/lxc.yaml](../../examples/lxc.yaml) にある。

## フィールド

| フィールド | |
|---|---|
| `metadata.name` | ホスト名。`spec.hostname` は無い（名前は 1 つ、解決のキーも 1 つ） |
| `targetNode` | 必須・不変。`apply` はコンテナを移動しない（`migrate` を使う） |
| `vmid` | 任意。書かなければ空いている次の番号。不変 |
| `unprivileged` | **必須**。`true` か `false`。作成時だけ（後述） |
| `osTemplate` | 作成時だけ。`volid`、または `url` + `sha256` + `storage`（後述） |
| `pool` | 作成時だけ。後の apply で別の pool を書くとエラー |
| `description` | |
| `resources.cores` / `memory` / `swap` | memory と swap は MiB。`swap: 0` は swap なし |
| `rootfs` | `storage` と `size`。大きくすると resize で広げる。小さくする・ストレージを変えるのはエラー |
| `mountPoints[]` | `name`（`mp0`、`mp1` …）、`storage`、`size`、`path`。広げ方は `rootfs` と同じ |
| `networks[]` | `name`（`net0` …）、`bridge`。任意で `interface`、`ip`、`gw`、`tag`、`hwaddr` |
| `nameserver` | |
| `features` | `nesting`、`keyctl`、`fuse`、`mknod`。書いたフラグだけを管理する |
| `root` | `passwordFrom`、`sshKeys`。作成時だけ送り、読み出さない（後述） |
| `runStrategy` | VM と同じく `Manual`（既定）・`Always`・`Halted` |
| `tags` | |
| `raw` | pvectl がまだ型を持たない API キー（bind mount など） |

NIC のサブキー:

- `interface` の既定は、スロット `net<N>` に対して `eth<N>`。書いた NIC の中の
  既定値であって、pvectl が NIC を足すことはない
- `ip` は `dhcp`、`manual`、または `10.0.0.50/24` のような CIDR
- `tag` は VM の NIC と同じ規則に従う。1〜4094 は VLAN タグ、`0` は「タグなし」の
  宣言で live のタグを外す。書かなければタグは管理外
- `hwaddr` は Proxmox が生成する。書いたときだけ比較し、更新では live の MAC を
  引き継ぐ。bridge を変えても MAC アドレスは変わらない

## `unprivileged` は必須

Proxmox の API は `unprivileged` を省くと **privileged** のコンテナを作る。Web UI の
既定は unprivileged であり、しかも作成後は変えられない。pvectl はマニフェストに
無い値を補わないので、書くことを求める。マニフェストの値が live と食い違えば、
黙って見過ごさずにエラーにする。

`get -o yaml` は `unprivileged` を常に出力する（`false` も出す）。

## `osTemplate`: テンプレートの 2 つの書き方

```yaml
osTemplate:
  volid: local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst
```

は、ノードのストレージに既に在るテンプレートを使う。この書き方では pvectl は何も
取り寄せない。`--dry-run=server` は、ファイルが無ければ警告する。

```yaml
osTemplate:
  url: https://download.example.test/debian-12-standard_12.7-1_amd64.tar.zst
  sha256: <64 桁の 16 進数>
  storage: local
```

は、pvectl に取り寄せさせる。ファイルは `targetNode` の `storage` に
`pvectl-<sha256 の先頭 16 桁>-<URL のファイル名>` という名前で置かれる。ハッシュが
名前に入るので、同じ URL の中身が差し替わっても別の名前になり、既に在る名前の
中身は宣言どおりとみなせる。取り寄せはその名前が無いときだけ行い、ハッシュの照合は
Proxmox 自身が行う。

- 取り寄せに失敗したら何も作らない。取り寄せたテンプレートを pvectl は消さない。
  ストレージの掃除は利用者が行う
- `--dry-run=server` は取り寄せが必要かを報告し、取り寄せない
- 大きなテンプレートは既定の `--timeout`（5 分）に収まらないことがある。待ち時間が
  尽きても、ノードでは取り寄せのタスクが続いている可能性がある。`--timeout` を
  延ばしてもう一度 apply する。ファイルがストレージに置かれた後は取り寄せない
- トークンにはストレージへの `Datastore.AllocateTemplate` が要り、Proxmox VE の版に
  よってはノードへの `Sys.Audit` / `Sys.Modify` も求められる。取り寄せが拒否
  （HTTP 403）されたら、エラーにこれらの権限名を添える。正確な組み合わせは
  実機でまだ確かめていない

`osTemplate` は作成元の宣言である。Proxmox はこれを記録しないので、更新では比較
せず、必須なのはコンテナがまだ無いときだけで、`get -o yaml` も出力しない。
`get` で書き出したマニフェストは `osTemplate` 無しでそのまま apply できる。

## root の初期認証

```yaml
root:
  passwordFrom: env:CT_ROOT_PASSWORD   # または file:/path/to/file
  sshKeys:
    - ssh-ed25519 AAAA... admin@example.test
```

Proxmox は root のパスワードと SSH 鍵を作成時にしか受け付けず、config にも残さない。
そのため pvectl はこれらを 1 回だけ送り、`passwordFrom` はコンテナを作る直前に
だけ解決し、比較・更新・書き出しは行わない。作成時のマニフェストをそのまま
apply し直すと「`spec.root` は変えなかった」という警告が出て、秘密の参照先が
無い環境でも止まらない。

## 書き出し

```bash
pvectl get ct web-01 -o yaml > web-01.yaml
pvectl get ct -o yaml > all.yaml
pvectl apply -f web-01.yaml      # unchanged
```

名前を付けた形も付けない形も round-trip する（出力を apply すると `unchanged`）。
表の出力（`get ct`、`-o wide`）はクラスタのリソース一覧を 1 回読むだけである。
名前を付けない `get` の `yaml` と `json` は、加えて各コンテナの config を読む
（コンテナ 1 つにつき API 呼び出し 1 回）。

## exec と migrate

- `exec` には対応しない。Proxmox VE の REST API にはコンテナの exec が無い
  （`pct exec` はノードの上でしか動かない）。pvectl は試さずにそう伝える
- `migrate` はコンテナが停止しているときだけ行える。`--online` を付けたり、
  稼働中のコンテナを指定したりするとエラーになり、先に停止するよう案内する

## まだ無いもの

- `spec.template: true`（ゲストのテンプレート化）。VM とコンテナで同時に入れる
- `-o wide` の IP 列
- 稼働中のコンテナの restart 移行、bind mount とデバイスの透過の型付きフィールド
  （`raw` で書ける）
