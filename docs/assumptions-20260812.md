# 合意済み前提リスト（2026-08-12）

要件定義練り直し（arch-requirements）セッションでユーザーと合意した前提と決定。

## 前提（Yes 確認済み扱い）

1. **体制**: 個人開発（1名）。Kubernetes の操作感に習熟
2. **既存資産**: cobra ベースの新アーキテクチャ（未コミット）を土台に発展させる。旧 `app/` 自作 CLI フレームワークは廃棄済み
3. **開発プロセス**: t_wada 流 TDD（Red-Green-Refactor）。テストは httptest フェイク PVE で実クラスタ不要
4. **Git 操作**: add/commit/push/tag は人間が実行（Claude はコマンド提示のみ）
5. **開発環境**: Docker コンテナ完結（ローカル汚染なし・セキュリティ）。DevContainer 可
6. **バージョン**: 技術選定時に最新へ更新する（Go 1.26 / cobra v1.10.2 / golangci-lint v2.12.2）

## ユーザー決定（AskUserQuestion で選択）

| 論点 | 決定 |
|---|---|
| ロードマップ | **縦に深く→横に展開**: M1 VM 完成 → M2 LXC → M3 storage/network/snapshot → M4 pool/user/ACL/HA。各マイルストーンでリリース可能品質 |
| IaC 深度 | **kubectl 相当**: 冪等 apply + diff コマンド + --dry-run=client/server + マルチ doc YAML / 複数 -f / stdin / JSON。prune や state ファイルは持たない |
| API クライアント | **Telmate/proxmox-api-go 採用**（commit d21834931666 固定）。旧 README の「no external dependencies」ゴールは撤回・書き換え |
| ドキュメント | **フルセット**: PRD + ADR-001..004 + 開発プロセス設計書を docs/ に整備 |

## 実装中の追加判断（設計判断として記録）

- SDK の `*WithTask` が UPID 完了まで内部でブロックするため、個別の `--wait` フラグは設けず**常に待機**とし、`--timeout` のみ提供（ADR-002）
- クライアント dry-run の apply 結果表示は `validated`（サーバー非接触では created/configured を判定できないため）
- `pvectl get vm -o yaml`（一覧）はサマリ由来の部分 spec、単体 `get vm NAME -o yaml` は完全な round-trip 可能 spec（一覧での N+1 config fetch を回避）
