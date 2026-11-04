# Discovery Context — pvectl

- **作成**: 2026-09-22
- **目的**: 対 Terraform の差別化を「採用導線で使える文言」と「ロードマップの判定基準」の両方に落とす
- **射程**: 現行 PRD 準拠 — 自宅ラボ〜小規模チーム（Proxmox VE クラスタ 1〜3 台、VM 10〜150 台規模）

## Phase 0 の回答

| 問い | 回答 |
|---|---|
| ① 何を作っているか | pvectl — Proxmox VE を kubectl の操作感で宣言的に管理する CLI（v0.1.0-rc.2 公開済み） |
| ② 誰のために | kubectl の操作感に慣れた個人・小規模チーム。詳細は [problem-space.md](problem-space.md) |
| ③ ステージ | 既存プロダクトの改善（M1+M1.5 実装済み、M2 以降が未着手） |
| ④ 体制・制約 | メンテナ 1 名の個人 OSS。レビューは best-effort。収益化しない |
| ⑤ 最大の不確かさ | 競合（Terraform Provider）との差別化が言語化されていない / 誰が最初のユーザーになるか未特定 |

## 手法の選択

- **パターン**: E（プラットフォーム・デベロッパーツール）× C（既存プロダクトの改善）
- **Cynefin**: Complicated — 分析で解ける。ADR-005 に 2026-09 時点の bpg/terraform-provider-proxmox v0.113.1 調査が既にあり、構造的差異の根拠は揃っている
- **採用**: JTBD（競合ジョブ分析）、ペルソナ、Lean Canvas（Customer Segments / Unfair Advantage に限定）、ロードマップ判定基準（Impact Mapping 相当）
- **不採用**: CJM・Service Blueprint（CLI で体験段階が浅く、得るものが少ない）、Pretotyping（動くものが既にある）、Design Sprint（1 人チーム）

## 制約として先に置くもの

- ADR-005 の中心原理「マニフェストに書いていないフィールドは存在しない」は所与。この Discovery はそれを**覆さず、誰に効くかを特定する**
- 非目標（state ファイル / 常駐リコンサイル / `--prune` / `qm` の網羅）も所与。ペルソナはこの制約の**内側**で定義する
