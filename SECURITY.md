> [日本語](SECURITY.jp.md)

# Security Policy

## Supported versions

pvectl releases track the milestones in [docs/prd.md](docs/prd.md):

| Version | Milestone | Status |
|---|---|---|
| `v0.1.x` | M1 — `VirtualMachine` (qemu) | **Supported** |
| `v0.2.0` | M2 — `Container` (LXC) | Planned |
| `v0.3.0` | M3 — `Storage` / `Network` / `Snapshot` | Planned |
| `v0.4.*` | M4 — `Pool` / `User` / `ACL` / `HA` | Planned |
| `v1.0.0` | After M4 is complete | Planned |

**Only `v0.1.x` receives security fixes today.** Fixes land on the latest `v0.1.x`
patch release; there is no backporting to earlier patch versions. The manifest API group
is `pve.io/v1alpha1` and breaking schema changes are expected before `v1.0.0`, so please
report against the newest release before assuming a bug is still present.

## Reporting a vulnerability

**Do not open a public issue, pull request, or discussion for a security problem.**

Use GitHub's **Private vulnerability reporting**:

1. Go to <https://github.com/cyokozai/pvectl/security/advisories/new>
2. Describe the issue, the affected version (`pvectl version` output), and the impact
3. Include a reproduction if you have one — a manifest and a command line is ideal.
   **Strip real tokens, passwords, and hostnames first.**

This opens a private advisory visible only to you and the maintainer, where a fix can be
prepared and a CVE requested before anything is public.

If private reporting is unavailable to you for any reason, contact the maintainer through
the details on their GitHub profile at <https://github.com/cyokozai>. As a last resort,
open a public issue that says **only** that you need to report a security concern, with
no technical details, and wait to be contacted.

pvectl has a single maintainer working on it in their own time. There is no guaranteed
response window, but reports are triaged ahead of feature work. Please allow a reasonable
period for a fix before disclosing publicly.

## What is not pvectl's to fix

Vulnerabilities in **Proxmox VE itself** go to the
[Proxmox security contact](https://www.proxmox.com/en/about/security), not here. The same
applies to API behaviour that pvectl merely passes through — in particular anything
supplied via `spec.raw`, which is forwarded to the API **unvalidated** by design
([ADR-006 §7](docs/adr/ADR-006-api-groups-and-schema.md)).

## How pvectl handles secrets

These are the properties pvectl intends to hold. A deviation from any of them is a
vulnerability worth reporting.

### The config file holds API credentials

`~/.pvectl/config` stores connection contexts including **API tokens and passwords** — it
is as sensitive as a `kubeconfig`.

- pvectl writes it with mode **`0600`** (owner read/write only), in
  `internal/config/loader.go`
- The path can be overridden with `--config` or `PVECTL_CONFIG`; precedence is
  `--config` > `PVECTL_CONFIG` > `~/.pvectl/config`
  ([ADR-004](docs/adr/ADR-004-config-context.md))
- **Do not commit this file, and do not mount it into a container that runs untrusted
  code.** The dev container deliberately does not mount host secrets — see
  [docs/dev-process.md §2](docs/dev-process.md)

### `pvectl config view` redacts secrets

`pvectl config view` replaces the `token` and `password` fields of every user entry with
the literal string `REDACTED` before printing. It is the safe command to paste into a bug
report; `cat ~/.pvectl/config` is not.

Note that redaction applies to `config view` output. If you paste raw output from any
other command, check it yourself.

### Keep passwords out of manifests: `cloudInit.passwordFrom`

Manifests are meant to live in version control, so pvectl has **no field that carries a
cloud-init password inline**. The earlier `spec.cloudInit.password` field was removed and
replaced by an external reference:

```yaml
spec:
  cloudInit:
    user: admin
    passwordFrom: env:PVE_VM_PASSWORD        # an environment variable
    # passwordFrom: file:/run/secrets/vmpw   # or a file's contents
```

- Only `env:NAME` and `file:/path` are accepted; anything else is a validation error
- The reference is resolved at `apply` time, and the resolved value is held in an
  unexported field so it can never be written back out to YAML or JSON
- Reference **syntax** is validated without reading the environment, so
  `--dry-run=client` works on a machine that does not hold the secret
- The value is write-only: the Proxmox API masks `cipassword`, so pvectl sets it on
  create and never diffs or updates it

A `passwordFrom` string is safe to commit and safe to paste into an issue. The value it
points at is not.

### Keeping secrets out of manifests you commit

If you put manifests in git — the intended workflow — then:

- Never inline a password. Use `passwordFrom` with `env:` or `file:`
- `sshKeys` entries are **public** keys and are fine to commit; private keys are never an
  input to pvectl
- `pvectl get <kind> <name> -o yaml` is round-trippable output and is safe to commit, but
  read it before committing rather than assuming
- Point `file:` references at a path outside the repository (`/run/secrets/…`), so a
  missing `.gitignore` entry cannot leak the value
- Review `spec.raw` by hand. Its keys are passed to the Proxmox API unvalidated, so a
  secret placed there is neither detected nor redacted by pvectl
- Prefer an **API token** with the narrowest privileges the manifests need over a root
  password

## Dependencies

Dependency updates arrive via Dependabot (gomod / github-actions / docker, weekly). The
Proxmox SDK is not semver-versioned, so its updates are validated per pull request rather
than merged automatically ([ADR-002](docs/adr/ADR-002-api-client.md)). A vulnerability in
a dependency that is reachable from pvectl is in scope; please report it privately as
above so the upgrade and the advisory can be published together.
