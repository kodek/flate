# fluxrr

A single-binary Go rewrite of [flux-local](https://github.com/allenporter/flux-local)
that renders and diffs Flux GitOps repositories **fully offline** — no
cluster, no `kubectl`, no shelling out. Helm, Kustomize, go-git, and
oras-go are linked in as native Go libraries.

```bash
go install github.com/buroa/fluxrr/cmd/fluxrr@latest
```

## Commands

| Command | Purpose |
| --- | --- |
| `fluxrr get ks\|hr\|cl` | list Kustomizations, HelmReleases, cluster summary |
| `fluxrr build ks\|hr\|all` | render manifests via the kustomize + helm SDKs |
| `fluxrr diff ks\|hr` | diff a working tree against a baseline (`--path-orig`) |
| `fluxrr test` | codegen + run table-driven Go tests against the repo |
| `fluxrr diag` | YAML / `.krmignore` sanity checks |

Every command takes `--path <dir>` (default `.`). Add `--path-orig <dir>`
to switch into **changed-only mode**.

## Changed-only mode (PR-fast)

Pass `--path-orig` to compare a working tree against a baseline. fluxrr
diffs files, picks the **most-specific Flux Kustomization that owns each
change** (longest matching `spec.path`, including `spec.components`), and
reconciles only that subtree.

What's in the keep-set:

- **direct edits** — every resource whose source file changed
- **chart sources / KS `sourceRef` / HR `valuesFrom`** — content
  dependencies, pulled in transitively
- **kustomize components** — touching a shared component (Flux v1
  `spec.components` or a kustomize-level `components:` entry) re-renders
  **every consumer Kustomization**

What's _not_:

- **`dependsOn`** — this is a reconcile-ordering signal in Flux, not a
  content dependency. Skipped resources still get marked `Ready` so
  downstream depwait completes naturally.
- **meta-Kustomizations** — a top-level KS rooted at `apps/` doesn't
  claim files inside `apps/media/plex/app/` when a deeper KS owns them.

```bash
git worktree add ../baseline main
fluxrr diff ks --path ./kubernetes --path-orig ../baseline/kubernetes
```

One-file PRs in a 70-Kustomization repo drop reconcile time from seconds
to tens of milliseconds.

## Narrow entries

`--path` can point at a Flux entry like `./kubernetes/flux/cluster` —
fluxrr iteratively follows each loaded KS's `spec.path` to discover the
full content tree (`apps/`, `components/`, …) without you having to
widen the flag.

## Diff format

`diff` output is unified diff with rich, reader-friendly headers and a
blank line after every separator:

```
--- HelmRelease: media/qui Deployment: media/qui

+++ HelmRelease: media/qui Deployment: media/qui

@@ -61,13 +61,13 @@

         envFrom:
         - secretRef:
             name: qui-secret
-        image: ghcr.io/autobrr/qui:v1.18.0@sha256:…
+        image: ghcr.io/autobrr/qui:v1.19.0@sha256:…
```

The header always shows the **parent** (HelmRelease or Kustomization) and
the rendered **child resource**. For KS parents the Flux `spec.path`
prepends the line.

## Namespace scoping

| Flag | Behavior |
| --- | --- |
| _(none)_ | every namespace |
| `-n foo` | only namespace `foo` |
| `--path-orig` | auto-scopes to the namespaces touched by the change set |

Explicit `-n` always wins.

## Fast-fail dependency waits

For offline use, `depwait` is tuned aggressively:

- **30-second per-dep ceiling** (vs. several minutes upstream).
- **2-second missing-grace** — if a dependency never lands in the store
  (typo'd `dependsOn`, broken `sourceRef`, missing chart source), fluxrr
  fails the dep with `dependency not found` rather than stalling out the
  full budget.

## Architecture

```
              ┌──────────────────────┐
              │   ResourceLoader     │
              │  walk + namespace    │
              │   inheritance        │
              └─────────┬────────────┘
                        ▼
              ┌──────────────────────┐
              │        Store         │◀── events ──┐
              │  objects + status +  │             │
              │  artifacts + pubsub  │             │
              └──┬──────┬────────┬───┘             │
                 │      │        │                 │
                 ▼      ▼        ▼                 │
       ┌────────────┐ ┌──────────────┐ ┌──────────────────┐
       │ SourceCtrl │ │ KSController │ │ HRController     │
       │ go-git +   │ │ krusty +     │ │ helm v3          │
       │ oras-go    │ │ Flux gen     │ │ (ClientOnly)     │
       └────────────┘ └──────────────┘ └──────────────────┘
```

Orchestrator pipeline: load → iterative spec.path discovery →
namespace inheritance → dependsOn validation → existence-only-ready →
change-filter resolution → controllers → diff/build/get.

## Defaults

- `--enable-helm`, `--enable-oci` → **true**.
- `--kube-version` defaults to the Kubernetes minor bundled with
  fluxrr's `k8s.io/api` dependency.
- Secrets are always replaced with `..PLACEHOLDER_<key>..` (matches
  flux-local).
- `--skip-crds`, `--skip-secrets` → **true** for `build` / `diff`.

## Deferred

- `diff --branch-orig <branch>` (auto-worktree)
- `shell` interactive REPL
- Bucket sources, ResourceSet (Flux Operator), in-cluster `secretRef`
  for OCI auth

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE) for upstream
attribution.
