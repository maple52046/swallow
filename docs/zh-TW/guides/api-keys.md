# API key

[English](../../../docs/en/guides/api-keys.md) · [文件首頁](../README.md)

API key 讓 CLI、script 或 CI 不必保存密碼，就能以你的身分呼叫 swallow。
它會使用你目前的角色，直到到期或被刪除為止。每個 client 應使用一把獨立且有名稱的
key，這樣撤銷其中一把時不會中斷其他 client。

API key 用於向 swallow API 驗證身分；它不是登入 Server 使用的 SSH access key，
也不是 installation 的 machine token。

## 在 Dashboard 建立 key

1. 使用密碼登入；既有 API key 不能建立另一把 key。
2. 開啟 **Account menu → API keys**。
3. 選擇 **Create API key**，輸入不重複的名稱，再選擇 30 天、90 天、一年或永不到期。
4. 將 `swk_…` secret 複製到 password manager 或 CI secret store，再確認已妥善保存。

Secret 只在建立對話框顯示一次。swallow 只保存單向 hash，之後無法還原 secret。
若 secret 遺失，請建立替代 key，再刪除舊 key。

列表會顯示每把 key 的識別 prefix、建立與到期日期，以及最後使用時間。Prefix
不能用來驗證身分。已到期的 key 仍會留在列表中，直到你將它刪除。

## 在 CLI 使用 key

將 key 存入 CLI profile：

```bash
swallow --endpoint https://swallow.example login --api-key-stdin < ./ci.key
swallow auth me
```

CI 或單次執行可改用 environment：

```bash
SWALLOW_ENDPOINT=https://swallow.example \
  SWALLOW_API_KEY="$CI_SWALLOW_API_KEY" \
  swallow servers list
```

Environment variable 會覆寫 profile 中保存的值。請勿把 secret 直接寫在 command line
或 repository file，以免從 shell history、process listing 或 version control 洩漏。

也可從以密碼登入的 CLI session 建立與管理 key：

```bash
swallow api-keys list
swallow api-keys create --name ci --expires-in 90d --secret-out ./ci.key
swallow api-keys delete <keyId>
```

`--secret-out` 會建立 mode-0600 的新檔案，且絕不覆寫既有檔案；未指定時，新 secret
只會印出一次。

## 到期與撤銷

Key 到達設定的到期時間後會自動停止驗證。刪除 key 會立即撤銷，例如 runner
退役或 secret 可能外洩時。刪除 key 不會結束密碼 session，也不影響其他 key。

Key 使用 owner 目前的角色，不帶獨立 scope；owner 被移除後，其 key 也無法再驗證。

## 疑難排解

- **建立操作被拒絕**：離開使用 API key 的 profile，改用密碼登入；建立 key 需要 Session。
- **呼叫回傳 `401 unauthorized`**：key 可能已到期、被刪除、輸入錯誤，或 owner 已被移除。
- **Secret 已看不到**：它無法還原，請建立新 key，再刪除舊紀錄。
- **難以辨識某把 key**：請比對名稱與畫面顯示的 prefix，絕不要把完整 secret
  貼進 ticket 或 log。

精確限制與 authentication 行為定義於
[API Keys contract](../../../api-server/docs/development/api-contracts/api-server/api-keys.md)。
