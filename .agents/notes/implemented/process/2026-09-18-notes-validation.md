# Agent Note: Validate engineering decision notes in CI

Status: implemented

## Problem

The repository requires non-trivial changes to preserve their decisions in agent notes. Without a shared validator or CI check, malformed records can pass review. A blanket hidden-directory ignore rule also prevents local records from appearing in normal Git status output.

History review at adoption found no existing notes directory or prior decision to update or supersede.

## Decision

The repository vendors the note tree, format, archive verifier, shared helper, and archive command from czm15053/write-notes-like-deepseek. Root npm scripts use a locked tsx dependency so local checks and GitHub Actions run the same tools. The ignore rules allow .agents so notes and other team documents can be versioned together, as requested for team collaboration.

触发策略由[禁止推送自动触发工作流](2026-09-18-manual-actions.md)部分取代：Verify Agent Notes 现在仅手动运行，使用 Node.js 22、完整 Git 历史和 HEAD 归档校验基线。原先在 PR 和 main 推送时自动校验，目的是为贡献者提供共享校验结果；当前按维护者要求改为主动执行。工具链设计保持有效，校验仍独立于应用构建与部署，PR 模板仍指向 docs/AGENT_NOTES.md。

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
