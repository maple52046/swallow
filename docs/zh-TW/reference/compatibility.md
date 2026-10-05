# Compatibility 與限制

[English](../../../docs/en/reference/compatibility.md) · [文件首頁](../README.md)

## 專案成熟度

swallow 正在積極開發中。Repository 沒有 stable SemVer release，因此 compatibility
guarantee 只限於 documented active contracts，以及 contract 明確記錄的 one-release
migration note。

## Supported development 與 installation target

- Production installation 目標是一台 Ubuntu 24.04 amd64 VM：以 Docker Compose 執行
  swallow，並共置 MAAS 3.6 region+rack。它使用 immutable image digest、產生權限受限的
  secrets，並在 port 80 提供 plain HTTP；需要 TLS 時在前面終結。需要能連到 GHCR、Docker Hub
  與 `images.maas.io`。
- Installation 只同步官方 `ubuntu/noble` amd64 OS Image；第三方 image 與 MAAS image 的
  離線 media 尚未自動化。
- Development Compose 是 contributor path。
- Native Ubuntu packaging 是 incomplete preview，因為 native Temporal／PostgreSQL
  systemd packaging 尚未完成。沒有完整 orchestration topology 的 native API
  無法執行 Workflow，release 也不發佈 native bundle。

## External systems

目前 implemented integration／automation 目標：

- Ubuntu MAAS 3.6 provisioning。
- Prometheus-compatible metrics、Alertmanager 與 Grafana。
- 透過 shipped k0s automation 佈署的 Kubernetes。
- 透過 shipped automation 佈署的 Slurm。
- 提供 durable orchestration 的 Temporal。
- 保存 swallow-owned state 的 MongoDB 8。

Exact release media version／digest 屬於 candidate/release 與 third-party manifest，
不在本 overview 固定。

## Compatibility aliases

- `/api/v1/operations` 與 Dashboard `/operations` 是 Workflow deprecated aliases。
- `/api/v1/clusters` 與 Dashboard `/clusters` 是 Platform deprecated aliases。
- 新 client 使用 canonical route 與 terminology。

## 尚未實作

Planned API design 提到 Team、user administration、datacenter/room/rack physical
topology、access/SSH-key management、workload abstraction 與 GPU-specific
observability。它們不能被呈現成 active capability。

## Security 與 support

Repository 目前沒有發布 license、formal support policy 或 security-reporting
policy。不能從 source／installation asset 可取得推論出這些承諾。
