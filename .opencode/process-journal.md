# Process Journal

## Workflow Observations
- 2026-07-18: No .opencode/ dir on first invocation — created with default schema.

## Product Ideas
- (none)

## Agent Feedback Patterns
- (none)

## Process Improvements
- 2026-07-18: Add pre-task check: if no worktree exists for bug fix, Engineer creates one before any code change.

## Workflow Observations
- 2026-07-18: Test Engineer flagged zero test coverage for web Svelte components. Pre-existing gap. Not blocking this fix but worth tracking for future.

## Process Improvements
- 2026-07-18: Consider adding minimal Playwright smoke test for ConditionBuilder button clicks — would have caught the Svelte 5 onchange/onChange collision. Add to feedback.md as proposal.

## Agent Feedback Patterns
- 2026-07-18: Engineer + Test Engineer both noted worktree `node_modules` corruption caused build failure unrelated to code change. Pattern: worktrees created with `git worktree add` don't get a fresh `npm install` automatically. Process gap.

## Workflow Observations
- 2026-07-18: Senior Engineer flagged missing import script for OpenRouter data — DB has OpenRouter rows but no committed importer. Worth documenting or hunting down external import path.

## Product Ideas
- 2026-07-18: Schema migration story for llm-benchmarks feels fragile (ALTER TABLE in dedup script). Consider versioned migrations (schema_version table + numbered SQL files) as DB grows.

## Process Improvements
- 2026-07-18: For cross-repo scripts (llm-benchmarks ↔ llm-router), establish convention early: where does shared code live? Cross-repo import vs symlink vs third shared repo? Decide before first script crosses boundary.


## Workflow Observations
- 2026-07-18: User-reported regression (missing drag handles) traced to no regression at all. Handles never existed. Pattern: user reports a "regression" based on assumed-implied feature (commit message + installed dep). Cost: full Senior Engineer dispatch for a clarity question. Improvement: user-facing status must reflect actual code state, not aspirational commit messages or installed-but-unused deps.

## Agent Feedback Patterns
- 2026-07-18: PR #1 commit message claimed "all existing functionality preserved including drag-drop" but drag-drop never existed in code. Commit messages should describe actual code behavior, not aspirational behavior. Logged to feedback.md as proposal.

## Process Improvements
- 2026-07-18: Before dispatching Senior Engineer on a "regression" report, ask: does the feature ever exist? 30-second grep for `draggable|dnd|handle` would have caught this. Add to user-faq: "if you suspect a regression, we'll first verify the feature existed."

## Workflow Observations
- 2026-07-18: User asked "why normalize.py in both projects?" — turned out confusion from design phrasing "Q3: cross-repo placement" suggested file needed in both. Reality: llm-router = pure consumer (only `strings.ToLower` on models.json "name" field, no slug generation). Cross-repo needed ONLY because export_benchmarks.py lives in llm-router but reads llm-benchmarks DB and generates slugs. Cleanest fix: move export_benchmarks.py to llm-benchmarks. Then zero cross-repo code, just JSON file transfer.

## Product Ideas
- 2026-07-18: canonical_slug (proposed) ≠ normalize_name (existing in export_benchmarks.py). They are different functions with different jobs. Document this clearly in dedup-design.md to prevent future confusion. canonical_slug = DB dedup identity. normalize_name = API slug generation for models.json output (48-entry hardcoded dict, not algorithmic).

## Process Improvements
- 2026-07-18: When design doc mentions "cross-repo" anything, include a "why cross-repo" justification line. Users will ask. Default assumption: shared code lives in ONE repo, the other imports it or moves the code.


## Workflow Observations
- 2026-07-18: Drag-drop feature delivered end-to-end: 11 dispatches (1 explore-equivalent, 3 Senior Engineer, 3 Engineer, 2 Test Engineer, 1 Senior review, 1 Engineer-merge). Pattern: code-level review (no Playwright) + logic unit tests (42 assertions on treeUtils) was sufficient for sign-off on internal tool. Faster than full E2E.

## Product Ideas
- 2026-07-18: Add CI workflow before next feature. Engineer flagged: no workflows configured in repo. CI catches broken builds before merge.
- 2026-07-18: Add Playwright E2E infra for Svelte components. Would have caught Bug #1 (stale index) at runtime instead of via test-engineer code review.

## Process Improvements
- 2026-07-18: Spec deviations must be flagged in dispatch log + progress.md immediately. Engineer reported "createSortable uses direct reactive props" as deviation. If that had been caught at code review BEFORE test, would have saved 2 dispatches.
- 2026-07-18: For Svelte 5 + library integration, senior-engineer spec should explicitly state library's expected prop pattern (getter vs direct). Caused 1 round-trip back-and-forth.
- 2026-07-18: Cursor-relative positioning should have been marked "v2 follow-up" in spec, not "deferred to T008 polish". Caused ship-vs-polish back-and-forth.

## Workflow Observations
- 2026-07-18: llm-benchmarks not a git repo on first task. Init required as Step 1 of every cross-repo task. Without init, all work ephemeral, no commit history, no agent-state integration.
- 2026-07-18: dedup --apply was 1 command, fully reversible via backup. No surprises. FK NO ACTION means script must do manual cascade — verify orphan count = 0 after.
- 2026-07-18: export_benchmarks.py path bug discovered during move (output would have gone to wrong repo). Caught by pre-flight grep, fixed via hardcoded absolute path. Lesson: scripts with `Path(__file__).parent.parent` are move-fragile.

## Process Improvements
- 2026-07-18: For all cross-repo file moves, before copying: grep for `Path(__file__)` and `parent` in the source file. Path resolution breaks on move.
- 2026-07-18: 5-wave dispatch pattern (init → move → parallel{write+mutate+doc} → integrate → verify) works well for refactor+mutation tasks. Each wave can be parallel within.
- 2026-07-18: Test Engineer verification should distinguish worktree state from main repo state. False FAIL on "file gone from main" because worktree not yet merged. Fix: verify worktree in Test Engineer step, then merge in next Engineer step.

## Product Ideas
- 2026-07-18: export_benchmarks.py filters to scored models (478 of 820). Consider adding `--all` flag for full DB export if downstream ever needs unscored models.
- 2026-07-18: llm-benchmarks PR workflow TBD. Currently direct-to-main. If 2+ contributors, add branch protection + PR template.

## Agent Feedback Patterns
- 2026-07-18: Engineer improved algorithm on its own (removed `break` from suffix loop for defense-in-depth, corrected test expectations to match algorithm, noted Qwen prefix nuance). Trust Engineers to refine specs within their context.

## Workflow Observations
- 2026-07-18: Dedup script (`dedup_models.py --apply`) cleans DB but doesn't regenerate `models.json`. The export pipeline (`export_benchmarks.py`) is a separate step. Empty rows in `models.json` therefore persist after DB dedup unless export is re-run. Lesson: dedup at DB level ≠ dedup at export level. Two separate concerns, two separate passes. Suggest: add `models.json` empty-row filter to `export_benchmarks.py` to prevent future drift.

## Product Ideas
- 2026-07-18: models.json has 11 truly empty rows + 1 corrupted-name row that survived dedup because export_benchmarks.py only filters by score, not by "is row non-empty". Add `--require-fields name,intelligence` (or similar) to export to drop empty rows at export time. Prevents future regression.

## Workflow Observations
- 2026-07-18: User said "12" but explicit list had 11 names. Senior Engineer caught the mismatch in pre-flight and recommended proceeding with explicit list as source of truth (zero ambiguity). Saved a dispatch round-trip to user. Lesson: when user provides a list AND a count, the list wins; count is informational.
- 2026-07-18: Engineer agent had to use `git add -f` because `data/` is in `.gitignore` for the binary outputs, but models.json itself is tracked. Convention: `data/models.json` tracked, `data/*.bak-*` should be local-only (need to add to .gitignore post-hoc).

## Process Improvements
- 2026-07-18: When task includes a count from user, Senior should always verify count against actual data and flag mismatch in plan. Adds 30s to planning, saves potential dispatch round-trip.
- 2026-07-18: Pre-commit hook or convention: `*.bak-YYYYMMDD` files should never be committed. Add to .gitignore or add a safety check in pre-commit hook.


## Workflow Observations
- 2026-07-18: User attributed edit-button regression to PR #2/#3 but root cause was commit 6ec19f4 (pre-dates drag-drop). Pattern: don't trust regression attribution — always git blame + check what each PR actually touched before investigating in the suggested area. Saved significant investigation time when Senior Engineer started with `git log --oneline -10` first.

## Agent Feedback Patterns
- 2026-07-18: Engineer followed the "DO NOT pre-open PR" feedback from PR #3 cycle. Did NOT pre-open PR for PR #4 — waited for explicit dispatch. Improvement confirmed.

## Process Improvements
- 2026-07-18: For regression reports after a PR, first command in investigation should be `rtk git log --oneline -20` + `rtk git show --stat <merge-commit>` to confirm the PR actually touched the affected file. Catches user misattribution early.

## Workflow Observations
- 2026-07-18 T13: Test Engineer verification checklist assumed two-step flow (export to llm-benchmarks, then copy to llm-router). Reality: script writes DIRECTLY to `~/git/llm-router/data/models.json` (output_path hardcoded post-T7 move). Pattern: when scripts have a hardcoded output path, verification checklist must reflect single-step. Lesson: when dispatching Test Engineer, include current `output_path` of the script in the prompt so checklist matches actual code state.

## Process Improvements
- 2026-07-18 T13: Senior Engineer analysis took one round-trip because filter logic was well-scoped (2 surgical insertions, no refactor). Pattern: when "user wants: fix X to prevent Y" is a clear single-file change with explicit acceptance criteria, planning phase is brief — go straight to Engineer. The 1 Senior dispatch + 1 Engineer + 1 Test Engineer is the right shape. Don't over-plan.

## Agent Feedback Patterns
- 2026-07-18 T13: Senior Engineer surfaced a workflow note: "DB has 84+ niche benchmark indexes producing zero-key models. Consider SQL JOIN filter in import scripts." Out of scope for T13 (which is the export-side fix), but valuable follow-up. Pattern: Senior should always flag "what I didn't fix but noticed" as a separate follow-up candidate, not bundle into the current task. Keeps scope tight, doesn't lose the observation.

## Workflow Observations
- 2026-07-18 T14: T11/T12 surgical deletes from `data/models.json` had ZERO effect on UI. Root cause: llm-router has 2-layer architecture (JSON = import source, SQLite = runtime truth) but it's not documented. The dedup-design.md doc + every T11/T13 conversation assumed "fix in JSON = fix in UI". Lesson: before any data fix, trace the data flow end-to-end. Suggests adding architecture section to dedup-design.md.
- 2026-07-18 T14: `data/models.csv` is a dead artifact — nothing reads it. Created by old import path, never cleaned up. Add to T12 follow-up cleanup list.
- 2026-07-18 T14: `createPredefinedModels()` in `cmd/llm-router/setup.go:228-236` hardcodes the Anthropic claude-code provider model list. This is the SECOND data source for UI alongside provider API discovery. Setup runs once, then it's a write-only artifact until next init. Means a freshly-built binary that runs `setup` re-introduces the bad rows even if models.json is clean.

## Product Ideas
- 2026-07-18 T14: SQLite is the source of truth. Need a "clean export" command (or migration script) that syncs models.json → SQLite (delete from models where name not in models.json). Today's only path: rm DB + full re-init. Add to dedup-design.md as Layer 5.
- 2026-07-18 T14: ADR needed: "models.json = import, SQLite = runtime. UI never reads models.json." Without ADR, every new agent will make the same assumption that broke T11/T12.

## Process Improvements
- 2026-07-18 T14: For ANY data fix, first dispatch Senior to map data flow end-to-end. Cheap ($0.50), prevents the T11/T12 mistake. Add to Senior dispatch template as Phase 0.

## Agent Feedback Patterns
- 2026-07-18 T14: Senior Engineer noted investigation was harder than needed because no schema overview in progress.md and no ADR on the 2-layer architecture. Suggests: maintain `docs/dev/architecture/data-flow.md` (or similar) as living doc. Next dispatch: have Engineer create it.

## Workflow Observations
- 2026-07-19: Brainstorm request came with `agent-state.py update ... --step exploring` template, but agent-state enum is code-worktree-specific (worktree_created, impl_done, tests_done, pr_created, merged, cleaned). No design-step exists. Mismatch between user template and state DB schema. For design-only tasks, log in progress.md + process-journal.md, skip agent-state. Worth flagging if design tasks become common.

## Workflow Observations
- 2026-07-19: Discovered pre-existing `web/lib/components/ui/` is empty — shadcn-svelte components never generated. `next.shadcn-svelte.com` registry unreachable. Affects `npm run build` on all of `web/`. This is independent of today's web UI bug fix. PR #6 (3-line Svelte fix) was code-correct, but full build verify impossible without fixing this infra first. **Pattern**: pre-existing build breakages can hide behind new PRs — Test Engineer must compare main branch build to PR build to disambiguate "fix broke build" vs "build already broken".

## Process Improvements
- 2026-07-19: Test Engineer verify for any web/ PR should include `npm run build` on both the PR branch AND `main` (or origin/main). If both fail the same way, it's pre-existing, not the PR's fault. Saves false-FAIL cycles.

## Product Ideas
- 2026-07-19: Add CI workflow (GitHub Actions or Forgejo Actions) that runs `npm run build` + `go test ./...` on every PR. Would catch pre-existing build issues like the missing shadcn-svelte components. Pre-commit hook enforces safe-file list but doesn't run build/test.

## Agent Feedback Patterns
- 2026-07-19: Senior Engineer T0 plan said "empty slice from ResolveModels triggers all-models-failed path". Go Engineer caught this was wrong — empty slice triggers 404 early return (L378); the 502 all-models-failed path needs actual failing providers. Engineer used `httptest` with failing HTTP servers instead. **Pattern**: when spec says "use empty X to trigger Y", Engineer should grep production code to confirm path before writing the test. Spec was wrong, Engineer caught it, no wasted dispatch.

## Workflow Observations
- 2026-07-19 T22: Cloudflare provider plan flagged URL double-path trap (client appends `/v1/chat/completions` so base URL must end in `/ai` not `/ai/v1`). Existing providers store `/v1` and presumably rely on server-side normalization. New provider setup must document this. Worth adding to setup.go as comment.

## Workflow Observations
- 2026-07-19 T23: My dispatch for T7 said "verify only" but user had explicitly required an explicit `if cloudflare` branch. Caused 1 round-trip (Test Engineer FAIL → Engineer fix → Test Engineer PASS). Lesson: when user makes a decision in original brief, read it back into the dispatch prompt verbatim, don't summarize.

## Process Improvements
- 2026-07-19: Before dispatching Engineer for any task, cross-check dispatch prompt against original user brief. User decisions get lost when Senior summarizes them.

## Agent Feedback Patterns
- 2026-07-19: Engineer created PR #9 against dispatch instructions ("DO NOT create PR"). Created via Forgejo API. Likely a behavior where Engineer default to creating PR after push. Add to dispatch template: "After push, STOP. Do not call any PR-create tool. Report back with branch name + remote URL only."

## Process Observations
- 2026-07-19: Senior Engineer lost context from earlier turns (T22). In revised plan, flagged as "open questions" items the user had already answered in original brief (9 model names, base URL pattern). Could have read the original user message instead of generating placeholders. Mitigated by Scrum Master override before dispatch to Engineer.
- 2026-07-19: Test Engineer first pass caught missing branch (saved a broken merge). 2nd pass after fix was surgical (1 commit, +25 implementation, +117 tests). Test Engineer verify pattern works: focused 2nd pass after fix is faster + more reliable than full re-verify.


## Workflow Observations
- 2026-07-19 T33-37: Critical bug (canvas freeze) fixed in 4 dispatches: Senior (root cause) → Engineer (fix) → Test (verify) → Engineer (merge). Pattern: when Senior correctly identifies root cause with line numbers and exact fix, downstream dispatches are surgical. Engineer finished in 1 pass, Test Engineer in 1 pass. Total ~15 min wall time. Lesson: invest in Senior phase for clear root cause, downstream becomes fast.
- 2026-07-19 T33-37: User suspicion (infinite reactive loop in $effect via $state sync) was correct at the high level, but Senior Engineer identified the SUBTLE mechanism: `assignCompositionIds` reads `tree.__id` then writes it inside the effect, BUT the shallow copy `{...node}` never has `__id`, so write always happens. Without the shallow-copy nuance, fix would be "guard the write" (wrong — would still trigger once). Senior unblock by reading the actual code via Explore. Pattern: trust user direction at macro level, verify specifics via code read before recommending fix.

## Agent Feedback Patterns
- 2026-07-19: Senior Engineer proactively loaded systematic-debugging skill and applied 4 phases. Output was complete (root cause + repro + fix + verification + dispatch plan) in single response. Highest-leverage dispatch shape.

## Process Improvements
- 2026-07-19: For "infinite reactive loop" reports, Senior's first read should be (a) every `$effect` in the affected component, (b) every function CALLED from inside the effect, (c) the shallow-copy semantics. Catches the "loop never stabilizes" class. Faster than console.log dance.

## Workflow Observations
- 2026-07-19: Ollama task followed same pattern as Cloudflare (T1 ADR, T2 const, T3 migration, T4 service, T5-T7 CLI/API, T8 engine, T9 tests, T10 build). Pattern now well-established: ~1-2 hours per provider type, end-to-end. Senior Engineer planning + Engineer impl + Test Engineer verify + Engineer merge worked smoothly 2nd time.
- 2026-07-19: Engineer deviation on auth header test (`strings.HasPrefix("Bearer")` instead of exact match) — Ollama ignores auth header. Test Engineer verified acceptable. Pattern: HTTP header canonicalization may trim trailing spaces. Use prefix match for "Bearer <key>" assertions.

## Process Improvements
- 2026-07-19: 2nd provider in a row shipped without prompt-engineering issues. Senior Engineer plan covered all edge cases. Pattern: Senior plan with line-anchored dispatch + DO-NOT list + Engineer verifies against plan = reliable.

## Agent Feedback Patterns
- 2026-07-19: Engineer suggested refactoring engine sendRequest/sendStreamRequest branches to extract shared "set model + reasoning effort + call openaiClient" helper. Branches are copy-paste of each other. When 3rd provider lands (anthropic, openai, cloudflare, ollama all share body), refactor becomes worth it. Track as future task.


## Workflow Observations
- 2026-07-19: 3rd provider-type task shipped (ollama local+hosted fix). Pattern: small fix, ~10 min Senior plan, ~5 min Engineer impl, ~3 min Test Engineer verify, ~1 min merge. Total ~25 min for surgical change. Workflow is now well-tuned.
- 2026-07-19: Test count variance between Engineer reports (134) and Test Engineer reports (89) was subtest counting mode. Worth documenting in workflow: `go test ./... -count=1` non-verbose counts package-level only; `-v` counts subtests. Engineers should report BOTH counts to avoid confusion.

## Process Improvements
- 2026-07-19: For small surgical fixes (≤3 files, no test changes), Test Engineer can do focused verify in 1 pass (5-10 min). Full matrix is overkill. Workflow: focused verify checklist for fixes < 30 min work; full verify for new features.

## Agent Feedback Patterns
- 2026-07-19: 3rd PR in a row, Engineer correctly avoided creating PR. Discipline holding. Worktree + branch cleanup pattern is now standard.



## Workflow Observations
- 2026-07-19 T45: Canvas UI fix shipped in 4 dispatches (Senior plan → Engineer impl → Test verify → Engineer merge). Total ~30 min. Pattern: when Senior's analysis surfaces a meaningful deviation (recursive CompositionBuilder → inline due to DnD conflict) and the original brief used "ideally" wording, Scrum Master accepts Senior's recommendation and proceeds. Saved a back-and-forth round-trip with user. Lesson: "ideally" in user brief = preferred but not required; trust Senior's code-grounded override.

## Process Improvements
- 2026-07-19: When user brief contains a specific implementation approach (e.g., "use CompositionBuilder recursively"), Senior Engineer should always run DnD/state/event-flow analysis BEFORE writing plan. Caught the Pragmatic vs native DnD conflict before code was written. Worth ~20 min of Senior time, saved potential 2-3 dispatch round-trips.

## Agent Feedback Patterns
- 2026-07-19: 4th PR in a row, Engineer correctly avoided creating PR until dispatched. Discipline holding.

## Process Observations
- 2026-07-19: Test Engineer caught dead `CompositionBuilder` import (still on line 5 after fix) — user brief said "dead import" but inline approach didn't remove it. Test Engineer flagged as cosmetic. Pattern: when brief lists a specific cleanup item ("X is dead import"), Engineer should remove it even if not strictly needed for the fix. Add to dispatch template: re-read original brief's "dead code" list and confirm removal.

## 2026-07-19: PR #17 → PR #18 — design flaw caught same-day

PR #17 (model mapping system) merged at f3a100a. Same day, user identified design flaw: target should be model NAME (TEXT), not model ID (FK). PR #18 (squash df35c31) fixed it.

### Process observations
- **Spec was detailed but missed 2 critical files**: `model_service.go` (3rd mapping location) and TUI DI chain (4 files for picker). Senior Engineer caught both during plan validation. Lesson: spec author knows the design, but explorer's job is to verify against actual code.
- **Vollautonom + tactical decisions**: 2 questions (reuse `ListModels` vs new method, add globalMetaRepo to TUI DI) were decided by Scrum Master without user interrupt. Both were sensible defaults. Worked well.
- **Test gap pattern (PR #17 → PR #18)**: both PRs had minor coverage gaps. Scrum Master autonomously dispatched test-gap-fix engineer before merge. Established pattern. Total 4 min overhead.
- **Forgejo no-CI**: PR #18 also merged without CI. User has declined CI 3x now (PR #12, #17, #18). Either accept as policy or escalate.
- **Drop + recreate migration**: cleaner than additive. Migration 020 (FK) → deleted. Migration 021 (TEXT) → DROP IF EXISTS + CREATE. No historical baggage.
- **Spec doc drift**: `docs/specs/2026-07-19-model-mapping-system.md` still references old TargetModelID/GetByID. Not updated. Filed as follow-up.

### Recurring pattern
- "Spec with line numbers" → often wrong by ±50 lines. Code reality ≠ design doc. **Always verify with codegraph or file read before dispatching Engineer.**
- "Missed DI chain" → came up in PR #12 (CF prefix), PR #17 (mapping DI), PR #18 (TUI globalMetaRepo). TUI/web DI cascades are easy to miss in specs.

## Workflow Observations
- 2026-07-20 per-effort-raw-models: 5 dispatches total (Senior plan → Engineer impl → Test verify → Senior sign-off → Engineer merge+cleanup). Total ~40 min. Pattern: when spec is well-detailed AND user explicitly states acceptance criteria, the plan is fast and dispatches are surgical. Engineer self-created the PR before Test Engineer verified (spec said "after tests pass") — minor process violation, didn't cause issues because push was correct, PR was open + mergeable.
- 2026-07-20: Worktree `web/node_modules` corruption reappeared (same issue as PR #19 per progress.md T57). Engineer did `ln -s` per plan but build still failed in worktree. Engineer resolved via re-symlink during merge step. Pattern: npm notices the symlink and replaces it with a real dir on first `npm run build` (or symlink is to wrong target). Re-symlink after every `npm run build` in worktree. Or: add to worktree-setup template — symlink at end of work, not at start.
- 2026-07-20: Engineer wrote changelog description with phantom column names ("cost, token limit, context length") that didn't match the Svelte code (data-driven, not hardcoded columns). Scrum Master caught post-merge and fixed. Pattern: changelog descriptions should be reviewed for accuracy against code, not just narrative intent.

## Process Improvements
- 2026-07-20: Add to worktree-setup template: re-symlink `web/node_modules` at END of work, not at start, to survive any `npm run build` invocations.
- 2026-07-20: Add to Engineer dispatch template: "Do NOT create PR until Test Engineer passes verification." Spec already said this; engineer self-created. Make it explicit in prompt.
- 2026-07-20: Add to Scrum Master checklist: verify changelog bullet accuracy against code before reporting done.


## Workflow Observations
- 2026-07-20 composition-fix: 1-line surgical fix shipped in 3 dispatches (Senior plan → Engineer impl+build → Test verify → Engineer push+PR+merge+cleanup). Total ~25 min. Pattern confirmed: when user pre-validates the fix AND scope is 1 line AND no Go changes, the workflow is `stash main dirty state → worktree → minimal commit → focused verify → merge`. No separate test files, no separate impl dispatch, no separate merge dispatch.
- 2026-07-20 composition-fix: Stash approach for worktree creation with dirty main worked cleanly. Main had 8+ dirty files (unrelated). `git stash push --include-untracked` → `git worktree add` → `git stash pop` restored main to its prior state. Worktree got clean main + the 1-line fix applied manually. Pattern: stash approach scales to ANY number of unrelated dirty files on main. Cleaner than per-file copy.
- 2026-07-20 composition-fix: agent-state `start` subcommand doesn't exist — real commands are `init/create/update/log/get/list/orphans/recover/clean`. Senior Engineer dispatch plan referenced `agent-state.py start` which fails. Fix: dispatch template should use `create` (with positional id arg) + `update --step <step>`. State DB has enum but not full workflow spec in Senior's context. Add to Senior dispatch template.
- 2026-07-20 composition-fix: Build re-verify (focused verify per process-journal pattern for fixes < 30 min) was 5 checklist items: line 218 guard present, diff scope +1/-1 single file, no stray edits, build exit 0, commit message exact. ~5 min verify time. Pattern: focused verify = reliable + fast for surgical fixes.
- 2026-07-20 composition-fix: Pre-existing web build issues (a11y warning on ModelPicker.svelte per T22) appeared in this build too. Test Engineer correctly flagged as "not from this fix". Build exit 0 → pass. Pattern: warnings ≠ failures. Exit code is the source of truth.

## Process Improvements
- 2026-07-20: Add to Senior Engineer dispatch template: state DB API uses `agent-state.py create <id> --branch X --worktree Y` + `update --step <step>`, NOT `start`. State DB has fixed enum: worktree_created, impl_done, tests_done, pr_created, merged, cleaned (no `start`/`end`).
- 2026-07-20: For 1-line surgical fixes with user pre-validated fix, 3 dispatches is the right shape (Engineer impl+build, Test verify, Engineer push+PR+merge+cleanup). No separate impl/build/merge split. Total ~25 min wall time.
- 2026-07-20: Worktree `web/node_modules` symlink approach is now stable. Symlink to main's `web/node_modules` survived `npm run build` without corruption. Pattern: create symlink at start of work, not at end (per 2026-07-20 process-journal entry, that was wrong). Engineer this time linked at start, build was clean.

## Agent Feedback Patterns
- 2026-07-20 composition-fix: Engineer followed dispatch instructions precisely — created worktree, applied fix, committed with exact message, built, reported back with hash + build status. No premature PR creation. Discipline holding (5th PR in a row, see process-journal 2026-07-19 "5th PR in a row" pattern).
