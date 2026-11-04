# ADR-010: LXC は kind `LinuxContainer` とし、OS テンプレートは内容ハッシュで冪等に取り寄せる

- **状態**: 採用（2026-10-05）
- **決定**: M2 で LXC を `kind: LinuxContainer`（`pve.io/v1alpha1`）として追加する。作成元の OS テンプレートは `spec.osTemplate` に **volid の直接指定**か **`{url, sha256}` の参照**で書き、後者は pvectl が内容ハッシュから volid を決定的に導出し、無ければ `download-url` で取り寄せる。`spec.unprivileged` は**必須**とする。template 化（`spec.template: true`）・ライブ以外の移行・IP 列は M2 に含めない

## 文脈

PRD の M2 は「`Container`（LXC）: `FindGuest` の一般化、`ostemplate`/`rootfs`/`mountPoints`」である。2026-09-27〜30 の対話で次が裁定済みだった。

- kind 名は `LinuxContainer`。`Container` は Kubernetes の Pod 内 container と混同し、`LXC` は略語の kind になる
- 持続する状態は宣言、1 回限りの操作は動詞（ADR-005 §3）
- `spec.template: true` は「template 化フラグ」として VM / LXC 共通に予約する（不可逆）
- image 参照は download-url 方式（Kubernetes の `image:` に最も近い）。ローカルパスをマニフェストに書く案は live と比較できないので採らない

LXC が VM と違う点は次のとおり。

| 観点 | VM（qemu） | LXC |
|---|---|---|
| 作成元 | 空 / clone | **OS テンプレート（vztmpl の tar.zst 等）から create** が主流 |
| 名前 | config の `name` | config の `hostname`（`/cluster/resources` の `name` に出る） |
| 初期認証 | cloud-init（`ciuser` / `cipassword` / `sshkeys`） | create 時のみの `password` / `ssh-public-keys`。**config に残らず読み出せない** |
| ディスク | `scsi0` 等、`PUT /resize` | `rootfs` / `mpN`、`PUT /resize`（`disk=rootfs\|mpN`） |
| NIC | `net0: virtio,bridge=…` | `net0: name=eth0,bridge=…,ip=…,gw=…`（**IP をコンテナ側で直接持つ**） |
| 権限モデル | — | `unprivileged`。**API の既定は 0（privileged）**、create 後は変更不可 |
| exec | guest agent | **REST に無い**（`pct exec` はノードローカル） |
| 移行 | ライブ移行可 | 停止中のみ。稼働中は restart 移行（一旦停止） |

## 決定内容

### 1. kind と名前解決

- `GVK = pve.io/v1alpha1, LinuxContainer`。短縮名は `lxc` / `ct` / `linuxcontainer` / `linuxcontainers`
- `metadata.name` は LXC の `hostname` に対応させる。別フィールドの `hostname` は持たない（名前が 2 つになると `get` の名前解決が曖昧になる）
- 名前解決は `/cluster/resources` を `type == lxc` に絞る。VM と同名の LXC が並存しても互いに干渉しない（`vm` 側の `findQemu` と対称）
- 共通化は行わない。ゲスト検索と電源状態の収束は `internal/resource/lxc` に**複製**する。kind が 3 つ目になった時点で抽出する（2 つで抽出すると vm パッケージへの変更が並列作業を直列化させる）

### 2. spec

```yaml
apiVersion: pve.io/v1alpha1
kind: LinuxContainer
metadata:
  name: web-01                 # = hostname
spec:
  targetNode: pve1             # 必須・不変
  vmid: 200                    # 任意・不変
  unprivileged: true           # 必須・create-only（§3）
  osTemplate:                  # create-only・出自の宣言（§4）
    url: https://download.example/debian-12-standard_12.7-1_amd64.tar.zst
    sha256: 3f2a…
    storage: local
  pool: lab                    # create-only
  description: ""
  resources:
    cores: 2
    memory: 1024               # MiB
    swap: 512                  # MiB
  rootfs:
    storage: local-lvm
    size: 8G                   # 拡張のみ。縮小はエラー
  mountPoints:
    - name: mp0
      storage: local-lvm
      size: 32G
      path: /data
  networks:
    - name: net0
      interface: eth0          # 省略時は slot 番号から eth0, eth1 …
      bridge: vmbr0
      ip: dhcp                 # dhcp | CIDR | manual
      gw: 10.0.0.1
      tag: 10
  nameserver: 10.0.0.53
  features:
    nesting: true
    keyctl: true
  root:                        # create-only・write-only（§5）
    passwordFrom: env:CT_ROOT_PASSWORD
    sshKeys:
      - ssh-ed25519 AAAA…
  runStrategy: Manual          # VM と同じ 3 値
  tags: [web]
  raw: {}                      # ADR-006 §7 のエスケープハッチ
```

- 宣言したキーだけを扱う（ADR-005）。`features` は書いたサブキーだけを比較し、書かなかったサブキーは live に残す
- NIC の `hwaddr` は Proxmox が生成する値なので、マニフェストに書いた場合だけ比較する（VM の `macaddr` と同じ）
- config の PUT は `netN` / `features` の値を丸ごと置き換える。そのため更新時は、**書いていない live のサブキー（`hwaddr` など）を引き継いで** PUT する。引き継がないと NIC を更新するたびに MAC が振り直される
- `interface` の既定値は、VM の `model: virtio` と同じく「書いた NIC エントリの中のサブキーの既定」であり、NIC そのものを補うものではない

### 3. `unprivileged` は必須にする

Proxmox の API は `unprivileged` を省略すると privileged コンテナを作る。WebUI は既定で unprivileged を選ぶため、UI に慣れた利用者ほど API で privileged を作ってしまう。しかも create 後は変更できない。

3 案を比べた。

| 案 | 評価 |
|---|---|
| Proxmox の既定に任せる | 原則どおりだが、privileged が黙って作られる |
| pvectl が既定で `true` を送る | 安全側だが、マニフェストに無い値を pvectl が送ることになり、ADR-005 の原則から外れる |
| **必須にする** | 原則を保ったまま事故を防げる。書く量は 1 行 |

- `get -o yaml` は常に `unprivileged` を出力する（live に `unprivileged` が無ければ `false`）。round-trip が成り立つ
- 更新時に manifest と live が食い違ったらエラーにする（変更不可のため。警告に留めると apply が毎回 `configured` を名乗れない）

### 4. `spec.osTemplate` — 2 つの書き方

```yaml
osTemplate:
  volid: local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst
# または
osTemplate:
  url: https://…/debian-12-standard_12.7-1_amd64.tar.zst
  sha256: <64 桁>
  storage: local
```

- `volid` と `url` は排他。`url` のときは `sha256` と `storage` が必須
- **volid の導出**: `{storage}:vztmpl/pvectl-{sha256 の先頭 16 桁}-{url の basename}`。内容ハッシュを名前に含めるので、同じ URL の中身が差し替わっても別ファイルとして扱われ、名前が同じなら中身も同じとみなせる。API ではストレージ上のファイルのハッシュを読めないため、名前で内容を保証する
- **取り寄せ**: `GET /nodes/{targetNode}/storage/{storage}/content?content=vztmpl` に導出 volid があれば何もしない。無ければ `POST /nodes/{targetNode}/storage/{storage}/download-url`（`content=vztmpl`, `filename`, `url`, `checksum`, `checksum-algorithm=sha256`）。照合は Proxmox 側が行い、不一致ならタスクが失敗する
- 取り寄せ先は `targetNode`。ストレージが共有でない場合に、作成するノードから読めることを保証するため
- `--dry-run=server` は「取り寄せが必要か」を報告し、取り寄せない
- `osTemplate` は **出自の宣言**であり、更新時に live と突き合わせない（LXC の config は作成元を記録しない。VM の `spec.clone` と同じ扱い）。`get -o yaml` は `osTemplate` を出力しない
- したがって `osTemplate` が必須なのは **create の経路だけ**である。共通の検証で必須にすると、`get -o yaml` の出力を apply する round-trip が成り立たない
- 取り寄せたテンプレートを pvectl は消さない。ストレージの掃除は pvectl の責務ではない
- 必要な権限（実機で確定させる）: `download-url` には対象ストレージへの `Datastore.AllocateTemplate` が要り、加えてノードへの `Sys.Audit` / `Sys.Modify` を要求する版がある。不足時は 403 をそのまま返し、メッセージに権限名を添える

M3 で VM のディスクに `image: {url, sha256}` を足すときは、§4 の導出規則と取り寄せの流れを再利用する（content 種別が `import` になる点だけが異なる）。

### 5. 初期認証は `spec.root` に分ける

LXC の `password` / `ssh-public-keys` は create 時にしか受け付けられず、config に残らない。cloud-init ではないため `spec.cloudInit` には入れない。

- `spec.root.passwordFrom`（`env:` / `file:`）と `spec.root.sshKeys` は create 時にだけ送る。差分の対象にも更新の対象にもしない（VM の `cloudInit.passwordFrom` と同じく write-only）
- `get -o yaml` は `root` を出力しない
- 更新時に `root` が書かれていても警告に留める（manifest を作成時と同じまま apply し続ける運用を壊さないため）

### 6. 動詞ごとの対応

| 動詞 | LXC での扱い |
|---|---|
| `get` / `describe` / `apply` / `diff` / `delete` / `start` / `stop` | 対応（`Handler` / `Applier` / `Deleter` / `Starter` / `Stopper`） |
| `exec` | 非対応。「LXC には REST の exec が無い」と明示するエラーにする |
| `migrate` | 停止中のみ対応。稼働中または `--online` 指定はエラーとし、先に停止するよう案内する。restart 移行は後の版で検討する |

### 7. M2 に含めないもの

- **template 化（`spec.template: true`）**: VM と同時に入れるべき共通機能であり、v0.2.x で別 ADR とする
- **`-o wide` の IP 列**: LXC は agent 不要の `GET …/lxc/{vmid}/interfaces` を持つが、ADR-009 の IP 列と同じ版で揃える
- **restart 移行**、**bind mount / デバイス透過**（`raw` で書ける）

## 結果

- ✅ kind 追加が `internal/resource/lxc` + `Register` 1 行 + `internal/api` のメソッド追加に閉じる（ADR-001 の見積りどおり）
- ✅ OS テンプレートの取り寄せが API だけで閉じ、内容ハッシュにより冪等になる。M3 の VM image にもそのまま流用できる
- ✅ privileged コンテナが黙って作られる経路が無くなる
- ⚠️ `unprivileged` 必須は、他ツールから移ってきた利用者にとって 1 行の手間になる
- ⚠️ `download-url` の権限要件が版によって異なる可能性があり、実機で確かめるまで文書に断言できない
- ⚠️ ゲスト検索と電源状態の収束が vm と lxc に重複する。3 つ目の kind で抽出する

## 却下した代替案

- **kind 名 `Container` / `LXC`**: 文脈に記載のとおり
- **`osTemplate` を volid 文字列のみにする**: 閉域では十分だが、Kubernetes の `image:` に相当する体験が得られず、M3 で同じ仕組みを別途作ることになる
- **URL の basename をそのまま volid にする**: 同じ URL の中身が差し替わった場合に検出できない
- **ストレージ上のファイルを毎回ダウンロードして照合する**: apply のたびに転送が発生し、冪等な no-op にならない
- **`hostname` を `metadata.name` と別フィールドにする**: 名前解決のキーが 2 つになる
- **共通処理を先に `internal/resource/guest` へ抽出する**: vm パッケージへの変更が LXC の並列実装を直列化させる。kind が 2 つの段階では抽出の形も定まらない
