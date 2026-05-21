# Testdata

This directory holds fixtures used by `pkg/` unit tests and the `test/e2e/`
integration suite.

## Layout

- `simple/` — a hand-crafted minimal cluster used by E2E tests. It exercises
  the Kustomization + HelmRelease pipeline end-to-end without any network
  access.
  - `cluster/` — Flux GitRepository, Kustomization, and HelmRelease objects.
  - `apps/` — kustomize-rendered ConfigMap + Namespace.
  - `charts/mychart/` — a tiny local Helm chart referenced via
    `sourceRef.kind: GitRepository`.

## Upstream attribution

fluxrr's controller architecture and behavior follow the design of
[`flux-local`](https://github.com/allenporter/flux-local). The fluxrr
`testdata/simple` corpus is intentionally minimal and was authored from
scratch for the Go port; the larger `flux-local/tests/testdata/*` clusters
remain available in upstream for reference.
