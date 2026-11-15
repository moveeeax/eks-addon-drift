# Examples

Sample output from `eks-addon-drift check` against a cluster `prod` running
Kubernetes `1.29`, with one add-on on the latest compatible version, one behind,
and one pinned to a version no longer compatible.

## Table (default)

```
$ eks-addon-drift check
CLUSTER  K8S   ADDON       CURRENT             RECOMMENDED         STATUS
prod     1.29  coredns     v1.11.1-eksbuild.9  v1.11.1-eksbuild.9  ok
prod     1.29  kube-proxy  v1.29.0-eksbuild.1  v1.29.3-eksbuild.2  behind
prod     1.29  vpc-cni     v1.16.0-eksbuild.1  v1.18.3-eksbuild.2  unsupported

1 ok, 1 behind, 1 unsupported
```

## Target version

```
$ eks-addon-drift check --target-version next
CLUSTER  K8S   ADDON       CURRENT             RECOMMENDED         TARGET  TARGET-COMPATIBLE  TARGET-RECOMMENDED  STATUS
prod     1.29  coredns     v1.11.1-eksbuild.9  v1.11.1-eksbuild.9  1.30    yes                v1.11.3-eksbuild.1  ok
prod     1.29  kube-proxy  v1.29.0-eksbuild.1  v1.29.3-eksbuild.2  1.30    no                 v1.30.0-eksbuild.3  blocks-upgrade
prod     1.29  vpc-cni     v1.16.0-eksbuild.1  v1.18.3-eksbuild.2  1.30    no                 v1.18.3-eksbuild.2  unsupported

1 ok, 0 behind, 1 blocks-upgrade, 1 unsupported
```

In JSON each finding then also carries `targetVersion`, `targetCompatible` and
`targetRecommended`, and the summary's `blocksUpgrade` count is filled in.

## JSON

[`sample-output.json`](sample-output.json) — `eks-addon-drift check -o json`.

## SARIF

[`sample-output.sarif`](sample-output.sarif) — `eks-addon-drift check -o sarif`.
Upload it to GitHub code scanning to see drift as annotations. Only `behind`
(warning), `blocks-upgrade` (error) and `unsupported` (error) add-ons become
SARIF results.
