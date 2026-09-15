<div align="center">
  <img src="images/logo.png" alt="pvectl" width="200"/>

# pvectl

**A kubectl-like CLI for Proxmox VE.**
Declare your virtual machines in YAML/JSON manifests and apply them idempotently.

</div>

## Goals

- **kubectl-like UX** — the verbs you already know: `get`, `describe`, `apply`, `diff`, `delete`
- **Declarative management (IaC)** — resources described as YAML or JSON manifests; `apply` converges live state to the manifest and is safe to re-run
- **Multi-context** — a kubeconfig-style `~/.pvectl/config` switches between clusters and credentials
- **Task-aware** — every mutation waits for the Proxmox task (UPID) to finish and surfaces its exit status

pvectl talks to the Proxmox VE REST API through
[Telmate/proxmox-api-go](https://github.com/Telmate/proxmox-api-go),
wrapped behind an internal interface so resource logic stays testable
without a real cluster.

## Install

```bash
go install github.com/cyokozai/pvectl/cmd/pvectl@latest
```

Or build from source (runs in a container, keeps your host clean):

```bash
make dev-up && make build
```

## Configuration

`~/.pvectl/config` (override with `--config` or `PVECTL_CONFIG`):

```yaml
apiVersion: v1
kind: Config
users:
  - name: admin@pam
    user:
      token: admin@pam!pvectl=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
nodes:
  - name: prod-pve
    node:
      server: https://pve.example.com:8006
contexts:
  - name: production
    context:
      user: admin@pam
      node: prod-pve
current-context: production
```

Authentication is either an API token (`user@realm!tokenid=secret`) or
`username` + `password` — exactly one per user. See
[examples/config.yaml](examples/config.yaml).

## Usage

```bash
# Observe
pvectl get vm                        # table of all VMs
pvectl get vm web-server -o yaml     # full manifest-shaped output
pvectl get vms -o wide               # more columns
pvectl describe vm web-server

# Declare
pvectl apply -f vm.yaml              # create or update (idempotent)
pvectl apply -f a.yaml -f b.yaml     # multiple files
pvectl apply -f manifests/           # every manifest in a directory
cat vm.yaml | pvectl apply -f -      # stdin
pvectl apply -f vm.yaml --dry-run=server   # show the would-be action
pvectl diff -f vm.yaml               # exit 0 = in sync, 1 = drift

# Operate
pvectl start vm web-server
pvectl stop vm web-server
pvectl delete vm web-server
pvectl delete -f vm.yaml

# Contexts
pvectl config get-contexts
pvectl config use-context development
pvectl get vm --context production   # one-off override

# Shell completion
pvectl completion bash|zsh|fish
```

Global flags: `--config`, `--context`, `-o/--output table|wide|yaml|json|name`,
`--timeout` (task wait bound, default 5m).

### How apply works

`apply` resolves the VM by `spec.vmid` (if pinned) or `metadata.name`,
then:

- **not found** → create — directly, or via `spec.clone` followed by a
  post-clone configuration pass so your declared resources, networks,
  cloud-init, and tags win over the template's values
- **found** → diff only the fields your manifest declares against the
  live config, normalize server noise (disk volume names, generated MACs,
  tag order, ssh-key encoding), and `PUT` just the changed keys;
  disk growth becomes a resize call
- **no change** → `unchanged`, zero writes

Guardrails: `vmid` and `targetNode` are immutable (mismatch is an error,
never a silent recreate); disks cannot shrink or change storage/format
in place; `clone`/`pool` are create-only (warned and ignored on update);
`cloudInit.password` is write-only.

`pvectl get vm NAME -o yaml` round-trips: applying its output reports
`unchanged`.

## Manifest reference

See [examples/vm-full.yaml](examples/vm-full.yaml) for every field with
comments, and the other [examples](examples/) for typical shapes
(direct create, clone, multi-document). JSON manifests work anywhere
YAML does.

## Roadmap

| Milestone | Scope |
|-----------|-------|
| **M1 (current)** | VirtualMachine: idempotent apply, diff, dry-run, clone, cloud-init, lifecycle |
| M2 | LXC containers (`kind: Container`) |
| M3 | Storage, network, snapshots |
| M4 | Pools, users, ACL, HA |

New kinds plug into the same verbs through a resource registry — no new
commands.

## Development

Everything runs in a container (see [docs/dev-process.md](docs/dev-process.md)):

```bash
make dev-up     # build & start the dev container
make check      # vet + lint + race tests
make build      # build ./pvectl with version ldflags
```

Tests run against an in-memory fake Proxmox VE API
([test/pvefake](test/pvefake)) — no cluster needed.

## License

[MIT](LICENSE)
