# Release 與 promotion

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

Pull request 會執行 Go tests/vet、Dashboard lint/build、documentation／Compose／
contract validation、production image build 與 Trivy scan。Merge 到 `main`
會 build/sign 一組 candidate；branch name 不選擇 environment。

Candidate workflow 的 repository variables 必須提供 MongoDB、Temporal
PostgreSQL、Temporal Server 與 Temporal UI 的 approved exact digests。Workflow
會把它們與 API、Dashboard digests 一起記錄，將六個 images 放進 air-gap archive，
並用同一組 digests 啟動 ephemeral testing stack。

`vX.Y.Z` tag 只有在相同 commit 已有成功 Candidate workflow 時才接受。
Promotion 對已驗證 digest 加 tag，不重新 build。

Release artifacts：

- `swallow-oci-<version>.tar`。
- Native／Compose `.tar.zst` bundles。
- Release／third-party compatibility manifests。
- API／Dashboard SPDX SBOM。
- SHA-256 checksums 與 Sigstore checksum bundle。

Shared testing 使用 candidate `release-manifest.json`；installation 使用 promoted
manifest 與 SemVer artifacts。目前 repository 尚未發布 stable SemVer release。
