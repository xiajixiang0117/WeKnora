# Agent Note: Integrate upstream while preserving fork behavior

Status: proposed

## Problem

The fork contains website synchronization, embedded chat, citation restoration,
retrieval traces, and shared-server deployment changes. Upstream has advanced
since 85b76d1, including overlapping frontend changes and database migrations
whose numeric versions collide with the fork's deployed history.

## Proposal

Merge upstream on an integration branch, preserve the fork's feature contracts
and deployment workflows, and resolve overlapping changes at their owning
interfaces. Keep already deployed fork migration versions stable and append
new upstream migrations in dependency order, deduplicating equivalent MCP
migrations. Verify both fresh databases and upgrades from the previous fork.
Do not deploy or modify live database state as part of this integration.

## Existing note audit

The active notes were searched for upstream, merge, and migration decisions.
The notes-validation process note concerns maintenance tooling, not application
schema integration, and remains independent. There is no existing owner for
this integration decision.

## Alternatives considered

- Keep upstream migration numbers and renumber fork migrations. This minimizes
  divergence from upstream, but existing fork databases would interpret their
  recorded versions as different migrations and skip required schema changes.
- Cherry-pick selected fixes. This limits immediate scope, but leaves requested
  upstream features and their dependency chain unsynchronized and complicates
  future merges. A normal merge preserves ancestry and makes later updates
  easier to identify.

## Acceptance criteria

- The integration contains the selected upstream head and the previous fork head.
- Migration versions are unique and upgrades preserve fork data and schema.
- Frontend compilation and targeted tests cover embed, citations, and tracing.
- Backend checks cover conflicting packages and SQLite migration upgrades.
- Repository note validation passes.

## Risks

The resulting migration sequence belongs to this fork; importing a database
created by stock upstream requires a separate migration-history conversion.
Future upstream updates must repeat the version mapping review. PostgreSQL
extension upgrades require deployment-time validation and a restorable backup.
