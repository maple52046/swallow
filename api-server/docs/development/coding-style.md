# Go Coding Style

本文件定義本專案撰寫與修改 Go 程式碼時的 coding style rule，適用於所有開發人員與 AI agent。

本文整理自 Google Go Style 文件，並以本專案實務為準則：

- [Go Style overview](https://google.github.io/styleguide/go/)
- [Go Style Guide](https://google.github.io/styleguide/go/guide)
- [Go Style Decisions](https://google.github.io/styleguide/go/decisions)
- [Go Style Best Practices](https://google.github.io/styleguide/go/best-practices)

若本文件、工具設定與外部 style guide 有衝突，優先順序為：本文件 → 既有 `gofmt`/`go vet` 與測試設定 → 鄰近 package 既有風格 → Google Go Style。

## 基準環境

- Go 1.25，module `github.com/maple52046/swallow`。
- 主要相依：Fiber v2（HTTP）、gRPC + protobuf（agent 通道）、MongoDB driver、Cobra（CLI）、golang-jwt。
- 提交前必須執行：`gofmt -l .`（必須無輸出）、`go vet ./...`、`go build ./...`、`go test ./...`。若環境有 `golangci-lint`，一併執行並修正回報，不得以 nolint 掩蓋。

## 核心原則

Go 程式碼必須優先滿足以下目標，順序不可顛倒：

1. 清晰：讀者能理解程式在做什麼，以及為什麼這樣做。
2. 簡單：使用足以完成需求的最小機制，避免炫技與不必要抽象。
3. 精簡：保留高訊號內容，移除重複命名、重複邏輯與雜訊。
4. 可維護：讓未來修改者能正確、安全地演進程式。
5. 一致：遵守 Go 社群慣例、Google Go Style，以及同 package 既有風格。

若規則之間有衝突，優先選擇更清楚、更容易維護的寫法。

## 格式與命名

- 所有 Go 檔案必須使用 `gofmt` 格式化；imports 應使用 `goimports` 或等效工具整理。
- 多字名稱使用 `MixedCaps` 或 `mixedCaps`，不得使用 snake_case。例外包含 `*_test.go` 中的測試、benchmark、example 函式名稱。
- Package name 必須簡短、全小寫、不可使用底線；避免 `util`、`common`、`helper`、`model` 等無明確領域意義的名稱。
- Exported identifier 使用大寫開頭，unexported identifier 使用小寫開頭。
- Initialism 與 acronym 必須保持一致大小寫，例如 `URL`、`nodeID`、`dbClient`、`ipmiHost`，不得寫成 `Url` 或 `Id`。Domain 中以大寫呈現的術語（`GPU`、`IPMI`、`SSH`、`BMC`）沿用 glossary 的寫法。
- Receiver name 應短且一致，通常為型別縮寫，例如 `func (s *Server)`；不得使用 `this`、`self`。
- Getter 不使用 `Get` 前綴，除非概念本身就是 get。可能阻塞或遠端呼叫時可使用 `Fetch`、`Load`、`Compute`。
- 變數名稱長度應與 scope 成正比。小 scope 可用 `i`、`r`、`w`、`ctx`；跨多行或多概念時必須使用更明確名稱。
- 避免在名稱中重複 package、receiver、型別或參數已表達的資訊。
- 命名必須符合 root glossary 的 ubiquitous language。若程式碼概念與 glossary 不一致，先處理 glossary，再改名稱。

## 註解與文件

本專案要求「必要註解必須撰寫」。註解不是裝飾，而是 API 契約與維護知識的一部分。

只重述 identifier 名稱、型別種類或程式碼表面動作的註解不合格。這類註解即使能讓 lint 通過，也視為缺少文件。例如 `// Service coordinates services.`、`// Config stores config.`、`// Save saves server.` 都不是有效註解。

合格註解必須至少補足讀者無法只從名稱與型別安全推論出的資訊：責任邊界、呼叫者義務、錯誤語意、資料一致性假設、生命週期、相容性限制，或為何採用某個取捨。

### 必須撰寫註解的情況

- 所有 exported package、type、interface、func、method、const、var 必須有 doc comment。
- Package comment 必須直接位於 `package` 宣告上方；若內容較長，可放在 `doc.go`。
- Unexported type、function、method、const、var 只要行為、用途或限制不直覺，也必須撰寫註解。
- 即使未 export，只要是 use case、repository interface、transaction boundary、Fiber handler、gRPC handler、config loader、Mongo adapter、JWT/token issuer、middleware、long-running worker，或跨 component/shared package boundary，也必須撰寫有維護價值的註解。
- 任何業務規則、權限判斷、資料一致性假設、效能取捨、相容性考量或特殊 edge case 必須註解說明。
- API 若涉及 context cancellation、concurrency safety、resource cleanup、error sentinel、特定 error type、goroutine lifetime、stream ownership 或 reconnect 行為，必須在 doc comment 中說明契約。
- 忽略 error、刻意使用空 identifier、非典型條件判斷、複雜型別轉換或容易被誤讀的程式碼，必須在旁註說明原因。
- 涉及 JWT / access token、password 或 credential storage、admin 授權判斷、Mongo 寫入與唯一性約束、config default/override 優先順序、resource cleanup、goroutine lifecycle、agent 身分解析、gRPC stream 生命週期、跨 component shared infrastructure 時，必須註解安全假設與維護限制。

### Doc Comment 最低審查標準

Exported API 與重要 internal boundary 的 doc comment 必須能回答與該 symbol 相關的問題。不是每個 symbol 都需要回答所有問題，但缺少 relevant answer 時視為文件不足。

- Responsibility boundary：此 type/function/interface 負責什麼，不負責什麼。
- Caller obligations：呼叫者必須提供什麼前置條件、權限、context、transaction 或 cleanup。
- Error semantics：會回傳哪些 domain error，哪些錯誤可重試，哪些錯誤代表權限不足或資料不存在。
- Data consistency：是否需要 transaction、是否寫入 durable storage、是否只允許 test fake 或 in-memory implementation。
- Resource lifecycle：是否持有 connection、goroutine、stream、timer、lock，何時釋放。
- Concurrency and context：是否可 concurrent use，是否尊重 context cancellation/deadline。
- Compatibility and security：哪些行為是 API contract、security boundary 或未來 migration 不能隨意破壞的。

### Clean Architecture 分層註解標準

- Entity/domain type：註解必須描述 domain meaning、invariant、allowed/disallowed state，不得只描述 Mongo 欄位或 JSON shape。
- Port interface：註解必須描述實作者必須保證什麼、哪些錯誤是 domain miss、哪些實作限制只適用於測試。
- Use case/application service：註解必須描述 application flow、port dependency、授權假設，以及回傳錯誤的語意。
- Interface adapter（`delivery` / `infra`）：註解必須描述外部 contract 如何映射到內層 model，包括 HTTP/gRPC status、DTO、Mongo document 或 error mapping 的邊界。
- Framework/driver（`internal/app`、`config`、`bootstrap`）：註解必須描述 config source、resource lifecycle、startup/shutdown、network、secret 或 external dependency 的 operational assumption。
- Shared internal package（`internal/shared/*`）：註解必須描述哪些 component 可以依賴它、哪些行為是 shared contract，以及不得放入 component-specific rule 的限制。

### 註解寫法

- Doc comment 必須是完整句子，通常以被描述的 identifier 開頭。
- 註解應說明 why、contract、assumption、edge case，不應重述程式碼已清楚表達的 what。
- 註解必須與程式碼同步更新；過期註解視為 bug。
- 註解行沒有硬性長度限制，但應換行到容易閱讀的寬度，並保持同檔一致。
- Godoc 內容使用空行分段；範例程式碼優先放在 `Example...` 測試中，必要時才放在註解內。
- 對 struct field 的短旁註可使用片語，但描述 public API 的 doc comment 必須完整。

```go
// ServerRepository defines the persistence contract for Server entities.
//
// Implementations must be safe for concurrent use and must not create nodes
// implicitly: inventory and agent updates must validate that the node already
// exists before writing. Callers should treat ErrServerNotFound as a domain
// miss, not as an infrastructure failure. Production implementations must use
// durable storage; in-memory implementations are limited to tests.
type ServerRepository interface {
	Create(ctx context.Context, server *Server) error
	FindByID(ctx context.Context, id string) (*Server, error)

	// UpdateInventory replaces only the inventory sub-document for the given node.
	// Returns ErrServerNotFound if the node does not exist.
	UpdateInventory(ctx context.Context, id string, inv Inventory) error
}
```

不合格註解：

```go
// Service coordinates server services.
type Service struct {
	repo ServerRepository
}

// Create creates the server.
func Create(ctx context.Context, s Server) error {
	// ...
}
```

撰寫有維護價值的註解：

```go
// Service coordinates server use cases without depending on HTTP, gRPC, or
// MongoDB.
//
// Service owns registration policy and application error semantics. Persistence
// and clock access are supplied through ports so use cases can be tested
// without starting external systems.
type Service struct {
	repo ServerRepository
}

// Environment variables have the highest priority so deployment systems can
// override mounted config files without rewriting them.
```

## 錯誤處理

- 可失敗的函式應回傳 `error`，且 `error` 必須是最後一個回傳值。
- Exported 函式回傳 `error` interface，不回傳具體 error 型別；被包進 interface 的 typed nil pointer 不是 `nil`。
- 呼叫端遇到 error 必須明確處理、包裝後回傳，或在極少數情況下終止流程；不得默默丟棄。
- 若確定某個 error 永遠不會發生，忽略時必須附註原因。
- Error string 不以大寫開頭，不以句點結尾，除非開頭是專有名詞或 exported name。
- 包裝 error 時應加入有助定位的脈絡；需要保留原始 error 語意時使用 `%w`，讓呼叫端能用 `errors.Is`/`errors.As` 判斷。
- Sentinel error 宣告在擁有該語意的層（domain）；driver 專屬錯誤不得越層洩漏。
- 不使用 magic value 表示錯誤；應回傳 `(value, ok)` 或 `(value, error)`。
- 先處理 error 與 terminal condition，再讓正常流程維持在主路徑，避免不必要的 `else` 巢狀。
- 不使用 `panic` 處理一般錯誤；`MustXxx` 只可用於程式啟動期或測試 helper。

```go
server, err := repo.FindByID(ctx, id)
if err != nil {
	return nil, fmt.Errorf("find server %q: %w", id, err)
}

return server, nil
```

## API 與語言使用

- 優先使用 Go 核心語言機制與 standard library；只有在需求明確時才新增 dependency 或 abstraction。
- 不為測試或未來想像建立過早 interface。Interface 必須有清楚契約；本 project 的 port 由內層擁有（見 [`architecture-spec.md`](architecture-spec.md)）。
- 使用 generics 前必須確認能降低重複或提升型別安全；不可只為抽象而抽象。
- 建立外部 package 型別的 struct literal 時必須使用 field names，避免耦合欄位順序。
- 回傳空集合時優先使用 nil slice；判斷空集合用 `len(s) == 0`，不用 `== nil`。
- 使用 `any` 而非 `interface{}`。
- Receiver 型別依 method set 決定：會變動狀態、含不可複製欄位、含指向可變物件的指標，或 struct 較大時使用 pointer；同一型別的 method 全部 pointer 或全部 value。
- 啟動 goroutine 的程式碼必須清楚說明其生命週期、停止條件與錯誤處理方式。
- `context.Context` 應作為第一個參數傳入，命名為 `ctx`；不得存入 struct 作為長期狀態，也不得自訂 context 型別。`context.Background()` 只出現在 entry point。
- 重連與重試迴圈（例如 agent 的 stream loop）必須使用有上限的 exponential backoff，並區分 fatal 與 transient error。
- 產生 token、secret 或 ID 必須使用 `crypto/rand`，不得使用 `math/rand`。

## Imports 與 Package 組織

- Imports 應分組為 standard library、第三方、本專案套件、generated protobuf、side-effect import，並交由 `goimports` 排序。
- 避免 import rename；只有在避免衝突、generated package、慣例縮寫或改善可讀性時才使用（例如 `agentv1`）。
- 禁止 dot import。Blank import 必須加註用途，且只允許出現在 `main` package 或測試。
- Package 應聚焦單一領域；若名稱變得含糊，通常代表邊界需要重新整理。
- 不得為了共用方便建立 `common`、`util`、`model` 等模糊 package；跨 feature 共用物放 `internal/shared/<concern>`，並在 package comment 說明可依賴它的範圍。

## 測試規範

- 測試必須驗證行為，不應只複製實作細節。
- 多案例測試優先使用 table-driven tests；案例名稱應描述行為差異，不使用 index 標示 row。
- 使用 subtests 時，名稱必須能定位失敗情境，且不得依賴其他案例的執行結果。
- 測試錯誤訊息使用 `got` before `want`，並包含函式名稱與輸入等可行動資訊。
- 錯誤語意用 `errors.Is`/`errors.As` 驗證，不得比較錯誤訊息字串。
- Test helper 必須呼叫 `t.Helper()`。
- 需要比較複雜結構時，應使用穩定、可讀的 diff；避免依賴不穩定順序。
- 優先使用 `t.Error` 讓一次執行回報所有失敗；只有在繼續執行無意義時才用 `t.Fatal`。
- 測試資料與 setup 應盡量限制在需要的 test scope 內，避免跨測試共享可變狀態。
- 標準 `testing` package 是唯一允許的框架；不得引入 assertion library。

```go
for _, tc := range tests {
	t.Run(tc.name, func(t *testing.T) {
		got, err := svc.Create(ctx, tc.input)
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("Create(%v) error = %v, want %v", tc.input, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("Create(%v) = %v, want %v", tc.input, got, tc.want)
		}
	})
}
```

## Agent 執行規則

AI agent 修改 Go 程式碼時必須遵守以下規則：

- 先閱讀鄰近 package 的既有風格，再進行修改。
- 新增 exported API 時必須同步新增 doc comment，不得留下 lint 或 reviewer 才補。
- 修改 API contract、error semantics、concurrency behavior、resource lifecycle 或 context 行為時，必須同步更新註解與測試。
- 若程式碼需要複雜寫法，必須優先嘗試簡化；無法簡化時，用註解說明必要原因。
- 形式註解、identifier summary comment、只描述 `what` 的 comment 視為不符合規範；必須補足 contract、assumption、boundary 或 why。
- 新增 use case、adapter、infrastructure、shared internal package，或 config/Mongo/auth/token/security 相關程式碼時，必須先確認註解是否達到本文件的最低審查標準。
- 不得用大量低價值註解填充；註解必須幫助未來讀者避免誤用或誤改。
- 不得引入與本 package 風格不一致的抽象、命名或測試工具，除非修改本身就是為了統一風格。
- 完成修改前應執行 `gofmt -l .`、`go vet ./...`、`go build ./...` 與 `go test ./...`，或明確建議使用者執行。
