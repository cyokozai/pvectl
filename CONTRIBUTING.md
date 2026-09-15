> [日本語](CONTRIBUTING.jp.md)

# Contributing to pvectl

Thanks for your interest in pvectl. This document covers what the project accepts, how to
get a change reviewed, and the one contribution path that matters most: adding a new
resource kind.

pvectl currently has **one maintainer** ([@cyokozai](https://github.com/cyokozai)), who
reviews and merges every pull request. That is the binding constraint on this project, so
most of the rules below exist to keep review cheap rather than to impose process.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## 1. Before you write code: is your change in scope?

pvectl is deliberately narrow. Read
[ADR-005: pvectl の存在理由と非目標](docs/adr/ADR-005-purpose-and-non-goals.md) before
proposing anything substantial — it is the document a maintainer will point at when
declining a change.

### The central principle

> **A field that is not written in the manifest does not exist as far as pvectl is concerned.**

Comparison and update target only the keys a manifest declares. Removing a field means
"stop managing it", not "restore the server default". Differences against server-side
defaults and generated values are ignored permanently. This is a 2-way merge; pvectl has
no `last-applied-configuration` and no state file.

**Proposals that contradict this principle are not accepted, however useful they are.**
"Fields I deleted did not revert" is the specified behaviour, not a bug.

### What pvectl adds on top of the Proxmox VE REST API

Only these four layers ([ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md)):

| Layer | What the raw API does not give you |
|---|---|
| Auth & connection | Context switching, token management, `0600` storage ([ADR-004](docs/adr/ADR-004-config-context.md)) |
| Convergence | Desired state → diff → PUT of changed keys only. `internal/resource/vm/normalize.go` is the actual product |
| Tasks | UPID waiting turned into exit codes ([ADR-002](docs/adr/ADR-002-api-client.md)) |
| Validation | Strict, typed manifest validation before any write ([ADR-001](docs/adr/ADR-001-resource-model.md)) |

Being a thinner or wider wrapper around the API is not a goal in itself.

### Explicit non-goals

These have already been decided against. Reopening one needs a new ADR, not a pull request:

- A local state file equivalent to `tfstate` — the server is the single source of truth
- A resident reconcile loop or `watch` — `apply` is a one-shot convergence
- `--prune` — deferred until ownership semantics are designed (M3 or later)
- Implicit live migration during `apply` — offered only as the `migrate` verb
- Building the Proxmox cluster itself, or adding nodes
- Exhaustive coverage of the `qm` command surface — `spec.raw` is the escape hatch
  ([ADR-006 §7](docs/adr/ADR-006-api-groups-and-schema.md))
- Node-local interactive operations (`qm terminal` / `monitor` / `sendkey`)

### The two questions a feature must pass

1. Does it fit inside "manage declared keys only"?
2. Is it a **persisting desired state** (belongs in `spec`) or a **one-shot action**
   (belongs in an imperative verb, leaving no trace in `spec`)?

`spec.runStrategy` (`Halted` / `Always` / `Manual`) is the declarative side.
`pvectl start` / `stop` / `exec` / `migrate` are the imperative side.

## 2. Open an issue before writing a feature

For anything beyond a typo or an obvious bug fix, **open an issue and get agreement before
you write the code.** With a single maintainer, a large unsolicited pull request that turns
out to be out of scope wastes your time more than anyone's.

Use the issue templates:

- [Bug report](.github/ISSUE_TEMPLATE/bug-report.yml)
- [Feature request](.github/ISSUE_TEMPLATE/feature-request.yml) — it asks you to confirm
  the proposal does not collide with the ADR-005 non-goals

Small, self-evident fixes (typos, broken links, a failing test, a clearly wrong error
message) can go straight to a pull request.

### `help wanted` and `good first issue` are looser

If an issue carries **`help wanted`** or **`good first issue`**, the scope question is
already settled: the maintainer has decided the change is wanted. Leave a comment to claim
it so two people do not do the same work, then send the pull request. No design discussion
is needed, and you do not have to re-argue the case in the issue first.

These labels are the intended entry point if you would like to contribute but do not have
a specific problem of your own.

## 3. Development environment

**Everything runs in a container.** Do not install Go or linters on your host to work on
pvectl. Build, test, and lint all happen inside the dev container.

The setup, the make targets, the test layers, and the release checklist are documented in
**[docs/dev-process.md](docs/dev-process.md)** — follow that document rather than
reinventing a local toolchain. It is the single source of truth for the workflow; this
file deliberately does not repeat it.

## 4. Tests are required

**You do not need a real Proxmox VE cluster to develop or test pvectl.**
[`test/pvefake`](test/pvefake/pvefake.go) is an `httptest`-based fake PVE API that covers
ticket login, token auth, and task (UPID) polling, so the whole stack can be exercised
offline.

Every behavioural change needs a test, and bug fixes start from a failing reproduction
test. Pick the layer that matches what you changed — the table of layers and tools is in
[docs/dev-process.md §1](docs/dev-process.md). In short:

- Pure functions (convert / normalize / validate / diff) → table tests
- Output formatting → golden files (regenerate with `go test ./internal/printer -update`)
- Resource handlers → `internal/api/apitest` fake client
- End to end → `test/e2e` against `test/pvefake`

Manifests under [`examples/`](examples/) are parsed by the test suite, so if you change the
schema you must update them or tests fail. That is intentional: it keeps the docs from
rotting.

CI (lint / test / build) must be green before review. Run the checks in the container
first — see [docs/dev-process.md §2](docs/dev-process.md).

## 5. Commit messages

pvectl uses **Conventional Commits**, and commit bodies may be written in Japanese or
English. [`.gitmessage`](.gitmessage) is the canonical template — read it, and wire it up
so you get it automatically:

```bash
git config commit.template .gitmessage
```

Format:

```
<type>(<scope>): <description>

Explain what changed and why it was necessary.
```

- Types: `fix` / `feat` / `docs` / `style` / `refactor` / `perf` / `test` / `chore`
- Scope is the package or area touched (`vm`, `cmd`, `api`, `resource`, `docs`, …)
- Append `!` for a breaking change: `feat(vm)!: replace startOnBoot with runStrategy`
- Match the existing history — run `git log --oneline` and follow what you see. Note that
  the emoji table in `.gitmessage` is a reference list; the actual history does not use
  emoji, so do not add them

## 6. Sign-off (optional)

pvectl does **not** require a Developer Certificate of Origin sign-off, and there is **no
CLA**. A CLA would be disproportionate for an MIT-licensed personal project, and enforcing
a DCO on every commit needs a bot this project does not run.

A `Signed-off-by` line is welcome if you would rather state your provenance explicitly:

```bash
git commit -s -m "feat(vm): add ballooning support"
```

which appends:

```
Signed-off-by: Your Name <your.email@example.com>
```

This is an offer, not a requirement. No pull request will be blocked or delayed for
missing one, and you will never be asked to rewrite history to add it. If you are curious
what the line means, the text is at
[developercertificate.org](https://developercertificate.org/).

## 7. Pull request scope

**One pull request = one independent change.** Split work along lines that will not produce
merge conflicts, and keep refactoring separate from behaviour changes.

- A branch per change, off the development branch, with a short descriptive name
- Do not bundle an unrelated cleanup into a feature PR — it makes the diff unreviewable and
  forces an all-or-nothing decision
- Do not reformat files you did not otherwise touch
- Breaking changes to the `v1alpha1` manifest schema are acceptable while the API group is
  alpha, but say so explicitly in the PR description
- Fill in [the pull request template](.github/pull_request_template.md), including how you
  tested the change

## 8. AI-assisted contributions

Using an AI coding assistant is **not prohibited** — pvectl itself is developed with one.
What is reviewed is the state the change arrives in, not the tool that produced it. You do
not have to disclose that you used one, though you are welcome to.

What is required either way:

- **You understand the diff.** You must be able to explain why each line is there and what
  breaks if it is removed. "The model wrote it" is not an answer to a review question, and
  a pull request whose author cannot answer questions about it will be closed.
- **You ran the tests.** Not "the tests should pass" — you ran them in the container,
  against [`test/pvefake`](test/pvefake/pvefake.go), and watched them pass. Say which
  command you ran in the pull request description. Since no real cluster is needed, there
  is no excuse for an untested change.
- **You verified the facts against this codebase**, not against what a model remembers
  about pvectl. The schema changed recently and generated answers are frequently stale:
  `spec.startOnBoot` no longer exists (it is `spec.runStrategy`),
  `spec.cloudInit.password` no longer exists (it is `cloudInit.passwordFrom`), `spec.raw`
  *does* exist, `exec` and `migrate` *are* implemented, and `unlock` is **not**. Read
  [`internal/resource/vm/types.go`](internal/resource/vm/types.go) and
  [`internal/cmd/root.go`](internal/cmd/root.go) rather than trusting a plausible-looking
  summary.
- **No invented APIs, flags, or citations.** Do not reference a method, field, or ADR
  section that does not exist in the tree you are working on. Quoting an ADR as saying
  something it does not say is worse than not quoting it at all.
- **Generated prose gets the same scrutiny as generated code.** Documentation that
  describes behaviour the code does not have is a bug, and the `examples/` manifests are
  parsed by the test suite precisely so that this gets caught.

None of this is anti-AI. It is the same bar as for hand-written code, stated explicitly
because assistants make it unusually easy to produce a confident, plausible, wrong patch.

## 9. Adding a new resource kind

This is the most valuable contribution path in pvectl, and the architecture is built for
it: the generic verbs (`get` / `describe` / `apply` / `diff` / `delete` / `start` / `stop` /
`exec` / `migrate`) dispatch through a registry, so **a new kind needs a handler and a
registration line — no changes to the command layer**
([ADR-001](docs/adr/ADR-001-resource-model.md)).

### Step 1 — implement the required read side

Create `internal/resource/<kind>/`, and implement
[`resource.Handler`](internal/resource/interfaces.go). Only six methods are mandatory:

```go
type Handler interface {
	GVK() runtime.GVK                   // {Group: "pve.io", Version: "v1alpha1", Kind: "Storage"}
	Aliases() []string                  // lowercase CLI names: "storage", "storages", "sto"
	Columns(wide bool) []printer.Column // table output; the printer stays kind-agnostic

	Get(ctx context.Context, c api.Client, name string) (printer.Object, error)
	List(ctx context.Context, c api.Client) ([]printer.Object, error)
	Describe(ctx context.Context, c api.Client, name string, w io.Writer) error
}
```

A read-only kind — a node exists whether or not a manifest says so — implements exactly
this and nothing more.

### Step 2 — opt in to the write verbs you support

`Apply` and `Delete` are **not** part of `Handler`. They are optional capability
interfaces resolved by type assertion
([ADR-006 §5](docs/adr/ADR-006-api-groups-and-schema.md)), so implement only the ones your
kind can honour:

| Interface | Methods | Enables |
|---|---|---|
| `Applier` | `Apply` + `Diff` | `pvectl apply`, `pvectl diff` |
| `Deleter` | `Delete` | `pvectl delete` |
| `Starter` | `Start` | `pvectl start <kind> <name>` |
| `Stopper` | `Stop` | `pvectl stop <kind> <name>` |

A kind with no lifecycle (Storage, for example) simply omits `Starter` / `Stopper`, and the
verb fails with an explicit "this kind does not support …" rather than a panic or a silent
no-op. Do not add a stub that returns `nil`.

### Step 3 — register it

One line in `DefaultRegistry()` in [`internal/cmd/root.go`](internal/cmd/root.go):

```go
func DefaultRegistry() *resource.Registry {
	reg := resource.NewRegistry()
	reg.Register(vm.NewHandler())
	reg.Register(storage.NewHandler())  // ← your kind
	return reg
}
```

Manifests resolve on an exact `(group, version, kind)` match; the CLI short names resolve
through a separate alias map, and an alias claimed by two kinds is an ambiguity error.

### Step 4 — follow the schema conventions

[`internal/resource/vm`](internal/resource/vm) is the reference implementation. Match its
split: `types.go` (schema) / `decode.go` (strict decode) / `validate.go` / `convert.go`
(manifest → flat API params) / `normalize.go` (live → comparable) / `apply.go` /
`handler.go`.

The conventions your kind must respect:

- **Declared keys only.** Diff and update touch only keys present in the manifest
  ([ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md)). This is not per-kind policy; it
  applies to every kind.
- **Strict decoding.** Unknown fields in `spec` are an error, so typos are caught rather
  than silently dropped.
- **One resource = one document.** Do not build a kind that owns other objects; apply,
  diff, and delete work per object, and a coarser grain breaks idempotency
  ([ADR-006 §4](docs/adr/ADR-006-api-groups-and-schema.md)).
- **Union types use a discriminator, not separate kinds.** `spec.type: nfs` plus a
  `spec.nfs:` sub-struct; any sub-struct not matching `spec.type` is a validation error
  ([ADR-006 §3](docs/adr/ADR-006-api-groups-and-schema.md)).
- **`status` is server-owned.** `get -o yaml` includes it, and `apply` ignores a `status`
  in the input without erroring ([ADR-006 §8](docs/adr/ADR-006-api-groups-and-schema.md)).
- **Round-trip must hold.** `pvectl get <kind> <name> -o yaml` piped back into
  `pvectl apply -f -` must report `unchanged`. Cover it with a test.
- **New API group?** Do not add one yet. Everything stays in `pve.io/v1alpha1` until M3
  ([ADR-006 §1](docs/adr/ADR-006-api-groups-and-schema.md)).
- Prefer `spec.raw` over modelling every API key up front. Frequently used keys get
  promoted to typed fields over time
  ([ADR-006 §7](docs/adr/ADR-006-api-groups-and-schema.md)).

### Step 5 — test it

Handler tests use the `internal/api/apitest` fake; add an end-to-end test under `test/e2e`
that goes through `test/pvefake`, and an example manifest under `examples/` (which the test
suite parses). A handler arriving without tests will be sent back.

## 10. Review and merge

- The maintainer reviews every pull request and is the only person who merges
- **CI must be green** — lint, test, and build all pass — before review starts
- Expect questions about scope first and implementation second: a change that does not fit
  [ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md) will be declined regardless of code
  quality
- Review may take a while. A single maintainer working on this in their own time cannot
  promise a turnaround, and a ping after a week or so is welcome rather than rude
- Merges are squash merges onto a release-ready main branch

This will change once there is more than one maintainer. The current arrangement is
documented so that it is a stated policy rather than a surprise.

## 11. Reporting security issues

**Do not open a public issue for a vulnerability.** Use GitHub's private vulnerability
reporting — see [SECURITY.md](SECURITY.md), which also covers how pvectl handles API
tokens, config file permissions, and keeping secrets out of manifests.

## Licence

pvectl is MIT licensed ([LICENSE](LICENSE)). By opening a pull request you offer your
contribution under that same licence, and you confirm you have the right to do so.
