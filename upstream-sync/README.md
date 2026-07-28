# Upstream Sync Incremental Audit

This directory is the versioned control plane for synchronizing the long-lived
product fork with `upstream/main`. Large generated artifacts and indexes are
stored under `.git/upstream-sync/` and are never committed.

## Repository baseline

The first baseline captures the verified `v0.1.166` merge:

- product integration branch: `feat/inspira-ui`
- product merge commit (`M0`): `906f1f278bcd751ebfb511ea64981c4b075c83d3`
- verified upstream (`U0`): `59ce11c78000bde5bdd74930b5885753037a5841`
- product head (`H`): `906f1f278bcd751ebfb511ea64981c4b075c83d3`
- upstream remote/branch: `upstream/main`

`origin/main` is not currently the product integration tip. Before changing the
branch model, explicitly promote a tested product commit and update
`baseline.yaml`; the audit tool does not infer or mutate branch ownership.

## Incremental boundaries

Every audit uses two independent ranges:

```text
upstream delta = U0..U1
local delta    = M0..H
```

Historical customization intent is retained in `features/*.yaml`. The tool does
not reconstruct all historical local commits on every run. If `U0` is not an
ancestor of `U1`, the tool stops because an upstream history rewrite requires a
separate full audit.

## Assets

- `baseline.yaml`: last verified upstream and product merge boundary.
- `features/*.yaml`: high-risk local features, semantic invariants, anchors,
  contracts, provenance, merge policy, and focused tests.
- `rules/risk.yaml`: risk definitions and generated/dependency classifications.
- `test-policy.yaml`: serial, two-worker test selection policy.
- `decisions.ndjson`: reusable semantic merge decisions.
- `merge-ledger/schema.yaml`: decision fingerprint and reuse contract.

The initial catalog intentionally covers only production-critical paths. Add a
feature when a local capability needs stable ownership, not merely because a
file differs from upstream.

## Commands

Use the repository wrapper:

```bash
tools/upstream-sync catalog lint
tools/upstream-sync baseline show
tools/upstream-sync index update --target upstream/main
tools/upstream-sync impact analyze --target upstream/main
tools/upstream-sync merge simulate --target upstream/main
tools/upstream-sync context build --target upstream/main
tools/upstream-sync verify
tools/upstream-sync verify --run
tools/upstream-sync record decision \
  --feature BILLING-001 \
  --resolution "combine upstream usage fields with local tier policy" \
  --rationale "preserves exactly-once deduction and fast pricing"
tools/upstream-sync record baseline \
  --upstream <verified-upstream-sha> \
  --merge <verified-merge-sha> \
  --head <product-head> \
  --version v0.1.167
```

The complete preflight is:

```bash
tools/upstream-sync preflight --target upstream/main
```

It performs catalog validation, incremental indexing, impact analysis, a
read-only virtual merge, and bounded context generation. It does not modify the
working tree, stage files, resolve conflicts, fetch remotes, or execute tests.

For a historical calibration run, override all four boundaries:

```bash
tools/upstream-sync preflight \
  --from 2730c1c43b29be003925b033f3f9e645e726bb8c \
  --target 59ce11c78000bde5bdd74930b5885753037a5841 \
  --local-from 7b0ea8ccdb132c341f51244f7c3276395ccab486 \
  --head b5278fb2620366b40fac5608259b634a801462bc
```

## Risk routing

- `none`: no feature path, symbol, dependency, or contract intersection; no
  Codex semantic review.
- `low`: same feature file, but disjoint declared symbols and contracts; run
  selected compile/type/test checks and keep only a short summary.
- `review`: a declared symbol, one-hop call/type reference, or non-breaking
  contract changed; generate one feature context pack.
- `blocker`: a conflict, same-symbol local/upstream edit, deleted anchor, or
  route/config/JSON/database contract change; Codex review is mandatory.

The first version uses Go AST declaration and call extraction plus bounded
Vue/TypeScript contract extraction. It is deliberately fail-closed for
high-risk contracts. Generated Ent and Wire output is classified separately;
source schema and provider-set changes remain reviewable.

## Generated output

Each run writes under:

```text
.git/upstream-sync/
  blob-index/<blob-sha>.json
  runs/<timestamp>/
    impact.json
    summary.txt
    context/
      overview.md
      <feature-id>.md
```

Only `overview.md` should be given to Codex first. Feature packs contain
business invariants, matching reasons, bounded diffs, relevant declarations,
historical decisions, and selected tests. Full files, lock files, generated
trees, old commit lists, and full test logs are excluded by default.

## Successful merge update

After a merge has been reviewed and verified:

1. Record every non-trivial `review` or `blocker` resolution.
2. Commit the merge with exact upstream SHA/version trailers.
3. Update `baseline.yaml` so `verified_upstream` and `target_upstream` are the
   merged upstream SHA, `merge_commit` is the verified merge commit, and
   `product_head` is the product tip.
4. Run `catalog lint` and a zero-delta `preflight`.

Git `rerere` restores text resolutions only. It never approves semantic reuse:
high-risk files still require the feature invariants and selected tests.
