# ADR-011: IaC 化の範囲・置き場所・権限の前提を先に決め、export は v1.0 で出す

- **状態**: 提案（2026-11-16、草稿）
- **決定**: v1.0 で `export`（ADR-005 §5）を出すまでに、Proxmox VE の設定をどこまでマニフェストにするかを決め、kind をマイナーバージョンで順に足す。この ADR は、利用者が決めた 4 点と、それを形にする提案と、利用者が決める論点を分けて書く。提案の柱は 3 つある。権限は「`PVEAdmin` で書ける・`Administrator` が要る・root@pam でしか書けない」の 3 段に分けて表に持ち、書き込む前に不足を警告する。置き場所は API の上で効く範囲で分ける。書き込み専用の値は書き出さない

## 文脈

### 利用者の決定（2026-11-16）

1. pvectl の大きな目的は、PVE クラスタをマニフェストで IaC 化することである。利用者は PVE を立てた後に API トークンを発行して pvectl をつなぐ。つないだ時点のクラスタを、VM と LXC を含めてまとめてマニフェストに起こすコマンドが欲しい
2. そのコマンドは ADR-005 §5 の `export` であり、**v1.0 で出す**。それまでに全リソースの IaC 化と構造化を議論し、kind をマイナーバージョンで実装していく
3. トークンは `PVEAdmin` で使うことを推奨し、前提にする。権限が足りないときは、試しの実行（dry-run など）で警告を出す
4. 秘密の値は伏せるか暗号化し、GitHub などに載せられる形を検討する（方式は未決）

### 既に決まっていること

| 決定 | 出典 |
|---|---|
| マニフェストに書いていないフィールドは存在しない（宣言キーのみ管理） | ADR-005 §2 |
| export の正しさ = 出力をそのまま apply して全件 `unchanged` | ADR-005 §5 |
| クラスタ自体の構築・ノード追加は非目標。PRD はさらに広く「クラスタ自体の構築・ノード管理」を非ゴールとする | ADR-005 非目標、`docs/prd.md` §2 |
| グループは書き込みの性質で分ける（core / `sdn.pve.io` / `access.pve.io` / `storage.pve.io`） | ADR-006 §1 |
| access グループは差分ではなく集合で調停する（ACL を含む） | ADR-006 §1、PRD M4 |
| 1 リソース = 1 ドキュメント。Proxmox の型ごとに kind を割らない。status は export で落とす | ADR-006 §3・§4・§8 |
| 所有権の範囲はプールで表し、推奨の ACL は `/pool/<pool>` に `PVEVMAdmin` 相当 | ADR-009 §3 |
| 版の順: M3 = Storage / Network / Snapshot、M4 = Pool / User(+token) / ACL / HA、その後 v1.0 | `docs/prd.md` §4 |

この PR の ADR-005 追補は、§3 の「`startOnBoot` を `runStrategy` に置き換える」を改め、onboot を `spec.startOnBoot` に戻す（案 A）。分けた後の `get -o yaml` は `startOnBoot` を出し、`runStrategy` は出さない（`Manual` と同じ）。

### Proxmox VE の権限（2026-11-16 に一次資料で確認）

git.proxmox.com の各リポジトリの HEAD を読んだ。PVE 8.x / 9.x の配布版との差は見ていない。

**組み込みロール。** `PVEAdmin` は各分類の admin / user / audit の権限を持つが、root 用の分類（`Sys.PowerMgmt`、`Sys.Modify`、`Sys.Incoming`、`Sys.AccessNetwork`、`Realm.Allocate`）と `Permissions.Modify`、`Mapping.Modify` を持たない（`pve-access-control` の `src/PVE/AccessControl.pm`）。

**API の要求。** 各 API 定義の `permissions` から抜き出した。

| API | 要求 | `PVEAdmin` |
|---|---|---|
| `POST /storage`・`PUT /storage/{id}` | `/storage` に `Datastore.Allocate` | 書ける |
| `PUT /cluster/sdn`（SDN の反映） | `/sdn` に `SDN.Allocate` | 書ける |
| `POST /cluster/ha/resources` | `/` に `Sys.Console` | 書ける |
| `POST・PUT /nodes/{node}/network` | `/nodes/{node}` に `Sys.Modify` | 書けない |
| `POST /cluster/backup` | `/` に `Sys.Modify`。`tmpdir`・`dumpdir`・`script` は root@pam だけ | 書けない |
| `PUT /cluster/firewall/options` | `/` に `Sys.Modify` | 書けない |
| `POST・PUT・DELETE /access/roles` | `/access` に `Sys.Modify` | 書けない |
| `PUT /access/acl` | 書くパスに `Permissions.Modify`。無くても `/vms/<id>` は `VM.Allocate`、`/storage/<id>` は `Datastore.Allocate`、`/pool/<id>` は `Pool.Allocate` で書ける。ただし付ける role の権限が自分の権限の範囲にあり、伝播を付けるなら自分の権限も伝播付きであること | 一部だけ |
| `POST /nodes/{node}/storage/{storage}/download-url` | 保存先に `Datastore.AllocateTemplate` があり、そのうえで「`/` に `Sys.Audit` と `Sys.Modify`」か「`/nodes/{node}` に `Sys.AccessNetwork`」のどちらか | 書けない |
| `GET /access/permissions` | だれでも、自分（と自分のトークン）の実効権限を読める | 読める |

**root@pam でしか書けない設定。** API のコードが呼び出し元を `root@pam` と比べて拒むものがある。`Administrator` のトークンでも通らない見込みである（トークンの ID が `root@pam` と一致しないため。未確認）。`/access/permissions` の応答にも現れない。

- LXC: `features` の nesting 以外（`keyctl`・`fuse`・`mknod` など）、bind mount などのマウントの種類、`devN`、`hookscript`（`pve-container` の `src/PVE/LXC.pm`）
- VM: `args` など、権限の分類に入っていない設定（`qemu-server` の `PVE/API2/Qemu.pm`「only root can set」）。VM の `startup` は `/` に `Sys.Modify` が要る

**pvectl の既存の機能への影響。**

- LinuxContainer の `osTemplate: {url, sha256}` は download-url を使うので、`PVEAdmin` では失敗する。ADR-010 は「版によっては要る」と書いていたが、現行の版では必ず要る。M3 で予定する VM の `image: {url, sha256}` も同じ経路を使う
- ADR-010 の spec の例（`features: {nesting: true, keyctl: true}`）と、ADR-006 §7 の `spec.raw` の例（`args`）は、root@pam でしか書けない
- `internal/resource/lxc/apply.go` の 403 の案内は「`/nodes/<node>` に `Sys.Audit` と `Sys.Modify`」と書いており、パスが一次資料と違う

## 提案（利用者の承認を待つ）

### 1. 権限は 3 段に分け、フィールドの単位で表に持つ

| 段 | 意味 | 例 |
|---|---|---|
| `PVEAdmin` | 推奨の前提で書ける | ゲストの大半、Storage、SDN、Pool、HA、User、Group |
| `Administrator` | `PVEAdmin` に無い権限が要る | ノードの Network、バックアップのジョブ、クラスタのファイアウォール、Role、Realm、`/` などへの ACL、download-url、VM の `startup` |
| root@pam | トークンでは書けない | LXC の `features`（nesting 以外）・bind mount・`devN`・`hookscript`、VM の `args` |

- 推奨の構成は、**利用者とトークンの両方**に `/` で `PVEAdmin` を付けることにする。privilege separation ありのトークンの実効権限は、利用者とトークンの権限の積になるためである
- ADR-009 の推奨（`/pool/<pool>` に `PVEVMAdmin`）は、借りて返すエージェント（P5）のための狭い構成として残す。クラスタ全体を IaC にする使い方と、P5 の使い方で推奨を分ける
- root@pam の段は、ADR-005 追補の未決「認証方式によって可否が変わる動詞をどう扱うか」と同じ論点として扱う

### 2. 書き込む前に、不足を警告する

- `diff` と `apply --dry-run=server` は、入力のマニフェストに含まれる kind と宣言されたフィールドについてだけ、1 の表から必要な権限とパスを引く
- パスごとに `GET /access/permissions?path=<パス>` を問い合わせる。パスを付けない応答は、既定のパスと ACL に書かれたパスしか含まず、継承を pvectl が計算することになるためである
- 足りなければ「どの kind の、どのフィールドに、どのパスで、どの権限が足りないか」を警告する。root@pam の段のフィールドは、トークンの接続では書けない旨を警告する

### 3. kind の候補と、効く範囲

| 効く範囲 | kind の候補 | グループ（ADR-006） | 権限の段 |
|---|---|---|---|
| ゲスト 1 台 | VirtualMachine、LinuxContainer（済み） | core | `PVEAdmin`（一部のフィールドは上の段） |
| クラスタ全体 | Storage | `storage.pve.io` | `PVEAdmin` |
| | Zone、VNet、Subnet（SDN） | `sdn.pve.io` | `PVEAdmin` |
| | Pool | `access.pve.io` | `PVEAdmin` |
| | HA の resource / rule | 未定 | `PVEAdmin` |
| | バックアップのジョブ、クラスタのファイアウォール | 未定 | `Administrator` |
| 認証と認可 | User、Group | `access.pve.io` | `PVEAdmin` |
| | Token | `access.pve.io` | `PVEAdmin`（秘密は作成時に 1 回しか返らない） |
| | Role、Realm | `access.pve.io` | `Administrator` |
| | ACL | `access.pve.io` | `/vms`・`/storage`・`/pool` の下は `PVEAdmin`、それ以外は `Administrator` |
| ノード 1 台 | Network（NIC・bridge・bond） | 未定 | `Administrator` |

どの kind をどの版に入れるかは、この表では決めない。PRD の M3 の「Network」が SDN を指すのか、ノードの NIC を指すのかは読み分けられない。ノードの NIC は PRD の非ゴール「ノード管理」に当たるおそれもある。PRD の M3 にある Snapshot は動詞が主体なので、この表に入れていない。

### 4. 置き場所の方針（v1.0 の export）

- ゲストはノードの下に置かず、効く範囲で分ける。案は `<context>/{guests,cluster,access,nodes/<node>}/` で、1 ファイルに 1 ドキュメントとする
- 一番上に context（クラスタ）の層を置く。2 つのクラスタを同じリポジトリに書き出しても衝突しないためである
- ゲストを移行しても、ファイルのパスは変わらない。ただし `spec.targetNode` はファイルの中身なので、`migrate` の後や HA が動かした後は、ファイルの書き換えが要る（いまの apply は、live と食い違うと失敗する）

v1.0 までに解くべき問題が、少なくとも 5 つある。

1. いまの `apply -f <ディレクトリ>` は、入れ子のディレクトリを読まない（`internal/runtime/decoder.go`）。入れ子を読むようにしても名前順に読むので、ゲストがノードの bridge より先に適用されうる
2. ファイル名が一意にならない場面がある。Proxmox は同じ名前の VM を許す。ノードの `vmbr0` はどのノードにもある。ACL には自然な名前が無い。HA の `vm:100` は `:` を含む。macOS の既定のファイルシステムは大文字と小文字を区別しない
3. 名前の無い VM の `metadata.name` が決まっていない
4. 見る権限の無いものは一覧の API が返さないので、権限の狭いトークンで export すると一部だけが出る。export は、読めなかった範囲を報告する必要がある
5. 書き出せなかった種類（kind の無いもの）の一覧を出す必要がある。黙って省くと、IaC 化が済んだと誤解されるためである

### 5. 書き込み専用の値と、見せたくない値

- パスワード、トークンの秘密、秘密鍵などの書き込み専用の値は、export に出さない。いまの `get -o yaml` と同じ扱いである。参照の空欄（`passwordFrom: env:…`）を出すと、参照先が無い環境で apply が失敗し、`unchanged` にならない。どのフィールドを書き出さなかったかは、出力と一緒に報告する
- 見せたくない値を git に載せる方式は、利用者の決定を待つ。候補は、値だけを暗号化する SOPS と age の組み合わせ（`.sops.yaml` の `encrypted_regex` で対象を選べる）、ファイルを丸ごと暗号化する git-crypt、pvectl 自身が暗号を扱う案である
- どの方式でも、名前は隠せない。LXC の `hostname` は `metadata.name` そのもの（ADR-010 §1）で、ファイル名にも出る。SOPS はキー名も暗号化しない
- 伏せる処理（IP を `x.x.x.x` に置き換えるなど）は、不具合の報告のような共有用に限る。伏せた出力を apply すると `unchanged` にならない

### 6. 各版で kind を足すときの決まり

- kind を足す PR は、その kind の「`get -o yaml` の出力をそのまま apply して `unchanged`」のテストを、偽のノード（`test/pvefake`）の e2e で持つ
- kind の説明に、必要な権限の段とパスを書く

## 結果

- ✅ v1.0 の export の前に、各版で round-trip と権限の表を積み上げられる
- ✅ 権限の不足が、書き込みの途中の 403 ではなく、書き込む前の警告として分かる
- ✅ root@pam でしか書けない設定を、`Administrator` で解決すると誤解せずに済む
- ⚠️ `PVEAdmin` の前提では、Network・バックアップのジョブ・Role・Realm・ACL の一部・download-url は書けない
- ⚠️ 権限の表は Proxmox の版で変わりうる。表の誤りは警告の誤りになる
- ⚠️ 置き場所の方針は、apply が入れ子のディレクトリと kind の順序を扱えるまで、そのままでは使えない

## 未決の論点（利用者が決める）

1. **提案 1〜6 を採るか。** とくに、推奨を `/` の `PVEAdmin` にし、ADR-009 の狭い構成と使い方で分けること
2. **普通の `apply` での不足の扱い。** 警告して書き込みを続けるか、書き込む前に止めるか。止める場合、権限の表が古いと、通るはずの操作まで止まる
3. **root@pam でしか書けないフィールドの扱い。** トークンの接続では宣言を拒むか、警告だけにするか、root@pam のパスワード認証を pvectl に足すか（ADR-005 追補の未決と同じ）
4. **ACL の kind の単位。** ADR-006 は集合で調停すると決めている。パスごとに 1 オブジェクトにするか、主体（利用者・グループ）ごとにするか。あわせて、pvectl のトークン自身の権限を削る変更を止めるか
5. **Pool の所属の二重管理。** Pool の kind が所属のゲストを持つと、ゲストの `spec.pool`（作成時だけ有効）と二重になる
6. **`targetNode` の意味。** 移行や HA で動いた後も apply を通すため、作成時だけ有効にするか
7. **ノードの NIC を範囲に入れるか。** PRD の非ゴール「ノード管理」との関係を決める
8. **見せたくない値の方式。** 5 の候補から選ぶ
9. **`osTemplate.url` と、M3 の VM の `image.url`。** `PVEAdmin` で動かないことを文書に書くだけにするか、`/nodes/{node}` に `Sys.AccessNetwork` を足す構成を推奨に含めるか
10. **権限を確かめる動詞を足すか。** kubectl の `auth can-i` に当たる `pvectl auth can-i apply -f <ファイル>`

## 却下した代替案

- **ノードごとにディレクトリを切り、ゲストもその下に置く**: 移行のたびにファイルが動き、git の履歴が途切れる
- **SDN の単位で分ける**: SDN は IaC の対象の一部でしかなく、ストレージやアクセス権の置き場所が決まらない
- **推奨を `Administrator` にする**: 書ける範囲は広がるが、root@pam の段は解決しない。手元から叩く道具に広い権限を持たせることにもなる
- **export に書き込み専用の値の参照を出す**: 参照先が無い環境で apply が失敗し、ADR-005 §5 と両立しない
- **権限の確認を `GET /access/permissions` の 1 回で済ませる**: 応答に入らないパスの継承を pvectl が計算することになり、伝播やプール由来の ACL の規則を作り直すことになる

## 影響する文書とコード

- ADR-005: 追補で「export は v1.0」と onboot の分離を書く（この PR で実施）
- ADR-006 §7・ADR-010 §2: 例の `args` と `keyctl` が root@pam でしか書けないことを追補で注記する
- ADR-010: download-url の権限の記述を、本 ADR の表に合わせて追補する
- `docs/prd.md` §4: v1.0 の行に export を足す（この PR で実施）
- `internal/resource/lxc/apply.go` の 403 の案内と、`docs/quick-start/linuxcontainer{,.jp}.md` の権限の記述: パスを一次資料に合わせて直す（別の PR）
- `docs/quick-start/README{,.jp}.md`: 推奨するトークンの権限（提案 1 が採用されたら書く）
