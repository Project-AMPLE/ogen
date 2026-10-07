# What `ample` carries beyond upstream

Everything on `ample` that is not on upstream `ogen-go/ogen` `main`, so a rebase knows what to keep, re-apply or drop. Base: upstream `main` at `25694787` (v1.24.0 plus a goldmark bump; v1.24.0 is upstream's latest release). Remotes and branch rules: `AGENTS.md`.

Last checked 2026-10-07: every SHA with `git rev-parse --verify`, every upstream state with `gh` against `ogen-go/ogen`. Re-check both before acting on a row.

## Carried upstream PRs

Merged into `ample` from the PR head at the SHA shown. When upstream releases a PR, rebase `ample` onto that release and drop the merge.

| PR | Head taken | Merge on `ample` | Upstream state | Evidence we rely on | Drop when |
| --- | --- | --- | --- | --- | --- |
| [ogen#1758](https://github.com/ogen-go/ogen/pull/1758) — `$ref` siblings like `default` affecting all ref instances | `b7f1b3ae` | `15a751c7` | open, not merged, no review (last update 2026-09-15) | laddice-v2's metapiece merge-patch wrote `node_type=narrative` when the body omitted it (red-light test `services/cortex/internal/apiconv/merge_patch_defaults_test.go`, red before, green after); regen deleted only defaults no site declares; ogen suite green | upstream releases #1758 |
| [ogen#1715](https://github.com/ogen-go/ogen/pull/1715) — SSE server response generation | `1b00fa8c` | `04b16352` | open, not merged, no maintainer review (last update 2026-09-30); tracking issue [#1742](https://github.com/ogen-go/ogen/issues/1742) open | laddice-v2 serves `POST /api/loom/analyze/stream` and `/api/loom/pipeline/stream` through the generated typed sender (`services/cortex/internal/handler/loom_stream_test.go`, 4 tests); laddice-v2's 7 ogen-requiring Go modules `replace` the runtime with this fork | upstream releases #1715; then re-apply `17efb2d9` + `403230f6` on top and re-verify |

## Our own patches

| SHA | What | Why | Upstreamable |
| --- | --- | --- | --- |
| `124b1e69` + `25b8fb4e` | `unevaluatedProperties` parsed and treated as `additionalProperties` on a non-composing object schema; allowed by the per-type field validator, with parser tests | OpenAPI 3.1 / JSON Schema 2020-12 writes an open object with declared properties this way; without it every TypeSpec `Record<T>` generated as an empty struct, silently | Yes: general 3.1 support. No upstream issue or PR exists (searched 2026-10-07) |
| `d38a0c2c` | An untyped schema in a multipart part is recognized as a binary file | OpenAPI 3.1 removed `format: binary`; a 3.1 binary part has no type, and generated as `jx.Raw`, breaking upload handlers | Yes: general 3.1 support. No upstream issue or PR exists |
| `c3f2e54e` | `cmd/ogen` clears AI-agent environment variables before goimports runs | Toolchain shims (proto, mise) print an NDJSON banner to stdout of every wrapped `go` command when they detect an agent; goimports' `go env -json` parse then fails on a random template | Not as is: it works around a shim, not an ogen defect. A goimports-robustness fix upstream would be the general form |
| `17efb2d9` | Parse OpenAPI 3.2 `itemSchema` and `contentSchema` for SSE media types (event discriminator inferred from per-variant consts) | laddice-v2 emits OpenAPI 3.2; without it ogen reads only `schema`, so a typed event stream is never generated | Yes, after #1715 lands: it builds on #1715 and addresses [#1610](https://github.com/ogen-go/ogen/issues/1610) (open) for SSE |
| `403230f6` | Distribute an object envelope's `oneOf` branches into complete events; an untyped const `event` takes the envelope's type; a JSON `contentSchema` payload takes the referenced type; inline branches are named after their `event` const | Without it the generated SSE package does not compile (`e.Raw("chunk")`) and payloads are `jx.Raw`; with it laddice-v2's Loom events carry `LoomStreamChunk`/`Done`/`Error`/`Progress` | Yes, as a follow-up to #1715, with two points a reviewer will weigh: the const-based branch naming is our choice, and a JSON `contentSchema` whose type is a string is sent unquoted (ogen's existing rule for string data) |

## Fork documentation (never upstream)

| SHA | What |
| --- | --- |
| `256303ce` | `AGENTS.md`: remotes and branches |
| `a08f9608` | `AGENTS.md`: carried-PR table entry for #1758 |
| `deceb9f2` | `AGENTS.md`: #1715 and the SSE commits; the runtime `replace` |

## Offering upstream

Deferred until Ben reviews this table. Nothing has been posted to `ogen-go/ogen`.
