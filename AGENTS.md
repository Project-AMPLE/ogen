# ogen (Project AMPLE fork) — Agent Instructions

A fork of upstream [`ogen-go/ogen`](https://github.com/ogen-go/ogen), the OpenAPI → Go generator, canonical on GitLab with a GitHub mirror. laddice-v2's root `build-ogen` task builds `.build/ogen` from this checkout, and every generated Go package in laddice-v2 comes from that binary.

| Remote | URL | Role |
| --- | --- | --- |
| `gitlab` | `gitlab.com/enfilad/ogen` | canonical (private); push here; what the constellation `repos.json` clones |
| `origin` | `github.com/Project-AMPLE/ogen` | public mirror: the only place an upstream PR can come from, and the module source for a `replace github.com/ogen-go/ogen => github.com/Project-AMPLE/ogen <commit>` (the Go proxy cannot reach a private GitLab repo) |
| `upstream` | `github.com/ogen-go/ogen` | read-only; fetch an open PR's head with `git fetch upstream pull/<N>/head:pr-<N>` |

Push to `gitlab`, naming the remote. Push a commit to `origin` too whenever laddice-v2's Go modules will `replace` onto it; otherwise update `origin` deliberately.

## Branches

- `ample` — ours: upstream release + our patches. **Everything that regenerates laddice-v2 builds this branch**, so nothing lands here unverified.
- `ample-next` — staging for carried upstream PRs. Merge a PR head at a recorded SHA, verify in laddice-v2 (rebuild, regen, `check-generated-clean.sh`, `go build` with and without `GOWORK=off`), then fast-forward `ample`.
- `main` — mirror of `upstream/main`, a rebase target only.
- `feat/sse-itemschema` — the OpenAPI 3.2 `itemSchema` parser spike; blocked on server-side SSE encoding (ogen#1715). Never merge into `ample` on its own: it would drop both Loom SSE operations from laddice-v2's server silently.

## Our patches on `ample`

`124b1e69` + `25b8fb4e` (`unevaluatedProperties`), `d38a0c2c` (OpenAPI 3.1 binary multipart part), `c3f2e54e` (clear AI-agent env vars so goimports can parse `go env`). Put the why in each commit body: the next reader is reconciling against an upstream release.

## Upstream PRs carried on `ample`

| PR | Head taken | Merge | Upstream state (2026-10-06) | Why | Drop when |
| --- | --- | --- | --- | --- | --- |
| [ogen#1758](https://github.com/ogen-go/ogen/pull/1758) | `b7f1b3ae` | `15a751c7` | open, mergeable, unreviewed | a `$ref`'s sibling `default` leaked onto every reference; laddice-v2's metapiece merge-patch wrote `node_type=narrative` when omitted | upstream releases it |

Upstream state, carried PRs and what each would change for laddice-v2: `../../laddice-v2/docs/external-libraries/ogen/upstream-status-2026-10.md`. The work plan: `../../laddice-v2/docs/plans/ogen-fork-vendor-and-upstream-picks.md`.
