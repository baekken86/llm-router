# Team Feedback Log

## Raw Feedback
- 2026-07-18 | Test Engineer | "No existing tests for any of the 7 affected Svelte components. The web app has zero test coverage for this area. Consider adding at least a basic Playwright smoke test for ConditionBuilder button clicks — this exact bug class would have been caught by a single 'click + Condition → new row appears' assertion."
- 2026-07-18 | Test Engineer | "Worktree node_modules corruption caused build failure unrelated to code change. Worktrees created via `git worktree add` don't get a fresh `npm install` automatically."
- 2026-07-18 | Engineer | "Build failures in worktree are environment, not code. Main branch builds clean."

## Under Review
### Proposal 1: Add Playwright smoke test for ConditionBuilder
- **Date**: 2026-07-18
- **Affects**: web/e2e/ (new), web/package.json (dev deps)
- **Pattern**: Zero test coverage for Svelte components caused Svelte 5 onchange/onChange collision to ship
- **Proposed change**:
  - **File**: `web/e2e/conditionbuilder.spec.ts` (new)
  - **Section**: N/A — new file
  - **Current**: N/A
  - **Proposed**: Add minimal Playwright spec that loads /virtual-models/new, clicks "+ Condition", asserts new row appears with FieldSelector. Repeat for Group, SortBuilder + Sort.
- **Risk**: minor (new dev dep, CI config)
- **Status**: awaiting user decision

### Proposal 2: Document worktree node_modules bootstrap step
- **Date**: 2026-07-18
- **Affects**: AGENTS.md, .opencode/agent/engineer.md
- **Pattern**: Worktree created via `git worktree add` lacks node_modules; build fails on first run
- **Proposed change**:
  - **File**: `AGENTS.md`
  - **Section**: "Multi-agent workflow" or similar
  - **Current**: (no mention of node_modules bootstrap in worktree)
  - **Proposed**: After `git worktree add`, run `cd <worktree>/web && npm install` before any build/check/dev. Add to agent prompt templates.
- **Risk**: none
- **Status**: awaiting user decision

## Applied
- (none)

## Rejected
- (none)

- 2026-07-18 | senior-engineer | "PR #1 commit message claiming 'drag-drop' preserved created false expectation. Commit messages should only claim what code actually does. Dead deps (@dnd-kit) also signal 'feature exists' when it doesn't — clean up or document as planned."

- 2026-07-18 | engineer | "Spec getter pattern `id: () => id` for createSortable is wrong for Svelte 5 reactive props — $effect in library assigns input.id directly, would be function not value. Use direct reactive getter from $props() instead. Should be clarified in future specs."

- 2026-07-18 | test-engineer | "Reported @dnd-kit missing from package.json (Bug #3) — was wrong, deps exist at lines 21-22. Also flagged LogsView.svelte missing button as potentially drag-drop regression — pre-existing (commit 0ec2f9cc). Should verify package.json + git blame before reporting blockers/regressions."

- 2026-07-18 | senior-engineer (PR #2 review) | "DENIED Bug #3 (@dnd-kit missing from package.json) citing 'web/package.json:21-22' — was wrong. Actual package.json has only @lucide/svelte, bits-ui, clsx, tailwind-merge, tailwind-variants in dependencies. @dnd-kit installed in node_modules but never declared. Test engineer was right. Cost: ghost dep shipped, would break fresh clone. Should verify file contents with Read tool, not rely on memory or previous output."

- 2026-07-18 | engineer | "Pre-opened PR #3 (feature/drag-drop-composition) despite instruction 'DO NOT open PR (Scrum Master dispatches merge)'. Pattern from PR #2 cycle too. Engineer agent seems to consider PR opening part of 'done' status. Consider: explicitly enumerate forbidden actions in 'DO NOT' section, or change workflow to allow Engineer to open PR (Scrum Master just merges)."

- 2026-07-18 | senior-engineer (PR #4 review) | "Declared '1 file, 2 lines, zero risk' fix done based on code-read alone. Did not test in dev server, did not verify parent component actually passes onEdit callback, did not trace click→onEdit→route flow. Fix turned out to be incomplete — user reports STILL broken. Process gap: for click-handler fixes, must trace end-to-end (button → handler → callback → navigation → route → page) before declaring done. Logic check + code-read is not the same as runtime verification."

## Raw Feedback
- 2026-07-19 | user (via Chef) | "Cloudflare has a format for their models in the format @cf/Provider/model so these do not map to the models from models.json" → Triaged: metadata gap, not routing bug. See progress.md 2026-07-19 T38. Awaiting user decision on Option A (data fix) vs Option B (alias logic).

## Raw Feedback
- 2026-07-19 | senior-engineer | "11-model list should have been captured during triage. Would have avoided this blocker. Recommend Scrum Master always pin user-provided model/data lists to progress.md immediately." → Applied: list now pinned in progress.md 2026-07-19 T40.
- 2026-07-19 | senior-engineer | "12-model list never specified" (false negative — list WAS in dispatch prompt as code block + comma-separated enumeration). Possible: Senior agent missed list due to length, did not parse full prompt. Process note: when listing items, repeat list at top of prompt in dedicated code block, do not embed mid-prose.

## Raw Feedback
- 2026-07-19 | senior-engineer (post-fix) | "Test Engineer's 10 checks were thorough but missed `GetByModelEffort` path. Consider adding 'all callers of `GetByModelEffort` with CF IDs' to verification checklist next time." → Note: Test Engineer dispatch did not include second-function check. Future dispatches: when fixing one lookup function, list sibling functions and require verification that they all have the same logic.
- 2026-07-19 | senior-engineer (merge gate) | "No CI pipeline exists for this repo. Policy forbids merge when CI is missing. CI pipeline should exist before more PRs land. Recommend prioritize before next feature." → Escalated to user. Previous PRs (#10, #11) merged without CI — either policy was overridden, CI was removed, or policy is new. Awaiting user decision.
