<div align="center">
  <img src="images/logo.png" alt="pvectl" width="200"/>

# pvectl

**A kubectl-like CLI for Proxmox VE.** · [日本語](README.jp.md)
Declare your virtual machines in YAML/JSON manifests and apply them idempotently.

</div>

## Why

pvectl is declarative convergence with no state file, plus the imperative
operational verbs, in one binary you run from your own machine. Its central
principle — **a field your manifest does not declare does not exist as far as
pvectl is concerned** — keeps server defaults and template-inherited values from
showing up as drift. Full argument and non-goals: [ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md).

## Install

Grab a tarball for your platform (linux/darwin × amd64/arm64) from the
[latest release](https://github.com/cyokozai/pvectl/releases/latest), verify it
against the `SHA256SUMS` published beside it, and put `pvectl` on your `PATH`:

```bash
sha256sum --ignore-missing -c SHA256SUMS   # macOS: shasum -a 256 --ignore-missing -c
tar -xzf pvectl_v0.1.0_linux_amd64.tar.gz
```

With a Go toolchain:

```bash
go install github.com/cyokozai/pvectl/cmd/pvectl@latest
```

Neither? Cross-build or run it in a container — see [quick-start](docs/quick-start/README.md).

## Example

```yaml
# vm.yaml
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata: { name: web-server }
spec:
  targetNode: pve-node1
  resources: { cpu: { cores: 2 }, memory: 2048 }
  disks:
    - { name: scsi0, size: 32G, storage: local-lvm }
  networks:
    - { name: net0, bridge: vmbr0 }
```

```bash
pvectl apply -f vm.yaml            # create or update; re-running writes nothing
pvectl diff -f vm.yaml             # exit 0 in sync, 1 drift
pvectl get vm web-server -o yaml   # round-trips back into apply
```

## Documentation

| | |
|---|---|
| [quick-start](docs/quick-start/README.md) | install, configure a context, apply your first VM, every verb and flag |
| [quick-start/manifests](docs/quick-start/manifests.md) | `spec` reference, what `apply` does, power state, secrets, escape hatch |
| [quick-start/development](docs/quick-start/development.md) | 開発・テスト・インストール（コンテナ完結） |
| [examples/](examples/) | annotated manifests: direct create, clone, multi-document |
| [docs/adr/](docs/adr/) | architecture decisions |
| [docs/prd.md](docs/prd.md) | product requirements and milestones |

## Roadmap

| Version | Milestone | Scope |
|---|---|---|
| `v0.1.0` (next) | M1 + M1.5 | `VirtualMachine`: idempotent apply, diff, dry-run, clone, cloud-init, lifecycle, `exec` / `migrate`, `spec.raw`, `spec.runStrategy`, `cloudInit.passwordFrom` |
| `v0.2.0` | M2 | LXC containers (`kind: Container`) |
| `v0.3.0` | M3 | Storage, network, snapshots |
| `v0.4.*` | M4 | Pools, users, ACL, HA |
| **`v1.0.0`** | — | M4 complete, tested, feedback addressed — the manifest schema stabilizes and leaves `v1alpha1` |

Development happens on `dev`; **`main` tracks released versions** — every release merges `dev` into `main` and is tagged there. A `v0.x` release may still break the schema. New kinds plug into the same verbs through a resource registry — no new commands.

## Contributing

Open an issue before writing a feature — pvectl has a narrow scope, and
proposals outside it are declined on purpose
([non-goals](docs/adr/ADR-005-purpose-and-non-goals.md)). Commits need a DCO
sign-off (`git commit -s`). There is one maintainer, so reviews are
best-effort. Details: [CONTRIBUTING.md](CONTRIBUTING.md),
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), [SECURITY.md](SECURITY.md).

## Development

`make dev-up && make check` runs vet, lint and race tests in a container,
against an in-memory fake Proxmox VE API ([test/pvefake](test/pvefake)) — no
cluster needed. Details in [quick-start/development](docs/quick-start/development.md).

## License

[MIT](LICENSE)
