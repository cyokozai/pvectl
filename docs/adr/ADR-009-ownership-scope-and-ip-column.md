# ADR-009: 所有権の範囲はプールで表し、期限と方針は pvectl の外に置く

- **状態**: 採用（2026-09-27）
- **決定**: 「この CLI で作った VM だけをこの CLI から消せる」という要求に対し、pvectl は **tag や独自の印を発明せず、Proxmox のリソースプールを所有権の範囲として使う**。context 設定に任意の `pool` を持ち、指定があれば作成時に所属させ、`delete` はその外側を既定で拒む。この機能は実験的（明示の feature gate 付き）として出す。有効期限・許可範囲・ssh や NIC の既定値は pvectl の責務ではなく、pvectl を呼ぶ側（スキルやスクリプト）が持つ。あわせて `get vm -o wide` に IP 列を足すが、取得経路は guest agent 1 本であることを明記する

## 文脈

### 新しい利用者像

2026-09 に次の意見を受けた。AI エージェントがミドルウェアの検証に VM を使いたい。スキルから CLI を叩き、template と cpu / memory / disk を許可された範囲内で選び、**この CLI で作った VM だけを CLI から消せる**こと、可能なら一定期間放置で自動削除されること、ssh と NIC の設定が設定ファイルに従って入り、割り当てられた IP をスキルから取得できること。

これは discovery brief（`docs/discovery-brief.md`）の P1〜P4 に無い利用者である。「使い捨ての VM を 1 台借りて、自分の分だけ確実に返す」という消費者であり、ADR-005 が非目標とした大量生成・一括破棄とは別物である。以下 **P5「借りて返すエージェント」** と呼ぶ。

### 既にあるもの

| 要求 | 現状 |
|---|---|
| template から clone し cpu / memory / disk を指定 | `spec.clone` と `spec.resources` / `spec.disks` で実装済み |
| ssh 鍵と NIC 設定 | `spec.cloudInit.sshKeys` / `spec.cloudInit.ipConfig` で実装済み |
| プールへの所属 | `spec.pool`（create-only）で実装済み |
| 所有権の判定 | **無い**。`delete` は名前だけで消す |
| IP の表示 | **無い**。`-o wide` は UPTIME と TAGS まで |

### 検討したが採らなかった印

所有権を示す印の候補と、採らなかった理由。

| 候補 | 却下理由 |
|---|---|
| tag（`pvectl.io/managed` のような名前空間付き tag） | tag は `VM.Config.Options` 権限で誰でも書き換えられる。組織ごとに tag へ載せたい情報が異なり、pvectl が名前空間を決めて強制するのはポリシーへの侵入になる。`privileged-tags` で保護はできるが、その datacenter 設定を pvectl が要求することになる |
| `description` への構造化された行 | tag と同じく誰でも書け、しかも利用者の表示領域である（ADR-005 §2 で同じ理由により last-applied の埋め込みを見送っている） |
| VM 名の接頭辞 | 規約であって強制ではない。手で同じ名前を付ければ破れる |
| ローカルの台帳ファイル | tfstate 相当の状態を持つことになり、ADR-003 / ADR-005 で却下済み |
| **プール** | Proxmox が VM の管理単位として用意した一級の器で、用途が明確。ACL の対象になるため、API トークンの権限をプールに絞れば **サーバー側で** 範囲が強制される。pvectl は何も発明しない |

### 期限を pvectl に持たせない理由

「一定期間放置で自動削除」は 2 つの点で pvectl に合わない。

- 期限をサーバー上に置くなら tag か description しか無く、上表の理由で改変されやすい
- 期限を監視して消すには常駐か cron 起動の `gc` 動詞が要る。常駐は ADR-005 の非目標であり、`gc` は「いつ何を消すか」という方針を pvectl が持つことになる

期限は VM を借りた側の関心である。借りた側が自分の台帳に持ち、切れたら `pvectl delete` を呼ぶ。pvectl は state を持たず、方針も持たない。

### IP の取得経路

QEMU VM の実 IP を返す API は guest agent の `network-get-interfaces` だけである。WebUI が「agent 入りの VM でしか IP を表示しない」のは、WebUI もこの 1 本しか持たないためである。`ipconfig0` は cloud-init へ渡す望ましい設定であり、`ip=dhcp` のとき実 IP を含まない。LXC には agent 不要の `interfaces` API があるが、これは M2 の範囲である。

## 決定内容

### 1. context 設定に `pool` を持つ（実験的）

```yaml
contexts:
  - name: lab
    context:
      user: agent-token
      node: pve1
      pool: ai-sandbox          # 任意。所有権の範囲
features:
  - ownership                   # 無ければ pool 設定は無視され、既存の挙動と変わらない
```

- `features` は Config のトップレベルに置く。`ownership` が無いとき `context.pool` は読まれるがどこにも効かない。**既存利用者の挙動は 1 バイトも変わらない**
- `features` に未知の名前があれば `config.Validate()` がエラーにする
- 実験的である間は `ownership` の意味論（後述の 2 と 3）を破壊的に変えてよい。v1alpha1 のうちに固める

### 2. 作成時の所属

`ownership` が有効で `context.pool` があるとき、

- マニフェストに `spec.pool` が無ければ `context.pool` を補って作成する
- `spec.pool` があり `context.pool` と異なれば **エラー**。黙って上書きも黙って無視もしない
- `spec.pool` は create-only のまま（`docs/quick-start/manifests.md` の既存の約束）

これは「マニフェストに書いていないフィールドは存在しない」原理の例外に見えるが、そうではない。`context.pool` は **マニフェストの内容ではなく接続先の範囲**であり、ADR-005 の 4 層のうち「認証・接続」層に属する。同じ context の中で `get` すれば `spec.pool` は live 値として現れ、export → apply は `unchanged` になる。

### 3. delete の既定ガード

`ownership` が有効で `context.pool` があるとき、`delete` は対象 VM のプールを確かめ、

- `context.pool` に所属していれば消す
- 所属していなければ **拒否**し、`--outside-pool` 相当の明示フラグを要求する。手で作った VM を消すには意図の表明が要る
- プールが読めない（権限不足）ときは拒否側に倒す

このガードは **利便性のための二重化**であり、強制の本体ではない。強制は Proxmox の ACL で行う。推奨する設定は、API トークンに `/pool/<pool>` に対する `PVEVMAdmin` 相当と、template と storage への必要最小限を与え、`/vms` 全体には権限を与えないこと。この構成なら pvectl のガードが無くてもプール外の VM は消せない。ガードは、権限が広いトークンを使っているときの事故防止と、拒否理由を人が読める形にするためにある

### 4. `get vm -o wide` に IP 列を足す

列名は `IP`。埋め方は 3 段。

1. guest agent が応答すれば、その実 IP（ループバックを除き、IPv4 を先に）
2. 応答しなければ、`ipconfig0` が静的（`ip=` が `dhcp` 以外）のときだけその値
3. どちらも無ければ空欄。`describe` には agent 未応答の旨を出す

`-o yaml` / `-o json` では `status.ipAddresses` として出す。**`status` は apply の比較対象ではない**（ADR-001）ので、宣言キーの原理には触れない。

agent への問い合わせは VM ごとに 1 リクエスト増える。`-o wide` を明示したときだけ行い、既定の table では行わない。

### 5. pvectl の外に置くもの

P5 の要求のうち以下は、pvectl を呼ぶ側（スキル・スクリプト）の責務とする。

| 要求 | 置き場所 | 理由 |
|---|---|---|
| cpu / memory / disk の許可範囲 | 呼ぶ側の設定 | 範囲検査は方針である。サーバー側で強制したいなら Proxmox の ACL とプールで行う |
| ssh 鍵・NIC の既定値 | 呼ぶ側がマニフェストを生成する | context 設定に VM の既定値を混ぜると「マニフェストに無いフィールドが存在する」状態になる |
| IP の払い出し | 呼ぶ側が範囲から静的に割り当て `ipConfig` に書く | 起動前に IP が確定し、template に agent が入っているかを問わない。§4 の経路依存を回避できる |
| 有効期限と自動削除 | 呼ぶ側の台帳 + `pvectl delete` | 上記「期限を pvectl に持たせない理由」 |

pvectl 側の約束は「呼ぶ側がこれらを組める材料を揃える」ことに限る。具体的には `spec.pool` / `spec.cloudInit` / `status.ipAddresses` と、`-o json` による機械可読な出力である。

### 非目標（この ADR で明示するもの）

- tag による所有権表明。組織の tag 運用に干渉しない
- 有効期限の保持、`gc` 動詞、常駐の回収処理
- リソース上限や許可範囲の検査
- context 設定への VM 既定値（cpu / memory / ssh 鍵 / bridge）の追加
- DHCP リースや ARP からの IP 推定

## 結果

- ✅ 「作った VM だけ消せる」が pvectl の発明無しに、サーバー側の ACL で強制できる。pvectl のガードは事故防止と説明のための層に留まる
- ✅ pvectl は state も方針も持たず、ADR-005 の定義を保つ
- ✅ P5 が要る機能は `context.pool` + delete ガード + IP 列の 3 点に縮む。いずれも既存の層（接続・観測）に収まる
- ✅ `ownership` は feature gate 付きなので、既存利用者への影響が無い
- ⚠️ 期限・範囲・既定値を呼ぶ側に置くため、P5 の体験の大半は pvectl の外で決まる。スキル側の設計は別途起こす
- ⚠️ IP 列は agent 依存であることを利用者に説明し続ける必要がある。「IP が出ない」は不具合ではなく agent 未導入である
- ⚠️ `context.pool` による `spec.pool` の補完は、同じマニフェストを別 context に apply すると結果が変わる。これは `targetNode` と同じ性質の「接続先依存」であり、ドキュメントに明記する

## 影響する文書

- `docs/prd.md`: P5 とこの 3 点をどの版に載せるかは未裁定（discovery brief Q1 と合わせて決める）
- `docs/discovery-brief.md`: ペルソナに P5 を追加する
- `docs/quick-start/README.md`: `features` と `context.pool` の記述、推奨 ACL の例
- `docs/quick-start/manifests.md`: `status.ipAddresses` と `-o wide` の IP 列
- ADR-004: Config に `features` トップレベルが増える（追補で参照）
- ADR-005: 「`--prune` は所有権表明の設計が固まる M3 以降」と書いている。本 ADR がその所有権表明の最初の形になるが、`--prune` の採否は引き続き別に判断する
