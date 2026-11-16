---
name: migrate-cursor-to-codex
description: >-
  Copy Cursor project rules, .cursorrules, commands, and Agent Skills into a
  separate Codex-readable setup using AGENTS.md and .agents/skills while leaving
  every original Cursor file unchanged. Use only when the user explicitly invokes
  /migrate-cursor-to-codex to add Codex support to a Cursor-configured repository.
disable-model-invocation: true
---

# migrate-cursor-to-codex

This is a thin Claude Code wrapper. This skill has no copy under `skills/`; its
canonical instructions are the Codex skill
[`.agents/skills/migrate-cursor-to-codex/SKILL.md`](../../../.agents/skills/migrate-cursor-to-codex/SKILL.md)
(relative to the repository root). Read that file and follow it.
