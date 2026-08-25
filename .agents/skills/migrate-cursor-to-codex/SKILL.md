---
name: migrate-cursor-to-codex
description: >-
  Copy Cursor project rules, .cursorrules, commands, and Agent Skills into a
  separate Codex-readable setup using AGENTS.md and .agents/skills while leaving
  every original Cursor file unchanged. Use when adding Codex support to a
  Cursor-configured repository without replacing its Cursor setup. Do not use
  when the user wants to move, delete, deduplicate, or redesign the source setup.
---

# Copy Cursor Configuration to Codex

Create a separate, reviewable Codex setup from the target repository's existing
Cursor guidance. Treat `.cursor/**`, `.cursorrules`, and Cursor user settings as
read-only sources: never modify, move, rename, delete, or replace them.

"Copy" means a non-destructive migration. Skill directories and their resources
are physically copied. Rules and commands may require format adaptation in the
new Codex files, but the Cursor originals must remain byte-for-byte unchanged.
The Codex copies are snapshots and do not automatically follow later Cursor
changes.

This skill is intentionally self-contained. It requires no bundled references,
scripts, assets, templates, or other skills.

## When to Use This Skill

- The user wants Codex to understand an existing `.cursor/` setup.
- The user wants Cursor rules or commands copied into `AGENTS.md` or skills.
- The user wants to add Codex support while retaining the complete Cursor setup.

Do not migrate unrelated `.cursor/` components such as MCP configuration,
hooks, permissions, or editor settings unless the user includes them in scope.

## Installation Shape

Distribute only this `SKILL.md`. The recipient places it at either:

```text
# Repository scope
<project>/.agents/skills/migrate-cursor-to-codex/SKILL.md

# User scope
~/.agents/skills/migrate-cursor-to-codex/SKILL.md
```

The folder name must remain `migrate-cursor-to-codex` so it matches the skill's
`name`. After installation, start a fresh Codex session if the skill does not
appear immediately.

## Target Outcome

Produce a setup where:

- All original Cursor files and settings remain unchanged.
- Codex receives intended project instructions through `AGENTS.md` files.
- Reusable workflows are valid Agent Skills discoverable from `.agents/skills/`.
- Existing Codex instructions and unrelated user changes are preserved.
- Scope, explicit-only behavior, references, and safety boundaries are not lost.
- Codex copies are physical files rather than links back to Cursor sources.
- The handoff makes clear that future synchronization is manual.

## Cursor-to-Codex Mapping

Use these as defaults, not mechanical replacements. Classify the original
behavior and preserve its intent.

| Cursor source or behavior | Recommended Codex representation | Important notes |
| --- | --- | --- |
| Cursor User Rules in settings | `~/.codex/AGENTS.md` | User-global scope; require the user to provide the text and authorize global changes. |
| Root `.cursorrules` | Root `AGENTS.md`, sometimes split into skills | Legacy files often mix always-on policy and workflows; classify each section. |
| `.cursor/rules/*.mdc` with `alwaysApply: true` | Root `AGENTS.md` | Remove MDC frontmatter and merge without overwriting existing instructions. |
| Auto-attached MDC rule with `globs` | Root `AGENTS.md` with explicit path conditions, or nested `AGENTS.md` | Codex does not use Cursor's MDC glob trigger. Nested files only help when the Codex launch path includes that directory. |
| Agent-requested MDC rule | `.agents/skills/<name>/SKILL.md` | Use a discriminating description to retain relevance-based activation. |
| Manual MDC rule | Explicit-only Agent Skill | Add `agents/openai.yaml` with `policy.allow_implicit_invocation: false`. |
| `.cursor/commands/<name>.md` | Usually an explicit-only Agent Skill | Preserve arguments, required inputs, approvals, and expected output. |
| `.cursor/skills/<name>/SKILL.md` | Physical copy at `.agents/skills/<name>/SKILL.md` | Copy the complete skill directory, including sibling resources; adapt only the Codex copy. |
| Existing root `AGENTS.md` | Preserve and merge into it | Add missing Cursor semantics without regenerating or replacing existing content. |
| Nested `.cursor/rules/` | Root path-conditional guidance or subtree `AGENTS.md` | Choose based on the directory from which Codex is normally launched. |
| Cursor `@file` reference | Markdown link or explicit read instruction | Verify the target exists and make the path unambiguous. |
| Cursor skill `paths` field | Scope in description/body or subtree placement | Codex does not use this Cursor-specific field as an equivalent glob trigger. |
| Cursor `disable-model-invocation: true` | Codex explicit-only policy | Leave the Cursor field unchanged; add `agents/openai.yaml` only to the Codex copy. |

## Workflow

### 1. Establish Scope and Inspect Safely

Inspect before writing:

- Repository root and current working directory.
- Git status and existing uncommitted changes.
- Root and nested `AGENTS.md` or `AGENTS.override.md` files.
- `.cursorrules` and root or nested `.cursor/rules/**/*.mdc` files.
- `.cursor/skills/**/SKILL.md`, `.agents/skills/**/SKILL.md`, and existing links.
- `.cursor/commands/*.md` when command migration is requested or clearly useful.
- Referenced files, scripts, assets, and paths used by each rule or skill.

Treat user-level Cursor rules from application settings separately. They are not
necessarily stored in the repository. Ask the user to provide their content if
global copying is requested, and require explicit authorization before merging a
copy into `~/.codex/AGENTS.md`. Never edit the Cursor application settings.

Honor repository conventions where they do not weaken this copy-only contract.
For the Cursor-source-to-Codex-target relationship, this skill's requirement for
independent physical copies takes precedence over adapter, symlink, or shared
canonical-content conventions.

Before writing, record a source baseline for every in-scope Cursor file. Prefer
Git status and `git diff -- .cursor .cursorrules` for tracked files, plus a file
inventory or checksums when untracked files are present. This baseline is the
proof that copying did not mutate the source.

### 2. Build a Copy Inventory

Classify every source item by behavior:

- Always-on project guidance.
- File- or directory-scoped guidance.
- Relevance-selected guidance.
- Explicit-only rules or commands.
- Agent Skills that are already portable.
- Cursor-specific configuration outside the requested scope.

For each item, record its source, trigger, dependencies, intended destination,
and whether the Codex copy will be direct or semantically adapted. Every source
item remains unchanged. Detect duplicate skill names before creating files. Do
not begin mutation until every in-scope item has a proposed destination.

### 3. Create Separate Codex Copies

Use this default when the repository has no stronger convention:

```text
project/
|-- AGENTS.md
`-- .agents/
    `-- skills/
        `-- skill-name/
            |-- SKILL.md
            |-- scripts/       # optional
            |-- references/    # optional
            `-- assets/        # optional
```

Create Codex targets alongside the Cursor sources. Duplication is intentional in
this workflow. Do not replace either side with a symlink, hard link, wrapper that
delegates to the Cursor source, or a shared canonical directory. The Codex setup
must remain usable if `.cursor/` is later unavailable.

When a Codex target already exists, merge conservatively. Never overwrite an
existing `AGENTS.md`, skill, resource, or unrelated user change merely to make it
match Cursor. Stop and present the conflict when both versions contain
incompatible behavior.

### 4. Copy Rules by Semantics

Apply the mapping table and these rules:

- Always-on repository rules become concise root `AGENTS.md` instructions.
- Directory-scoped rules may become a nearby `AGENTS.md` when Codex is normally
  launched from that subtree. Otherwise use root guidance with an explicit path
  condition.
- Glob-scoped rules have no exact `AGENTS.md` frontmatter equivalent. Preserve
  the condition in plain language, such as "When editing `**/*.go`...".
- Relevance-selected rules that describe repeatable capabilities usually become
  Agent Skills.
- Manual rules and slash commands usually become explicit-only Agent Skills.
- Split legacy `.cursorrules` by intent in the Codex copy instead of copying its
  syntax wholesale. Leave the original file untouched.
- Translate Cursor `@file` references into Markdown links or explicit
  read-before-action instructions with verified paths.

Merge with existing `AGENTS.md`; do not overwrite it. Keep broad rules near the
root and specialized rules as close as practical to their intended scope. Avoid
duplicating the same instruction among Codex instruction files; duplication
between the preserved Cursor source and the new Codex copy is expected.

#### Always-On Example

Cursor source:

```markdown
---
description: Repository conventions
alwaysApply: true
---

- Run unit tests after changing application code.
- Never commit generated credentials.
```

Merge into root `AGENTS.md` as plain Markdown:

```markdown
## Repository conventions

- Run unit tests after changing application code.
- Never commit generated credentials.
```

Do not retain `description`, `globs`, or `alwaysApply` as inert YAML in
`AGENTS.md`.

#### Glob-Scoped Example

Cursor source:

```markdown
---
description: Go source conventions
globs: "**/*.go"
alwaysApply: false
---

- Run `go test ./...` after changing Go code.
```

Portable root `AGENTS.md` form:

```markdown
## Go files

When modifying files matching `**/*.go`, run `go test ./...` before handoff.
```

Do not assume a nested `AGENTS.md` reproduces Cursor's per-file auto-attachment
when Codex is always launched from the repository root.

### 5. Copy and Adapt Skills and Commands

For every copied skill:

- Physically copy the complete `.cursor/skills/<name>/` directory to
  `.agents/skills/<name>/`; do not link the destination to the source.
- Keep one folder per skill with a `SKILL.md` containing valid `name` and
  `description` frontmatter. Make compatibility edits only in the destination.
- Copy sibling `scripts/`, `references/`, `assets/`, and other required files,
  then verify their relative paths from the destination.
- Make the description state what the skill does and when it should activate.
- Preserve operational constraints, required inputs, approvals, stopping
  conditions, and output expectations.
- Update invocation examples when helpful: Cursor commonly uses `/skill-name`;
  Codex can explicitly mention `$skill-name`.
- Treat Cursor `paths` as a Cursor-specific hint. Express that boundary in the
  Codex description or instructions, or place the skill in the relevant subtree
  when that matches how Codex will be launched.

For explicit-only behavior, add:

```yaml
# agents/openai.yaml
policy:
  allow_implicit_invocation: false
```

Leave Cursor's `disable-model-invocation: true` field in the source. It may also
remain in the Codex copy when harmless, but Codex explicit-only behavior comes
from `agents/openai.yaml`. Host-specific fields must not replace portable `name`
and `description` fields.

Audit nested `SKILL.md` files. Some hosts recursively discover skill roots, so a
template or example named exactly `SKILL.md` can appear as an unintended second
skill. Rename inert templates or keep them outside discovered roots unless they
are intentionally invocable.

### 6. Preserve Existing AGENTS.md Semantics

Never regenerate an existing `AGENTS.md` from scratch:

1. Identify instructions already represented there.
2. Add only missing Cursor semantics.
3. Reconcile contradictions explicitly; instructions closer to the Codex launch
   directory take precedence.
4. Keep always-on context practical. Move repeatable workflows into skills.

`AGENTS.override.md` is an override, not a second base file. Preserve its role
unless the user explicitly wants to remove the override behavior.

### 7. Leave Unrelated Cursor Configuration Alone

Do not translate these merely because they live under `.cursor/`:

- MCP server configuration.
- Hooks or automation definitions.
- CLI permissions and allow/deny rules.
- Editor settings, extensions, themes, or keybindings.
- Cached or generated files.

List them as out of scope. If requested, handle each as a separate compatibility
task because Codex may use different configuration and security models.

This protection also applies to in-scope Cursor sources: reading them is allowed,
but editing, formatting, renaming, relocating, deleting, or replacing them is not.

### 8. Validate Observable Behavior

Before declaring success:

- Ensure every source rule is mapped, deliberately retained, or excluded.
- Validate YAML frontmatter and confirm each skill name matches its folder.
- Confirm every referenced file, script, and asset resolves from the Codex copy.
- Confirm copied Codex skills and resources are regular files/directories, not
  symlinks or hard links to `.cursor/` sources.
- Check for duplicate skill names across all discovered locations.
- Ensure explicit-only workflows remain explicit-only in each supported host.
- Verify file or directory boundaries were not silently widened.
- Start a fresh Codex run from the repository root and ask it to list or
  summarize active instruction sources and skills.
- Repeat from relevant subdirectories if nested instructions or skills matter.
- Compare the final Cursor source state with the recorded baseline. For tracked
  files, require `git diff -- .cursor .cursorrules` to show no new changes caused
  by this workflow. For untracked sources, compare the saved inventory or
  checksums. Any source mutation is a failed copy and must be reverted safely or
  reported before proceeding.
- Verify the original Cursor skills still appear and Cursor-specific rules remain
  intact.
- Review the final diff for unrelated edits, duplicated content, secrets, and
  accidental deletion of the original setup.

Stop and present evidence plus options when a Cursor glob, user-level rule, or
host-specific behavior cannot be represented faithfully. Do not silently widen
the scope of a rule.

## Handoff

Report:

- Original Cursor paths inspected and confirmation that they remained unchanged.
- Codex files created or merged and any compatibility adaptations made only to
  those copies.
- Semantic differences or unsupported Cursor behavior.
- That the Codex copies are snapshots and future Cursor changes require an
  intentional re-copy or manual synchronization.
- Validation performed and anything requiring a fresh application session or
  user confirmation.

When current product behavior matters, prefer official documentation:

- [Codex custom instructions with AGENTS.md](https://learn.chatgpt.com/docs/agent-configuration/agents-md)
- [Codex Agent Skills](https://learn.chatgpt.com/docs/build-skills)
- [Cursor Rules](https://docs.cursor.com/context/rules-for-ai)
- [Cursor Agent Skills](https://cursor.com/docs/skills)
