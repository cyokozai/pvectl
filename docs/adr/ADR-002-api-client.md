# ADR-002: API クライアント — Telmate/proxmox-api-go を内部インターフェースの背後に採用

- **状態**: 採用（2026-08-12）
- **決定**: `github.com/Telmate/proxmox-api-go`（commit `d21834931666` 固定）を採用し、`internal/api.Client` インターフェースの背後に完全に隠蔽する

## 文脈

旧実装は 534 行の手書き `net/http` クライアントだったが、タスク(UPID)待機なし・タイムアウトなし・リトライなし・cloud-init 未送信という欠陥を抱えていた。ユーザー決定により SDK 採用へ（旧 README の「no external dependencies」ゴールは撤回）。

## 決定内容

1. **SDK に任せるもの**: セッション/認証（API token + ticket ログイン）、TLS、タスク待機（`PostWithTask`/`PutWithTask`/`DeleteWithTask` は内部で UPID 完了までポーリング）、リトライ付き GET
2. **SDK に任せないもの**: `ConfigQemu` 等の巨大構造体は使わない。設定の読みは `GetItemConfigMapStringInterface`（生 key-value）、書きはフラットな param map を `PostWithTask`/`PutWithTask` に渡す
   - 理由: diff エンジンが生の key-value を必要とする。`ConfigQemu` は unversioned で変更が激しく、依存面積を最小化したい
3. **バージョン固定**: upstream に semver タグが存在しないため pseudo-version `v0.0.0-20260811170036-d21834931666` に固定。更新は dependabot PR + CI で検証
4. **隠蔽**: SDK の型は `internal/api` パッケージから一切漏らさない。リソースハンドラは `api.Client`（自前インターフェース）のみに依存し、`apitest.Fake` でテストする
5. **エスケープハッチ**: SDK が型付きで扱えないリソース（M3 SDN、M4 ACL/HA）は `Client.Raw()`（Get/PostTask/PutTask/DeleteTask）でパス直指定。**第二の HTTP スタックは絶対に作らない**

## タスク待機のセマンティクス

SDK の `*WithTask` は完了までブロックするため、「タスクを待たない」パスは存在しない（旧実装の UPID 破棄バグの構造的解消）。待機上限は `--timeout`（既定 5m）→ SDK の `TaskTimeout` にマップ。個別 `--wait` フラグは不要と判断し提供しない。

## 却下した代替案

- **手書きクライアント継続**: タスク待機・リトライ・チケット更新を自前保守するコストが SDK 追従コストを上回る
- **luthermonson/go-proxmox**: API カバレッジと実績で Telmate に劣後（コミュニティ規模）
- **apidoc スキーマからのコード生成**: 生成基盤の構築が M1 を遅らせる。M4 以降に必要なら再検討

## 追補（2026-09-16、SDK の固定 commit を更新）

本文の決定（commit 固定・内部インターフェースの背後に隠蔽）は変えない。固定先の commit のみ更新する。

| | commit | 日付 |
|---|---|---|
| 旧 | `v0.0.0-20260811170036-d21834931666` | 2026-08-11 |
| 新 | `v0.0.0-20260914192510-57a492a8ca45` | 2026-09-14 |

### 更新理由

Go 1.27.1 へのツールチェイン更新に合わせて依存全体を `go get -u ./...` で追従させた際、upstream が 21 commit（54 ファイル）進んでいた。内容は LXC interfaces、ノード一覧、LXC の pending 修正、メモリレイアウト最適化が中心で、pvectl が使う経路の変更は含まれない。

**本文 §3 の前提は変わっていない。** upstream には依然として semver タグが存在せず（2026-09-16 時点で tags API は空）、pseudo-version 固定を続ける以外の選択肢がない。

### 依存面積の実測

本文 §2 / §4 の「依存面積を最小化する」判断が機能していることを、この更新で確認できた。pvectl が SDK から参照している識別子は 6 個だけである。

```
proxmox.ApiToken  proxmox.Client  proxmox.GuestID
proxmox.GuestQemu proxmox.NewClient proxmox.NewVmRef
```

`ConfigQemu` を避け生 key-value（`GetItemConfigMapStringInterface`）とフラットな param map に留めた結果、**54 ファイルが変わった upstream 更新に対してコード変更が 0 行で済んだ**。`gofmt` / `go vet` / `go test ./...` / `go test -race ./...` はいずれも全通過している。

この「上げてもテストが通る」状態を確認できる限り、以後の SDK 更新も同じ手順（更新 → 全テスト → 本 ADR に追補）で進める。
