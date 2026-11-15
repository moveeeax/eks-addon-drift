# eks-addon-drift

[![ci](https://github.com/moveeeax/eks-addon-drift/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/eks-addon-drift/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

Every EKS cluster ships `coredns`, `kube-proxy` and `vpc-cni` as managed add-ons,
and every Kubernetes minor bump quietly widens the gap between what you run and
what AWS recommends. **eks-addon-drift** reads your live add-on versions, asks EKS
which versions are compatible with the cluster's control-plane version, and tells
you exactly what is behind before an upgrade bites you.

It is **read-only** — only `List`/`Describe` EKS calls — so it is safe to run in CI.

## What it does

For every managed add-on on every cluster (or a filtered subset) it reports one of:

- **ok** — on the latest version compatible with the cluster's Kubernetes version.
- **behind** — older, but still compatible.
- **blocks-upgrade** — compatible today, but not compatible with the
  `--target-version` you plan to upgrade to. Only reported when
  `--target-version` is set.
- **unsupported** — the running version is not in the compatible set for the
  cluster's Kubernetes version (an upgrade risk).

Output as a table, JSON, or SARIF, with a `--fail-on` gate for pipelines.

## Install

```shell
go install github.com/moveeeax/eks-addon-drift@latest
```

Or grab a source checkout and `go build .`.

## Usage

```shell
# Check every EKS cluster in the current account/region
eks-addon-drift check

# One cluster, JSON output
eks-addon-drift check --cluster prod --output json > drift.json

# Fail CI when any add-on is unsupported, emit SARIF for code scanning
eks-addon-drift check --output sarif --fail-on unsupported > addons.sarif

# Before a control-plane bump: which add-ons must be upgraded first?
eks-addon-drift check --target-version next --fail-on blocks-upgrade
```

Example table:

```
CLUSTER  K8S   ADDON       CURRENT             RECOMMENDED         STATUS
prod     1.29  coredns     v1.11.1-eksbuild.9  v1.11.1-eksbuild.9  ok
prod     1.29  kube-proxy  v1.29.0-eksbuild.1  v1.29.3-eksbuild.2  behind
prod     1.29  vpc-cni     v1.16.0-eksbuild.1  v1.18.3-eksbuild.2  unsupported

1 ok, 1 behind, 1 unsupported
```

With `--target-version` the table gains three columns: the target version,
whether the running add-on version is compatible with it, and the latest
version compatible with it:

```
CLUSTER  K8S   ADDON       CURRENT             RECOMMENDED         TARGET  TARGET-COMPATIBLE  TARGET-RECOMMENDED  STATUS
prod     1.29  coredns     v1.11.1-eksbuild.9  v1.11.1-eksbuild.9  1.30    yes                v1.11.3-eksbuild.1  ok
prod     1.29  kube-proxy  v1.29.0-eksbuild.1  v1.29.3-eksbuild.2  1.30    no                 v1.30.0-eksbuild.3  blocks-upgrade
prod     1.29  vpc-cni     v1.16.0-eksbuild.1  v1.18.3-eksbuild.2  1.30    no                 v1.18.3-eksbuild.2  unsupported

1 ok, 0 behind, 1 blocks-upgrade, 1 unsupported
```

See [`examples/`](examples/) for full JSON and SARIF samples.

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--cluster` | all clusters | Cluster name to check; repeat for several. |
| `--region` | env/profile | AWS region. |
| `-o, --output` | `table` | `table`, `json`, or `sarif`. |
| `--fail-on` | `none` | Exit non-zero when a finding is at least this severe: `none`, `behind`, `blocks-upgrade`, `unsupported`. |
| `--target-version` | off | Also check every add-on against this Kubernetes version: `MAJOR.MINOR` (e.g. `1.31`) or `next` for each cluster's minor + 1. |

Severity order for `--fail-on`: `ok` < `behind` < `blocks-upgrade` < `unsupported`.
An add-on that is already `unsupported` stays `unsupported` even when it is also
incompatible with the target.

`--target-version` notes:

- Clusters already on or above an explicit target get no target check (their
  target fields are empty).
- If EKS returns no add-on versions at all for the target Kubernetes version
  (a typo, or a version EKS does not offer yet in the region), the run fails
  with exit code `1` instead of marking every add-on `blocks-upgrade`.
- JSON findings gain `targetVersion`, `targetCompatible` and `targetRecommended`;
  without the flag those fields are omitted. The JSON summary always has a
  `blocksUpgrade` count (`0` without the flag).
- SARIF reports `blocks-upgrade` add-ons under rule `addon-blocks-upgrade`
  (level `error`).

Exit codes: `0` clean (or drift below `--fail-on`), `2` drift met the `--fail-on`
threshold, `1` an error occurred.

## How it works

1. `ListClusters` (or your `--cluster` subset), then `DescribeCluster` for each
   cluster's Kubernetes version.
2. `ListAddons` + `DescribeAddon` for each installed add-on's current version.
3. `DescribeAddonVersions` scoped to the cluster's Kubernetes version returns the
   compatible set; the highest of those is the recommended version.
4. With `--target-version`, one more `DescribeAddonVersions` per add-on scoped
   to the target version, plus one unscoped call per target version to confirm
   EKS offers it. The add-on is `blocks-upgrade` when its running version is
   not in the target's compatible set.
5. Version comparison understands the `vX.Y.Z-eksbuild.N` scheme AWS uses, so a
   bumped `eksbuild` counter is ordered correctly against a minor/patch bump.

## Credentials

Standard AWS SDK credential resolution (env vars, shared config, IRSA, instance
profile). The least-privilege policy is in [`docs/iam-policy.json`](docs/iam-policy.json):

```json
{
  "Effect": "Allow",
  "Action": [
    "eks:ListClusters",
    "eks:DescribeCluster",
    "eks:ListAddons",
    "eks:DescribeAddon",
    "eks:DescribeAddonVersions"
  ],
  "Resource": "*"
}
```

## Development

```shell
go test ./...
go build .
```

## License

MIT — see [LICENSE](LICENSE).
