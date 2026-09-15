<div align="center">
  <img src="images/logo.png" alt="pvectl" width="200"/>

# pvectl

**A kubectl-like CLI for Proxmox VE.**
Declare your virtual machines in YAML/JSON manifests and apply them idempotently.

</div>

## Why

pvectl is declarative convergence with no state file, plus the imperative
operational verbs, in one binary you run from your own machine. Its central
principle — **a field your manifest does not declare does not exist as far as
pvectl is concerned** — keeps server defaults and template-inherited values
from showing up as drift, which is what makes clone-based workflows idempotent
without a `tfstate` or an `import` step.
Full argument and non-goals: [ADR-005](docs/adr/ADR-005-purpose-and-non-goals.md).

## Install

```bash
go install github.com/cyokozai/pvectl/cmd/pvectl@latest
```

No Go toolchain? Cross-build or run it in a container — see [quick-start](docs/quick-start/README.md).

## Example

```yaml
# vm.yaml
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server
spec:
  targetNode: pve-node1
  resources:
    cpu: { cores: 2 }
    memory: 2048
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

Development happens on `dev`; **`main` is not updated until `v1.0.0`.**

| Version | Milestone | Scope |
|---|---|---|
| `v0.1.0` | M1 | `VirtualMachine`: idempotent apply, diff, dry-run, clone, cloud-init, lifecycle |
| `v0.1.x` | M1.5 (current) | `exec` / `migrate` verbs, `spec.raw`, `spec.runStrategy`, `cloudInit.passwordFrom` |
| `v0.2.0` | M2 | LXC containers (`kind: Container`) |
| `v0.3.0` | M3 | Storage, network, snapshots |
| `v0.4.*` | M4 | Pools, users, ACL, HA |
| **`v1.0.0`** | — | M4 complete, tested, feedback addressed, merged to `main` |

New kinds plug into the same verbs through a resource registry — no new commands.

## Development

`make dev-up && make check` runs vet, lint and race tests in a container,
against an in-memory fake Proxmox VE API ([test/pvefake](test/pvefake)) — no
cluster needed. Details in [quick-start/development](docs/quick-start/development.md)
and [docs/dev-process.md](docs/dev-process.md).

## License

[MIT](LICENSE)
