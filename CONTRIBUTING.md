> [日本語](CONTRIBUTING.jp.md)

# Contributing to pvectl

Thanks for your interest in pvectl. By participating you agree to the
[Code of Conduct](CODE_OF_CONDUCT.md). Security problems go through
[SECURITY.md](SECURITY.md), never a public issue.

## 1. Scope — read this before writing code

pvectl is deliberately narrow, and the central principle is:

> **A field that is not written in the manifest does not exist as far as pvectl is concerned.**

Comparison and update target only the keys a manifest declares. Removing a field means
"stop managing it", not "restore the server default". This is a 2-way merge: there is no
`last-applied-configuration` and no state file, so "the field I deleted did not revert" is
the specified behaviour, not a bug.

**Proposals that contradict this principle are not accepted, however useful they are.**
What pvectl adds on top of the Proxmox VE REST API is limited to four layers — auth and
connection, convergence, task waiting, and manifest validation. Being a wider wrapper
around the API is not a goal. See
[ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md) for the reasoning.

### Already decided against

Reopening one of these needs a new ADR, not a pull request:

- A local state file equivalent to `tfstate` — the server is the single source of truth
- A resident reconcile loop or `watch` — `apply` is a one-shot convergence
- `--prune`, until ownership semantics are designed (M3 or later)
- Implicit live migration during `apply` — offered only as the `migrate` verb
- Building the Proxmox cluster itself, or adding nodes
- Exhaustive coverage of the `qm` command surface — `spec.raw` is the escape hatch
- Node-local interactive operations (`qm terminal` / `monitor` / `sendkey`)

### The two questions a feature must pass

1. Does it fit inside "manage declared keys only"?
2. Is it a **persisting desired state** (belongs in `spec`) or a **one-shot action**
   (belongs in an imperative verb, leaving no trace in `spec`)?

`spec.runStrategy` (`Halted` / `Always` / `Manual`) is the declarative side;
`pvectl start` / `stop` / `exec` / `migrate` are the imperative side.

### Open an issue first

For anything beyond a typo or an obvious bug fix, **open an issue and get agreement before
you write the code** — a large pull request that turns out to be out of scope wastes your
time more than anyone's. Small, self-evident fixes can go straight to a pull request.

Issues labelled **`help wanted`** or **`good first issue`** are the exception: the scope
question is already settled, so claim it in a comment and send the pull request.

## 2. Development environment

Everything runs in a container — do not install Go or linters on your host. The setup,
make targets, test layers, and release checklist all live in
**[docs/dev-process.md](docs/dev-process.md)**. Follow that document; it is the single
source of truth for the workflow and is deliberately not repeated here.

## 3. Tests

**You do not need a real Proxmox VE cluster.** [`test/pvefake`](test/pvefake/pvefake.go)
is an `httptest`-based fake PVE API covering ticket login, token auth, and task (UPID)
polling, so the whole stack runs offline.

Every behavioural change needs a test, and bug fixes start from a failing reproduction
test. Match the layer you changed ([docs/dev-process.md §1](docs/dev-process.md) has the
full table):

- Pure functions (convert / normalize / validate / diff) → table tests
- Output formatting → golden files (`go test ./internal/printer -update` regenerates)
- Resource handlers → the `internal/api/apitest` fake client
- End to end → `test/e2e` against `test/pvefake`

Manifests under [`examples/`](examples/) are parsed by the test suite, so a schema change
must update them too. That is intentional — it keeps the docs from rotting.

## 4. Commits and pull requests

**One pull request = one independent change.** Keep refactoring separate from behaviour
changes, do not reformat files you did not otherwise touch, and fill in
[the pull request template](.github/PULL_REQUEST_TEMPLATE.md). Breaking changes to the
`v1alpha1` manifest schema are acceptable while the API group is alpha, but say so in the
description.

Commit messages use **Conventional Commits**; bodies may be Japanese or English.
[`.gitmessage`](.gitmessage) is the template (`git config commit.template .gitmessage`):

```
<type>(<scope>): <description>
```

- Types: `fix` / `feat` / `docs` / `style` / `refactor` / `perf` / `test` / `chore`
- Scope is the package or area touched (`vm`, `cmd`, `api`, `resource`, `docs`, …)
- Append `!` for a breaking change: `feat(vm)!: replace startOnBoot with runStrategy`
- The emoji table in `.gitmessage` is a reference list; the real history uses no emoji, so
  do not add them. Run `git log --oneline` and match what you see
- **No sign-off or CLA is required.** `git commit -s` is welcome if you prefer to state
  your provenance, but nothing is blocked for missing it

## 5. AI-assisted contributions

Using an AI coding assistant is **not prohibited** — pvectl itself is developed with one.
What is reviewed is the state the change arrives in, not the tool that produced it, and
you do not have to disclose that you used one. What is required either way:

- **You understand the diff.** You can explain why each line is there and what breaks if
  it is removed. "The model wrote it" is not an answer to a review question, and a pull
  request whose author cannot answer questions about it will be closed.
- **You ran the tests.** Not "they should pass" — you ran them in the container against
  [`test/pvefake`](test/pvefake/pvefake.go) and watched them pass, and you say which
  command you ran. No real cluster is needed, so there is no excuse for an untested change.
- **You verified the facts against this codebase**, not against what a model remembers.
  Generated answers are frequently stale: `spec.startOnBoot` no longer exists (it is
  `spec.runStrategy`), `spec.cloudInit.password` no longer exists (it is
  `cloudInit.passwordFrom`), `spec.raw` *does* exist, `exec` and `migrate` *are*
  implemented, and `unlock` is **not**. Read
  [`internal/resource/vm/types.go`](internal/resource/vm/types.go) and
  [`internal/cmd/root.go`](internal/cmd/root.go).
- **No invented APIs, flags, or citations.** Quoting an ADR as saying something it does not
  say is worse than not quoting it at all.
- **Generated prose gets the same scrutiny as generated code.** Docs describing behaviour
  the code does not have are a bug.

This is the same bar as for hand-written code, stated explicitly because assistants make it
unusually easy to produce a confident, plausible, wrong patch.

## 6. Adding a new resource kind

This is the most valuable contribution path, and the architecture is built for it: the
generic verbs dispatch through a registry, so **a new kind needs a handler and one
registration line — no changes to the command layer** ([ADR-001](docs/adr/ADR-001-resource-model.md)).

1. **Implement the read side.** In `internal/resource/<kind>/`, implement
   [`resource.Handler`](internal/resource/interfaces.go). Only six methods are mandatory:
   `GVK`, `Aliases`, `Columns`, `Get`, `List`, `Describe`. A read-only kind — a node exists
   whether or not a manifest says so — implements exactly this and nothing more.
2. **Opt in to the write verbs you support.** `Apply` and `Delete` are *not* part of
   `Handler`. They are optional capability interfaces resolved by type assertion
   ([ADR-006 §5](docs/adr/ADR-006-api-groups-and-schema.md)): `Applier` (`Apply` + `Diff`),
   `Deleter`, `Starter`, `Stopper`. Implement only what your kind can honour — omitting one
   makes the verb fail with an explicit "not supported" message, which is the desired
   behaviour. Do not add a stub that returns `nil`.
3. **Register it** with one line in `DefaultRegistry()` in
   [`internal/cmd/root.go`](internal/cmd/root.go). Manifests resolve on an exact
   `(group, version, kind)` match; CLI short names resolve through a separate alias map,
   and an alias claimed by two kinds is an ambiguity error.
4. **Follow the conventions.** [`internal/resource/vm`](internal/resource/vm) is the
   reference implementation — read it and match its file split (`types.go` / `decode.go` /
   `validate.go` / `convert.go` / `normalize.go` / `apply.go` / `handler.go`). Non-obvious
   rules: unknown `spec` fields are an error (strict decode); one resource = one document;
   union types use a `spec.type` discriminator rather than separate kinds
   ([ADR-006 §3](docs/adr/ADR-006-api-groups-and-schema.md)); `status` is server-owned and
   `apply` ignores it; everything stays in `pve.io/v1alpha1` for now; prefer `spec.raw`
   over modelling every API key up front.
5. **Test it.** Handler tests use the `internal/api/apitest` fake, plus an end-to-end test
   in `test/e2e` through `test/pvefake` and an example manifest in `examples/`. Also cover
   the round-trip: `get <kind> <name> -o yaml` piped into `apply -f -` must report
   `unchanged`. A handler arriving without tests will be sent back.

## 7. Review and merge

**pvectl has one maintainer, [@cyokozai](https://github.com/cyokozai), who reviews every
pull request and merges every pull request.** No one else has merge rights.

- **CI must be green** — lint, test, and build all pass — before review starts
- Expect questions about scope first and implementation second. A change that does not fit
  [ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md) will be declined regardless of code
  quality, which is why §1 comes before everything else
- Review may take a while, since this is one person working in their own time. A ping after
  a week or so is welcome rather than rude
- Merges are squash merges

## Licence

pvectl is MIT licensed ([LICENSE](LICENSE)). By opening a pull request you offer your
contribution under that same licence, and you confirm you have the right to do so.
