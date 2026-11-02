# 參與 swallow 貢獻

[English](CONTRIBUTING.md) · [專案 README](README.zh-TW.md)

swallow 是具有嚴格 component／contract boundary 的 monorepo。良好的 contribution
只改動最小 owning component、保持 shared language 與 provider contract 正確，
並完成與風險相稱的驗證。

swallow 的 core 是通用的：組合負責和外部系統溝通的 provider，以及加入 swallow
能管理之物的 plugin，就成為開箱即用的 datacenter 管理。新的能力是一個 plugin
或 provider；core 不含任何廠商、產品、Platform 或 GPU 廠商的知識。
[設計主張](docs/development/architecture-spec.md#swallow-的設計主張)寫出這個理念、
目前的組合，以及每個變更都要遵守的原則。

## 開始前

1. 閱讀[系統總覽](docs/development/system-overview.md)：swallow 做什麼、有哪些
   process 與它們在背景做什麼、一次 Platform 部署怎麼走、資料存在哪裡由誰寫入，
   以及程式在哪裡。
2. 閱讀 root [codebase structure](docs/development/codebase-structure.md) 與 root
   [architecture specification](docs/development/architecture-spec.md) 開頭的設計主張。
3. 判斷 owning component：`api-server`、`dashboard` 或 `cli`，並閱讀該 component 的
   `AGENTS.md` 與 architecture specification。
4. 閱讀你要修改的領域的規格：
   - HTTP API 行為：provider-owned
     [API contract](api-server/docs/development/api-contracts/README.md)，先於或與程式同步更新。
   - OS 部署、OS Image、Deployment Template、Zone 與 Pool：
     [OS provisioning](docs/development/os-provisioning.md)。
   - Enrollment、硬體檢視、Power Configuration、Boot Media：
     [Server enrollment and boot media](docs/development/server-enrollment-and-boot-media.md)。
   - Platform（Kubernetes、Slurm）：[platform deployment](docs/development/platform-deployment.md)。
   - Managed Software：[software deployment](docs/development/software-deployment.md)。
   - Workflow 怎麼執行、重試與取消：[Workflow execution](docs/development/workflow-execution.md)。
   - SSH key、登入帳號、API key：[access and credentials](docs/development/access-and-credentials.md)。
   - `api-server/modules/` 底下的 playbook、role 或 plugin：
     [模組撰寫指南](docs/development/module-authoring.md)。
5. 新增受管理的資源或欄位、改變事實的擁有者，或新增 provider 或 plugin 時，另外閱讀
   [desired-state model](docs/development/desired-state-model.md)、
   [core and extensions](docs/development/core-and-extensions.md) 與
   [glossary workflow](docs/development/glossaries/README.md)。

不是每個變更都需要書面計畫。寫了計畫時，放在 repository root 的
`docs/plans/manuscripts/`，不要放在 component 內，也不要只留在工具自己的目錄。

## Development environment

Contributor golden path 使用 Docker Compose：

```bash
cd deploy/dev
docker compose up -d
docker compose ps
```

Hot reload、configuration、seeding、remote access 與 troubleshooting 請見
[development environment guide](deploy/dev/README.zh-TW.md)。
[開發與測試指南](docs/development/development-and-testing.md)說明各種測試放在哪裡、
共用的 fake 與 fixture，以及測試用的環境變數。

Component checks（在 host 執行，需要 Go、Node 24 與 Python 3）：

```bash
# API
cd api-server
gofmt -l .
go vet ./...
SWALLOW_TEST_MONGO_URI='mongodb://localhost:27017/?directConnection=true' go test ./...
go build ./...
# 改了 OpenAPI fragment 之後
UPDATE_OPENAPI=1 go test ./internal/shared/apicontract

# CLI
cd ../cli
gofmt -l .
go vet ./...
go test ./...
go build ./...

# Dashboard
cd ../dashboard
npm ci
npx playwright install chromium   # 第一次執行 e2e 前安裝瀏覽器
npm run contract:check            # 改了 OpenAPI 時先執行 contract:generate
npm run lint
npm run build
npm run test:e2e
```

Mongo 整合測試需要開發環境的 replica set，沒有設定 `SWALLOW_TEST_MONGO_URI` 時會
skip。迭代時可只跑相關 checks；送出前必須完成全部 required set。

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
