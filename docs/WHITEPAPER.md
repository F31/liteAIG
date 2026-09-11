# LiteAIG 技术白皮书

> 版本：1.0
> 日期：2026-09
> 对象：技术决策者、平台工程师、AI 基础设施团队
> 范围：LiteAIG —— 面向 Models、Tools、Agents 的统一企业级 AI 治理与流量控制平面

---

## 摘要

LiteAIG 是一个**模块化单体、单二进制优先**的企业 AI 网关与治理控制平面。它把模型调用、工具调用与 Agent 协作（A2A）统一收敛到一条**固定七阶段治理管线**之内，使身份、授权、成本、数据边界、故障与审计语义对所有流量一致生效；同时提供从「配置 → 预演 → 发布 → 观测 → 回滚」的完整运营闭环，以及从单机 SQLite（Lite）到 Postgres + Redis 多副本（Standard）的平滑演进路径。

本文档基于当前代码库（Go 104 个包、非测试代码约 3.8 万行、含测试约 6.6 万行；34 个数据库迁移；Console 约 0.9 万行 TypeScript/TSX）描述其功能、技术架构、产品特色与创新点。

---

## 一、产品定位与价值主张

### 1.1 一句话定位

> LiteAIG 是面向 Models、Tools 与 Agents 的统一 Enterprise AI Governance & Traffic Control Plane（企业级 AI 治理与流量控制平面）。

### 1.2 不做的事（明确边界）

LiteAIG 不提供：Agent Runtime/Workflow、RAG 流水线、Prompt IDE、模型训练与评测平台、通用 API 网关（Envoy/Higress 类）、应用市场。它可以部署在这些组件之后，由其负责网络层治理，LiteAIG 专注 **AI 治理**。

### 1.3 部署档位

| 档位 | 核心存储 / 协调 | HA 基线 | 推荐用途 |
|---|---|---|---|
| Lite | SQLite（单写者）+ 内存 | 单实例、LKG 本地兜底、优雅排空 | 开发、小团队、边缘（≤200 RPS） |
| Standard | PostgreSQL HA + Redis/Valkey | Gateway ≥3 / Control ≥2、多 AZ、PDB/Topology Spread、记账 Spool | 主流生产（≤5000 RPS） |

两种档位共享**同一个二进制与同一套代码路径**，差异只体现在存储与协调的装配上。

---

## 二、核心功能能力

### 2.1 统一 AI 接入（AI Access）

- **多协议接入面**：OpenAI、Anthropic 兼容协议，MCP（`tools/call`、`server/discover`），A2A（Agent-to-Agent，双线协议，见 6.4）。
- **主流 Provider 原生适配**：OpenAI、Anthropic、Azure、Bedrock（含 SigV4）、Vertex（含 OAuth Token）；长尾通过 OpenAI-Compatible 或 Bridge 接入。
- **Logical Model 抽象**：业务只感知逻辑模型别名，底层 Provider 可替换而无需改动业务代码。
- **虚拟密钥（Virtual Key）**：Tenant/Project 作用域 + Model ACL；Key 密文加密存储，支持 Console Reveal 与一次性完整展示；创建/吊销即时重编译 Tenant Runtime 生效，无需发布新配置版本。
- **Setup Wizard**：Provider 测试、模型发现、首调验证一条龙完成，目标「5 分钟接入」。

### 2.2 可解释的智能路由

- 路由策略支持 lowest-latency / lowest-cost / lowest-load / ID 等策略，并纳入**健康过滤**（Circuit State + 上游探测）与**软评分**（滚动 1 分钟窗口的延迟/成本/负载/成功率指标）。
- **Explainability**：Playground 与 Timeline 展示每个候选端点的淘汰/入选证据（eligible / excluded），回答「为什么选它」。
- **Cache 亲和**：语义缓存命中优先路由到同源部署，降低缓存穿透。

### 2.3 治理管线（固定七阶段）

所有数据面流量（模型、MCP、A2A、Agent 内部调用）必须经过同一管线：

1. **Admission（准入）**：认证、速率限制、并发控制、请求体积上限。
2. **Input Guardrail（输入护栏）**：关键词/规则 + 外部安全模型评测 + 上下文守卫。
3. **Cache（缓存）**：精确缓存 + 语义缓存，命中即短路（仍完整记账）。
4. **Resolution（解析）**：路由、模型/工具/Agent 解析、预算预留。
5. **Execution Resilience（执行韧性）**：重试、回退、熔断、凭据池、超时。
6. **OutputStream Guardrail（输出护栏）**：流式三级护栏 + 输出评判（Judge）。
7. **Accounting & Telemetry（记账与遥测）**：Token 计量、成本归因、审计、指标、追踪。

任何新能力只能以「检查点」挂接到既有阶段，不允许绕过管线（架构 CI 强制）。

### 2.4 治理能力域

- **Guardrail（护栏）**：内置规则集 + 自定义策略（Fast Publish 即时生效）；流式三级护栏（Inline 滚动窗口 / Buffered 边界缓冲 / Shadow 异步外部检查），注入即切断、违规留痕；输出 Judge 模型评审。
- **Rate Limit / 并发**：RPM 令牌桶 + TPM（估计-预留-对账），突发可控。
- **Budget（预算）**：Token/成本预算，支持 regional / global_soft / global_hard 一致性；预留（reserve）-结算（settle）-释放（release）语义，故障按档位 fail-open/fail-closed。
- **Human Approval（人工审批）**：高风险工具/Agent 动作阻塞在数据面，直到操作者在 Console 决策；审批者身份必须由租户本地账号或 IdP 背书。
- **Data Boundary（数据边界）**：项目声明允许数据区域，关系/调用按区域强制执行（advisory / strict），支持"合同化"绑定。
- **MCP / A2A / Tool 治理**：工具调用与外部 Agent 调用同样进入管线，接受身份、预算、护栏与审计治理。

### 2.5 FinOps 成本归因

- **Durable Accounting Spool**：记账事件先落本地 Spool 再批量冲刷，数据库故障不丢账（至少一次 + 幂等摄取）。
- 按请求、项目、模型、部署进行成本归因；重试/回退成本单独统计；缓存命中成本显式计入。
- FinOps 视图 + 优化建议（Recommendations）；预算审计独立成表。

### 2.6 告警与通知

- **类型化告警规则**：budget / rate / cost_anomaly 三类，阈值/窗口/级别可配；生命周期 firing → ack → silence → resolve。
- **默认规则集**：内置默认规则契约（`deploy/alerts/default-rules.json`），Console 或 API 一键幂等导入。
- **Webhook 通知**：只发送元数据（事件 kind、rule_id、severity 等白名单），绝不携带 Prompt/响应/证据/密钥；目标 URL 经 egress 策略校验、禁 userinfo；**运行时热重载**，保存即生效，无需重启。

### 2.7 可观测性

- **OTLP Tracing**（可选）、**Prometheus `/metrics`**（低基数聚合指标）。
- **Live Tail**：请求摘要 SSE 实时流（仅摘要，绝不流式正文）。
- **Audit**：租户级审计事件（配置发布/回滚、RBAC 变更、护栏命中、密钥轮换、联邦历史等）。
- **Evidence Export**：按时间范围导出只读证据归档，Prompt/响应与密钥按构造排除。
- **Push Outbox 下钻**：`GET /api/admin/federation/push-outbox?status=…` 枚举 pending/sending/delivered/failed 每行投递状态（脱敏）。

### 2.8 Web Console

React + antd 的单页控制台（中英双语），覆盖 Setup、Dashboard、Resources、Playground、Requests、Configuration、FinOps、Health、Alerts、Governance、Users 等页面；无障碍（axe WCAG 2A/AA）、Lighthouse 门禁、Playwright 端到端用例持续守护。

---

## 三、总体技术架构

### 3.1 模块化单体 / 单二进制

- **单二进制** `cmd/liteaig`：Admin API、数据面网关、就绪服务器、嵌入式 Console 全部由同一进程提供。
- **内部模块化**：Kernel（稳定核心）→ Access/Protocol（协议适配）→ Connectors（模型/Agent 连接器）→ Governance（治理域）→ Platform（存储/安全/协调/邮件等）→ Web Console，依赖方向由架构 CI 强制，禁止反向依赖。
- **WebKit 微内核**：HTTP 层使用 Go 标准库 `net/http` 微内核（洋葱中间件 + 统一 APIError），**不引入任何第三方 Web 框架**。

### 3.2 三层服务架构

| 层 | 模块 | 职责 |
|---|---|---|
| 表示层 | `internal/controlplane/adminapi` | 纯 HTTP/会话/RBAC 接线，类型化服务签名 |
| 业务编排层 | `internal/controlplane/backend` | ControlBackend + 视图契约（Narrow Capability Interfaces） |
| 持久层 | `internal/platform/storage/sqlrepo` + `sqlite`/`postgres` | 唯一仓库构造点 `Store`；存储适配器对业务层零依赖 |

`Backend` 的 41 方法 God Interface 已拆为按路由域划分的窄能力接口（Setup/Project/Dashboard/Key/Config/Runtime/Playground/Request/Alert/Security/Federation/Identity/Evidence/…），新能力通过 `Set*` 槽位接线，杜绝 God Interface 回归。

### 3.3 可执行架构守卫

`cmd/architecture-test` 依据 `architecture/{modules,forbidden-imports,table-owners}.yaml` 执行五类检查并纳入 CI 门禁：

1. Import Graph（模块依赖方向）；
2. Module Owner（模块属主）；
3. Table Owner（一表一属主）；
4. Migration Owner（迁移文件 `-- owner:` 头）；
5. Direct SQL 引用（禁止跨层裸 SQL）。

### 3.4 配置生命周期

- **Draft → Validate → Diff → Publish → Rollback**：每次发布生成版本号，可语义 Diff、可一键回滚。
- **RuntimeBundle（签名运行时包）**：控制面签名发布，网关带认证拉取并**原子激活**；支持 Prepare/ACK/NACK。
- **Last Known Good（LKG）兜底**：启动或同步失败时回退到最近已知良好版本，保证数据面可用性不因配置同步故障而中断。
- Guardrail 策略支持 Fast Publish（即时覆盖 Active Snapshot，无需新配置版本）。

---

## 四、数据面：七阶段治理管线（实现视角）

数据面由 `internal/kernel/pipeline` 的固定 Stage 枚举驱动（Admission → InputGuardrail → Resolution/Execution（含缓存短路）→ OutputStreamGuardrail → AccountingTelemetry）。所有协议面（OpenAI/Anthropic/MCP/A2A）都归一化为 `interaction.UnifiedRequest` 后进入管线；归一化适配器（`internal/access/protocol/*`）负责线协议 ↔ 统一交互模型的双向转换，包括：

- 角色映射（A2A `user/agent` 与 `ROLE_USER/ROLE_AGENT` → 规范 chat 角色）；
- 多文本 part 拼接；
- 流式增量帧编码（SSE）；
- 严格 JSON-RPC 2.0 校验（错误码 -32700/-32600/-32601/-32602）。

管线关键机制：

- **流式护栏三级模型**（Inline / Buffered / Shadow）在 `streamCollector` 中实时执行，注入内容在首个违规 chunk 即被切断，泄漏窗口有界。
- **记账 Spool** 把记账与请求热路径解耦：请求不被 Spool 阻塞，但账绝不丢。
- **幂等重放**：A2A 任务按幂等键去重；错误重试受 hop/call 上限约束。

---

## 五、安全架构

### 5.1 身份与访问

- 数据面：Bearer Token（虚拟密钥）+ Tenant Runtime 即时重编译；跨实例协调租约防并发写。
- 控制面：本地管理员（Argon2id）+ OIDC SSO（PKCE、JWT、Admin/Operator subject 映射）+ RBAC 角色（tenant_admin / tenant_operator / viewer），写操作需 CSRF Token，响应统一 `Cache-Control: no-store` + CSP。
- 密码重置：SMTP 一次性验证码；未配置邮件时回退本地应急表单。

### 5.2 密钥与加密

- **Master Key Sidecar**：SQLite 旁生成 0600 `*.masterkey`；Postgres 部署要求 `LITEAIG_MASTER_KEY`。
- **AES-GCM Cipher**：API Key 密文、Secret Vault、A2A Push 回调 URL/Bearer/Payload 入库前全部加密（`sealed:` 前缀）。
- Secret Provider 组合：Pepper + Vault（密文解密）+ 环境变量，凭据解析集中化。

### 5.3 出站 SSRF 防护（egress）

所有出站连接（模型、MCP、A2A、Webhook）共用同一策略：

- 拒绝私网/链路本地/云元数据 CIDR（`10/8`、`172.16/12`、`192.168/16`、`169.254/16`、环回（默认）、`::1`、`fc00/7` 等）；
- **拨号时逐地址校验**（防 DNS Rebinding）；
- 拒绝重定向链、响应体封顶；Webhook 目标禁 userinfo。

### 5.4 数据面 RLS（Postgres）

Standard 档在 Postgres 上启用**行级安全（RLS）**作为第二道防线：租户隔离策略覆盖全部租户级表，受控平台角色 BYPASSRLS 仅用于运维。

### 5.5 零信任联邦

- Agent Card 签名（JWS）验证只产生**候选**，不产生信任；
- 操作者 review + 已验证锚点 + 显式项目/能力授权后关系才激活；
- 入站联邦调用同样进入管线与数据边界检查，激活状态可随时 Suspend。

---

## 六、Agent 互联（A2A）：信任、持久化与投递

这是 LiteAIG 最具差异化的能力域。

### 6.1 Agent Card 信任生命周期

- **发现**：拉取 Agent Card → 归一化 → **JWS 签名验证**（RS256，可选公钥）→ 生成 `candidate`。
- **评审**：操作者核对已验证锚点、显式授予项目/能力。
- **激活/挂起**：激活后入站联邦身份才可解析；材料变更 fail-closed（重新验证）。
- Console 评审 UX 有 Playwright 端到端守护。

### 6.2 持久化任务（Durable Task）

- 入站 A2A 调用落为持久任务，按幂等键去重；
- `pending → running` **原子 CAS**，配合 `a2aTaskRunningTTL` **陈旧运行恢复**；
- 出站采用增量 delta 流式转发（SSE），暂停/恢复与完成态精确。

### 6.3 签名推送 Outbox（Exactly-Once 投递）

- 非流式完成结果按 `pushNotificationConfig.url/token` 进入**持久 Outbox**；
- 回调 URL / Bearer / Payload 全部 AES-GCM 加密落库；
- 投递由 `coordination_leases` 单例租约守护，`pending → sending` 原子认领，陈旧认领自动回收；
- 每次投递带 `X-LiteAIG-A2A-Push-ID`（幂等去重）+ 时间戳 + HMAC 签名（防重放/防篡改）；
- **双进程 / 双网关（Postgres+Redis）黑盒冒烟**证明：任一队列项恰好投递一次，SIGTERM 优雅排空。

### 6.4 双线协议互操作（0.3 + 1.0）

`POST /a2a` 同时支持两种线协议：

- `method: "message/send"` → 0.3 风格 parts（`[{"kind":"text","text":…}]`）；
- `method: "SendMessage"` → A2A 1.0 protobuf-JSON（`ROLE_*` 枚举、裸 `text` part、oneof message）。

已用 **最新官方 `a2a-sdk`（v1.1.2）实测互通**：官方 SDK 客户端驱动真实网关，其自身类型正确解析我方 `ROLE_AGENT` 回显消息（`docs/A2A_CONFORMANCE.md`）。CI 中 `conformance.yaml` 在每次 PR/推送用最新 SDK 重新验证。

### 6.5 联邦治理

- Relationship Store 持久化；admin 侧激活即时可被网关解析；
- 数据边界区域校验；审批工作流；Agent Graph 视图；外部 Agent 成本单列。
- 下钻 API 全状态（pending/sending/delivered/failed）枚举，URL/Token/Payload 永不泄漏。

---

## 七、高可用与部署

### 7.1 Lite（单实例）

- SQLite 单写者；`*.masterkey` sidecar；`/healthz`、`/readyz`（就绪门控已发布租户 Runtime）；
- SIGTERM 优雅排空（先拒新流量、等在途/长流、冲刷记账 Spool）；
- LKG 本地兜底。

### 7.2 Standard（多副本 split-plane）

- **分体模式**：`--mode=all | gateway | control`；
- Gateway 多副本共享 Postgres + Redis 协调（预算账本、并发租约、在途计数）；
- 控制面签名 RuntimeBundle，网关认证拉取并原子激活；
- `coordination_leases` 为单例任务（sweeper、push outbox 等）提供跨进程互斥与陈旧接管；
- 双网关进程 + Postgres/Redis 的 exactly-once 投递已有进程级黑盒验证。

### 7.3 容器与 Kubernetes

- **Dockerfile**：多阶段 → distroless static **nonroot（65532）**，SQLite 数据挂载 `/data`，无运行时依赖；
- **Helm Chart**：Deployment/Service/PDB/HPA（drain-aware）/拓扑分布约束/Pod 反亲和/PriorityClass/Secret 引用（不内联密钥）；`extraArgs`/`extraEnv` 支持 Standard 档 flag；
- 提供 split-plane 示例 values；CI 用 Helm lint + template + **kubeconform strict** 校验渲染产物。

### 7.4 发布流水线

- `ci.yml`：格式门禁（`gofmt -l`）、`go vet`、架构守卫、race 全量测试、**govulncheck**、Console `npm audit`（prod，high 以上）、镜像构建 + 容器冒烟、官方 SDK conformance、Helm/kubeconform；
- `release.yaml`：`v*` 标签触发 → GHCR 推送 + SPDX SBOM + GitHub Release；**支持 workflow_dispatch dry-run**（构建 + SBOM 产物，跳过发布）；
- `.github/dependabot.yml`：Go/npm/Actions 每周分组 PR；
- 发布/升级/回滚 runbook：`docs/RELEASE.md`、`docs/UPGRADE_DRILL.md`。

---

## 八、工程化与质量

- **测试资产**：Go 测试约 6.6 万行、全量 94 包通过；进程级双写者冒烟（`a2a_two_process_smoke_test.go`）、Standard 档双网关冒烟（`a2a_standard_process_smoke_test.go`）、金标/混沌/浸泡测试目录。
- **前端质量**：Prettier 格式、i18n 完整性、硬编码字符串检查、Vitest 单测 + axe 无障碍 + Lighthouse、Playwright 端到端（setup→login→playground→requests→config、Agent Card 评审、告警通知配置）。
- **契约守护**：默认告警规则 JSON ↔ Go 常量一致性测试；视图 JSON 线格式 e2e 防漂移；A2A 官方 SDK 互通测试。

---

## 九、产品特色与创新点

1. **一条治理管线治理所有流量**（Model/Tool/Agent）：把不同协议归一化为统一交互模型，身份、成本、护栏、审计对一切一致生效，而非为每种流量各写一套治理。
2. **可执行的架构约束**：三层服务分层 + 窄能力接口 + 五类架构检查器进 CI，把「架构文档」变成「编译/CI 期强制」，从机制上杜绝 God Interface 与依赖腐化。
3. **A2A 企业级信任闭环**：从 Agent Card 签名验证 → 人工评审激活 → 持久化任务 CAS → 加密签名 Push Outbox exactly-once 投递 → 全状态下钻，覆盖外部 Agent 协作的信任、持久性与可排障全链路。
4. **最新官方 SDK 互通（双线协议）**：不是「我们自己的客户端自测通过」，而是官方 `a2a-sdk` 1.x 实测互通；0.3/1.0 双线并存，兼容存量与演进。
5. **记账 Spool 与预算账本**：记账与请求热路径解耦（不阻塞请求、不丢账），预算支持 regional/global_soft/global_hard 一致性与故障档位语义，是 FinOps 类网关里少见的「预留-结算-释放」闭环。
6. **LKG + 签名 RuntimeBundle + 原子激活**：配置同步故障不中断数据面；网关回退到最近已知良好版本，控制面/数据面解耦可靠。
7. **单二进制 + distroless nonroot + 双档位同路径**：Lite 到 Standard 是装配差异而非产品分叉，边缘到生产同一产物，降低运维心智与供应链风险。
8. **零框架 HTTP 微内核 + 内建安全原语**：WebKit 微内核（纯标准库）、统一 egress SSRF 策略（防 DNS Rebinding）、RLS 第二道防线、密钥全加密落库，安全能力内建而非外挂。
9. **质量门禁工程化**：govulncheck / npm audit / kubeconform / 官方 SDK conformance / dry-run 发布，把安全与兼容性检验自动化到每次提交。

---

## 十、指标概览（当前代码库）

| 维度 | 数值 |
|---|---|
| Go 包数量 | 104 |
| Go 非测试代码 | ≈ 3.8 万行 |
| Go 含测试代码 | ≈ 6.6 万行 |
| 数据库迁移 | 34 |
| Console 前端 | ≈ 0.9 万行 TS/TSX |
| 全量 Go 测试 | 94 包全绿 |
| A2A 官方 SDK 互通 | a2a-sdk v1.1.2 实测通过 |
| OpenAI/Anthropic 官方 SDK 互通 | openai v3.9.0、anthropic v1.4.0 实测通过 |
| 容器 | distroless nonroot，`/data` 挂载 SQLite |

---

## 十一、边界与后续方向

- **能力域边界**：不做 Agent Runtime/Workflow、RAG、Prompt IDE、模型训练、通用网络网关（见 §1.2）。
- **后续重点**（既有路线图）：K8s 多副本 Standard 拓扑的 `scripts/kind-standard-e2e.sh` 已在本地 kind 全链路跑通（setup→多副本收敛→pod 杀恢复→readyz），等待 `k8s-kind-standard` CI 首次 green；随后推进多租户 SaaS、MCP Tasks、通知渠道扩展等产品决策项。
- **运维建议**：Standard 档生产部署请遵循 `docs/DEPLOYMENT.md`、`docs/RELEASE.md`、`docs/UPGRADE_DRILL.md`，并启用 CI 中的官方 SDK conformance 与 govulncheck 门禁。

---

*本白皮书基于代码库现状编写；指标与验证结果均来自仓库内测试与本地实跑。*
