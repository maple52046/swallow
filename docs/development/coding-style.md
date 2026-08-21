# Dashboard TypeScript + React Coding Style

本文件定義 dashboard 撰寫與修改 TypeScript + React 程式碼時的 coding style rule，適用於所有開發人員與 AI agent。

本文以本專案實務為準，並整理自以下參考：

- [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)
- [Rules of React](https://react.dev/reference/rules)
- [Airbnb React/JSX Style Guide](https://github.com/airbnb/javascript/tree/master/react)

若本文件、工具設定與外部 style guide 有衝突，優先順序為：

1. 本文件。
2. Codebase 內既有 ESLint、TypeScript、Vite 設定。
3. 鄰近檔案既有風格。
4. Google TypeScript Style Guide。
5. Airbnb React/JSX Style Guide。

## 基準環境

- TypeScript 5.9；`target`/`lib` ES2022；`module: ESNext`；`moduleResolution: bundler`；由 Vite 7 打包。
- React 19，automatic JSX runtime（`jsx: react-jsx`）——不需要為了寫 JSX 而 import React。
- 元件庫 Radix Themes（`@radix-ui/themes`）搭配必要的 Radix Primitives（`@radix-ui/react-toast`、`@radix-ui/react-accordion`）與 `@radix-ui/react-icons`；Themes 未提供的元件（分頁、toast、可搜尋 select、accordion、app shell、nav link）集中在 `src/presentation/components/radix/`。路由 `react-router-dom` 7；日期 `dayjs`；圖表 `recharts`。
- `strict: true`，另加 `noUnusedLocals`、`noUnusedParameters`、`noFallthroughCasesInSwitch`、`noUncheckedSideEffectImports`。**不得為了讓程式碼編譯而放寬任何一項。**
- `verbatimModuleSyntax: true`：只作為型別使用的 import **必須**寫成 `import type { … }`，否則產出的模組是錯的。
- 路徑別名 `@/*` 對應 `src/*`。
- **本專案沒有 formatter。** 格式以既有檔案為準：兩格縮排、單引號、**語句不加分號**。這是專案慣例，優先於 Google guide 的分號與檔名規則。日後若導入 Prettier，必須以此設定為準，而不是反過來重排 codebase。
- 提交前必須執行：`npm run lint` 與 `npm run build`（`tsc -b` 型別檢查 + Vite build）。`react-hooks` 與 `react-refresh` 規則屬於必過門檻，不是建議。

## 核心原則

Dashboard 程式碼必須優先滿足以下目標，順序不可顛倒：

1. 清晰：讀者能理解 UI 在呈現什麼、資料從哪裡來，以及互動會造成什麼影響。
2. 可維護：component、hook、helper 的責任邊界明確，未來修改者能安全演進。
3. 型別安全：用 TypeScript 表達資料契約，避免讓 runtime 才暴露可由型別捕捉的錯誤。
4. 可存取性：互動元件與內容必須能被鍵盤、輔助科技與不同使用情境正確使用。
5. 一致：遵守本文件、工具設定與同目錄既有寫法。

若規則之間有衝突，優先選擇更清楚、更容易維護、較不容易誤用的寫法。

## 檔案與模組

- 所有 TypeScript/TSX 檔案使用 UTF-8。
- React component 檔案使用 `.tsx`；純 TypeScript helper、type、constant 使用 `.ts`。
- 檔案順序固定為：檔案層級 JSDoc（若需要）、imports、module 層 constants/types、implementation。
- **只使用 named exports**；不使用 default export（沒有 canonical 名稱、import 打錯也不會報錯）。
- 只 export 模組外真正會用到的符號，讓每個目錄的對外面積保持最小。
- 同一檔案原則上只放一個主要 React component；只服務該 component 的小型子元件可放在同檔。
- 為了 Fast Refresh，匯出 component 的模組不應同時匯出不相關的值（`eslint-plugin-react-refresh` 會擋）。helper 放到同名的 `camelCase.ts`，例如既有的 `ModelLabel.tsx` / `modelLabelUtils.ts`。
- 跨層或跨 feature 的 import 使用 `@/*` 別名；同目錄內才用相對 `./`。**不得**出現 `../../../`。
- Side-effect import 只可用於 CSS 或明確需要註冊 side effect 的模組，且非直覺者必須加註說明。
- 不使用 `namespace`、`import x = require(...)`、`/// <reference>`。
- 檔名依模組角色決定，而非單一全域規則：模組產物是一個 class/interface 時用 `PascalCase.ts`（`ServerRepository.ts`、`ListHostsUseCase.ts`）；工具與 client 模組用 `camelCase.ts`（`serverApi.ts`、`modelLabelUtils.ts`）；domain 型別模組用 `types.ts`；含 JSX 才用 `.tsx`。

## 命名

- Component、type、interface、enum 與 type parameter 使用 `UpperCamelCase`。
- 變數、函式、hook、method、property 與 props 使用 `lowerCamelCase`。
- Module 層常數可使用 `CONSTANT_CASE`；區域變數即使是 `const` 也使用 `lowerCamelCase`。
- React component 檔名應與 component 名稱一致，例如 `StatusBadge.tsx` 匯出 `StatusBadge`。
- Hook 必須以 `use` 開頭，且名稱描述其取得的狀態或封裝的行為，例如 `useServerList`。
- Event handler prop 命名 `on<Event>`，實作命名 `handle<Event>`。
- Boolean 讀起來像判斷式（`isLoading`、`canAssign`、`hasAlerts`），不用只有兩種狀態的 `flag`/`status`。
- Acronym 視為單字處理，例如 `serverId`、`apiClient`。例外：平台 glossary 與 API contract 已以大寫呈現的術語（`GPU`、`IPMI`、`SSH`、`BMC`）沿用原寫法，例如 `ListGPUDevicesUseCase`；同一模組內保持一致。
- 名稱描述內容與用途，不描述型別或來源：用 `servers` 而非 `serverList`；只有當 scope 內同時存在兩種形式時才加限定詞（`ageString` vs `age`）。
- 不使用 Hungarian notation、`opt_` 前綴或含糊縮寫。
- 不使用 `_` 作為 prefix/suffix。唯一例外：callback 簽名強制保留但用不到的參數，可用 `_` 前綴滿足 `noUnusedParameters`；能移除參數或調整簽名時優先那樣做。
- 命名必須符合平台 glossary 的 ubiquitous language。

## TypeScript 規範

- 讓 TypeScript 推斷明顯型別；當 generic 會推成 `unknown`（`new Set<string>()`）或標註能讓複雜表達式更好讀時才明確標註。Exported function 的回傳型別建議標註。
- Object shape 優先使用 `interface`；union、tuple、mapped type、utility type composition 使用 `type`（例如 `export type ServerStatus = 'live' | 'warning' | …`）。
- **禁止任意使用 `any`。** 若因第三方 API 或逐步遷移不得不用，必須用註解說明原因與安全邊界。
- 尚未驗證的外部資料使用 `unknown`，narrowing 後再使用。
- Optional field/parameter 使用 `?`，不要把 `| undefined` 放進共用 type alias；也不要把 `| null` 藏進廣泛共用的 alias，缺值語意應靠近產生或消費的位置表達。
- Array 型別：簡單型別使用 `T[]` 或 `readonly T[]`；複雜 union/object 使用 `Array<T>` 或 `ReadonlyArray<T>`。
- 優先使用 `readonly` 表達不可變資料，尤其是 props、設定、查詢結果與不應由 component 修改的集合。
- 優先使用 `Record<Keys, V>`、`Map`、`Set`，而不是寬鬆的 index signature；必要時給 key 有意義的名稱。
- 不使用 wrapper 型別（`String`、`Number`、`Boolean`、`Object`），也不使用 `{}`。
- 型別斷言（`as`）與非空斷言（`!`）只能用在已知安全但 TypeScript 無法推斷的情境，且必須註解說明為何成立；使用 `as` 語法，double assertion 走 `unknown`。物件字面值用型別標註（`const x: Foo = {…}`）而不是斷言，錯誤才會報在宣告處。
- **不得使用 `@ts-ignore`、`@ts-expect-error`、`@ts-nocheck`。** 編譯錯誤是要修型別，不是要消音。

## React 與 JSX

以下是正確性規則，不是偏好。違反它們產生的 bug 只在 Strict Mode、concurrent rendering 或 re-render 時才出現。

- **Component 與 custom hook 必須是純的。** 相同 props、state、context 下，render 結果相同且不改動自身作用域外的任何東西：render 期間不發網路請求、不寫 storage、不修改 props/state/context/module 層變數。
- **不得就地修改 state。** 建立新的物件或陣列；修改 React 已 render 的值不會觸發更新，還會破壞後續 diff。
- **Render 必須 idempotent。** 假設一次 commit 可能 render 兩次（Strict Mode 刻意如此），也可能 render 完就被丟棄。
- **Hook 必須在頂層無條件呼叫，順序固定。** 不放進條件、迴圈、callback、`try`/`catch`，也不放在 early return 之後。
- **只有 component 與其他 hook 能呼叫 hook。** 需要 hook 行為的普通函式，必須自己變成 `useSomething`。
- **不得直接呼叫 component 函式**（`MyComponent()`）；render `<MyComponent />` 讓 React 擁有呼叫時機。
- 優先撰寫 function component，不新增 class component。
- Props 名稱使用 `lowerCamelCase`；若 prop 接收 component，可使用 `UpperCamelCase`（`Icon={StatusIcon}`）。
- Boolean prop 為 `true` 時省略值（`<Dialog open />`）。
- JSX attribute 使用雙引號；一般 TypeScript 字串使用單引號。
- 多行 JSX 每個 prop 獨立成行，closing bracket 與內容縮排保持一致。
- 條件渲染必須讓 false/null/undefined 的顯示結果清楚。複雜條件先抽成具名變數，不在 JSX 裡堆疊難讀邏輯。
- List rendering 必須使用穩定 key（例如 entity id）；會重排、篩選、增刪的列表不得用 array index 當 key。
- Spread props 只用在 HOC、proxy component 或已知且範圍小的 props 物件；傳到 DOM 前必須過濾非 DOM attribute。
- `useMemo`、`useCallback` 只在能改善 referential stability、避免昂貴計算或符合 dependency contract 時使用；不可作為預設包裝。
- Dependency array 必須誠實。相依項不對時，修法是改程式碼（把值移進去、讓它穩定、或改成推導），不是刪陣列項目。抑制 `react-hooks` 規則必須加註說明為何安全。
- 能由 render、memo 或 event handler 完成的邏輯，不塞進 effect。`useEffect` 只描述與外部世界的同步。
- 優先使用受控的 Radix 元件搭配明確 state，而不是用 ref 讀取非受控 widget。
- 顏色、間距、字體來自 Radix Themes token（CSS 變數，如 `var(--accent-9)`、`var(--gray-11)`、`var(--space-2)`）與 `presentation/app/theme` 的 `<Theme>` 設定，不寫死 hex 值。

## 元件共用與組合（DRY）

Dashboard 遵守 platform architecture spec 的「共用實作 / Reuse-first」原則。UI 是最容易散落重複的地方，因此以下規則為強制要求（屬 coding-style completion gate 的一部分）：

- 一個共用功能的呈現 = 單一共用、可組合的 component。多個頁面需要同一功能時，必須組合（compose）同一個共用 component，不得各自複製一份卡片/區塊的組合。
- 頁面之間的差異透過 props / slot / render callback 注入，例如按鈕的 disabled 條件、handler、是否顯示某欄位、可選的 action。差異不是複製整塊 JSX 的理由。
- 為共用功能新增欄位或子區塊時，必須修改該共用 component，讓所有 consumer 一致獲得，不得只加在其中一頁。
- 抽出共用 component 時，放到能被所有 consumer 匯入的合理位置（`src/presentation/components/`），並用 JSDoc 說明它的使用情境、資料來源與各 consumer 的差異點。
- Loading、error、empty 狀態一律使用既有的 `LoadingState`、`ErrorState`、`EmptyState`，不另造一份。狀態徽章一律使用既有的 `StatusBadge`。
- 只在「行為/語意確實不同」時才分成不同 component；單純換文案、換 handler、換 disabled 條件都屬於同一 component 的參數化。
- 發現既有重複（同一功能有兩份組合）時，優先重構為共用 component 並讓呼叫點改用，而不是再貼一份。

## 可存取性

- 所有 `<img>` 必須有 `alt`。裝飾圖片使用空字串 `alt=""`。
- `alt` 不重複寫入 "image"、"photo"、"icon" 等輔助科技已會朗讀的字詞。
- 互動元素優先使用語意化元件或 HTML：Radix `Button`/`Link`/`TextField`，或 `<button>`、`<a>`、`<label>`。
- 若必須使用非語意元素模擬互動，必須補齊 `role`、keyboard interaction、focus state 與必要 ARIA attributes。
- 只使用有效且非抽象的 ARIA role。ARIA 不應用來掩蓋錯誤的 HTML 結構。
- 不使用 `accessKey`。
- 表單欄位必須有可辨識 label，錯誤訊息必須能被使用者與輔助科技理解。
- 狀態不得只靠顏色傳達；狀態徽章與圖表必須同時有文字或 aria 標示。

## 註解與 JSDoc

本專案要求「必要註解必須撰寫」。註解不是裝飾，而是 API 契約、UI 意圖與維護知識的一部分。

只重述 component、hook、helper、type 或 props 名稱的 JSDoc 不合格。這類 JSDoc 即使能滿足形式要求，也視為缺少文件。例如 `/** Renders the server table. */`、`/** Server props. */`、`/** Calls the API. */` 都不是有效文件。

合格 JSDoc 必須補足讀者無法只從名稱、props 型別與 JSX 推論出的資訊：使用情境、資料來源、權限與可見性假設、loading/error/empty state、side effect lifecycle、accessibility 意圖，或跨 API/glossary 邊界的 contract。

### 必須撰寫註解的情況

- 所有 top-level export 必須有 JSDoc，包括 component、hook、type、interface、constant 與 helper function。
- Page component、route guard、app shell、provider、custom hook、API adapter、storage adapter、DTO mapper、use case、port、permission helper 即使未 export，只要是重要 application boundary，也必須有 JSDoc。
- Port interface 必須說明實作者必須保證什麼、`null` 代表什麼、哪些錯誤屬於 domain miss。
- Reusable component 必須說明用途、主要情境，以及重要 props 的語意。
- Custom hook 必須說明它封裝的狀態來源、副作用、訂閱、快取或生命週期。
- Shared helper 必須說明輸入輸出契約、錯誤語意、單位、排序或 filter 條件。
- 非直覺 UI 行為、權限判斷、資料一致性假設、效能取捨、相容性考量、edge case 必須註解說明。
- `useEffect` 若包含訂閱、timer、network request、DOM API 或 cleanup，必須用註解說明副作用目的與 cleanup 條件。
- 刻意忽略錯誤、刻意省略 dependency、使用型別斷言、使用 `any`、使用 index key 或繞過 lint 時，必須註解原因。
- 跨模組 contract 或與後端 API / glossary 相關的資料語意，必須在 type 或轉換邏輯附近說明，包含「此前端型別為何刻意與後端 DTO 不同」。
- 涉及 JWT/session storage、admin 與 current-user 可見性、API DTO mapping、token 顯示、browser storage、network effect、route protection、form validation、accessibility workaround、theme/i18n 邊界時，必須註解安全假設與維護限制。

### JSDoc 最低審查標準

Top-level export 與重要 internal boundary 的 JSDoc 必須能回答與該 symbol 相關的問題。不是每個 symbol 都需要回答所有問題，但缺少 relevant answer 時視為文件不足。

- Usage context：此 component/hook/helper 在哪個 user flow 或 route 中使用。
- Data source and ownership：資料來自 props、use case、adapter、browser storage、route 或後端 API；資料屬於 current user、admin scope 或 public scope。
- Props and return semantics：重要 props、callback、hook return state 的語意與限制。
- UI state contract：loading、empty、error、success、permission-denied 狀態如何呈現。
- Side effect lifecycle：network request、subscription、timer、storage、navigation、focus management 的啟動與 cleanup 條件。
- Accessibility intent：非標準互動、ARIA、keyboard behavior、focus management 或狀態訊息的使用原因。
- Compatibility and security：哪些行為是 API contract、glossary term、權限規則或未來改版不能任意破壞的。

### Clean Architecture 分層註解標準

- Domain type：JSDoc 必須描述 dashboard 使用的 domain meaning、allowed state 與與 glossary/API term 的關係，不得只描述畫面欄位。
- Port：JSDoc 必須描述 use case 需要的能力、回傳型別的語意、實作者義務。
- Use case：JSDoc 必須描述 user intent、port dependency、狀態轉換、錯誤語意與權限假設。
- Adapter/DTO mapper：JSDoc 必須描述外部 API / browser storage / route shape 如何轉成內層 model，以及哪些欄位屬於 published contract。
- UI component/page：JSDoc 必須描述使用情境、資料可見性、重要 props、狀態呈現與 accessibility 意圖。
- Composition root / provider：JSDoc 必須描述哪些 adapter 被選用、為何選用，以及切換 mock 與真實 API 的影響範圍。

### 註解寫法

- 使用 `/** JSDoc */` 描述程式碼使用者需要知道的 API、component、hook、type 與 module contract。
- 使用 `//` 描述 implementation detail，例如某段分支為何存在或某個 workaround 的原因。
- 註解應說明 why、contract、assumption、edge case，不應重述程式碼已清楚表達的 what。
- 註解必須與程式碼同步更新；過期註解視為 bug。
- JSDoc 可以使用 Markdown；多個重點用 Markdown list，不用手動空白對齊。
- TypeScript 已表達的型別不要再寫在 `@param` 或 `@returns` 中；只有在補充限制、單位、範圍或副作用時才使用 tag。
- 多行 implementation comment 使用連續 `//`，不要使用 `/* … */` 區塊註解，也不要畫註解框。
- `TODO` 格式：`// TODO(<owner-or-issue>): <缺什麼，以及什麼條件成立後可以移除>`。
- 廢棄 API 標記 `@deprecated`，並寫出具體的遷移方式。

```tsx
/**
 * ServerStatusBadge renders a Server's operational status in inventory tables
 * and on the server detail page.
 *
 * `status` is the domain value from the platform glossary, not a display label:
 * `maintain` is rendered as "Maintenance" here, but the domain value must be
 * what flows through filters, API payloads, and persistence. Status is never
 * conveyed by colour alone — the badge always carries text.
 */
export function ServerStatusBadge({ status }: ServerStatusBadgeProps) {
  // Unknown is a real domain state (not-yet-probed), so it renders as a neutral
  // badge rather than an error — operators must be able to tell it from offline.
  return <Badge color={statusColor(status)}>{statusLabel(status)}</Badge>
}
```

避免無意義註解：

```tsx
/** Renders the inventory page. */
function InventoryPage() {
  return <ServerTable />
}

/** Calls the API. */
export async function loadServers(): Promise<readonly Server[]> {
  // ...
}
```

撰寫有維護價值的註解：

```tsx
/**
 * InventoryPage is the admin-only server inventory screen.
 *
 * Every server endpoint it uses requires the `admin` role, so a non-admin who
 * reaches this route must be stopped by RoleGuard rather than by an empty
 * table — an empty table would look like "no servers" instead of "no access".
 */
function InventoryPage() {
  return <ServerTable />
}

/**
 * listServers retrieves Servers through the ServerRepository port.
 *
 * The returned values are dashboard domain types, not backend DTOs. The adapter
 * owns response validation and maps authorization failures to an application
 * error that components can display without exposing backend internals.
 */
export async function listServers(): Promise<readonly Server[]> {
  // ...
}

// Keep the previous rows visible while refetching so frequent status polling
// does not make the table flicker between empty and populated.
const rows = pendingServers ?? previousServers
```

## 狀態、資料與副作用

- Component state 只保存 UI 真正需要記住的狀態；可由 props 或既有 state 計算的值在 render 時推導，不再存一份 `useState`。
- 遠端資料、URL state、form state、local UI state 應保持邊界清楚，不混在同一個大型 state object 中。
- Loading、error、empty state 必須明確建模；優先使用單一 discriminated union，而不是多個可互相矛盾的 boolean。
- 資料轉換放在具名 helper 或 hook 中；複雜的 map/filter/sort 不直接塞在 JSX 內。
- 每個啟動了什麼的 effect 都必須停止它：回傳 cleanup 以中止請求、清除 timer 或移除 listener。假設 effect 會「執行 → cleanup → 立刻再執行」（Strict Mode 就是這樣）。
- 防止過期回應：以 `ignore` flag 或 `AbortController` 追蹤最新請求，讓較早、較慢的回應無法覆寫較新的 state。
- 所有非同步工作以 promise 為基礎，優先 `async`/`await`；不留下 floating promise（`await` 它、回傳它，或明確 `void` 並加註）。
- 獨立請求用 `Promise.all` 併發；只有真的有相依時才依序 `await`。
- 對外部資料在進入 UI 前完成必要 parsing、validation 或 narrowing，不讓 component 內散落重複防禦邏輯。
- Polling 與 refresh interval 由擁有該畫面的 component 或 hook 管理，interval 必須註解說明並在 unmount 時清除。
- Module 層不放可變狀態（除刻意且有註解的 singleton/cache），import 時不做任何工作。

## 錯誤處理

- 只 throw `Error` 或其子類，且一律用 `new` 建構。呼叫端必須能區分的失敗，定義專屬 error class。
- 不以 `-1`、`''` 或 magic value 表示失敗。`null` 只在 port 明確宣告時代表「找不到」（例如 `getServer(id): Promise<Server | null>`）。
- 可失敗的 async helper 必須清楚回傳錯誤、丟出錯誤，或轉成明確 UI state；不得默默吞掉錯誤。
- catch 時假設值是 `Error`，讀 `.message` 前先 narrow；只有已知會違反慣例的 API 才防禦非 `Error`，並註明是哪一個。空 catch 必須有註解說明為何靜默。
- `try` 區塊只包會 throw 的呼叫。
- Transport 失敗在 infrastructure 層轉成 domain 有意義的錯誤：use case 不該收到 HTTP status code，presentation 也不該解析錯誤字串來決定畫面。
- 使用者可見錯誤訊息應描述可採取的下一步；診斷資訊放在 log 或 debug context，不直接暴露敏感資料。
- 不使用 `alert` 作為一般錯誤處理或使用者通知機制；使用共用的 toast（`useToast`，`src/presentation/components/radix/toast/`）。

## 測試規範

- 現況：本專案尚未接入 unit test runner（`playwright` 已安裝但無 script）。在補上之前，仍必須以「可測性」為設計前提：值得測的規則要放在 use case、domain 模組或可直接呼叫的 helper，而不是埋在 JSX 裡。
- 補上測試後：單元測試命名 `*.test.ts` / `*.test.tsx` 放在被測程式碼旁；end-to-end Playwright spec 放在 `tests/`。
- 測試應驗證使用者可觀察行為與資料契約，不只驗證 implementation detail。
- Component test 優先從 role、label、文字與互動結果查詢元素，不依賴 class name 或 component 內部結構。
- 測試名稱必須描述場景與期待結果。
- 建構完整的期待值一次比較，而不是逐欄位檢查；失敗訊息要能看出輸入、實際值與期待值。
- 錯誤語意驗證可觀察的型別或已文件化的屬性，不比較格式化後的訊息字串。
- Test fixture 應小而明確；避免共享可變 fixture 造成測試彼此影響。

## Agent 執行規則

AI agent 修改 dashboard 程式碼時必須遵守以下規則：

- 先閱讀鄰近檔案與本文件，再進行修改。
- 新增 top-level export、component、hook、type 或 helper 時，必須同步新增有維護價值的 JSDoc。
- `top-level export` 只是最低門檻；page、route guard、provider、app shell、adapter、storage、use case、port、permission/data visibility boundary 即使未 export，也必須撰寫有維護價值的 JSDoc。
- 修改 props、hook return value、資料轉換、錯誤語意、副作用或 accessibility 行為時，必須同步更新註解與測試。
- 若實作需要複雜條件、workaround、型別斷言、`any` 或 lint disable，必須先嘗試簡化；無法簡化時用註解說明必要原因。
- 形式 JSDoc、identifier summary comment、只描述 `what` 的 comment 視為不符合規範；必須補足 usage context、state contract、data visibility、side effect lifecycle、accessibility intent 或 why。
- 新增 session storage、API adapter、route guard、network effect、theme token mapping 或任何顯示敏感值的程式碼時，必須先確認註解是否達到本文件的最低審查標準。
- 不得用大量低價值註解填充；註解必須幫助未來讀者避免誤用或誤改。
- 新增或修改功能呈現前，先確認是否已有共用 component；若有，組合它並以 props 注入差異，不得複製一份。為共用功能加欄位/子區時必須改共用 component（見「元件共用與組合（DRY）」）。
- 不得引入與既有工具鏈不一致的抽象、命名、狀態管理或測試工具，除非修改本身就是為了統一風格。
- 完成修改前應執行 `npm run lint` 與 `npm run build`，或明確建議使用者執行。
