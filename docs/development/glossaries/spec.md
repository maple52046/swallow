# Glossary Authoring Spec

This document defines how to create and modify glossary files.

Read this file before changing any glossary content. For read-only terminology lookup, this file does not need to be loaded.

## Canonical Location

All glossary term documents must live under:

```text
docs/development/glossaries/terms/
```

Do not place standalone term files beside `README.md`, `outline.md`, or `spec.md`.

## File Naming

Use lowercase kebab-case file names:

```text
terms/<bounded-context-or-term>.md
```

Examples:

```text
terms/server.md
terms/server-status.md
terms/allocation-state.md
```

Avoid vague names such as:

```text
terms/common.md
terms/misc.md
terms/resource.md
```

If no precise bounded context or term name is available, clarify the bounded context before creating a file.

## Term File Format

Each glossary term file should use this structure:

```markdown
# <Term>

- Bounded context: The context in which this definition applies.
- Definition: The precise meaning of this term inside the bounded context.
- Allowed meaning: Valid usage of this term.
- Disallowed meaning: Meanings that must not be mixed with this term.
- Synonyms: Accepted synonyms; use None if there are none.
- Deprecated terms: Old names or forbidden names; use None if there are none.
- Examples: Sentences or scenarios that validate the meaning.
- Related terms: Related terms and their relationships.
- Change note: Why this definition was added or changed.
```

Related terms are written as plain term names, not as links. A term document must
stay readable without loading its neighbours, and the outline is the only
navigation index.

## Writing Rules

- Define terms by domain meaning, not by database tables, API fields, UI labels, or implementation details.
- If the same word has different meanings in different bounded contexts, define those meanings separately.
- Prefer precise bounded-context language over generic umbrella terms.
- Do not create `common`, `misc`, or `resource` glossaries to avoid deciding context boundaries.
- If a term is only a technical implementation detail and not domain language, it usually does not belong in the glossary.
- Glossary changes that affect code names, API contracts, data models, tests, or docs must be synchronized with those artifacts.
- Documentation, test names, API names, and code names should use the primary terms from glossary files whenever practical.
- Do not invent a definition to fill a gap. If the existing usage is ambiguous or
  contradictory across documents or code, resolve the contradiction first, then
  record what the resolution was based on in the Change note.

## Value Sets And Enumerations

A term whose domain meaning is a closed set of values (for example a status)
gets its own term file listing every value and its meaning.

- The value list in the glossary is authoritative for domain language. API
  contracts and code must use exactly these values.
- If a UI label differs from the domain value, state both and mark which one is
  the domain term.
- Adding, removing, or renaming a value is a model change: update the term file,
  the affected API contracts, and each sub project that uses it.

## Outline Updates

When adding or renaming a term file, update [`outline.md`](outline.md).

The outline should:

- Link to the term under `terms/`.
- Include a short one-line summary.
- Keep terms grouped by bounded context or topic.
- Avoid repeating full definitions that already exist in term files.
- Remove a term from Pending Terms when its term file is created.

## Agent Checklist

Before modifying glossary content, an AI agent must confirm:

- It has read [`README.md`](README.md).
- It has read [`outline.md`](outline.md).
- It has read this `spec.md`.
- It knows the affected bounded context.
- It is editing only the relevant file under `terms/`.
- It will update `outline.md` if a term file is added, removed, renamed, or regrouped.
- It has checked whether the glossary change affects code, APIs, data models, tests, or other documentation.

If any item is unclear, stop and ask before editing.
