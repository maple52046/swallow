# Native runtime media

[English](README.md) · [Third-party overview](../README.zh-TW.md)

此 media 提供 native swallow path 需要、但不屬於 MongoDB 的完整 Ubuntu 24.04
amd64 package closure，例如 Nginx、`python3-venv` 與 dependencies。

Packages 放在 `packages/`，version 固定於 release media inventory，並對每個
delivered file 產生 `SHA256SUMS`。

Native installer 會驗證此 media 與 separate MongoDB media，再執行
`apt-get --no-download`。Public apt access 被阻擋時也必須成功。

此 media 不會補齊 native Temporal／PostgreSQL service packaging；該 path 仍是 preview。
