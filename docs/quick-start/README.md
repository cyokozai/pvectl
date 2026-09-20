# Quick start

[日本語](README.jp.md)

Read this page top to bottom and you will have pvectl installed, a context
configured, and your first VM created from a manifest.

| | |
|---|---|
| [manifests.md](manifests.md) | manifest fields, what `apply` does, secrets, escape hatch |
| [development.md](development.md) | 開発・テスト・インストール（コンテナ完結） |

## 1. Install

### From a release

Every `v*` tag publishes `pvectl_<version>_<os>_<arch>.tar.gz` for linux and
darwin on amd64 and arm64, plus one `SHA256SUMS` covering all four. Each
archive holds the `pvectl` binary, `LICENSE` and `README.md`.

```bash
version=v0.1.0
target=linux_amd64        # or linux_arm64, darwin_amd64, darwin_arm64
base=https://github.com/cyokozai/pvectl/releases/download/$version

curl -fsSLO "$base/pvectl_${version}_${target}.tar.gz"
curl -fsSLO "$base/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS   # macOS: shasum -a 256 --ignore-missing -c SHA256SUMS

tar -xzf "pvectl_${version}_${target}.tar.gz"
install -m 0755 "pvectl_${version}_${target}/pvectl" ~/.local/bin/pvectl
```

`--ignore-missing` is what lets a single downloaded archive be checked against
a `SHA256SUMS` that lists all four. Without it the three you did not download
are reported as failures.

Prereleases (`-rc`, `-beta`, `-alpha` in the tag) are marked as prereleases on
the releases page, so `/releases/latest` never resolves to one.

### With a Go toolchain

```bash
go install github.com/cyokozai/pvectl/cmd/pvectl@latest
```

### Neither

No release build for your platform and no Go toolchain on the host? Cross-build
inside the dev container — the full recipe is in
[development.md](development.md):

```bash
make dev-up
docker compose exec dev sh -c 'GOOS=darwin GOARCH=arm64 go build -o pvectl-darwin ./cmd/pvectl'
install -m 0755 pvectl-darwin ~/.local/bin/pvectl && rm pvectl-darwin
```

The [Dockerfile](../../Dockerfile) also builds a distroless image whose
entrypoint is pvectl itself:

```bash
docker build -t pvectl .
docker run --rm -v ~/.pvectl/config:/config:ro pvectl --config /config get vm
```

Check the install with `pvectl version`, and generate a completion script for
your shell with `pvectl completion bash|zsh|fish|powershell`
(see [development.md](development.md) for the zsh wiring).

## 2. Configure a context

pvectl reads `~/.pvectl/config`, overridable with `--config` or
`PVECTL_CONFIG`. The file is kubeconfig-shaped: `users` hold credentials,
`nodes` hold endpoints, and a `context` pairs one of each.

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
      # insecureSkipTLSVerify: true    # for a self-signed certificate
contexts:
  - name: production
    context:
      user: admin@pam
      node: prod-pve
current-context: production
```

Each user carries either a `token` (`user@realm!tokenid=secret`) **or**
`username` + `password` — exactly one of the two.
[examples/config.yaml](../../examples/config.yaml) shows both shapes and two
contexts. pvectl writes the file with mode 0600, and `config view` redacts
every secret before printing.

```bash
pvectl config view                    # the config with secrets REDACTED
pvectl config get-contexts            # every context, current one marked
pvectl config current-context
pvectl config use-context development # switch the default
pvectl get vm --context production    # or override for one command
```

## 3. Apply your first VM

```yaml
# vm.yaml
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server
spec:
  targetNode: pve-node1
  resources:
    cpu:
      cores: 2
    memory: 2048
  disks:
    - name: scsi0
      size: 32G
      storage: local-lvm
  networks:
    - name: net0
      bridge: vmbr0
```

```bash
pvectl apply -f vm.yaml --dry-run=client   # validate the manifest, zero API calls
pvectl apply -f vm.yaml --dry-run=server   # read live state, report the would-be action
pvectl apply -f vm.yaml                    # converge
pvectl get vm web-server
pvectl describe vm web-server
```

`apply` is idempotent: run it again and it reports `unchanged` without writing
anything. [manifests.md](manifests.md) explains what it compares, which fields
are immutable, and how `spec.runStrategy` controls the power state.

## 4. Verbs

| Verb | |
|---|---|
| `get TYPE [NAME...]` | list or show resources |
| `describe TYPE NAME` | detailed live state |
| `apply -f FILE` | create or update from manifests (idempotent) |
| `diff -f FILE` | managed-key diff; exit 0 in sync, 1 drift, >1 error |
| `delete TYPE NAME` / `delete -f FILE` | delete by name or by manifest |
| `start TYPE NAME` / `stop TYPE NAME` | power a guest on or off |
| `exec TYPE NAME -- CMD` | run a command in a guest via the QEMU guest agent |
| `migrate TYPE NAME --to NODE` | move a guest to another node |
| `config SUBCOMMAND` | `view` / `get-contexts` / `current-context` / `use-context` |
| `completion SHELL` | `bash` / `zsh` / `fish` / `powershell` |
| `version` | version, commit, and build date |

`TYPE` is a kind or one of its aliases: `vm`, `vms`, `virtualmachine`, and
`virtualmachines` all name `VirtualMachine`. New kinds join through a resource
registry, so they gain every verb above without a new command.

Global flags, accepted by every verb:

| Flag | |
|---|---|
| `--config` | config file path (env `PVECTL_CONFIG`, default `~/.pvectl/config`) |
| `--context` | context to use (env `PVECTL_CONTEXT`, default `current-context`) |
| `-o`, `--output` | `table` \| `wide` \| `yaml` \| `json` \| `name` |
| `--timeout` | how long to wait for a Proxmox task, default `5m` |

Every mutation waits for its Proxmox task (UPID) to finish and turns a failed
task into a non-zero exit code, so `apply` in a pipeline never reports success
for work the server rejected.

`-f` is repeatable and accepts a file, a directory, or `-` for stdin:

```bash
pvectl get vms -o wide
pvectl get vm web-server -o yaml      # round-trips: applying this reports unchanged
pvectl apply -f a.yaml -f b.yaml
pvectl apply -f manifests/            # every *.yaml / *.yml / *.json in the directory
cat vm.yaml | pvectl apply -f -
pvectl delete -f vm.yaml
```

## 5. Imperative verbs

`start`, `stop`, `exec`, and `migrate` are one-off operations. They leave no
trace in any manifest: nothing about "I ran this once" is a desired state to
converge on, so nothing about them belongs in `spec`. The declarative
counterpart for the power state is
[`spec.runStrategy`](manifests.md#power-state).

`exec` runs a command inside a guest without SSH, through the QEMU guest
agent, and adopts the guest's stdout, stderr, and exit code as its own:

```bash
pvectl exec vm web-server -- systemctl is-active nginx
pvectl exec vm web-server -- /bin/sh -c "df -h / | tail -1"
pvectl exec vm web-server --exec-timeout 5m -- apt-get -y dist-upgrade
```

Everything after `--` reaches the guest untouched. The guest needs `agent: 1`
in its config and a running `qemu-guest-agent`. The wait is bounded by
`--exec-timeout` (default 60s), which is deliberately separate from the global
`--timeout`: waiting on a Proxmox task and waiting on a command someone just
typed are different kinds of waiting.

`migrate` is the only verb that moves a guest between nodes, and `--online`
live-migrates a running one:

```bash
pvectl migrate vm web-server --to pve2
pvectl migrate vm web-server --to pve2 --online
```

`apply` never migrates — when `spec.targetNode` disagrees with the node the
guest actually runs on, it reports an error instead of moving it.

## Next

- [manifests.md](manifests.md) — every `spec` field and the exact semantics of `apply`
- [examples/](../../examples/) — annotated manifests for direct create, clone, and multi-document files
- [../adr/](../adr/) — why pvectl works this way
