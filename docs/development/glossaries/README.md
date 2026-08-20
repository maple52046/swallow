# Glossaries

本目錄集中管理 root project 的 domain glossary。`README.md` 是 glossary documentation 的主要入口，負責說明如何以最少必要文件完成查詢或修改。

Glossary 是 ubiquitous language 的來源。若詞彙定義不清，程式碼、API、資料模型與測試都不得先行猜測。

## Documentation Map

- [`outline.md`](outline.md): bounded-context directory and glossary index.
- [`spec.md`](spec.md): glossary authoring and modification rules.
- [`terms/`](terms): canonical location for glossary term documents.

## Read-only Glossary Lookup

若只需要理解既有 terminology，依序讀取：

```text
README.md
  -> outline.md
    -> terms/<bounded-context-or-term>.md
```

Expected behavior:

1. Read this `README.md`.
2. Read [`outline.md`](outline.md) to identify the relevant bounded context or term.
3. Read only the required file under [`terms/`](terms).

Do not load unrelated glossary files. This keeps context usage small and preserves bounded-context isolation.

## Glossary Authoring or Modification

若需要新增或修改 glossary content，依序讀取：

```text
README.md
  -> outline.md
  -> spec.md
    -> terms/<bounded-context-or-term>.md
```

Expected behavior:

1. Read this `README.md`.
2. Read [`outline.md`](outline.md) to identify the affected bounded context or term.
3. Read [`spec.md`](spec.md) before changing glossary content.
4. Create or modify only the relevant file under [`terms/`](terms).

The glossary writing specification must always be read before glossary modification.

## Canonical Location

All glossary term documents must live under [`terms/`](terms).

Use [`outline.md`](outline.md) as the navigation index. Do not place standalone term files beside `README.md`, `outline.md`, or `spec.md`.

## Pending Terms

[`outline.md`](outline.md) 的 Pending Terms 章節列出「已被既有文件或程式碼使用，但尚未定義」的術語。

- 若任務用到 pending term，必須先依 authoring 流程補上該 term，或與使用者確認語意，不得沿用推測的定義。
- 若 pending term 的既有用法在不同文件間互相矛盾，必須先收斂矛盾再寫入 glossary，並在 Change note 記錄收斂依據。

## Required Agent Behavior

- For glossary lookup, read only the minimum documents required by the lookup flow.
- For glossary changes, read [`spec.md`](spec.md) before editing.
- Do not load unrelated bounded contexts or term files.
- If a domain term is missing, unclear, or changing meaning, update or create the relevant glossary before continuing with design or implementation.
- If the affected bounded context is unclear, stop and ask before creating new glossary files.
