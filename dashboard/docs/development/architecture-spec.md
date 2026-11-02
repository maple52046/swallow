# Dashboard Architecture Spec

本文件是 `dashboard` component 的 Clean Architecture constitution。Root
architecture spec、shared glossary 與 api-server published contract 同時適用。

## Component Boundary

`dashboard` 是 `api-server` HTTP API 的 conformist consumer。它負責 user
interaction、presentation state、browser lifecycle 與 accessibility，不擁有 API
contract、provider fact 或 backend policy。

Production composition 只綁定 real HTTP adapters。Playwright API fixtures 是
deterministic test drivers，不是 runtime implementation 或 contract source。

## Dependency Rule

```mermaid
flowchart TB
    framework[React Router Chakra Browser]
    presentation[Presentation Adapters]
    application[Application Use Cases and Ports]
    domain[Domain Model]

    framework --> presentation
    presentation --> application
    application --> domain
```

- `src/domain/` 不得 import React、router、Chakra、HTTP、storage 或 browser API。
- `src/application/` 可 import domain，擁有 ports/use cases，不得 import
  infrastructure、presentation 或 React。
- `src/infrastructure/` 實作 HTTP/browser-storage adapters，將 transport DTO
  映射到 domain/application types。
- `src/presentation/` 組合 pages、components、hooks、contexts 與 accessible UI，
  不得 import `@/infrastructure/**`。
- `src/di/` 是唯一 composition root，可看到 ports 與 implementations。
- `src/router.tsx`／`src/main.tsx` 是 framework entry points。

## Directory responsibilities

```text
src/
├── domain/<area>/             concepts, enums, invariants
├── application/
│   ├── ports/                 inner-owned data/effect interfaces
│   └── usecases/              workflows with real application logic
├── infrastructure/
│   ├── api/                   HTTP clients, DTOs, mappers, SSE
│   └── persistence/           browser preferences/session adapters
├── presentation/
│   ├── app/                   shell, navigation, theme
│   ├── pages/                 routed console workspaces
│   ├── components/            reusable accessible UI
│   ├── contexts/              cross-page presentation state
│   └── hooks/                 React lifecycle adapters
├── di/                        container and provider
├── shared/                    framework-free generic helpers
├── router.tsx
└── main.tsx
```

`shared/` 不得成為模糊 business-logic dumping ground，也不得 import 任何 layer。

## Data Crossing Boundaries

- API DTO 只存在 infrastructure；map 後才進入 application/domain/presentation。回應的形狀與 domain 型別相同時，adapter
  可以直接回傳它，由 `src/infrastructure/api/contractCompatibility.ts` 在 compile time 檢查可以指派（見 API and Error Boundary）。
- Domain/application 不接受 `Response`、`URLSearchParams`、route object、
  DOM event、storage record 或 Chakra props。
- Route/query/form input 在 presentation adapter 轉成 typed application input。
- View-specific formatting 留在 presenter/component，不污染 domain value。
- Opaque IDs 保持 identity；hostname/address 只作 display。

## Ports and Use Cases

Port 描述 UI 真正需要的 capability，使用 domain language，回傳 inner types。
Adapter 以 driver 命名並集中 transport/auth/error mapping。

不需要額外邏輯的單純 CRUD 可由 container 暴露 repository port；多來源協調、
eligibility、working-set refresh 或 presentation-independent rule 才建立 use case。
不得建立只 passthrough 的 use case layer。

## React and Presentation

- Page 組合 hooks、ports/use cases 與 reusable presentation components。
- Hook 處理 React lifecycle/network cancellation，不承載可獨立測試的 business rule。
- `useEffect` 只同步 external world，不用來建立 domain invariant。
- Loading、empty、error、success 使用互斥 state model，不以矛盾 booleans 表示。
- URL owns shareable filters/scope；browser storage 只保存 UI preference/session。
- Status、error、staleness 與 destructive consequence 必須文字化，不只靠色彩。

## Session and credentials

- Access token 只存在 `src/infrastructure/api/session.ts` 的記憶體中，不寫入 `localStorage`；refresh token
  是網頁讀不到的 HttpOnly cookie（[decision 042](../../../docs/decisions/042-authentication-sessions-and-api-keys.md)（authentication sessions and api keys））。Presentation 永遠拿不到任何 token，只透過
  `AuthRepository` 取得 domain User。
- `apiRequest`／`apiRequestText`／`apiUpload` 在 token 即將到期時先換發，收到 `401` 時換發一次並重送；
  換發本身是同分頁 single-flight、跨分頁以 Web Locks 排隊。換發失敗時 adapter 發出 session-ended
  訊號，由 `AuthContext` 清除使用者並交給 `ProtectedRoute` 帶著原位置導回 login。
- Event stream（SSE）以 access token query parameter 連線；瀏覽器放棄連線時由 adapter 換發後以
  backoff 重連。API key 只供 CLI／script 使用，dashboard 不會以 API key 驗證。

## API and Error Boundary

- Adapter 使用 Active provider contract，不從 backend code 猜測 behavior。
- `src/infrastructure/api/generated/openapi.ts` 由 provider 的 OpenAPI（`api-server/docs/development/api-contracts/openapi/openapi.yaml`）
  以 `npm run contract:generate` 產生，不得手改；`npm run contract:check` 確認它是最新的。
- `src/infrastructure/api/contractCompatibility.ts` 以 compile-time 檢查確認每個 adapter 讀取的 contract response
  型別能指派給 adapter 回傳的 dashboard 型別。檢查失敗代表契約與 dashboard 型別不一致：修正 dashboard 型別，
  或回報 provider contract，不得放寬檢查。
- Adapter 可直接以 `src/infrastructure/api/contractTypes.ts` 的 `JsonResponse`／`JsonRequestBody` 從產生的型別
  取出 route 的回應與 request body，不另寫 DTO interface；這類 adapter 不需要 `contractCompatibility.ts` 的檢查。
  新 adapter 用這種方式；既有 adapter 逐步改寫。
- Shared error envelope 轉成 typed client error；UI 顯示 human message 與 request ID。adapter 的錯誤實作
  `src/application/ports/RequestFailure.ts`；presentation 以 `src/presentation/components/failure.ts` 的 `failureOf`
  讀出訊息與 request ID，在錯誤容器內以 `FailureMessage`（或 `ErrorState` 的 `requestId`）呈現；只收純文字的地方
  （toast 的 description、嵌進句子的字串）用 `failureText`。來自 API payload 的錯誤欄位（例如 Task 的 `error`）
  沒有 request ID，不經過這些 helper。
- `401` 先嘗試換發一次，失敗才結束 session；role visibility 不取代 server authorization。
- SSE ownership、reconnect/backoff、abort/cancellation 與 cleanup 必須集中且有註解。
- Unknown enum 使用 least-privileged/safe presentation，不 fail open：影響可執行動作或權限的值，未知時以最保守的方式呈現；
  只供顯示的值（例如 Server Origin），契約允許時原樣顯示。

## Desired and observed state

依 root 的 [`desired-state-model.md`](../../../docs/development/desired-state-model.md) §11，資源遷移到
desired-state 模型後：

- 詳情畫面分開呈現 desired（spec）與 observed（status），並標出兩者的差異與 observed 的觀察時間；
  observed 變舊不得呈現成失敗。
- Conditions 以文字呈現 type、status 與 reason（例如「尚未實現：provider 不支援」），不只靠色彩。
- Drift 顯示偏離的欄位，並提供 adopt（把 spec 改成目前的 observed 值）與 reapply（重新實現 desired）兩個動作；
  reapply 屬於破壞性時，走 Change Plan 而不是直接執行。
- 待核准的 Change Plan 有核准畫面：列出每個 target 的動作、標示破壞性動作與預期影響（例如哪些 Server
  會離開服務），並清楚說明核准後不可撤回；`superseded` 或 `expired` 的 plan 不可核准。
- 寫入 spec 時帶 API 契約定義的預期 `generation`；收到 conflict 時重新讀取並告知 user，不默默覆蓋。

這些規則是 presentation 與 application 的規則；判斷是否破壞性、是否 drift 一律以 API 的回應為準，
dashboard 不自行推導。

## Module UI

依 root 的 [`core-and-extensions.md`](../../../docs/development/core-and-extensions.md) §7：

- 第一方擴充模組（Kubernetes、Slurm、AMD、Managed Software）可以有專屬畫面，由共用的 Platform／Software
  外殼依模組 id 選擇；外殼本身不依平台或 GPU 廠商名稱分支。
- 沒有專屬畫面的模組，依 API 提供的 spec schema 產生通用的表單與詳情。
- 使用者概念以 port 呈現（例如「provisioner」），不以 adapter 名稱呈現（例如「MAAS」）；adapter 專屬的設定
  畫面（例如 MAAS Integration 的連線設定）例外。

註冊方式（2026-11-02 定案）：

- 模組 UI 放在 `src/modules/<種類>/<id>/`（對應 api-server 的 `modules/<種類>/<id>/`），每個模組匯出一份
  manifest，型別在 `src/modules/types.ts`：Platform 模組提供 Overview 本體、分頁與部署表單，Accelerator 模組提供
  它帶給某個 Platform 類型的區塊與部署功能。
- 部署 wizard 是外殼：Site、名稱、候選 Server、OS 準備、時鐘檢查與送出屬於外殼；Platform 模組的
  `deploy.useForm` 提供類型專屬的欄位、角色欄、Features、額外步驟、review 卡片與請求欄位；其他模組經
  `deployFeatures` 把功能（例如 AMD GPU Operator）加進某個類型的 Features 步驟。這兩種 hook 依類型固定，
  外殼每次 render 以相同順序呼叫。
- `src/modules/registry.ts` 靜態註冊所有 manifest，是 `src/modules/` 以外唯一可以 import 的模組檔案；外殼
  （例如 Platform 詳情）依模組 id 查 registry，不 import 模組，也不依類型名稱分支。
- lint 規則：`src/presentation/` 只能經 registry 使用模組；domain、application、infrastructure 不使用模組 UI；
  模組不 import 其他模組（要用別的模組的貢獻就經 registry），也和 presentation 一樣不 import infrastructure。
- 模組可以使用核心的 presentation 元件、hook 與 port；模組專屬的 API adapter 放在模組資料夾，由 `src/di` 綁定。

## Experimental features

開發中的 dashboard 功能（目前為 `monitoring`、`osImageUpload`、`deploymentTemplates`、`virtualMachines`）只在
development build（Vite dev server，含 Playwright）顯示；release build（`vite build`）一律隱藏。
這只影響 presentation，不改 API、CLI 或後端行為。

- Feature 清單與 `ExperimentalFeatureSettings` port 在 `src/application/ports/`。
- `src/di/container.ts` 是唯一讀取 build mode（`import.meta.env.DEV`）的地方：development 綁定
  localStorage 實作（`swallow.dev.experimentalFeatures`，預設全開、只存關閉的項目），release 綁定
  永遠關閉且不可調整的實作。Presentation 不得自行判斷 `import.meta.env`。
- Presentation 以 `useExperimentalFeature(feature)` 讀取。獨立入口（navigation、tab、action、
  route）在關閉時移除；route 一律經 `FeatureRoute` 保護（Not Found 或保留 query string 的 redirect），
  避免書籤或手打 URL 掛載未完成畫面並發出其 API request。
- 共用頁面上的值（例如 health）在關閉時保留位置，統一使用 `NOT_AVAILABLE_IN_RELEASE` 文案，
  不顯示成空值、unknown 或 failure；依賴該功能的 URL filter 在 parse 時忽略、在 canonicalize 時移除。
- Development build 在帳號選單提供「Experimental features」對話框，逐項切換並即時重繪。
- 新增實驗功能時：擴充 `ExperimentalFeature` 與對話框文案，gate 所有入口，並在
  `tests/e2e/experimental-features.spec.ts` 與 `tests/production/` 補驗證。功能完成後移除 gate 與清單項目。

## Shared UI and DRY

同一功能只有一個 canonical composed component。Server summary、status badges、
dialogs、release options、pagination、loading/error/empty states 等須 reuse/extend，
不得在 page 間 copy-paste。差異以 typed props 或 composition 表達。

## Accessibility

- 使用 semantic control、label、heading hierarchy 與 keyboard interaction。
- Modal focus、escape/close、async busy state 與 error announcement 必須可用。
- Status 不只以 color 表達；icon 也需要 accessible name 或 decorative treatment。
- Table 在 narrow viewport 需提供等價 responsive view。
- Destructive action 必須清楚描述 target、consequence 與不可逆性。

## Testing

`npm run test:e2e` 使用 Playwright（Vite dev server）；`npm run test:e2e:production` 以
`vite build` + `vite preview` 驗證 release build 隱藏實驗功能：

- deterministic API fixtures 驗證 user journeys；
- active route、auth、action、failure/recovery behavior；
- 以獨立 visual-review workflow 產生 disposable desktop/mobile screenshots，由
  agent 或 developer 實際開啟檢查，不進行 pixel-baseline comparison；
- accessibility role 與 visible user outcome。

Domain/use-case logic 應保持可在不 render React、不啟動 browser、不呼叫 live API
的情況下測試。新增 unit runner 前，仍須以純 functions／ports 維持此能力。

## Prohibited Designs

- Domain/application import React、router、Chakra、browser 或 infrastructure。
- Presentation import concrete infrastructure implementation。
- Component/hook 直接依賴 transport DTO 或 parse backend error string。
- Mock/test fixture 進入 production container。
- UI 發明 Planned endpoint、field、enum 或 authorization behavior。
- 將 `null` unknown 呈現成 failure，或製造 combined Server status。
- 以 duplicated composed UI 取代 shared component。
- 在 dashboard 自行推導 drift、破壞性分類或 Change Plan 內容，而不是讀取 API。
- 在新的 UI 中以 adapter 或廠商名稱作為使用者概念，或在共用外殼中依平台、GPU 廠商名稱寫死分支
  （現有畫面於所屬功能遷移時調整）。

## Completion Gate

任何 source change 必須：

1. 確認所用的 Active provider contract；引入或改變 domain term 時確認 glossary。
2. 保持 layer direction、real-data behavior 與 accessibility。
3. 通過 root `docs/development/development-and-testing.md` §6 的 dashboard 檢查；迭代時可只跑相關的 Playwright
   tests，宣告完成前跑完整的 `npm run test:e2e`。
4. User-visible UI 變更使用獨立 visual-review workflow 截取受影響 route 的 dark
   desktop/mobile；theme、color 或 shared-style 變更加驗 light。Agent 必須實際
   開啟 screenshots，修正發現的 layout、responsive 或 contrast 問題後重新檢查。
5. 手動檢查新增或修改的宣告與 UI 的 JSDoc、cleanup、auth/storage、error/loading/empty
   state、responsive behavior 與 shared-component reuse；同一檔案中沒有改到的既有程式不在範圍內。
6. 同步更新受影響的 tests 與 architecture；user-visible 的變更（定義見 root `AGENTS.md` 的 Documentation
   Audiences）同時更新雙語 public docs。
