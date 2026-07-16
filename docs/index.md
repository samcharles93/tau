---
layout: home

hero:
  name: Tau
  text: Provider-agnostic coding agent
  tagline:
    A terminal UI, Web UI, agentic tool use, skills, and a plugin system - talking to any OpenAI-compatible model.
  image:
    src: /favicon.svg
    alt: Tau
  actions:
    - theme: brand
      text: CLI Reference
      link: /cli-reference
    - theme: alt
      text: Architecture
      link: /architecture
    - theme: alt
      text: GitHub
      link: https://github.com/samcharles93/tau

features:
  - title: Provider-agnostic
    details:
      Works with any OpenAI-compatible API - DeepSeek, OpenRouter, Ollama, self-hosted - via config or env vars, no code
      changes.
  - title: Terminal + Web UI
    details: A fast inline TUI by default, with an optional browser UI as a first-class peer over the same event stream.
  - title: Agentic tool use
    details: File read/write/edit/glob/grep, shell execution, and a plugin SDK for adding your own tools.
  - title: Skills
    details:
      Drop a SKILL.md into a project or your user config to give the agent reusable, project-specific instructions.
  - title: Session persistence
    details: Resume, list, and export past sessions across TUI and Web UI.
  - title: Built for agents too
    details:
      Docs are embedded into the tau binary itself, so the agent can search and read its own documentation while helping
      you.
---
