# 參與 swallow 貢獻

[English](CONTRIBUTING.md) · [專案 README](README.zh-TW.md)

swallow 是具有嚴格 component／contract boundary 的 monorepo。良好的 contribution
只改動最小 owning component、保持 shared language 與 provider contract 正確，
並完成與風險相稱的驗證。

## 開始前

1. 閱讀 root [codebase structure](docs/development/codebase-structure.md)。
2. 判斷 owning component：`api-server`、`dashboard` 或 `cli`。
3. 閱讀該 component 的 `AGENTS.md` 與 architecture specification。
4. Domain behavior、data model、core flow 或 cross-component work 需閱讀 root
   [architecture specification](docs/development/architecture-spec.md)，並遵循
   [glossary workflow](docs/development/glossaries/README.md)。
5. API work 需先找到並更新 provider-owned
   [API contract](docs/development/api-contracts.md)，再實作或同步實作。

Implementation plan 一律放在 repository root `docs/plans/manuscripts/`，
不要建立 component-local plan tree。

## Development environment

Contributor golden path 使用 Docker Compose：

```bash
cd deploy/dev
docker compose up -d
docker compose ps
```

Hot reload、configuration、seeding、remote access 與 troubleshooting 請見
[development environment guide](deploy/dev/README.zh-TW.md)。

Component checks：

```bash
# API
cd api-server
gofmt -l .
go vet ./...
go test ./...
go build ./...

# CLI
cd ../cli
gofmt -l .
go vet ./...
go test ./...
go build ./...

# Dashboard
cd ../dashboard
npm ci
npm run lint
npm run build
npm run test:e2e
```

迭代時可只跑相關 checks；送出前必須完成全部 required set。

## Component boundaries

- `api-server` 擁有 HTTP API、intent、policy、identity mapping、reconciliation
  與 durable execution adapters。
- `dashboard`、`cli` 消費 published HTTP contract，不 import 或推測
  api-server private implementation。
- Cross-component behavior 只透過 provider-owned contract 與 shared glossary，
  不透過 shared internal package。
- 同一個 behavior 在 owning component 內只能有一個 canonical implementation。

## API 與 domain change

- 只有 Active contract 可直接實作。
- Behavior 改變時，同步更新 contract status、request/response shape、error、
  auth 與 compatibility note。
- Consumer model 不得從 private provider struct 推導。
- 使用新 term、enum、state 前先定義或更新 domain language。
- Breaking public-contract change 需要 migration note 與 `BREAKING CHANGE` footer。

## 文件

- 公開文件以英文為 default，並提供內容對等的繁體中文版本。
- `docs/en/` 與 `docs/zh-TW/` 下的 cross-project public pages 必須有相同 relative path。
- Colocated public reference 使用 `README.md`／`README.zh-TW.md`，或
  `name.md`／`name.zh-TW.md`。
- 公開文件可以連到 development source of truth；AGENTS、development spec、
  contract、ADR、glossary、skill 不得反向連到公開指南。
- Screenshot 只能來自 deterministic Playwright fixture；不得發布真實 endpoint、
  credential、customer name 或 inventory。

執行：

```bash
python3 scripts/check-docs.py
```

## Commit 與 pull-request checklist

遵循 [commit specification](docs/development/commit-spec.md)。Commit message
使用英文 Conventional Commits。

送出 review 前：

- [ ] 變更有清楚 owning component 或 documented cross-component boundary。
- [ ] Domain term 與 API contract 已更新。
- [ ] Tests 涵蓋 success、failure 與 compatibility behavior。
- [ ] Go comments 或 TypeScript/JSDoc 符合 component completion gate。
- [ ] User-visible behavior 的公開文件與兩種語言已同步。
- [ ] Local documentation links 與 directionality checks 通過。
- [ ] 沒有 commit secret、credential、customer data 或 generated local artifact。
