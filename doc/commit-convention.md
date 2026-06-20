# Commit & changelog convention

partrack uses [Conventional Commits](https://www.conventionalcommits.org/) for
commit subjects and a **strict opt-in trailer** to drive the user-facing
changelog. This document is the source of truth for both.

## Commit subject (Conventional Commits)

```
<type>(<optional scope>): <subject>
```

Common types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `chore`, `ci`,
`build`, `style`. Scopes mirror the codebase areas (`bom`, `parts`, `web`,
`llm`, `admin`, …). The subject stays developer-oriented — it does **not** have
to read well for end users; that's what the `Changelog:` trailer is for.

## User-facing changelog: the `Changelog:` trailer

The user-facing changelog and the in-app "What's changed" dialog (Phase 2) are
**strictly opt-in**. The changelog is **not stored in the repo** — it is
generated on demand from git history with [git-cliff](https://git-cliff.org)
(`git cliff`, or `git cliff --unreleased` for what's pending) and published in
the GitHub Release notes for each tag. A commit appears there **only** if it
carries a `Changelog:` trailer in its footer:

```
feat(bom): broaden generic part matching (package-only, multi-param)

Implementation detail in the body as usual…

Changelog: BOM import now matches generic parts by footprint alone.
```

Rules:

- The trailer **value is what users see** — write one clear, user-facing
  sentence in present tense. The commit subject is ignored for the changelog.
- A trailer is a git footer: `Changelog:` must be at the **start of a line** in
  the last paragraph, separated from the body by a blank line.
- `Changelog: skip` explicitly excludes a commit (handy if a script or rebase
  would otherwise carry a trailer you don't want).
- Commits **without** the trailer never appear in the changelog — no exceptions,
  regardless of type. There is no automatic inclusion of `feat`/`fix`.

One trailer per commit. If a single commit deserves multiple user-facing notes,
prefer splitting it into multiple commits.

## Versioning (CalVer)

Releases use **CalVer**: `YEAR.MONTH.PATCH`, e.g. `2026.6.0`. The first release
of a month is `.0`; subsequent releases that month bump the patch (`2026.6.1`).
A new month resets the patch (`2026.7.0`).

Tags are the release marker and have no `v` prefix:

```
git tag 2026.6.0
git push origin 2026.6.0
```

Untagged/dev builds report `git describe` output (e.g. `2026.6.0-3-gb2170a1`)
or `dev` when no tag exists yet.

## How a release is cut

1. Push a CalVer tag (`2026.6.0`).
2. `.github/workflows/release.yml` runs git-cliff against
   [cliff.toml](../cliff.toml) and creates a GitHub Release whose notes are the
   curated `Changelog:` entries for that tag.
3. Build/deploy the image with the version stamped in (the Dockerfile takes
   `VERSION`/`COMMIT`/`DATE` build args; `docker compose build` reads them from
   `PARTRACK_VERSION`/`PARTRACK_COMMIT`/`PARTRACK_DATE`).

The running build's metadata is served at `GET /api/v1/version` and shown in the
sidebar footer.
