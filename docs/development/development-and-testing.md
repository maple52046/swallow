# 開發與測試指南

本文件說明在 swallow 開發時：開發環境怎麼運作、測試放在哪裡、怎麼寫、需要哪些環境變數，以及每個 component 完整的
檢查指令。每個 component 的完成門檻（要做哪些審查、何時算完成）仍以各自的 `AGENTS.md` 為準；本文件只補上「怎麼做」。

- **讀者**：要寫或執行測試、在本機啟動 swallow、或第一次跑完整檢查的開發者與 AI agent。
- **不在範圍內**：host automation 內容（playbook、role）的測試，見 [module-authoring.md](module-authoring.md) §6；
  各 component 的程式風格與測試寫法（table-driven、只用標準 `testing` 等），見各自的 coding style。

---

## 1. 開發環境

開發環境在 `deploy/dev/`，以 Docker Compose 啟動。host 只需要 Docker Engine 與 Compose plugin 就能跑起整個 stack；
Go、Node、MongoDB、Temporal 與 Ansible 都在容器內。

| 容器 | 內容 |
| --- | --- |
| `mongo` | MongoDB single-node replica set `rs0`，第一次啟動時由 healthcheck 初始化；host 以 `localhost:27017` 連線 |
| `temporal`、`temporal-postgresql`、`temporal-ui` | Temporal server 與它的 PostgreSQL；UI 在 port 8233 |
| `api-server`、`worker`、`ansible-executor` | 同一個 image（`deploy/dev/api-server.Dockerfile`），各自以 Air 執行 `swallow-api` 的一個 process role |
| `prometheus` | 開發用的 Prometheus |
| `pulp-db`、`pulp-init` | 一次性，每次 `up` 都執行：在 `temporal-postgresql` 建立 Pulp 的 role 與 database `pulp`；補上金鑰、遷移 schema、設定 admin 密碼（`PULP_ADMIN_PASSWORD`，預設 `admin`）與 deb 簽章服務 |
| `pulp-api`、`pulp-content`、`pulp-worker`、`pulp-web` | 開發 Site 的 [Local Repository](glossaries/terms/local-repository.md)（Pulp 3，[local-repository.md](local-repository.md)）；`pulp-web` 發佈在每個介面的 `PULP_HTTP_PORT`（預設 8080），api-server 以 `http://pulp-web:8080` 呼叫它 |
| `dashboard` | Vite dev server（port 5173，HMR） |

- **hot reload**：Air 監看 `api-server/` 的 `.go`、`.yaml`、`.yml`（排除 `tests/`、`docs/` 與 `_test.go`），存檔就重新
  建置並重啟這三個 process。重啟 `ansible-executor` 會中斷執行中的 Ansible Task，該 Task 停在 `requires_attention`
  （`executor_outcome_unknown`）。`modules/manifest.json` 不在監看的副檔名內，改了它要手動重啟。
- **沒有 MAAS 時**：MAAS 是選用的。沒有設定 MAAS，stack、登入、Site 與 metrics Integration 都能用，但不會有任何
  Server：Server 只來自 provisioner 的 reconcile。開發 provisioning 功能不需要真的 MAAS：Go 的測試用 fake provider 與
  MAAS adapter 的 httptest server（§3.1），dashboard 用 Playwright fixture（§4）。
- **時間同步**：api-server 的 `SWALLOW_API_NTP_SERVER` 是空的時候，每一次 OS 部署都會被拒絕。要在開發環境部署 OS，
  先把它設成 Server 連得到的 NTP server（規則見 [installation.md](installation.md) 的時間同步）。
- **Local Repository**：lab 的 Server 從 Pulp 取得部署要的每個 artifact，Pulp 也以它的 origin 寫出絕對 URL，所以要在
  `deploy/dev/.env` 把 `PULP_ORIGIN` 設成 Server 連到這台主機 `PULP_HTTP_PORT` 的位址（例如 `http://10.170.168.20:8080`），
  再重建 Pulp 的 services；預設的 `http://localhost:8080` 只有這台主機自己連得到。設了 `PULP_ORIGIN` 時，`deploy/dev/seed.sh`
  登錄 Site 的 `repository` Integration（endpoint `http://pulp-web:8080`、setting `contentOrigin` 等於 `PULP_ORIGIN`、
  credential 是 Pulp 的 admin 密碼）並寫入預設的 `ubuntu` 來源（[local-repository.md](local-repository.md) §4.1、§5.2）。
  Site 沒有 ready 的 Local Repository 時，會從它取得 artifact 的部署在受理時就被拒絕。
- **CLI** 不在任何容器內；在 host 以 Go 建置（`cd cli && go build -o bin/swallow ./cmd/swallow`）。

## 2. 在哪裡執行檢查

§6 的檢查指令在 host 執行，需要：

- Go（版本見 `api-server/go.mod` 與 `cli/go.mod`）、Node 24 與 npm；
- `python3`（`scripts/check-docs.py` 與部分 host automation 內容測試），部分內容測試另外需要 `cc`、`bash` 與 `timeout`；
- 跑 Mongo 整合測試時，一個 replica set：開發環境的 `mongo` 容器即可。

容器內不適合跑完整檢查：dashboard 容器沒有掛載 `api-server/`，`contract:generate` 與 `contract:check` 讀不到 OpenAPI；
它的 image 也沒有 Playwright 的瀏覽器。api-server 容器有 Go toolchain，但以 `CGO_ENABLED=0` 建置，不能用 `-race`。

## 3. api-server 的測試

### 3.1 測試放在哪裡

| 要測的東西 | 位置與做法 |
| --- | --- |
| domain 規則、use case | 與程式同 package 的 `_test.go`，fake 定義在該 package。transaction 用 `internal/core/store/storetest` 的 `Transactor`（它不能 rollback，「失敗時沒有寫入」由下面的 Mongo 整合測試驗證）；provisioner 設定的 in-memory store 在 `internal/core/provisioning/domain/settingstest`；建 `ChangePlanService` 需要的 Change Plan 政策 store 在 `internal/core/convergence/convergencetest`（`Policies`，零值是送出即核准） |
| 跨 context 的 HTTP 行為 | `api-server/tests/`：`setup_test.go` 的 `setupPlatform` 以 in-memory fake 接上真的 use case 與 handler（沒有 Mongo、Temporal、MAAS），`auth_test.go` 的 `doRequest` 送出 request。路由在 `setupPlatform` 內**手動**註冊，新路由要加在那裡，否則測試得到 404。可照著寫的範例：`tests/server_deletion_test.go` |
| 單一 handler | 同 package 的 delivery 測試：自己建 Fiber app，第一個 middleware 掛 `apicontract.ResponseValidator(t)`，例如 `internal/core/provisioning/delivery/http_proxy_handler_test.go` |
| 回應符合契約 | `apicontract.ResponseValidator(t)` 依 OpenAPI 檢查每個回應的 status、Content-Type 與 body（不允許多出的欄位）；`setupPlatform` 已掛上 |
| 路由與契約一致 | `internal/app/routes_contract_test.go` 檢查註冊的路由與契約列出的路由雙向一致 |
| Mongo repository 與 transaction | `internal/adapters/store/mongodb/<context>/`；需要 `SWALLOW_TEST_MONGO_URI`（§3.2），每個測試建立自己的 database 並在結束時刪除（沒有共用 helper；可照著寫的範例：`server/platform_claim_transaction_test.go` 的 `claimTestDatabase`） |
| MAAS adapter | `internal/adapters/osprovisioning/maas/provider_test.go` 的 `fakeMAAS`（httptest server）與 `newTestProvider` |
| 其他 provider 的 fake | `tests/fakes_test.go`：`fakeProvider`（預設有 MAAS 的全部 capability，可注入錯誤並記錄呼叫）、`minimalProvider`（沒有任何 capability）、`fakeProviderFactory`、`executingLauncher`（在 request 內同步執行部署，取代 Temporal）。沒有共用的 fake provider package，其他 package 各自定義小型 fake |
| Temporal driver | `internal/adapters/executor/temporalworkflow/driver_test.go`，用 Temporal SDK 的 testsuite，不需要 Temporal server |
| 組裝（模組、plan catalog、完成行為） | `internal/app/*_test.go`，例如 `workflow_plan_test.go`（每個 Task kind 已登錄、每個 Job 有顯示名稱）、`modules_test.go` |
| 依賴方向與 core 的廠商詞彙 | `tests/layout_test.go`、`tests/core_vendor_terms_test.go`（規則見 api-server architecture spec 的 Layout） |
| host automation 內容 | `internal/adapters/hostautomation/ansible/`，見 [module-authoring.md](module-authoring.md) §6 |

### 3.2 環境變數

| 變數 | 用途 |
| --- | --- |
| `SWALLOW_TEST_MONGO_URI` | 啟用 Mongo 整合測試；沒有設定時這些測試 skip。開發環境的值是 `mongodb://localhost:27017/?directConnection=true`（replica set 的成員名稱是 `mongo:27017`，host 必須直連） |
| `UPDATE_OPENAPI=1` | 重新產生 OpenAPI bundle：`go test ./internal/shared/apicontract`（流程見 api-server 契約 README 的 OpenAPI 一節） |
| `UPDATE_VENDOR_BASELINE=1` | 重寫 core 廠商詞彙的基準：`go test ./tests/ -run TestCoreVendorTermsOnlyDecrease`。數字下降而沒有重寫時，測試同樣失敗 |
| `SWALLOW_TEST_IPXE_DIR` | 啟用真的 Boot ISO 建置測試（`internal/adapters/bootiso/genfsimg`）；沒有設定時 skip |
| `APICONTRACT_AREAS` | 只載入指定的 OpenAPI fragment，用於縮小除錯範圍；完整的測試必須在沒有設定時通過 |

## 4. dashboard 的測試

- **沒有 unit test runner**。行為以 Playwright e2e 驗證。
- **e2e**：`dashboard/playwright.config.ts`，測試在 `dashboard/tests/e2e/`。`dashboard/tests/e2e/fixtures.ts` 的 `installApiFixtures(page, options)`
  在瀏覽器端攔截所有 `**/api/v1/**`，完全不連 api-server；沒有 fixture 的路徑回 `404 not_found`。`FixtureOptions`
  分四類：注入失敗（例如 `deploymentFailureIds`、`softwareInstallError`）、塑造狀態（例如 `fleetSize`、`noProvisioners`）、
  監聽請求（`on*Request` callback）與控制回應時機的 gate（例如 `deploymentRequestGate`）。
  - 個別測試需要不同的回應時，在 `installApiFixtures` 之後註冊 `page.route`（後註冊的優先），不處理的請求以
    `route.fallback()` 交回 fixture，例如 `dashboard/tests/e2e/server-tags.spec.ts`。新測試可以照著它的 `beforeEach` 寫。
  - 需要新的錯誤情境時，在 `FixtureOptions` 加選項，而不是在測試內重寫整個路由。例如 OS Image 上傳的 fixture
    目前只會回 `201`。
  - 第一次執行前安裝瀏覽器：`npx playwright install chromium`。
- **release build**：`npm run test:e2e:production` 以 production build 驗證開發中的功能被隱藏
  （`dashboard/playwright.production.config.ts`）。
- **visual review**：`npm run review:visual` 以 `dashboard/playwright.visual-review.config.ts` 拍下深色的桌面與手機畫面，存在
  `test-results/visual-review/`，不是 baseline，要逐張打開檢視；何時必做見 dashboard 的 `AGENTS.md`。
- **契約型別**：`npm run contract:generate` 由 OpenAPI 產生 `src/infrastructure/api/generated/openapi.ts`；
  `npm run contract:check` 在產生的檔案過期時失敗。

## 5. cli 的測試

- 測試以 httptest server 代替 api-server：`cli/internal/command/sshkeys_test.go` 的 `useTestServer`，以
  `cli/internal/command/supersede_test.go` 的 `runCommand` 執行指令（範例：`cli/internal/command/platform_claim_test.go`；
  `cli/internal/command/plans_test.go` 直接呼叫 `cmd.Execute()`，兩種都可以）。
- `useTestServer` 替換 process 全域的狀態，所以這些測試不能平行執行。

## 6. 完整檢查指令

迭代時只跑相關的部分；宣告完成前跑完該 component 的全部指令。各 component 的 `AGENTS.md` 與 coding style 提到的檢查都以
這一節為準。每一段都從 repository root 開始執行。

```bash
# api-server
cd api-server
gofmt -l .
go vet ./...
SWALLOW_TEST_MONGO_URI='mongodb://localhost:27017/?directConnection=true' go test -count=1 ./...
go build ./...
# 改了 OpenAPI fragment 時
UPDATE_OPENAPI=1 go test ./internal/shared/apicontract

# dashboard
cd dashboard
npm ci
npm run contract:generate   # 改了 OpenAPI 時
npm run contract:check
npm run lint
npm run build
npm run test:e2e
npm run test:e2e:production # 改了開發中功能的開關或 release build 時
npm run review:visual       # 改了畫面時，打開截圖檢視

# cli
cd cli
gofmt -l .
go vet ./...
go test ./...
go build ./...

# 文件（repository root）
python3 scripts/check-docs.py
```

`gofmt -l .` 沒有輸出才算通過。repository 沒有 CI，這些檢查都在送出前手動執行。
