---
name: module-dev
description: Develop a new IAgent module end-to-end following the project SOP — read goal + architecture docs, read the relevant spec, plan, implement step-by-step, write a dev record, commit + push, then run the auditor subagent.
---

## What You Do

Build a new IAgent module from spec to push, following a fixed Standard Operating Procedure. Work step by step and stop only when the module is fully implemented, committed, and audited.

## When to Use Me

Use when asked to build, create, or implement a new module, feature, or component in the IAgent project. Trigger keywords: build module, create module, implement module, develop, new feature, new component.

## SOP

Follow these steps in order. Read files in parallel where possible.

### Step 1: Understand the Project Purpose

Read `docs/braionstorm/goal.md`.

### Step 2: Understand the Design Picture

Read `docs/spec/00-overview.md` and `docs/spec/01-architecture.md` in parallel.

### Step 3: Read the Relevant Spec

Search `docs/spec/` for the spec file(s) relevant to the module being built. Read them. Understand the dev purpose and goals.

### Step 4: Make a Plan

Before writing any code, produce a brief plan:
- What packages / files need to be created
- What dependencies are needed
- What the implementation order is
- How each piece will be verified

Present the plan and wait for user confirmation before proceeding.

### Step 5: Implement Step by Step

Implement one piece at a time. After each piece:
- Verify it compiles / imports correctly
- Run existing tests
- Write tests for the new code if applicable

Follow existing code conventions from sibling modules. Do not modify unrelated files. Stop only when all planned pieces are done and verified.

### Step 6: Write the Dev Record

Write a dev record to `docs/dev/<module>.md`:

```
# <module> — Dev Progress

| Field | Value |
|-------|-------|
| **Spec** | `docs/spec/<spec-file>.md` |
| **Status** | Implemented |
| **Last Updated** | <today> |
| **Imports** | <verification command and result> |

## Packages Implemented

| Package | Path | Status |
|---------|------|--------|
| ... | ... | Done |

## Key Design Decisions

- ...

## Known Gaps / TODOs

- [ ] ...
```

### Step 7: Commit and Push

1. `git add -A`
2. `git diff --cached` — review that no secrets or unwanted files are included
3. `git commit -m "<concise module description>"`
4. `git push`

If commit or push fails, fix the issue and retry until success. Do NOT force-push.

### Step 8: Audit

Launch the `auditor` subagent to audit the new module. Provide the module name and spec file path. The auditor outputs a report to `docs/audit/<module>.md`.

If the auditor subagent fails, do NOT produce the audit yourself. Output a warning noting the audit step was skipped.

## Rules

1. Respect existing conventions — match code style, package structure, and naming from sibling modules.
2. Do not modify unrelated code — touch only files needed for the module.
3. Verify each step — never proceed to the next piece until the current one is verified.
4. Run lint + typecheck before committing (use commands from AGENTS.md).
5. Review the diff before every commit — never commit secrets.
6. Always use the auditor subagent for Step 8 — never audit your own code inline.
