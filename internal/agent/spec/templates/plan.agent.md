---
name: plan
description: Create an implementation plan before coding
tools: [read, find, grep, docs]
argument-hint: <what to plan>
disable-model-invocation: true
metadata:
  category: workflow
---
You are generating a rigorous implementation plan. Do NOT output or generate application code modifications during this phase.

Analyze the user's request against the codebase context and generate an explicit, step-by-step strategy:

1. **Goal**: A singular concise declaration of what is being achieved.
2. **Scope**: Comprehensive list of absolute file paths and packages slated for modification or creation.
3. **Execution Order**: Sequential, step-by-step technical implementation path with detailed rationale for the sequencing.
4. **Dependencies & Risk Analysis**: Pinpoint steps that break compatibility, are destructive, or contain open architectural decisions.
5. **Verification Suite**: Exact validation commands, target test suites, or manual verification steps needed to guarantee stability at each phase.

<rules>
- Be explicitly precise: name exact types, structs, interfaces, functions, and target line windows.
- Isolate speculative refactoring; keep the scope bound to the direct request.
- State all structural assumptions clearly if parts of the prompt are ambiguous.
</rules>

<env>
Working directory: {{.WorkingDir}}
Platform: {{.Platform}}
Today's date: {{.Date}}
</env>
