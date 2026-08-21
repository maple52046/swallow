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
- [`deploy/`](deploy/) — 跨 sub-project 的環境編排，例如本機開發環境。

## Repository Layout

```text
.
├── api-server  -> src/swallow      # symlink, component
├── dashboard   -> src/dashboard    # symlink, component
├── AGENTS.md                       # AI agent 入口導引
├── deploy/                         # 跨 sub-project 環境編排
│   └── dev/                        # 本機開發環境 (Docker Compose)
├── docs/                           # platform 共通約束文件
│   ├── development/                # 平台層級開發規範
│   │   ├── architecture-spec.md    # Strategic DDD 架構憲法
│   │   ├── codebase-structure.md   # repo 結構契約
│   │   ├── api-contracts.md        # API contract discovery workflow
│   │   ├── commit-spec.md          # commit message 規範
│   │   └── glossaries/             # 共通領域術語（ubiquitous language）
│   ├── decisions/                  # 輕量 ADR
│   └── plans/                      # 歷史計畫紀錄
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

完整 component 註冊規則與新增流程請見
[`docs/development/codebase-structure.md`](docs/development/codebase-structure.md)。

## Documentation

開發（含 AI agent 協作）一律從 [`AGENTS.md`](AGENTS.md) 進入，它會依任務類型指出最小必讀集合。

- [`AGENTS.md`](AGENTS.md) — agent 入口導引（必讀起點）。
- [`docs/development/codebase-structure.md`](docs/development/codebase-structure.md) —
  repo 結構契約：component、submodule、symlink 與 docs 分類。
- [`docs/development/architecture-spec.md`](docs/development/architecture-spec.md) —
  Strategic DDD 架構憲法：bounded context、context map、distillation 與跨 context 規則。
- [`docs/development/glossaries/`](docs/development/glossaries/README.md) —
  跨 sub-project 共通的領域術語，定義 platform 的 ubiquitous language。
- [`docs/development/api-contracts.md`](docs/development/api-contracts.md) —
  API contract 的 provider-first discovery workflow；實際 contract 由 provider component 擁有。
- [`docs/development/commit-spec.md`](docs/development/commit-spec.md) — commit message 規範。
- [`docs/decisions/`](docs/decisions/README.md) — 影響面廣的架構決策紀錄（ADR）。

各 sub-project 採用完整 Clean Architecture，內部架構與 coding style 見其
`AGENTS.md` 與 `docs/development/`。

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

### 啟動本機開發環境

MongoDB 與所有 component 以 Docker Compose 一次啟動，host 只需要 Docker：

```bash
cd deploy/dev
docker compose up -d
```

dashboard 位於 <http://localhost:5173>，API 位於 <http://localhost:30051>，
預設 admin 帳號為 `admin` / `admin`。
完整說明（設定、hot reload、遠端存取）見 [`deploy/dev/README.md`](deploy/dev/README.md)。

各 sub-project 單獨的 build、run、test 指令請見其對應的 [`src/<repo>/`](src/) README。

## Contributing

- **新增 / 移除 component**：請依
  [`docs/development/codebase-structure.md`](docs/development/codebase-structure.md)
  的 Operating Conventions 流程執行（`git submodule add` → 建立 symlink →
  在 Component Alias 章節登錄）。
- **新增跨 sub-project 共通概念**：請依
  [`docs/development/glossaries/README.md`](docs/development/glossaries/README.md)
  的流程增補 term，並更新 outline，讓所有 sub-project 共享同一份事實。
- **新增 / 修改 API 行為**：請依
  [`docs/development/api-contracts.md`](docs/development/api-contracts.md)
  找到 provider component，並更新該 provider 擁有的 contract 後再實作。
- **sub-project 內部實作**：build、test、deploy、模組結構等細節
  在對應的 `src/<repo>/` 中處理，本 repo 不重複描述。
