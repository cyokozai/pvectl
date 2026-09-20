# ADR-008: リリース自動化 — goreleaser を採らず `make cross-build` の成果物を配る

- **状態**: 採用（2026-09-18）
- **決定**: `v*` タグの push で走る GitHub Actions のワークフロー（`.github/workflows/release.yaml`）を追加し、**配布バイナリは goreleaser ではなく既存の `make cross-build` で作る**。リリースノートは GitHub の自動生成に任せ、手書きの CHANGELOG は持たない

## 文脈

`v0.1.0`（M1 + M1.5）を出す直前である。バージョニングの方針は既に決まっている（`docs/prd.md`「バージョンと分岐の関係」）。

- 開発は `dev` で進み、**`main` はリリース済みバージョンを指す**
- リリースごとに `dev` → `main` をマージし、**`main` 上でタグを打つ**
- ブランチではバージョニングしない

決まっていなかったのは、そのタグから何をどう作るかである。`.github/workflows/` には `ci.yaml` しか無く、タグを打ってもリリース成果物は何も生まれない。`docs/quick-start` は「ビルド済みバイナリはまだ配布していない。goreleaser は M2 の検討項目である」と書いており、`docs/dev-process.md` §6 も同じ前提に立っていた。

この ADR は、その「M2 の検討項目」を前倒しで裁定し、**goreleaser を採らない**と決めるものである。

### 既にあるもの

| | |
|---|---|
| `Makefile` の `cross-build` | `PLATFORMS`（linux/darwin × amd64/arm64 の 4 つ）を `CGO_ENABLED=0` で順に `go build` し、`dist/pvectl_<goos>_<goarch>` を出す |
| `Makefile` の `LDFLAGS` | `internal/cmd` の `Version` / `GitCommit` / `BuildDate` を注入する。`VERSION` の既定は `git describe --tags --always --dirty` |
| `ci.yaml` の `build` / `cross-build` ジョブ | PR #17 以降、どちらも `make <target> HOST=1` を呼ぶ。CI は `go` を直接叩かない |

つまり **クロスビルドとバージョン注入は既に存在し、CI からも同じ経路で呼ばれている**。足りないのは、その出力をアーカイブにまとめて GitHub Release に載せる部分だけである。

## 決定内容

### 1. goreleaser を採らない

ADR-007「決定内容 3」は、ビルド定義を Makefile と CI に二重に持つと version 情報の注入が食い違い、手元のバイナリと配布物で `pvectl version` の出力が変わると述べ、**`LDFLAGS` の定義箇所は Makefile 1 箇所に保ち、CI からはターゲットを呼ぶ**ことを原則として定めている。

goreleaser はこの原則と正面から衝突する。goreleaser は `.goreleaser.yaml` に独自の `builds:`（`goos` / `goarch` / `ldflags` / `env`）を持ち、それを使わずに goreleaser を使う方法は無い。採用すれば、

- `LDFLAGS` の定義が Makefile と `.goreleaser.yaml` の **2 箇所に分裂する**
- 対象プラットフォームの一覧が `PLATFORMS` と `.goreleaser.yaml` の 2 箇所に分裂する
- 手元の `make cross-build`、CI の `cross-build` ジョブ、リリース成果物の 3 つが**別々の作り方をしたバイナリ**になる

3 つ目が特に悪い。手元と CI で検証したものと、利用者がダウンロードするものが別経路で作られると、**配布物だけが壊れていても誰も気付けない**。ADR-007 が避けようとしたのはまさにこの状態である。

加えて ADR-007 の理由 1（追加インストールを求めない）も同じ向きに働く。`make` と `tar` と `sha256sum` と `gh` は GitHub Actions のランナーに最初から入っており、リリース経路に新しい依存は一つも増えない。

### 2. ワークフローは「止める理由」と「作る作業」でジョブを分ける

`release.yaml` は 2 ジョブ構成にする。

| ジョブ | 権限 | 内容 |
|---|---|---|
| `verify` | `contents: read` | タグが `main` から到達可能かの検証、`go test -race ./...` |
| `release` | `contents: write` | `make cross-build HOST=1`、バージョン検証、アーカイブ、`SHA256SUMS`、Release 作成 |

ワークフロー全体の既定は `ci.yaml` と同じ `contents: read` に置き、**書き込み権限は `release` ジョブにだけ付ける**。テストの実行中に書き込み可能なトークンが手元にある状態を作らない。

分割のもう一つの効能は、「リリースしてはいけない理由」が全て `verify` に集まることである。`verify` が落ちれば `release` は開始すらしない。

### 3. タグが `main` から到達可能かを機械的に確かめる

「`main` はリリース済みバージョンを指す」は現状 文書上の約束でしかなく、`dev` や作業ブランチに誤ってタグを打てば、その約束は黙って破れる。

`verify` の最初のステップで `git merge-base --is-ancestor "$GITHUB_SHA" refs/remotes/origin/main` を評価し、偽なら即座に失敗させる。何も作られる前に止まるので、誤ったタグから成果物が生まれることはない。

### 4. `fetch-depth: 0` を置き、生成したバイナリのバージョンを実測で確かめる

両ジョブの checkout に `fetch-depth: 0` を置く。必要なのは主に「3. 到達可能性」の側である。実測すると、タグ push に対する既定の `fetch-depth: 1` はタグとその 1 コミットしか取らず、`refs/remotes/origin/main` は存在せず `git rev-list --count HEAD` は 1 になる。この状態では `git merge-base --is-ancestor` に答えるものが無い。

バージョン注入のほうは、`fetch-depth: 1` でもタグ自体は取得されるため `git describe --tags` はタグを返す（これも実測した）。つまり「浅いと短縮 SHA になる」は**タグ push に限れば起きない**。それでも深さを 0 に揃えるのは、2 つの checkout が同じものを見ていることを自明にするためである。

そのうえで、ビルド後に `dist/pvectl_linux_amd64 version` を実行し、出力が `pvectl version <タグ> ` で始まることを確認する。合わなければリリースを作らない。ここが壊れたまま配られるのが最悪の失敗であり、公開前に実測で潰せる唯一の場所である。**実行できるのはランナー自身のプラットフォームだけだが、4 つは同じ `LDFLAGS` の呼び出しを共有しているので、注入そのものの検証としてはこれで足りる。**

### 5. アーカイブは `pvectl_<タグ>_<os>_<arch>.tar.gz`

Go の CLI の慣習に合わせ、バイナリを裸で置かず tar.gz にまとめる。中身は `pvectl`（名前から os/arch を落とした素の名前）・`LICENSE`・`README.md` の 3 つ。

バージョン部分には **タグをそのまま入れる**（`pvectl_v0.1.0_linux_amd64.tar.gz`）。goreleaser の既定は先頭の `v` を落とすが、そうするとダウンロード URL の中に `download/v0.1.0/pvectl_0.1.0_...` と同じバージョンの 2 通りの綴りが並ぶ。利用者が打ち間違える箇所を減らすほうを採る。

`SHA256SUMS` は全アーカイブ分を 1 ファイルにまとめ、**パスを付けない裸のファイル名**で書く。利用者は通常 4 つのうち 1 つしか落とさないので、`sha256sum --ignore-missing -c SHA256SUMS` が落としたものだけを検証して通る（macOS では `shasum -a 256 --ignore-missing -c SHA256SUMS`。どちらも `--ignore-missing` を持つことを確認した）。

### 6. リリースノートは GitHub の自動生成に任せる

`gh release create --generate-notes` を使い、分類は `.github/release.yml` に置く。カテゴリは**実在するラベル**（`enhancement` / `bug` / `documentation`）だけで構成し、残りは `"*"` の catch-all に落とす。ラベルの付け忘れは掲載位置を失うだけで、**項目そのものが消えることはない**。

手書きの CHANGELOG は作らない。メンテナが 1 名の現状では、更新され続ける保証の無い 2 つ目の履歴を抱えるより、マージされた PR という単一の事実から生成するほうが壊れにくい。

### 7. prerelease は semver のハイフンで判定する

`-rc` / `-beta` / `-alpha` を含むタグは prerelease として作る。判定はこの 3 語の列挙ではなく、**semver がプレリリースを表すハイフン**（バージョン本体の後に続く `-`）の有無で行う。安定版のタグにハイフンは現れないので、列挙より安全で、`-pre` のような未知の綴りも取りこぼさない。

## 再検討の条件

この決定は永久ではない。**次のいずれかが起きたら、この ADR を改訂して goreleaser（または同等のリリースツール）を再評価する。**

| 兆候 | なぜ判断が変わるか |
|---|---|
| **Homebrew tap / Scoop / Linux パッケージ**（deb・rpm・AUR）の配布が必要になった | formula や nfpm の定義を自前のシェルで書くのは、`make cross-build` の薄い延長では済まない。goreleaser の本領はここにある |
| **署名**（cosign / Sigstore）や **SBOM** の添付が必要になった | 鍵の扱いと証明書の検証を自前で書くのは 割に合わない。goreleaser には既製の統合がある |
| **コンテナイメージをリリースと同時に push** する必要が出た | 現在 `Dockerfile` はあるがリリース経路に乗っていない。バイナリとイメージのバージョンを 1 箇所で揃える要求が生まれたら、統合ツールの利得が出る |
| アーカイブ生成のシェル片に**条件分岐が増え始めた**（プラットフォーム別の同梱物、`.zip` と `.tar.gz` の出し分け等） | ADR-007 の再検討条件「レシピの条件分岐」と同じ兆候。リリース定義が宣言的に書かれるべき規模に達したということ |

逆に、**これらが起きていない限り移行は検討しない**。goreleaser の普及度や設定の簡潔さそのものは再検討の理由にしない。

なお移行する場合でも、ADR-007「決定内容 3」の制約（version 注入の定義を 1 箇所に保つ）は生き続ける。goreleaser を採るなら `make build` / `make cross-build` 側を goreleaser 呼び出しに寄せ、**Makefile と `.goreleaser.yaml` の両方に `ldflags` が書かれた状態にはしない**こと。

## 結果

- ✅ 手元・CI・リリースの 3 つが `make cross-build` という**同じバイナリの作り方**を通る。配布物だけが壊れる経路が無い
- ✅ `LDFLAGS` と `PLATFORMS` の定義は Makefile 1 箇所のまま（ADR-007 決定内容 3 を維持）
- ✅ リリース経路に新しい依存が無い。`make` / `tar` / `sha256sum` / `gh` は全てランナーに preinstalled
- ✅ `main` から到達しないタグではリリースが作られない。分岐方針が文書ではなく機械で担保される
- ✅ 配布バイナリの `pvectl version` がタグと一致することが、公開前に実測で確かめられる
- ⚠️ アーカイブ生成・チェックサム・Release 作成は YAML 中のシェル片である。ローカルで `make` を叩くだけでは再現できず、変更時はワークフローを実際に走らせるか、シェル片を取り出して実行する必要がある
- ⚠️ 署名も SBOM も無い。利用者が検証できるのは `SHA256SUMS` による改竄検出までで、**それ自体が同じ Release に置かれている以上、出所の証明にはならない**。上記の再検討条件に回す
- ⚠️ `PLATFORMS` は linux / darwin × amd64 / arm64 の 4 つのまま。**Windows 向けバイナリは配布されない**（ADR-007 が Windows ネイティブをサポート外としている方針と揃っている）
- ⚠️ リリースノートの分類はラベルに依存する。`documentation` は手で付けるものなので、付け忘れると「Other changes」に落ちる

## 却下した代替案

- **goreleaser を採用する**: Go CLI のリリースでは最も普及しており、アーカイブ・チェックサム・Release 作成・Homebrew tap・パッケージ・署名までを 1 つの設定で賄える。却下の理由は機能ではなく**定義の分裂**である（決定内容 1）。`.goreleaser.yaml` が独自の `builds:` を持つ以上、ADR-007 が定めた「`LDFLAGS` は Makefile 1 箇所」が成立しなくなり、手元・CI・配布物の 3 経路が別々の作り方になる。現在の配布要件（4 プラットフォームの tar.gz とチェックサム）は `make cross-build` の 20 行程度の延長で満たせるため、その代償を払う理由が無い。再検討の条件は上に列挙した
- **`softprops/action-gh-release` などの Release 作成アクションを使う**: 記述は短くなるが、サードパーティ製アクションを 1 つ増やし、commit SHA でのピン留めと dependabot による追随の対象を増やす。`gh release create` はランナーに preinstalled で、やっていることが 1 行読めば分かる
- **手書きの CHANGELOG.md を維持する**: 生成物より読みやすい履歴を作れるが、更新を強制する仕組みが無いと必ず実態とずれる。メンテナ 1 名では、ずれた CHANGELOG のほうが無いより有害になる
- **リリース時にタグを打つのをワークフロー側の仕事にする**（`workflow_dispatch` でバージョンを入力し、CI がタグを作る）: タグ push を唯一のトリガーに保つほうが、「何がリリースを引き起こしたか」が git の履歴だけで説明できる。また `main` への到達可能性の検証が、人間の操作の後ではなく前に置ける
- **`PLATFORMS` に windows/amd64 を足す**: クロスビルド自体は 1 行で足せるが、`.tar.gz` ではなく `.zip` を期待される、拡張子 `.exe` の扱いが要る、実際に動くかを誰も検証していない、という 3 つが同時に来る。ADR-007 が Windows ネイティブをサポート外としている以上、検証されないバイナリを配るほうが不誠実である。要望が出た時点で別途決める
