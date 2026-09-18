# Engineering Decision Notes

Record the reasons, alternatives, and consequences of non-trivial changes in
`.agents/notes/`. Search existing notes first and update the owning note when the
decision still holds. Mechanical formatting and styling changes do not need notes.
The `.agents/` directory is versioned so these notes and other team documents
can be shared with the code.

Use `{lifecycle}/{class}/yyyy-mm-dd-topic.md`, with lifecycle `proposed`,
`implemented`, or `rejected`, and class `feature`, `bug-fix`, `simplification`,
`architecture`, `process`, or `testing`. Create directories only when needed.
Write proposals before implementation; move completed proposals to `implemented`
with the code change and describe the current decision rather than a future plan.

An implemented note has this structure:

```markdown
# Agent Note: Decision title

Status: implemented

## Problem
The concrete problem and constraints.

## Decision
The current choice and its reasons.

## Alternatives considered
Real alternatives, their strongest arguments, and why they were not chosen.

## Consequences
Benefits, costs, limitations, and verification results.
```

Proposed notes use `Proposal`, `Alternatives considered`, `Acceptance criteria`,
and `Risks` after `Problem`. When implemented, replace the proposal with `Decision`
and fold acceptance results and risks into `Consequences` or `Testing`.

From the repository root, using Node.js 22 and npm:

```sh
npm ci
npm run verify-notes
```

The `Verify Agent Notes` GitHub Actions workflow runs on pull requests and pushes
to `main`, and supports manual runs. It checks active note structure, required
sections, internal note links, and frozen archive hashes. CI compares archives
against the PR base or the commit before the push; local and manual checks default
to `HEAD`. These checks do not generate notes or detect missing decision records.
Require the check in GitHub branch protection if merging must depend on it.

Archive an implemented note only when it is no longer active and has low future
reference value. Use the helper to move it and create the immutable hash entry:

```sh
npm run archive-agent-note -- .agents/notes/implemented/process/yyyy-mm-dd-topic.md
npm run verify-notes
```

Update any inbound links reported by the helper. Archived notes and existing
manifest hashes must not be edited or removed. Rejected proposals are not archives.

The five `scripts/*agent-note*.ts` files are vendored from
[write-notes-like-deepseek](https://github.com/czm15053/write-notes-like-deepseek).
Update them deliberately and rerun validation when adopting upstream changes.
The root package owns only repository tooling; application packages retain their
own dependency manifests.
