# Memories Progress

## Status
composition-fix MERGED + CLEANED. Awaiting next dispatch.

## Active Task
none — awaiting next dispatch

## Dispatch Log
- 2026-07-20 T62 composition-fix: Senior plan → Engineer (worktree + impl + build) → Test verify (focused, 5 checks) → Engineer (push + PR #21 + merge + cleanup). 3 dispatches. ~25 min. Commit d04edf3, squash 842efff. Build pass.
- 2026-07-20 T61 per-effort-raw-models: PR #20 merged (squash e84c73d). Worktrees + branches cleaned. 154/0 Go tests.
- 2026-07-20: PR #20 merged (squash e84c73d). Worktrees + branches cleaned. PR #19 closed without merge.

## In Flight

## Recently Merged
- PR #21: fix(composition): container root node cleaned to null by cleanNode | 1 file, +1/-1 | squash 842efff | no tests (1-line surgical) | no version bump
- PR #20: feat(raw-models): per-effort rows for model list | 8 files, +460/-101 | squash e84c73d | 154/0 tests | version bump: none
- PR #18: fix: model mapping target is model name (TEXT), not FK to models.id | 18 files, +354/-191 | squash df35c31 | 150/0 tests | no version bump

## Key Decisions
- 2026-07-20: Stash approach for worktree creation with dirty main — scales to any unrelated dirty files. Stash push --include-untracked → worktree add → stash pop.
- 2026-07-20: No-CI policy continues (user declined 3x). PR #21 merged same way.
- 2026-07-20: PR #19 closed without merge — PR #20 supersedes its design intent.
- 2026-07-20: Remove `ModelWithProvider` entirely; use `ModelEffortEntry` as sole response type. Simpler.

## Blockers
none

## Next Steps
1. Await next task dispatch from Scrum Master.

## History
- 2026-07-20: PR #21 merged + cleaned. 1-line surgical fix. 3 dispatches. ~25 min.
- 2026-07-20: PR #20 merged + cleaned. 154/0 Go tests. PR #19 closed without merge.
- 2026-07-20: Per-effort-raw-models task received. Full spec from user.
- 2026-07-20: Raw model metadata fix — PR #19 created (2 files, +40/-11, commit f027279c, 150/0 tests, web 3928 modules). Superseded by PR #20.
- 2026-07-19: Model mapping design FIXED + MERGED (PR #18). 6 dispatches.
