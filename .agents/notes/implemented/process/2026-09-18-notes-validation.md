# Agent Note: Validate engineering decision notes in CI

Status: implemented

## Problem

The repository requires non-trivial changes to preserve their decisions in agent notes. Without a shared validator or CI check, malformed records can pass review. A blanket hidden-directory ignore rule also prevents local records from appearing in normal Git status output.

History review at adoption found no existing notes directory or prior decision to update or supersede.

## Decision

The repository vendors the note tree, format, archive verifier, shared helper, and archive command from czm15053/write-notes-like-deepseek. Root npm scripts use a locked tsx dependency so local checks and GitHub Actions run the same tools. The ignore rules allow .agents so notes and other team documents can be versioned together, as requested for team collaboration.

Verify Agent Notes runs on pull requests and pushes to main, with a manual trigger for diagnostics. It uses Node.js 22, fetches full Git history, and compares archive seals against the PR base SHA or pre-push SHA. Manual runs use HEAD. The check is read-only and independent of application build and deployment jobs. The PR template directs contributors to docs/AGENT_NOTES.md and asks for updated notes on non-trivial changes.

## Alternatives considered

- Run the upstream workflow's unpinned npx tsx commands. This avoids a root dependency manifest, but a newly resolved tool version can change CI behavior without a repository change. A committed lockfile makes updates explicit.
- Rely on the installed skill and local checks. This requires no CI maintenance, but contributors without the skill can submit invalid notes and get no shared result. A repository-owned check gives reviewers a consistent signal.

## Testing

- npm ci and npm run verify-notes pass from the repository root.
- Isolated fixtures confirm that valid notes and archives pass, while invalid filenames and lifecycle directories, missing alternatives, broken internal note links, and modified archive content fail.
- An isolated Git repository confirms that rewriting both an archive and its seal in a later commit fails against the earlier commit baseline.
- Git status includes the note without force-adding it. Git ignore checks confirm that ordinary .agents team documents are no longer excluded.
- The workflow YAML parses successfully. A hosted Actions run remains pending until the changes are pushed to GitHub.

## Consequences

Contributors and CI share one validation command, and team documents can travel with code changes. The repository gains a small Node-based maintenance dependency. Vendored scripts require explicit updates when upstream validation rules change. Structural checks cannot decide whether a code change needs a note or assess the quality of its reasoning. Making this check mandatory for merging is a separate GitHub branch-protection setting.

The upstream archive checker expects consistent physical filesystem paths when AGENT_NOTE_ROOT is overridden. Tests on macOS resolve the temporary directory's symlink before passing that override; the current workspace and Linux CI use physical paths. Revisit path normalization if custom symlink-based note roots become a supported workflow.
