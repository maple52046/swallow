# gdcm — GPU Datacenter Management

## Overview

本 repository 是 **GPU Datacenter Management (gdcm) platform** 的 monorepo
入口，採用 git submodule 統一管理整個 platform 所有的 sub-project，
並集中收納跨 sub-project 共通的約束文件。

## Why this repository

GPU Datacenter Management 由多個獨立 sub-project 組成
（後端 API、前端 dashboard，未來還會加入更多 component）。
若各 sub-project 各自為政，容易出現以下問題：

- 領域術語在不同 sub-project 中產生語意漂移。
- API 與資料契約沒有單一事實來源 (single source of truth)，整合時反覆對焦。
- vibe coding 缺乏全局 vision，難以掌握整個 platform 的演進方向。

本 repository 透過兩個機制解決上述問題：

- **以 git submodule 統一管理所有 sub-project**：
  所有原始碼以 submodule 形式集中於 [`src/`](src/)，
  並在 root 以 component symlink（如 `api-server`、`dashboard`）按角色分門別類。
- **以 [`docs/`](docs/) 收納 platform 共通約束文件**：
  glossary、API contract、共通資料模型等定義性文件，
  作為跨 sub-project 開發與維護的單一事實來源。

如此一來，整個 platform 的開發與維護皆在可控範圍內進行。

## Repository Composition

- [`src/`](src/) — 集中放置所有 sub-project 的 git submodule，不直接放原始碼。
- 根目錄 component symlinks — 以角色名稱（如 `api-server`、`dashboard`）
  指向 `src/<repo>`，讓路徑語意貼近領域而非 repo 命名。
- [`docs/`](docs/) — platform 共通約束文件，包含結構契約、glossary、API contract 等。

## Repository Layout

```text
.
├── api-server  -> src/swallow      # symlink, component
├── dashboard   -> src/dashboard    # symlink, component
├── docs/                           # platform 共通約束文件
│   ├── architecture.md             # repo 結構契約
│   ├── glossaries/                 # 共通領域術語
│   └── api-contracts/              # 跨 sub-project API / 資料契約
├── src/                            # 所有 sub-project 的 submodule
│   ├── swallow/
│   └── dashboard/
└── .gitmodules
```

## Components

目前已註冊的 components：

- **api-server** → [`src/swallow`](src/swallow)
  — Data Center API Service，提供 platform 的後端 HTTP API。
- **dashboard** → [`src/dashboard`](src/dashboard)
  — 前端 dashboard，採 React + TypeScript + Vite。

完整 component 註冊規則與新增流程請見 [`docs/architecture.md`](docs/architecture.md)。

## Documentation

- [`docs/architecture.md`](docs/architecture.md) — repo 結構契約（必讀）。
- [`docs/glossaries/`](docs/glossaries) — 跨 sub-project 共通的領域術語，
  定義 platform 的 ubiquitous language。
- [`docs/api-contracts/`](docs/api-contracts) — 跨 sub-project 的 API 與資料契約。

## Getting Started

### 初次 clone（含 submodule）

```bash
git clone --recurse-submodules <repo-url>
```

### 既有 clone 補拉 submodule

```bash
git submodule update --init --recursive
```

### 同步 submodule 至 upstream 最新版本

```bash
git submodule update --remote
```

各 sub-project 的 build、run、test、deploy 指令請見其對應的 [`src/<repo>/`](src/) README。

## Contributing

- **新增 / 移除 component**：請依 [`docs/architecture.md`](docs/architecture.md)
  的 Operating Conventions 流程執行（`git submodule add` → 建立 symlink →
  在 architecture.md 的 Components 章節登錄）。
- **新增跨 sub-project 共通概念**：請於 [`docs/glossaries/`](docs/glossaries)
  或 [`docs/api-contracts/`](docs/api-contracts) 增補對應定義文件，
  讓所有 sub-project 共享同一份事實。
- **sub-project 內部實作**：build、test、deploy、模組結構等細節
  在對應的 `src/<repo>/` 中處理，本 repo 不重複描述。
