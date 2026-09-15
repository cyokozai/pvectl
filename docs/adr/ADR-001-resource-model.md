# ADR-001: リソースモデル — Kubernetes 型エンベロープ + kind レジストリ

- **状態**: 採用（2026-08-12）
- **決定**: すべてのリソースを `{apiVersion: pve.io/v1alpha1, kind, metadata, spec}` エンベロープで表現し、kind → `resource.Handler` のレジストリで汎用動詞にディスパッチする

## 文脈

「全 Proxmox VE API リソースを kubectl と同じ体験で」が目標。リソースごとに動詞コマンドを実装すると kind × verb の組合せ爆発が起きる（M1 時点で 7 動詞、M4 で 9 kind 想定 = 60 超の実装点）。

## 決定内容

- マニフェストは `internal/runtime.Unstructured` として読む。`apiVersion`/`kind`/`metadata` は即時デコード、`spec` は `yaml.Node` のまま遅延し、ハンドラが型付き spec に strict デコード（未知フィールドはエラー）
- `resource.Handler` インターフェース（Kind/APIVersion/Aliases/Columns/Get/List/Delete/Apply/Diff/Describe）を kind ごとに実装
- `start`/`stop` は能力インターフェース（`Starter`/`Stopper`）の type assertion。ライフサイクルを持たない kind（Storage 等）は自然に非対応となる
- `Registry.Lookup("vm")` がコマンドライン名を、`Registry.ForObject(u)` がマニフェストを解決。新 kind は `Register(handler)` 1 行で全動詞に接続
- テーブル出力の列定義（`printer.Column`）もハンドラが持つ → printer は完全に kind 非依存

## 結果

- ✅ M2〜M4 の追加コストが「handler + テスト」に閉じる（cmd 層の変更ゼロ）
- ✅ `get vm x -o yaml` の出力がそのまま `apply -f -` に食える（round-trip をテストで保証）
- ⚠️ kind 間で挙動を揃える規律はレビューで担保する必要がある（インターフェースは形しか強制しない）

## 却下した代替案

- **リソースごとの専用コマンド**（`pvectl vm list` 型）: kubectl 体験から乖離、組合せ爆発
- **完全 Unstructured（型なし map）処理**: バリデーションと変換が書き殴りになり、strict デコードによる typo 検出を失う
