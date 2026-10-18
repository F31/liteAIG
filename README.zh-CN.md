# LiteAIG

**企业级 AI Governance 与 Agentic Traffic Control Plane**

[English](README.md) | 简体中文

LiteAIG 是一个模块化单体、单二进制优先的 AI 网关。它把模型调用、工具调用（MCP）与 Agent 协作（A2A）统一收敛到一条**固定七阶段治理管线**，使身份、授权、成本、数据边界、故障与审计语义对所有流量一致生效。它以一个自包含 Go 二进制交付，内嵌 Console，并可从单机 SQLite（Lite）平滑演进到 PostgreSQL + Redis 多副本（Standard），而数据面契约不变。

---

## 目录

- [为什么选择 LiteAIG](#为什么选择-liteaig)
- [产品功能特性](#产品功能特性)
- [技术架构](#技术架构)
- [与 LiteLLM、Higress、Kong AI Gateway 的对比](#与-litellmhigresskong-ai-gateway-的对比)
- [快速开始](#快速开始)
- [安全模型](#安全模型)
- [项目结构](#项目结构)
- [测试与质量门禁](#测试与质量门禁)
- [文档](#文档)
- [范围与限制](#范围与限制)
- [贡献](#贡献)

## 为什么选择 LiteAIG

大多数 AI 网关首先是一个优秀的代理。LiteAIG 首先是一个**治理控制平面**：

- **治理是结构性的，而不是插件副作用。** 所有流量统一归一化为 canonical interaction，并经过同一条管线。新能力只能挂接为检查点，不能绕过准入、记账或审计。
- **Agentic 流量是一等公民。** MCP 工具/任务与 A2A Agent 和模型调用共享同一套密钥、预算、策略、审批与证据链。
- **成本是正确性的一部分。** 预算预留、RPM/TPM、Token 计量、缓存节省与 Provider 成本归因都内建在请求路径中。
- **运维足够简单。** 一个静态二进制、内嵌 Console、SQLite 或 PostgreSQL、可选 Redis 协调；无 Python 运行时、无独立 UI 服务、不强制依赖 Kubernetes。
- **配置交付安全可回滚。** 租户配置编译为不可变运行时快照，签名为 RuntimeBundle，原子激活，并由 last-known-good 兜底。

### 一眼看懂

| | Lite | Standard |
|---|---|---|
| 存储 | SQLite，单写者 | PostgreSQL |
| 协调 | 进程内 | PostgreSQL 租约 + Redis 账本/租约 |
| 拓扑 | 单个 `mode=all` 进程 | `gateway` / `control` 分离副本 |
| 适用 | 边缘、实验室、小团队、单租户 | 生产高可用、多租户 |
| 二进制 | 一个静态 Go 二进制，内嵌 Console | 同一二进制 |
| 参考构建 | 约 35 MB stripped 二进制；空闲 RSS 约 25 MB | 通过外部存储水平扩展 |

> 资源数字来自本地参考构建，不构成容量承诺。请按自己的拓扑与模型组合压测。

---

## 产品功能特性

### 1. 统一 AI 接入

- Provider：OpenAI、Anthropic、Azure OpenAI、AWS Bedrock、Google Vertex AI、Gemini 原生，以及 OpenAI-Compatible 端点。
- 协议：OpenAI Chat Completions、Responses、Embeddings、Rerank、Audio transcription/translation/speech、Files、Batch；Anthropic Messages；MCP JSON-RPC；A2A 0.3/1.0。
- 支持 SSE 流式、Tool/Function Calling、结构化输出、Vision content parts 与多模态请求归一化。
- Logical Model 将应用别名与具体 Deployment 解耦，切换 Provider 或路由策略无需修改客户端。

### 2. 固定七阶段治理管线

所有模型、MCP、A2A 请求都经过：

1. **Admission（准入）**：认证、虚拟密钥、速率/并发限制、请求体上限。
2. **Input Guardrail（输入护栏）**：关键词/正则/Secret/PII/Prompt Injection 规则、上下文守卫、可选外部内容安全服务。
3. **Cache（缓存）**：精确缓存 + 语义缓存；命中即短路执行，但仍完整记账与上报遥测。
4. **Resolution（解析）**：模型/工具/Agent 路由、数据地域约束、预算预留、能力校验。
5. **Execution Resilience（执行韧性）**：重试、回退、熔断、凭据池、可配置的流式/Provider 超时。
6. **OutputStream Guardrail（输出护栏）**：Inline/Buffered/Shadow 流式三级护栏，可选 LLM-as-Judge 与 Groundedness 检查。
7. **Accounting & Telemetry（记账与遥测）**：Token 计量、成本归因、审计事件、指标、追踪与持久化 Spool。

### 3. 成本与 FinOps

- Tenant → Project → Key 多级预算；Standard 档支持分布式预算预留。
- RPM/TPM、Key 配额、并发租约与在途请求跟踪。
- Input/Output/Cache/Reasoning/Tool Token 计量，并与 Provider usage 对账。
- 缓存命中率、规避 Token、参考价成本估算与未定价 Token 报告。
- 语义缓存 embedding 与 LLM-as-Judge 等辅助调用也进入预算并写入带 `parent_request_id` 的辅助 usage 记录，不再游离于 FinOps 之外。

### 4. Agentic 治理

- **MCP**：工具目录、按工具策略、审批、速率/预算限制、工具调用账本、`tools/call` 与 `tasks/*` 治理。
- **A2A**：Agent Endpoint、能力路由、信任关系、委托跳数、联邦身份、任务幂等与 exactly-once Push Outbox。
- 人工审批与职责分离（SoD）工作流。
- 从记账账本派生 Agent/Task Graph，用于审计与优化分析。

### 5. 运维与安全

- 内嵌 Web Console：Setup Wizard、Provider、模型、密钥、项目、策略、护栏、缓存、FinOps、告警、审计与 Agent 视图。
- 本地 Argon2id 管理员 + OIDC SSO、CSRF 防护与 RBAC。
- PostgreSQL RLS：租户级连接池与 `app.tenant_id`。
- 密钥加密落库；SQLite Master Key Sidecar；Provider 与外部护栏统一 egress/SSRF 策略。
- RuntimeBundle 签名、原子激活；签名或校验失败时保留 last-known-good。
- OTLP Traces/Metrics/Logs、Prometheus `/metrics`、ClickHouse Analytics Sink、Webhook/邮件告警与 Evidence Export。
- 优雅 Drain、备份恢复脚本、DR Drill CLI，以及支持 HPA/PDB 的 Kubernetes Helm Chart。

### 6. 开发者体验

- OpenAI-Compatible API：现有 SDK 只需修改 Base URL 与 Key。
- 官方 Go、JavaScript/TypeScript、Python SDK。
- OpenAPI 契约、API Version Header 与 CI 兼容性门禁。
- 一条命令本地启动，内置 mock-provider 测试设施。

---

## 技术架构

```mermaid
flowchart LR
  C[客户端: OpenAI / Anthropic / MCP / A2A] --> G[LiteAIG Gateway]
  A[管理 Console] --> CP[Control Plane]
  CP --> DB[(SQLite / PostgreSQL)]
  CP --> RB[Signed RuntimeBundle]
  RB --> G
  G --> P[固定七阶段 Pipeline]
  P --> RT[路由与执行韧性]
  RT --> PR[模型 / 工具 / Agent Provider]
  G <--> RD[(Redis 协调: Standard)]
  P --> O[OTLP / ClickHouse / Webhook / Audit]
```

### 核心组件

| 组件 | 职责 |
|---|---|
| `internal/kernel` | Canonical Interaction、Pipeline Contracts、Runtime Snapshot |
| `internal/access/protocol` | OpenAI/Anthropic/MCP/A2A 线协议适配 |
| `internal/gateway` | 准入、护栏、缓存、路由、执行、流式处理 |
| `internal/connectors` | 模型、工具、Agent 连接器，统一受 egress 策略约束 |
| `internal/controlplane` | 配置编译/发布/回滚、Admin API、Console 后端 |
| `internal/finops` | 预算、记账、定价、Chargeback |
| `internal/federation` | A2A 信任、委托、Push Outbox |
| `internal/platform` | 存储、协调、加密、egress、可观测性 |
| `web/console` | 内嵌 React Console |
| `cmd/liteaig` | 组合根与单二进制入口 |

### 创新点

1. **Governance by construction。** 管线固定且由 CI 守护。扩展只能挂接既有阶段的检查点，不存在任意阶段重排或绕过路径。
2. **Compile once, sign, activate。** 配置校验后编译为不可变租户快照；控制面签名 RuntimeBundle；Gateway 原子激活，校验或签名失败时保留 LKG。
3. **一套契约，两档交付。** Lite 使用 SQLite 单进程；Standard 使用 PostgreSQL + Redis 分离平面。数据面行为与治理语义保持一致。
4. **Agentic 信任不是外挂。** A2A/MCP 与模型调用共享同一准入、预算、审批、记账与审计路径。
5. **FinOps 与审计在请求路径内。** 预算预留、Token 对账、证据与成本归因不是批处理报表的附属品。
6. **极小运维足迹。** 静态 Go 二进制、distroless 非 root 镜像、内嵌 Console，无强制外部运行时。

---

## 与 LiteLLM、Higress、Kong AI Gateway 的对比

> 下表用于产品定位参考，不是性能基准。各项目迭代很快，请以官方文档为准。

| 维度 | **LiteAIG** | **LiteLLM** | **Higress** | **Kong AI Gateway** |
|---|---|---|---|---|
| 核心定位 | AI 治理与 Agentic Traffic Control Plane | LLM Proxy/SDK 与 Provider 抽象 | 云原生 API Gateway + AI 插件 | 企业 API Gateway + AI 插件 |
| 运行时 | Go，单静态二进制 | Python 服务/SDK | Envoy/Istio，Kubernetes 原生 | Nginx/OpenResty，Kong 平台 |
| 配置模型 | 编译期校验快照 + 签名 Bundle | YAML + Python callback/hook | Kubernetes CRD + Wasm 插件 | Kong Entity + 插件配置 |
| 扩展机制 | 固定管线 + 受控检查点；编译期注册与受控外部护栏 Hook | Python 生态与 callback | Wasm 插件生态、CRD | 大型插件生态；Lua/Go/Python/JS |
| Provider 覆盖 | OpenAI、Anthropic、Azure、Bedrock、Vertex、Gemini、Compatible | 非常广泛的多 Provider 目录 | 通过 AI 插件支持广泛 Provider | 通过 AI 插件支持广泛 Provider |
| 治理深度 | 固定七阶段、预算、TPM/RPM、RLS、审计、证据 | Virtual Key、预算、限流、路由、缓存、Guardrail | L7 策略、Token 限制、WAF、AI Guardrail/Cache | AI 限流、Prompt Guard、PII、Semantic Cache |
| Agentic | A2A 0.3/1.0、MCP Tools/Tasks、审批、联邦、信任 | 非主要方向 | MCP/AI Gateway 能力；A2A 信任非主要方向 | MCP Proxy 与 AI 插件；A2A 信任非主要方向 |
| 部署 | Binary、Docker、Helm；SQLite 或 PostgreSQL + Redis | Python 服务 + DB/Redis | Kubernetes | Kong Gateway 部署 |
| 最适合 | 强治理、A2A/MCP 控制、FinOps、从边缘到 HA | 在 Python 生态中快速接入大量 Provider | 已使用 Envoy/Istio、需要 L7 + AI 的平台团队 | 已标准化在 Kong 上的企业 |

**取舍一览**

- **LiteAIG**：Provider 目录与插件生态小于成熟网关，且不是通用 L7/WAF 网关；当治理语义与轻量运维优先时最合适。
- **LiteLLM**：在 Python 生态中接大量 Provider 最快；对固定治理管线与 Agent 信任的约束较弱。
- **Higress**：Kubernetes/Envoy 集成与插件覆盖出色；需要云原生数据面与 K8s 运维模型。
- **Kong AI Gateway**：成熟的企业网关与插件生态；需要 Kong 平台、数据库/控制面运维，并考虑许可成本。

**选择 LiteAIG 的典型场景**：治理、成本归因、审计、A2A/MCP 信任与较小运维足迹，比“最大 Provider 目录”或“通用 L7 插件生态”更重要。

**LiteAIG 不替代通用 API Gateway。** 它更适合作为 AI 治理层，部署在 Kong、Higress、Envoy 或云负载均衡之后。

---

## 快速开始

### 前置条件

- Go 1.25+
- Node.js 22+：Console 资源会嵌入 Go 二进制且不纳入 Git 跟踪，因此全新
  源码检出必须先构建 Console；Docker 构建会自动完成
- Docker 与 Helm 可选

### 从源码运行（Lite）

```bash
git clone https://github.com/F31/liteAIG.git
cd liteAIG

# 全新检出先构建一次内嵌 Console 资源
(cd web/console && npm ci && npm run build)

go build -o liteaig ./cmd/liteaig

export LITEAIG_MASTER_KEY="$(openssl rand -base64 32)"
./liteaig \
  --db file:./liteaig.db \
  --admin-addr 127.0.0.1:8081 \
  --gateway-addr 127.0.0.1:8082
```

打开 `http://127.0.0.1:8081`，完成 Setup Wizard。向导会创建首个 Tenant、Provider、Logical Model 与 Virtual Key。

```bash
export LITEAIG_KEY="<setup 生成的 virtual key>"

curl -s http://127.0.0.1:8082/v1/chat/completions \
  -H "Authorization: Bearer $LITEAIG_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "default-chat",
    "messages": [{"role": "user", "content": "Hello from LiteAIG"}]
  }'
```

流式调用：

```bash
curl -N http://127.0.0.1:8082/v1/chat/completions \
  -H "Authorization: Bearer $LITEAIG_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "default-chat",
    "stream": true,
    "messages": [{"role": "user", "content": "从一数到五"}]
  }'
```

### Docker

```bash
docker build -t liteaig .

docker run --rm \
  -p 8081:8081 -p 8082:8082 \
  -v liteaig-data:/data \
  -e LITEAIG_MASTER_KEY="$(openssl rand -base64 32)" \
  liteaig \
  --db file:/data/liteaig.db \
  --admin-addr 0.0.0.0:8081 \
  --gateway-addr 0.0.0.0:8082
```

镜像以 distroless 非 root（`65532`）运行。生产环境请在反向代理或 Ingress 终止 TLS，并限制 Admin API 仅可信网络可达。

### Kubernetes / Standard

Helm Chart 支持 `gateway` / `control` 分离、HPA、PDB、滚动升级与 Drain 配置。Standard 档需要 PostgreSQL 与 Redis。

```bash
helm install liteaig ./deploy/helm/liteaig \
  -f ./deploy/helm/liteaig/examples/split-plane-values.yaml
```

安装前创建其引用的 Secret（`liteaig-standard`），包含 `database_dsn`、`coordinator_url`、`liteaig_master_key`、`bundle_token`、`bundle_signing_key`、`bundle_public_key`。具体见 Chart 注释与 [部署文档](docs/DEPLOYMENT.md)。

### 关键配置

| Flag / Env | 作用 |
|---|---|
| `--db` | SQLite DSN（Lite）或 PostgreSQL DSN（Standard） |
| `--mode=all\|gateway\|control` | 单进程或分离平面 |
| `--coordinator` | Standard 档 Redis 账本/租约地址 |
| `--postgres-rls` | 启用 PostgreSQL RLS 与租户连接池（默认 true） |
| `--postgres-tenant-pools` | 懒加载租户池上限（默认 16，每池 2 连接） |
| `--gateway-rpm`、`--gateway-tpm` | RPM/TPM 限制 |
| `--gateway-max-body-bytes` | 音频/视觉/文件场景调大 |
| `--stream-total-timeout` | `0` 表示流式不设总时长 |
| `--stream-idle-timeout`、`--first-token-timeout`、`--provider-timeout` | AI 流量超时调优 |
| `--external-guardrail-url` | Layer 3 外部内容安全服务 |
| `LITEAIG_MASTER_KEY` | Secret 加密主密钥（PostgreSQL 必填） |
| `--otlp-*` | OTLP Traces/Metrics/Logs 导出 |

---

## 安全模型

- 数据面使用 Virtual API Key；Key 变更后即时重编译租户快照。
- 控制面支持本地 Argon2id 管理员与 OIDC SSO，带 CSRF 防护与 RBAC。
- PostgreSQL RLS 默认启用：租户请求使用受限 `liteaig_app` 连接池并设置 `app.tenant_id`；控制面/后台任务使用可信平台连接。
- Secret 加密落库；Provider 与外部护栏统一 egress/SSRF 策略。
- RuntimeBundle 签名并原子激活；坏 Bundle 被拒绝，last-known-good 继续服务。
- 审计事件、Evidence Export、结构化安全事件与 OTLP 日志避免包含请求体与 Secret。

详见 [安全架构](docs/WHITEPAPER.md)。

---

## 项目结构

```text
cmd/liteaig/                 单二进制入口
internal/access/protocol/    OpenAI / Anthropic / MCP / A2A 适配
internal/gateway/            准入、护栏、缓存、路由、执行
internal/connectors/         模型、工具、Agent 连接器
internal/controlplane/       配置生命周期、Admin API、Console 后端
internal/finops/             预算、记账、定价、Chargeback
internal/federation/         A2A 信任、委托、Push Outbox
internal/platform/           存储、协调、加密、egress、遥测
internal/kernel/             Interaction、Pipeline Contracts、Snapshot
web/console/                 内嵌 React Console
deploy/helm/liteaig/         Kubernetes Chart
migrations/                  SQLite/PostgreSQL Schema
```

---

## 测试与质量门禁

```bash
make test      # go test -race ./...
make check     # go vet + 架构边界 + Console 检查
go build ./...
```

主要门禁：

- 架构边界测试（`cmd/architecture-test`）
- Gateway 负载 Smoke（`internal/app/gateway_load_smoke_test.go`）
- PostgreSQL RLS 集成与端到端测试
- 官方 SDK Conformance 与 OpenAPI 兼容性检查
- Kubernetes kind Standard 拓扑 Smoke（Postgres + Redis + 多副本）
- CI 中的 `govulncheck`、npm audit、Helm/Kubernetes manifest 校验

PostgreSQL 测试通过 `LITEAIG_TEST_POSTGRES_DSN` 启用；Redis 测试使用仓库的 coordination 测试环境。

---

## 文档

- [白皮书：产品与架构](docs/WHITEPAPER.md)
- [部署指南](docs/DEPLOYMENT.md)
- [差距分析与路线图](docs/GAP_ANALYSIS.md)
- [DR Runbook](docs/DR_RUNBOOK.md)
- [API 兼容性](docs/API_COMPATIBILITY.md)
- [SDK 兼容性](docs/SDK_COMPATIBILITY.md)
- [A2A Conformance](docs/A2A_CONFORMANCE.md)
- [网关性能基准](docs/GATEWAY_BENCHMARK.md)

---

## 范围与限制

LiteAIG 有意聚焦 AI 与 Agentic 治理。它**不是** Agent Runtime、RAG 平台、Prompt IDE、模型训练平台或通用 L7 网关。扩展机制是受控且编译期优先的，不以替代 Kong/Higress 插件生态为目标。

当前边界：

- Lite 档使用 SQLite，单写者；复制型生产拓扑请使用 Standard。
- 语义缓存默认是有界进程内缓存；Standard 的精确缓存与 RPM/TPM 已走 Redis，分布式向量索引仍在路线图中。
- 完整多租户 SaaS、多 AZ Active 切换、SAML/SCIM、MCP Task Store 与更广的插件桥接仍属于路线图项。实时清单见 [Gap Analysis](docs/GAP_ANALYSIS.md)。

---

## 贡献

欢迎提交 Issue 与 Pull Request。提交前请运行：

```bash
make test
make check
```

请保持固定管线不变量：新能力只能作为既有阶段的检查点接入；租户级数据访问必须携带 `TenantScope`/tenant context。
