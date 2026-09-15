# Manifests and apply semantics

Reference for the `VirtualMachine` manifest and for what `apply` actually does
with it. If you have not created a VM yet, start with
[quick-start](README.md).

## Shape

Every manifest is `{apiVersion, kind, metadata, spec}`, the same shape kubectl
uses. JSON works anywhere YAML does, and a single file may hold several
documents separated by `---`.

```yaml
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server          # the identity apply resolves by, unless spec.vmid pins one
  labels:
    app: nginx
spec:
  targetNode: pve-node1     # required, immutable
  ...
```

[examples/vm-full.yaml](../../examples/vm-full.yaml) lists every `spec` field
with comments; the other [examples](../../examples/) show the typical shapes
(direct create, clone, multi-document). `pvectl get vm NAME -o yaml` emits the
same shape, and applying its output reports `unchanged`.

## The rule that explains everything else

**A field your manifest does not declare does not exist as far as pvectl is
concerned.** Comparison and update cover exactly the keys the manifest
declares; server-side defaults and generated values (`vmgenid`, `digest`,
`smbios1`, `boot`, …) are ignored permanently. This is why there is no state
file and why re-applying a clone-derived manifest is a no-op rather than a
plan full of noise.

### Deleting a field does not restore the old value

This is a 2-way merge. pvectl does not keep kubectl's
`last-applied-configuration`, so it cannot tell "the user removed this field"
apart from "the user never set it".

> Removing a field from a manifest means **stop managing it**. The live value
> stays as it is.

**This is a specification, not a bug** ([ADR-005
§2](../adr/ADR-005-purpose-and-non-goals.md)). To undo a setting, change
it to the value you want rather than deleting the line — or change it outside
pvectl, with the Web UI or `qm`.

## How apply works

`apply` resolves the VM by `spec.vmid` (if pinned) or `metadata.name`, then:

- **not found** → create — directly, or via `spec.clone` followed by a
  post-clone configuration pass so your declared resources, networks,
  cloud-init, and tags win over the template's values
- **found** → diff only the fields your manifest declares against the live
  config, normalize server noise (disk volume names, generated MACs, tag
  order, ssh-key encoding, `cputype` flags), and `PUT` just the changed keys;
  disk growth becomes a resize call
- **no change** → `unchanged`, zero writes

Dry runs stop earlier: `--dry-run=client` decodes and validates without
touching the API and reports `validated`; `--dry-run=server` reads live state
and reports the would-be `created` / `configured` / `unchanged` without
writing. `pvectl diff -f` prints the managed keys that would change and exits
0 (in sync), 1 (drift), or >1 (error).

### Power state

After the config pass, apply converges the power state to `spec.runStrategy`:

| `runStrategy` | `onboot` | apply |
|---|---|---|
| `Manual` (default) | unmanaged — the key is not generated | never touches the power state |
| `Always` | `1` | starts the VM if it is stopped |
| `Halted` | `0` | stops the VM if it is running |

A power transition counts as a change, so an otherwise identical manifest
reports `configured` when it moves the VM; `unchanged` means the config *and*
the power state already match. `--dry-run=server` reports the transition
without performing it.

`get -o yaml` maps a live `onboot=1` to `Always` and everything else to
`Manual` — never to `Halted`, so replaying exported output cannot stop a VM
whose owner never declared it stopped. The imperative
[`start` / `stop`](README.md#5-imperative-verbs) verbs remain available and
leave no trace in `spec`.

### Guardrails

Detectable mismatches are errors. pvectl never deletes and recreates a VM to
satisfy a manifest.

| | |
|---|---|
| `spec.vmid`, `spec.targetNode` | immutable; a mismatch is an error, never a silent recreate or migration |
| disks | cannot shrink, and cannot change `storage` or `format` in place |
| `spec.clone` + `spec.disks` | mutually exclusive — a clone inherits its disks from the source |
| `spec.pool` | create-only; a mismatch with the live pool is an error, not a silent no-op |
| `spec.clone`, `spec.fullClone` | create-only but accepted on update without a warning: Proxmox records no provenance, so there is no live value to compare. Treat them as a statement of origin that stays in the manifest |
| `cloudInit.passwordFrom` | write-only; set on create, never diffed or updated |

## Secrets

Manifests reference the cloud-init password instead of carrying it, so they
stay safe to commit:

```yaml
spec:
  cloudInit:
    passwordFrom: env:PVE_VM_PASSWORD     # or file:/run/secrets/vmpw
```

`file:` references have their trailing newline trimmed. The reference is
resolved when apply runs; `--dry-run=client` checks the syntax and warns
(rather than fails) when the target is missing, since a client dry-run
validates the manifest, not the machine it runs on.

Because the value never appears in the manifest, there is nothing to compare —
which is exactly why `cipassword` needs no diff rule of its own. The pvectl
config file holds the only long-lived credentials, at mode 0600, and
`config view` redacts them.

## Unmodeled API fields

`spec.raw` passes flat Proxmox API config keys straight through when pvectl has
no typed field for them yet:

```yaml
spec:
  raw:
    hookscript: "local:snippets/hook.pl"
    args: "-cpu host,+vmx"
    bios: ovmf
    machine: q35
```

Values are not validated — a wrong key comes back as a Proxmox API error. The
declared-keys-only rule still holds: only the keys listed under `raw` are
compared and updated. Declaring a key pvectl already generates from a typed
field (`cores`, `scsi0`, `net0`, …) is an error.

## Background

- [ADR-001](../adr/ADR-001-resource-model.md) — resource model and the registry
- [ADR-003](../adr/ADR-003-apply-strategy.md) — managed-key diff, normalization rules, immutability
- [ADR-005](../adr/ADR-005-purpose-and-non-goals.md) — the declared-keys principle, the 2-way merge trade-off, non-goals
- [ADR-006](../adr/ADR-006-api-groups-and-schema.md) — API groups, schema, and the `raw` escape hatch
