# LiteAIG v8.5 —— 企业级 AI Governance & Agentic Traffic Control Plane

> 版本：v8.5  
> 日期：2026-09-02  
> 状态：Implementation Baseline / Service Layering & Executable Architecture Frozen  
> 目标：构建面向 Model / Tool / Agent 的统一企业 AI 治理与流量控制平面。采用模块化单体、单二进制优先架构；在稳定 Kernel、协议适配、连接器、治理域和 Production HA / Resilience / DR 体系之上，本版本把 V8.2 的模块边界从“文档约定”固化为“可执行约束”：引入 WebKit 微内核 HTTP 服务层、表示层/业务编排层/持久层三层服务架构、窄能力接口契约与类型化服务签名，并以 architecture manifest + CI 检查器强制 One Table One Owner、依赖方向与表归属，使模型、工具、内部 Agent、外部 Agent 与 Human Approval 都具有明确的身份、授权、数据边界、成本、故障与审计语义。

---

# 0. V8.5 架构收口摘要

V8.5 以 V8.2 完整产品、模块化、Agentic Governance 与 Production HA/DR 设计为基线，不新增产品能力域；本版本聚焦**服务层架构收口与边界可执行化**：

1. **HTTP 服务微内核（WebKit）**：Admin API 与 Gateway 收敛到统一的 Go 标准库 HTTP 微内核（`internal/platform/webkit`），不引入 Echo 等第三方 Web 框架；路由基于 Go 1.22 `http.ServeMux`，中间件洋葱模型，统一 APIError 错误契约。
2. **三层服务架构**：表示层（`adminapi`，纯 HTTP/会话/rbac 接线）、业务编排层（`controlplane/backend`，ControlBackend + 视图契约）、持久层（`platform/storage/sqlrepo` 统一仓库入口 + `sqlite/postgres` 纯适配器）彻底分离，依赖方向由 CI 强制。
3. **窄能力接口**：41 方法 `Backend` God Interface 拆分为 12 个按路由域划分的窄能力接口（Setup/Project/Dashboard/Key/Config/Runtime/Playground/Request/Alert/Security/Federation/Identity），`Backend` 保留为组合超集用于渐进迁移。
4. **类型化服务契约**：Admin API 服务签名禁止 `any` 返回（`ProjectSummary`、`DraftSummary`、`MeView`、`WizardSetupResponse`、`config.Version` 等具体视图类型），JSON 线格式逐字段对齐，e2e 验证无漂移。
5. **领域类型下沉**：localauth/pepper 归 `identity`、AuditRecord 归 `observability/audit`、Resource Catalog 视图归 `catalog`，存储层与业务层对表示层零依赖。
6. **可执行架构守卫**：`architecture/{modules,forbidden-imports,table-owners}.yaml` + `cmd/architecture-test`（import graph / 模块属主 / 表属主 / migration owner / 直接 SQL 引用五类检查器），`make check` 纳入 CI 门禁；存储层、backend 编排层、webkit/catalog/audit 的零依赖边界均有 forbidden-import 规则与负向测试。
7. **API Key 生命周期增强**：Key Secret 加密存储（`api_keys.key_ciphertext`）支持 Console Reveal 与一次性完整展示；Key 创建/吊销通过 Tenant Runtime 即时重编译生效，无需新建配置版本。

| 领域 | V8.5 冻结决策 |
|---|---|
| HTTP 服务框架 | **不引入 Echo/其他第三方 Web 框架**；`internal/platform/webkit` 微内核（Go 1.22 ServeMux + 洋葱中间件 + APIError）；内核对业务包零依赖 |
| 服务分层 | 表示层 `adminapi` 只做 HTTP/会话/rbac/错误渲染；业务编排 `controlplane/backend` 持有视图契约与编排逻辑；持久层 `sqlrepo.Store` 是仓库唯一构造点 |
| 能力契约 | 12 个窄能力接口 + `Backend` 组合超集；新路由接线只依赖最窄能力；服务签名禁止 `any` 返回 |
| 领域类型归属 | localadmin/pepper→`identity`；AuditRecord→`observability/audit`；Resource Catalog 视图→`catalog`；Tenant 列表→`tenancy.TenantLister` |
| 表所有权 | `architecture/table-owners.yaml` 为唯一事实源；migration `-- owner:` 注释与 Go 直接 SQL 引用（`CheckDirectSQL`）同受 CI 校验 |
| 依赖方向 | storage→adminapi 禁止；backend→adminapi/gateway/app 禁止；webkit/catalog/audit 业务零依赖；tenancy/identity→表示层禁止；Domain→concrete storage 禁止 |
| API Key | Key Secret 加密存储支持 Reveal；创建/吊销即时重编译 Tenant Runtime 生效；数据面验证仍走 HMAC（不依赖明文） |
| 错误契约 | Admin：`{"error":{"code","params"}}`；Gateway：OpenAI 风格 `{"error":{"message","type":"liteaig_error","code"}}`；Drain：固定 503 `{"error":{"code":"DRAINING",...}}` |
| 架构守卫 | 五类检查器（import graph / module owner / table owner / migration owner / direct SQL）纳入 `make check` 门禁；负向注入测试保证规则非空转 |
| 部署基线 | 单二进制 `liteaig --mode all`；readyz 端口独立；`setsid` 守护进程 + PID 文件；Playwright management-loop e2e 为发布门禁 |
| 不变项 | V8.2 全部冻结决策继续有效：Modular Monolith、固定七阶段 Pipeline、四类 Interaction、Canonical Identity、RuntimeBundle/LKG/Spool、Federated Agent Trust 体系 |

V8.2 基线决策（继续有效）：

| 领域 | V8.2 冻结决策 |
|---|---|
| 模块形态 | **Modular Monolith / 单二进制优先**，模块不等于微服务 |
| Kernel | 固定七阶段 Pipeline、RuntimeSnapshot、InteractionContext、核心 Contracts 独立稳定 |
| 协议层 | OpenAI / Anthropic / MCP / A2A 只做 Wire Protocol ↔ Unified Interaction，不直接执行业务治理 |
| Connector | Model / Tool / Agent 上游调用统一通过 Connector Contract，协议适配与上游连接分离 |
| Identity | User / Application / ServiceAccount / Agent 共用唯一 Identity/Principal 模型，取消重复 Agent Identity 子系统 |
| Governance | Guardrail、Policy、Budget、Delegation、Task、Approval 保持独立边界，不合并成“大 Governance 包” |
| Agent Versioning | 增加 Agent Version / Endpoint Version Pin / Drain 语义，Task 可固定解析版本 |
| Capability | Capability Registry 定义为**编译索引**，事实源仍是 Model/Tool/Agent 资源声明 |
| Team / Mesh | 不作为当前 Core 一级资源；P2 先作为 Agent Group / Task Graph 派生治理视图验证价值 |
| Observability | 模块发 Domain Event / Telemetry Contract，不允许所有模块直接依赖 Observability 实现 |
| Config | Data Plane 只读不可变 RuntimeSnapshot，不允许业务模块直接访问 Config Plane |
| 路线图 | 不新增 Phase 2.5/3.5；Agentic Foundation/Observability 作为跨 Phase Workstream 纳入现有 Phase 2/3/4 |
| 交付标准 | 保持 Scenario A-G；Scenario F 扩展跨组织 Federated Agent；Agent Mesh/Team/Anomaly 不新增 Golden Scenario |
| Data Plane HA | 默认 Active-Active / Shared-nothing；实例无 Leader，LB 任意分发 |
| Control Plane HA | CRUD Active-Active；单例任务使用 Lease/Leader Election；CP 故障不影响已加载 DP |
| Config HA | Signed RuntimeBundle + Prepare/ACK/NACK + Atomic Activate + Local Last Known Good |
| Accounting HA | Durable Accounting Spool，采用 At-least-once + Idempotent Ingest，数据库故障不直接丢账 |
| Dependency Degradation | Cache/Rate/Budget/Secret/DB/PubSub/Provider 分别定义 fail-open/fail-closed/bypass/fallback |
| Multi-AZ / DR | Standard ≥2 AZ；Enterprise ≥3 AZ，支持 Region DR / 可选 Multi-Region Active |
| Upgrade HA | Graceful Drain、SSE 长流保护、PDB、Topology Spread、Expand→Contract Migration |
| HA Release Gate | 故障注入、Failover、LKG、Spool Replay、Rolling Upgrade、Region DR 纳入 Chaos/RC 验收 |
| Federated Agent | Agent 资源显式区分 `INTERNAL / EXTERNAL_FEDERATED`；外部 Agent 是 Tenant 内受控投影，不等于本地运行资产 |
| Federation Trust | Discovery ≠ Trust；启用外部 Agent 前必须建立 Tenant 级 Trust Relationship，并至少具备一个有效 Verified Trust Anchor |
| Bidirectional A2A | 同时治理 Outbound 内部→外部 Agent 与 Inbound 外部→内部 Agent；外部调用方不得自报内部 User/Delegation |
| External FinOps | 外部 Agent 成本既计入 Task 总成本，又进入独立 External Procurement Budget/Cost 维度 |
| Task Consistency | Task Counter 显式选择 `regional / global_soft / global_hard`，复用统一 Counter/Quota Authority 基础设施 |
| Trust Failure | Trust/Revocation/Identity 校验失败默认向更保守方向降级；不得套用普通 Cache/Analytics 的 fail-open 哲学 |

---

# 1. 产品定位与边界

## 1.1 产品定位

LiteAIG 提供统一企业 AI Interaction 治理入口，对模型供应商、Tool、Agent 的协议、身份、权限、区域、成本和运行差异进行收敛，对下游应用与 Agent Runtime 提供稳定统一的治理边界，并在网关层完成：

- 多 Provider / 多模型统一接入；
- Logical Model 与 Deployment 管理；
- 智能路由、Credential Pool 与 Fallback；
- Timeout、Retry、Circuit Breaker、Concurrency Control；
- Exact Cache、Semantic Cache、Provider Prompt Cache Awareness；
- Token 计量、Provider Cost、Chargeback、Budget、Quota、Rate Limit；
- Prompt / Context / Response / Stream / Tool / Agent Guardrail；
- Request Explorer、Metrics、Trace、Alert、Audit；
- Draft → Validate → Publish → Activate → Rollback 配置生命周期；
- Model / Tool / Agent / Human Approval 四类 Interaction 的统一治理；
- Agent Identity、Delegation、Task / Session / Agent Graph 与 Agentic FinOps；
- External / Federated Agent Trust、跨组织 A2A 双向治理与外部 Agent Procurement Cost；
- MCP（Agent→Tool）与 A2A（Agent→Agent）协议适配；
- Web Console + Responsive PWA 管理；
- Playground、Live Tail、Routing Explain、Guardrail Impact Preview 与 Config Diff 形成“配置→预演→发布→观测→回滚”的闭环。

## 1.2 明确不做

LiteAIG 不承担以下职责：

- Agent Runtime / Agent Workflow；
- RAG Pipeline；
- Prompt IDE；
- 模型训练、微调、评测平台；
- 完整语音 RTC 平台；
- 通用工作流引擎；
- 应用市场。

MCP / A2A / Tool / Agent 只做连接、身份、授权、安全、预算、可靠性、审计与可观测治理；LiteAIG 不承担 Agent Planner、Task Decomposition、Agent Memory、Agent Workflow 或 Agent Runtime。

## 1.3 部署档位

| 档位 | 核心存储/协调 | HA 基线 | 推荐用途 | 建议规模 |
|---|---|---|---|---|
| Lite | SQLite + Memory | 单实例；支持本地 LKG / graceful shutdown；不承诺基础设施 HA | 开发、小团队、边缘 | ≤200 RPS |
| Standard | PostgreSQL HA + Valkey/Redis HA | Gateway ≥3、Control ≥2、≥2 AZ、LKG、Accounting Spool、PDB/TopologySpread | 主流生产 | ≤5,000 RPS/副本集 |
| Enterprise | PostgreSQL Multi-AZ + Valkey/Redis Cluster | Gateway/Control Active-Active、≥3 AZ、DR Region、可选 Multi-Region Active | 大规模多租户/关键业务 | 水平扩展，50k+ RPS |

Enterprise 高量 Analytics 可选 ClickHouse / Kafka-compatible Sink / Object Storage，不作为请求热路径和 Data Plane Ready 的强依赖。

高可用能力是 `CORE correctness`，不是单独收费功能开关；不同档位的差异是故障域、依赖部署形态和 SLO 目标。

## 1.4 产品竞争定位

LiteAIG 不以“Provider 数量最多”“插件数量最多”“通用网关能力最全”为目标。产品定位冻结为：

> **Enterprise AI Governance & Traffic Control Plane**

三类产品的边界应明确区分：

| 类型 | 典型优势 | LiteAIG 策略 |
|---|---|---|
| Model Access / LLM Proxy（如 LiteLLM） | Provider 适配广度、SDK/协议兼容、长尾模型接入 | 不正面复制 100+ Provider 适配器；原生做好主流 Provider，长尾通过 OpenAI-Compatible 或 Bridge 接入 |
| Cloud-native/API Gateway（如 Higress/Envoy 类） | Ingress、Gateway API、WAF、Service Mesh、Wasm、网络层高性能 | 不重新实现通用网关；允许部署在其后方，由其负责网络治理，LiteAIG 负责 AI 治理 |
| LiteAIG | Model/Tool/Agent 统一治理、成本归因、安全生命周期、可解释调度、AI 运维闭环 | 集中研发资源做深，不追求功能数量优势 |

产品对外定位使用一句话表达：

> **LiteAIG 是面向 Models、Tools 与 Agents 的统一 Enterprise AI Governance & Traffic Control Plane。**

内部仍保留八个 Engine 作为工程架构，但市场与产品层不以“八个引擎”作为卖点，避免把基础能力包装成差异化能力。

## 1.5 六个必须做到领先的核心产品能力

### A. AI Access

目标：企业应用 5 分钟内完成接入，底层 Provider 可替换而不修改业务调用代码。

必须做到：

- OpenAI / Anthropic 主协议高兼容；
- 主流 Provider 原生适配；
- OpenAI-Compatible 长尾接入；
- Logical Model 隐藏底层 Provider 差异；
- Virtual Key、Tenant/Project Scope、Model ACL；
- Provider Test / Model Discovery / Setup Wizard；
- 兼容官方 SDK，仅修改 `base_url` 即可使用。

**Provider 数量不是 KPI；成功接入率、协议兼容率、首次调用时间才是 KPI。**

### B. Explainable Smart Routing

目标：不仅自动选择模型，而且能够回答“为什么选它、如果换策略会怎样”。

必须做到：

- Hard Constraints：能力、Context、ACL、Region、Residency、Health；
- Soft Score：Cost / Latency / Load / Quality / Cache Affinity；
- Fallback / Credential Rotation / Circuit；
- Route Explain Trace；
- Routing Simulator 与生产 `PlanRoute()` 复用同一实现；
- 每次路由决策可在 Request Explorer 中解释；
- 后续可基于真实运行数据优化权重，但不得引入无法解释的黑盒自动调度作为默认策略。

### C. Enterprise AI FinOps

目标：任何一笔 AI 消费都能回答“谁、哪个部门、哪个项目、哪个应用/Agent、哪个模型、为什么花了这些钱”。

核心维度：

```text
Tenant
Project
Application / Agent / API Key
OrgUnit / User
Logical Model / Provider / Deployment
Token / Cache / Retry / Fallback / Guardrail Regeneration
Provider Cost / Customer Charge
```

必须做到：

- Usage Ledger 是唯一计量事实来源；
- Token、价格版本、Provider Cost、Chargeback 可对账；
- Project × Department / User / Agent / Federated Relationship 等交叉分析；
- Budget / Quota / RateLimit 与 Usage Ledger 闭环；
- Cache Savings、Retry Cost、Fallback Cost 可量化；
- Attribution Trust 明确展示，客户端自报身份不进入正式财务归因。

### D. Guardrail Lifecycle

目标：安全能力不是一个过滤插件，而是一套可配置、可测试、可发布、可观测、可优化的运行治理系统。

生命周期：

```text
Policy
  → Test Case / Playground
  → Impact Preview
  → Diff / Validate
  → Publish / Security Epoch
  → Runtime Enforcement
  → Security Event
  → False Positive / Retroactive Analysis
  → Policy Optimization
```

必须覆盖 Input / Context / Response / Stream / Tool / MCP，并保持“本地快速规则、缓冲本地检测、异步外部检测”的三层流式模型。

### E. AI Operations

目标：让企业人员不依赖抓包、日志搜索和手写 curl 就能理解和处理 AI 请求问题。

核心能力：

- Playground；
- Request Explorer；
- Live Tail；
- Decision Timeline；
- Routing Simulator；
- Config Diff；
- Guardrail Impact Preview；
- Health / Circuit / Alert；
- Config Rollback；
- Project Center 360° 视图。

### F. Agentic Governance & Audit

目标：统一治理 Agent→Model、Agent→Tool、Agent→Agent 交互，让企业能够回答“谁代表谁、调用了谁、为什么允许、花了多少钱、造成了什么后果”。MCP 与 A2A 是首批协议实现，但核心治理模型不绑定协议。

必须做到：

- Agent Registry / Capability / Endpoint / Agent Card；
- Agent Identity 与 Service Account 绑定；
- Delegation Chain，权限只允许收缩，不允许通过 Agent Handoff 放大；
- Tool ACL 与 Agent ACL 复用 System→Tenant→Project→Agent Policy 继承；
- `TOOL_REQUEST/TOOL_RESULT` 与 `AGENT_REQUEST/AGENT_RESPONSE/AGENT_HANDOFF` 检查点；
- Task / Root Task / Session / Parent Request 一致关联；
- Task Budget、Token/Cost/Duration/Model Call/Tool Call/Agent Hop 上限；
- Loop Detection 与异常调用链终止；
- Agent-to-Agent Request Explorer / Agent Graph；
- External / Federated Agent 的 Discovery→Review→Trust→Use→Suspend/Revoke 生命周期；
- 外部 Agent 的 Inbound / Outbound 双向信任、Data Boundary、Capability Grant 与 Procurement Budget；
- Task-level / Agent-level / Tool-level FinOps；
- 高风险 Tool/Agent Action 支持 `REQUIRE_APPROVAL`；
- 不运行 Agent Workflow，不持有业务 Agent Memory，不承担 Planner。

缓存、限流、Token Metering、Retry、熔断等能力是上述六项能力的底层基础设施，不单独作为产品差异化卖点。

## 1.6 Build / Integrate / Do-Not-Build 决策

为了避免重复发明轮子，后续所有开发任务必须先归入以下三类。

### Build —— LiteAIG 必须自己掌握的核心

- Tenant/Project/Identity Scope 与授权；
- Logical Model / Deployment / Credential Pool；
- RuntimeSnapshot 与 Config Draft/Publish/Rollback；
- Explainable Smart Routing；
- Resilience 调度语义；
- Usage Ledger / Budget Reservation / FinOps Attribution；
- Guardrail Engine 的执行、策略和生命周期管理；
- Request Explorer / Playground / Simulator / Diff；
- AI 语义的审计、安全事件和运营闭环；
- Agentic Governance 的 Identity、Delegation、Task、ACL、Provenance、Runtime Audit、Agent Graph 与 Approval Handoff 语义；
- Federated Agent 的 Trust Relationship、Trust Anchor、Capability/Project Grant、Data Boundary、External Cost 与 Revocation 语义。

### Integrate —— 优先复用成熟生态

- 长尾 Provider：OpenAI-Compatible，必要时 LiteLLM Bridge；
- 通用 Ingress / WAF / Gateway API：Higress / Envoy / Nginx 等外部网关；
- Metrics：Prometheus；
- Trace：OpenTelemetry + Tempo/Jaeger/第三方 APM；
- Log/Analytics：Loki/Elasticsearch/ClickHouse/Object Storage；
- External Guardrail：云厂商或第三方安全服务；
- Identity：OIDC/SAML/SCIM；
- Secret：KMS/Vault；
- Notification：Webhook/Email/Slack/Teams/PagerDuty 等；
- Semantic Cache 向量后端：pgvector / Redis Vector / 外部 Vector Store；
- Agent-to-Agent：A2A Protocol 1.0 Adapter；
- Agent-to-Tool：MCP 2026-07-28 Adapter；
- Agent Runtime：LangGraph/CrewAI/AutoGen/ADK/OpenAI Agents SDK/企业自研 Runtime 等通过协议接入。

### Do Not Build —— 明确禁止扩张的方向

- 100+ Provider 原生适配器竞赛；
- 通用 API Gateway / Ingress Controller；
- Service Mesh；
- 通用 WAF；
- 任意 Wasm/JS Plugin Runtime；
- 自建 Trace Storage / Metrics TSDB；
- RAG / Vector Database；
- Agent Runtime / Workflow；
- Prompt IDE；
- 模型训练/微调平台；
- 完整财务发票、税务和 ERP Billing；
- 自建 Identity Provider；
- MCP Tool Runtime，本产品只做 Proxy/Governance；
- Multi-Agent Planner / Team Runtime / Task Decomposition / Memory / Reasoning Engine；
- 以 LLM 自动替代业务 Orchestrator 决定“该把任务拆给哪些 Agent”的黑盒编排。

任何新增需求落入 Do-Not-Build 范围时，默认拒绝进入 Core Backlog；只有经过新的产品决策才能解冻。

## 1.7 产品成功标准

产品成熟度不以“功能是否存在”衡量，而以真实场景闭环衡量：

1. **接入成功**：企业应用能否在 5 分钟完成第一个真实模型调用；
2. **故障可控**：Provider 429/timeout/5xx 时能否稳定切换且决策可解释；
3. **成本可回答**：能否在 1 分钟内回答某 Tenant/部门/人员/项目/Agent 的 Token 和成本；
4. **安全可治理**：策略能否 Test→Preview→Publish→Observe→Rollback，而不是只能“打开一个过滤器”；
5. **问题可定位**：一次慢请求/失败请求能否在 Request Explorer 中直接定位是 Gateway、Guardrail、Routing 还是 Provider 导致；
6. **Agentic 可治理**：能否回答“哪个用户/Agent 通过怎样的 Delegation / Federation Trust 调用了哪个内部或外部 Agent/Tool、为何允许/拒绝、是否跨越组织/数据边界、一个 Task 总共花了多少钱、是否出现异常循环或越权”。

## 1.8 Trust & Assurance

企业版发布必须同时提供功能与可验证证据，认证本身不作为代码 Feature DoD。

### 1.8.1 Guardrail Benchmark

每个 Release 对内置 Guardrail 生成版本化报告，至少包含：

- corpus/version；
- precision / recall / false-positive rate / false-negative rate；
- 按类别与语言拆分结果；
- 测试参数、阈值和模型版本；
- 与上一版本的回归差异。

客户可使用自有测试集运行同一 Benchmark Harness。报告禁止承诺“零误报/零漏报”。

### 1.8.2 Evidence Export

Enterprise 提供只读 Evidence Export，导出指定 Tenant/时间范围内：

- Config Version / Policy Version / Security Epoch；
- RBAC / Role Assignment；
- Audit Event；
- Guardrail Event；
- External Guardrail / Provider 数据处理目的地与策略；
- Secret Rotation / Key Rotation 记录；
- Deployment / Release 版本；
- Federated Agent Relationship / Trust Anchor / Review / Suspend-Revoke / Data Boundary 变更历史。

导出内容默认不包含 Prompt/Response 正文和 Secret。

### 1.8.3 数据处理约束

默认：

- Prompt/Response 不用于 LiteAIG 内部或第三方模型训练；
- 只有 Tenant 显式 opt-in 的 replay-eligible 样本可用于该 Tenant 内的 Guardrail 预演/优化；
- Adaptive Governance 只使用当前 Tenant 数据，不做跨 Tenant 学习或统计共享；
- External Guardrail 数据出境必须执行 DLP、Residency 和 Provider Policy，并在 Request/Security Trace 中可追溯；
- External/Federated Agent 调用视为独立第三方数据处理边界；必须在调用前执行 DLP、Residency、Capability Grant 与 Data Processing Profile 检查。

### 1.8.4 Certification Track

SOC 2 Type II、ISO 27001、GDPR DPA 等作为独立组织/合规项目推进。产品必须提供其所需的访问控制、审计、变更管理、Secret 管理、数据保留和 Evidence Export 能力，但不得把“通过认证”写成单个软件迭代的完成条件。

## 1.9 Agentic Architecture 原则

当前架构把 Agentic 能力作为既有 Gateway 治理对象扩展，而不是建立第二套 Agent 平台。

冻结原则：

1. **Orchestrator 决定做什么，LiteAIG 决定能不能做、怎么连接、花多少钱、是否安全、如何审计。**
2. 核心治理对象固定为 `Model / Tool / Agent / Human Approval` 四类 Interaction；协议只是 Adapter。
3. MCP 是 Agent→Tool 首选协议；A2A 是 Agent→Agent 首选协议；HTTP/gRPC 可作为兼容 Adapter。
4. Agent Identity、Delegation、Task、Cost、Trace 属于 Gateway Core Governance；Agent Reasoning、Planning、Memory、Workflow 不属于 Gateway。
5. 多智能体能力不得破坏现有固定七阶段 Pipeline；通过 Interaction Context 在既有 Engine 中执行。
6. Agent Delegation 权限始终采用交集语义，任何 Handoff 不得产生 Privilege Escalation。
7. Agentic 自适应优化仍只生成 Recommendation，不自动修改路由、ACL、Budget 或 Guardrail。
8. **Agent 仍是四类 Interaction 中的 `Agent`，Internal/External 通过 `TrustBoundary` 区分，不新增第五种 InteractionKind。**
9. Discovery 只产生 Candidate Resource，不能自动产生 Trust；任何跨组织 Agent 使用必须经过显式 Trust Relationship。
10. Local Delegation 只描述“本 Tenant 内谁授权谁”；外部 Agent 的远端 Policy 不属于 LiteAIG 可见事实，不得伪装成可参与本地交集计算。
11. 外部 Agent 调用必须同时满足 Local Caller Permission、Delegation（如有）、Federated Trust Grant、Project/Capability Grant 与 Data Boundary Policy。
12. A2A Agent Card 的 JWS 签名属于协议支持能力而非唯一信任根；LiteAIG 对 Active External Relationship 要求至少一种经过验证的 Trust Anchor（JWS/mTLS/OIDC/Registry Attestation 等），禁止 `unverified` 直接 Active。

协议基线（2026-08-28）：

- MCP：`2026-07-28`，优先使用 stateless Streamable HTTP、`Mcp-Method/Mcp-Name`、`server/discover` 与 Tasks extension；Legacy SSE 仅兼容，不用于新部署。
- A2A：`1.0.0`，支持 Agent Card Discovery、可选 JWS Signed Agent Card、Task/Message/Artifact、Streaming/Push 与协议扩展。LiteAIG 的外部联邦信任策略可以比协议默认更严格，但不得把 JWS 写成唯一可接受的企业信任方式。

## 1.10 模块化设计原则

V8.5 继承并细化 **Modular Monolith**：

- 默认一个 `liteaig` 二进制；
- 模块按职责和数据所有权隔离，而不是按部署单元拆分；
- `mode=all|gateway|control` 只是运行模式，不意味着内部 RPC 微服务化；
- 只有出现独立扩缩、故障隔离或合规边界的真实需求时，才允许把模块拆成独立进程；
- 模块之间优先通过 Go interface / immutable contract / domain event 交互；
- 禁止为了“看起来模块化”引入内部 HTTP/gRPC 回环。

模块化目标：

1. 限制变化半径；
2. 让协议、Provider、Agentic 能力可扩展而不污染 Kernel；
3. 保持单二进制部署和本地函数调用性能；
4. 让每个数据表、配置对象和业务规则只有一个明确 Owner；
5. 通过依赖规则而不是目录层级本身保证边界。

### V8.5 补充：服务层三分与微内核

在 V8.2 模块划分之上，Admin API / Gateway 的 HTTP 服务实现固定为三层：

```text
┌────────────────────────────────────────────────────┐
│ 表示层 Presentation          internal/controlplane/adminapi   │
│   HTTP 路由 / 会话 / CSRF / rbac 中间件 / 错误渲染 / SSE 帧 │
├────────────────────────────────────────────────────┤
│ 业务编排层 Business Orchestration  internal/controlplane/backend │
│   ControlBackend 编排 + 12 窄能力接口 + 视图契约（View 类型）  │
├────────────────────────────────────────────────────┤
│ 持久层 Persistence           internal/platform/storage/*       │
│   sqlrepo.Store 统一仓库入口 + sqlite/postgres 纯连接适配器    │
└────────────────────────────────────────────────────┘
         ↑ 依赖只能自上而下；装配根 internal/app 除外（组合根可依赖全部层）
```

冻结规则：

- 表示层不持有业务编排逻辑（handler 保持薄：取参 → 调能力 → 渲染）；
- 业务编排层不 import 表示层、网关或装配层（`backend-orchestration-is-presentation-free`）；
- 持久层适配器不 import 任何表示层类型（`storage-does-not-use-presentation`）；领域接口定义在领域包（`identity.LocalCredentialStore`、`audit.Repository`、`tenancy.Repository`），存储包只做实现；
- HTTP 服务框架统一为 `internal/platform/webkit` 微内核：路由、上下文、中间件、错误渲染、Recover、安全头；内核对业务包零依赖（`webkit-is-implementation-free`），不引入 Echo 等第三方 Web 框架；
- 组合根 `internal/app` 负责打开连接、迁移、构造 `sqlrepo.Store`、装配 `ControlBackend` 与 HTTP 服务；组合根以外，任何业务文件不得 import 表示层。

---

# 2. 总体架构

## 2.1 八个核心引擎

```text
┌──────────────────────────────────────────────────────────────┐
│                  Web Console / Mobile PWA                    │
├──────────────────────────────────────────────────────────────┤
│ ① Access Engine        Model / MCP / A2A / Auth / Identity    │
│ ② Guardrail Engine     Prompt / Context / Tool / Agent        │
│ ③ Cache Engine         Exact / Semantic / Prompt Cache        │
│ ④ Routing Engine       Model / Provider / Agent Endpoint      │
│ ⑤ Resilience Engine    Retry / Circuit / Fallback / Timeout   │
│ ⑥ FinOps Engine        Token / Tool / Task / Cost / Budget    │
│ ⑦ Policy Engine        ACL / Delegation / Tenant / Residency  │
│ ⑧ Observability Engine Request / Task / Agent Graph / Audit   │
├──────────────────────────────────────────────────────────────┤
│ Config Plane        RuntimeSnapshot / Publish / Rollback     │
│ Data Plane          Stateless Hot Path                        │
│ Operations Plane    Alert / Audit / Health / Maintenance      │
│ HA / DR Plane        LKG / Spool / Drain / Failover / Recovery  │
└──────────────────────────────────────────────────────────────┘
```

## 2.2 Control Plane / Data Plane

```text
                         Admin / Console LB
                                │
                       ┌────────┴────────┐
                       ↓                 ↓
                  Control-1         Control-N
                  CRUD/RBAC          Compiler
                       └────────┬────────┘
                                │
                         Signed RuntimeBundle
                         version/checksum/signature
                                │
                     Prepare / ACK-NACK / Activate
                                │
          ┌─────────────────────┼─────────────────────┐
          ↓                     ↓                     ↓
      Gateway-1             Gateway-2             Gateway-N
      Active                Active                Active
      Local LKG             Local LKG             Local LKG
          │                     │                     │
          └────────────── Regional / External LB ─────┘
                                │
                   Models / MCP Tools / A2A Agents
```

核心不变量：

```text
Control Plane unavailable
    !=
Data Plane unavailable
```

Control Plane / PostgreSQL / PubSub 故障时，Data Plane 必须能够使用已验证的 Last Known Good RuntimeBundle 继续处理已允许的请求。此时配置写入、发布和依赖控制面数据库的管理操作暂停，但已加载 Runtime 不应被清空。

Data Plane 默认 Active-Active / Shared-nothing：

- Gateway 实例之间无 Master/Leader；
- 任一 Ready 实例可处理任意 Tenant 请求；
- Tenant Runtime 由不可变 Snapshot 构成；
- 临时请求状态保存在请求上下文或外部协调层；
- 任何单个 Gateway Crash 不得要求“接管 Leader”。

Control Plane CRUD/API 可 Active-Active；以下协调型任务必须采用 Lease/Leader Election 或分片 Ownership：

- Config Publish coordinator；
- Reservation Sweeper；
- Retention / Rollup；
- Alert evaluation；
- Scheduled Recommendation；
- DB Migration coordinator。

### 运行模式

- `mode=all`：Control Plane 与 Data Plane 同进程，默认模式；
- `mode=gateway`：仅 Data Plane；
- `mode=control`：仅 Control Plane。

`mode=all` 下内部直接 Go 函数调用，不做网络回环。

## 2.3 多租户 Runtime Registry

Data Plane 不直接访问 Control Plane ORM，不在每请求热路径查询业务数据库。运行时采用“平台级索引 + Tenant 独立快照”模型，避免任一租户变更导致全局 RuntimeSnapshot 重编译。

```go
type RuntimeRegistry struct {
    Global  atomic.Pointer[GlobalRuntime]
    Tenants TenantRuntimeIndex // tenant_ref -> atomic.Pointer[TenantRuntimeSnapshot]
}

type GlobalRuntime struct {
    Version             int64
    PublishedAt         time.Time
    TenantLocators      TenantLocatorIndex
    SystemSecurityEpoch int64
    SystemPolicy        CompiledSystemPolicy
    SharedProviders     SharedProviderIndex
    PricingCatalog      PricingIndex
}

type TenantRuntimeSnapshot struct {
    TenantID            string
    TenantRef           string
    Version             int64
    SecurityEpoch       int64
    PublishedAt         time.Time
    Status              TenantStatus

    Projects            ProjectIndex
    APIKeys             APIKeyIndex
    Users               UserIndex
    OrgUnits            OrgUnitIndex
    Applications        ApplicationIndex
    Agents              AgentIndex
    AgentEndpoints      AgentEndpointIndex
    AgentCapabilities   AgentCapabilityIndex
    FederatedAgents     FederatedAgentIndex
    FederationRelations FederationRelationshipIndex
    FederationAnchors   FederatedTrustAnchorIndex
    FederationGrants    FederatedGrantIndex
    DelegationPolicies  DelegationPolicyIndex
    ToolPolicies        ToolPolicyIndex
    TaskPolicies        TaskPolicyIndex
    ServiceAccounts     ServiceAccountIndex

    Providers           ProviderIndex
    Credentials         CredentialIndex
    LogicalModels       LogicalModelIndex
    Deployments         DeploymentIndex

    RoutePolicies       RoutePolicyIndex
    GuardrailPolicies   GuardrailPolicyIndex
    CachePolicies       CachePolicyIndex
    BudgetPolicies      BudgetPolicyIndex
    RatePolicies        RatePolicyIndex

    EffectiveSystemPolicy CompiledSystemPolicy
}
```

### 运行时解析

Virtual Key 中包含不可猜测的 `tenant_ref`：

```text
sk-lia-v1_<tenant_ref>_<key_public_id>_<secret>
```

请求开始时：

```text
token
  → tenant_ref
  → RuntimeRegistry.Tenants.Get(tenant_ref)
  → TenantRuntimeSnapshot
  → APIKeyIndex.Get(key_public_id)
  → verify HMAC
  → Project / Identity / Policy
```

`tenant_ref` 为随机公开标识，不使用数据库自增 ID、企业名称或可推断业务信息。

`tenant_ref` 对应 `tenants.public_ref`，创建后不可修改；Tenant Runtime Registry 以此作为外部定位键。

### 快照更新

- Tenant 配置发布只原子替换该 Tenant 的 `TenantRuntimeSnapshot`；
- System Shared Provider / System Policy / Pricing 变更时，Control Plane 计算依赖 Tenant，仅重编译受影响 Tenant；
- 每个请求固定读取一次 Tenant Snapshot 指针，在途请求不切换版本；
- Request Explorer 必须记录 `tenant_snapshot_version`、`tenant_security_epoch` 和 `system_runtime_version`；
- Tenant 被 `suspended` 后发布最小封禁快照，新请求在 Admission 拒绝，在途请求按配置完成或取消。

该模型确保单租户配置变更不会造成全平台配置抖动，并为 SaaS 大规模租户提供独立配置版本、独立安全策略和独立故障域。

### 2.3.1 RuntimeBundle

Control Plane 发布的运行配置必须封装为不可变 RuntimeBundle：

```go
type RuntimeBundle struct {
    SchemaVersion        string
    TenantID             string
    TenantRef            string
    ConfigVersion        int64
    SecurityEpoch        int64
    SystemRuntimeVersion int64
    PublishedAt          time.Time

    PayloadChecksum      string
    Signature            []byte
    Snapshot             *TenantRuntimeSnapshot
}
```

约束：

- `PayloadChecksum` 覆盖完整规范化 Payload；
- Standard/Enterprise Bundle 必须签名，Data Plane 在加载前验签；
- Snapshot Schema 必须声明兼容版本；
- Secret 默认以 `secret_ref` / 已授权的加密材料存在，禁止将长期明文 Secret 写入 Bundle 文件；
- Config 切换以 Bundle 为原子单位，禁止 Route/Guardrail/Budget/Credential 分批生效。

### 2.3.2 Prepare / Activate

```text
Compile Bundle v105
  → Sign
  → Distribute
  → Data Plane Prepare
       ├─ schema compatibility
       ├─ signature/checksum
       ├─ references
       └─ local resource validation
  → ACK / NACK
  → Atomic Activate
```

单个节点 NACK 不得使其加载半成品；该节点继续运行旧版本并进入 `config_drift`。

### 2.3.3 Last Known Good

每个 Data Plane 本地持久化：

```text
/data/runtime/
  global/
    active.bundle
    previous.bundle
  tenants/
    <tenant_ref>/
      active.bundle
      previous.bundle
```

要求：

- 写文件采用 temp + fsync + atomic rename；
- 启动时优先验证 Control Plane 当前版本；不可达时可从签名有效的 LKG 启动；
- `active.bundle` 损坏时尝试 `previous.bundle`；
- LKG 内容按磁盘加密/权限策略保护；
- LKG 只用于已知配置继续服务，不允许绕过 Tenant suspended/security epoch 等明确 fail-closed 策略。

### 2.3.4 Readiness

Data Plane Ready 不得错误依赖 PostgreSQL、Control Plane 或所有 Provider 同时健康。

默认：

```text
Ready =
  process not draining
  AND at least one valid runtime is loaded
  AND local runtime/kernel healthy
  AND strict security epoch policy satisfied
```

Provider 故障由 Circuit/Fallback 处理；Control Plane/DB 暂时不可用不应导致所有 Gateway 同时 NotReady。

## 2.4 与 LiteLLM / Higress 类产品的协同拓扑

LiteAIG 允许与现有基础设施协同部署，不要求客户迁移或替换已有通用网关。

### 长尾 Provider Bridge

```text
Application
   ↓
LiteAIG
   ├─ Native Provider: OpenAI / Anthropic / Gemini / Azure / Bedrock / Vertex
   ├─ OpenAI-Compatible: DeepSeek / Qwen / Moonshot / vLLM / Ollama / ...
   └─ Optional LiteLLM Bridge
          ↓
      Long-tail Providers
```

Bridge 只承担 Provider Aggregation，不承担 LiteAIG 的 Tenant、Routing、FinOps、Guardrail、Audit 主语义。LiteAIG 始终是最终治理与计量入口。

### 通用网络网关协同

```text
Internet / Enterprise App
        ↓
Higress / Envoy / Nginx / Existing API Gateway   (optional)
TLS / WAF / Ingress / Gateway API / Network Policy
        ↓
LiteAIG
Tenant / Virtual Key / AI Routing / FinOps / Guardrail / AI Audit
        ↓
LLM / MCP
```

原则：**网络治理交给通用网关，AI 语义治理留在 LiteAIG。** LiteAIG 不以替换企业已有 API Gateway 为销售或技术前提。

---

## 2.5 Unified Interaction Model

当前 Data Plane 统一使用 Interaction 语义，不为 Model/MCP/A2A 建立平行 Pipeline。

```go
type InteractionKind string

const (
    InteractionModel    InteractionKind = "model"
    InteractionTool     InteractionKind = "tool"
    InteractionAgent    InteractionKind = "agent"
    InteractionApproval InteractionKind = "approval"
)

type TrustBoundary string
type InteractionDirection string

const (
    TrustBoundaryInternal          TrustBoundary = "internal"
    TrustBoundaryExternalFederated TrustBoundary = "external_federated"

    DirectionInternal InteractionDirection = "internal"
    DirectionInbound  InteractionDirection = "inbound"
    DirectionOutbound InteractionDirection = "outbound"
)

type FederationContext struct {
    RelationshipID   string
    ExternalAgentID  string
    AssuranceLevel   string
    DataBoundary     string
    TrustAnchorID    string
}

type InteractionContext struct {
    Kind            InteractionKind
    Protocol        string // openai|anthropic|mcp|a2a|http|grpc
    TrustBoundary   TrustBoundary
    Direction       InteractionDirection

    TenantID        string
    ProjectID       string
    SessionID       string
    TaskID          string
    RootTaskID      string
    ParentTaskID    string
    ParentRequestID string

    Caller          PrincipalRef
    Target          ResourceRef
    Delegation      *DelegationContext
    Federation      *FederationContext
}
```

四类 Interaction 共享 Tenant Scope、Policy、Budget、Guardrail、Accounting 与 Audit，但各自拥有协议 Adapter 和专项策略。Core Engine 只能依赖规范化后的 `InteractionContext`，不能直接依赖 MCP/A2A Wire Type。

## 2.6 V8.5 模块架构

```text
liteaig
│
├─ Kernel
│   ├─ Pipeline
│   ├─ Runtime Registry / Snapshot
│   ├─ Interaction / Request Context
│   └─ Stable Contracts
│
├─ Access
│   ├─ Ingress Protocol Adapters
│   └─ Authentication / Principal Resolution
│
├─ Governance Domains
│   ├─ Policy Resolver
│   ├─ Guardrail
│   ├─ Budget / Rate / Quota
│   ├─ Delegation
│   ├─ Federation Trust
│   ├─ Task Governance
│   └─ Human Approval
│
├─ Decision / Runtime Domains
│   ├─ Routing
│   ├─ Resilience
│   ├─ Cache
│   └─ FinOps
│
├─ Resource Domains
│   ├─ Tenancy / Organization
│   ├─ Identity
│   ├─ Model Catalog
│   ├─ Agent Registry
│   └─ Tool Registry
│
├─ Connectors
│   ├─ Model Connectors
│   ├─ Tool Connectors
│   └─ Agent Connectors
│
├─ Control Plane
│   ├─ Admin API (Presentation)      webkit 路由 / 会话 / rbac / 错误渲染
│   ├─ Backend (Business)            ControlBackend 编排 + 窄能力接口 + 视图契约
│   ├─ Config Draft / Compiler
│   ├─ Setup Wizard
│   ├─ RBAC
│   └─ Approval
│
├─ Resource Domains (补充)
│   ├─ Catalog                       Provider/Model/Region 资源目录视图
│   └─ Federation                    联邦关系 / 信任锚 / Grant
│
└─ Platform
    ├─ WebKit                        HTTP 微内核（零业务依赖）
    ├─ Persistence                   sqlrepo 统一仓库 + sqlite/postgres 适配器
    ├─ Coordination
    ├─ Secrets
    ├─ Telemetry
    └─ Event / Sink
```

### 2.6.1 Kernel

Kernel 只拥有：

- Pipeline Stage Contract；
- `RequestContext / InteractionContext`；
- `RuntimeRegistry / TenantRuntimeSnapshot`；
- 稳定核心 interfaces；
- 生命周期和错误模型。

Kernel **不拥有业务表、不 import Provider SDK、不理解 MCP/A2A Wire Type、不实现 Guardrail/FinOps/Agent 业务规则**。

### 2.6.2 Ingress Protocol Adapter

职责仅限：

```text
Wire Request
  → Parse / Validate Protocol
  → Normalize Unified Interaction
  → Kernel Pipeline
  → Normalize Response
  → Wire Response
```

禁止：

- Protocol Adapter 直接查询 Tenant Repository；
- Protocol Adapter 直接调用 Guardrail / Budget / Routing；
- 在 MCP/A2A Adapter 中实现业务 ACL；
- 让 Core 结构依赖协议专有 DTO。

### 2.6.3 Connector

Connector 负责“如何调用目标”，不负责“是否允许调用”。

```go
type InteractionInvoker interface {
    Invoke(context.Context, InvocationRequest) (*InvocationResponse, error)
    Stream(context.Context, InvocationRequest, StreamWriter) error
    Health(context.Context, TargetRef) HealthStatus
    Capabilities(context.Context, TargetRef) CapabilitySet
    NormalizeError(error) *UpstreamError
}
```

实现：

- Model Connector；
- MCP/Tool Connector；
- A2A/Agent Connector。

Policy / Guardrail / Budget / Routing 在调用 Connector 之前由 Pipeline 完成。

### 2.6.4 Canonical Identity

系统只有一个 Principal/Identity 领域：

```text
Principal
├─ User
├─ Application
├─ Service Account
├─ Agent
├─ API Key
└─ System
```

`AgentIdentity` 是 Canonical Identity 的一种类型，不允许在 `agentic/identity` 再维护第二份主体模型。Agent-specific metadata 由 Agent Registry 管理，认证与主体解析由 Identity/Auth 管理。

### 2.6.5 Governance 不做“大包”

以下领域保持独立：

```text
Guardrail
Policy
Budget/Rate
Delegation
Task
Approval
Recommendation
```

它们可共同参与一次 Pipeline，但不得合并成单一 `governance` God Module。跨领域协调由 Pipeline/Application Service 完成。

## 2.7 模块依赖与数据所有权

### 2.7.1 依赖方向

```text
Protocol Adapter
      ↓
    Kernel
      ↓
Application / Pipeline Orchestration
      ↓
Policy / Guardrail / Routing / Resilience / Cache / FinOps
      ↓
Connector Contracts
      ↓
External Model / Tool / Agent

Control Plane
      ↓ compile
Immutable RuntimeSnapshot
      ↓
Data Plane
```

禁止反向依赖：

- Connector → Routing/FinOps/Guardrail；
- Domain → Admin API/Web Console；
- Data Plane → Control Plane ORM；
- Protocol → Governance Implementation；
- Domain → PostgreSQL/Redis concrete client；
- 业务模块直接调用 Observability 具体实现；

V8.5 新增（全部由 CI import graph 强制执行）：

- `platform/storage/*` → `controlplane/adminapi`（存储层不得引用表示层类型）；
- `controlplane/backend` → `adminapi / gateway / app`（业务编排层不得依赖表示层、网关或装配层）；
- `platform/webkit`、`catalog`、`observability/audit` → 任何业务/表示包（三个包实现零依赖，只依赖标准库与 catalog 领域类型）;
- `tenancy`、`identity` → `adminapi / gateway / app`（基础领域包不得反向依赖服务层）；
- 装配根 `app` 之外，任何业务包不得 import `adminapi` 视图类型。

### 2.7.2 Telemetry

业务模块只发标准事件/Span：

```go
type EventSink interface {
    Emit(context.Context, DomainEvent)
}
```

OTel、Metrics、Request Log、Analytics Sink 在 Platform/Observability 层消费。

### 2.7.3 Config

Data Plane 只能读取：

```text
GlobalRuntime
TenantRuntimeSnapshot
```

禁止读取 Draft、数据库 Config Row 或 Control Plane Service。Config Plane 负责 Validate / Compile / Publish；运行模块只消费编译后的 immutable view。

### 2.7.4 One Table, One Owner

唯一事实源是 `architecture/table-owners.yaml`（当前 Lite/Standard 实施基线）：

| 数据 | Owner 模块 |
|---|---|
| tenants / projects | tenancy |
| local_admins / api_keys / key_pepper_versions / key_pepper_secrets / secret_material | identity |
| providers / provider_credentials / model_deployments / logical_models / resource_*_catalog | catalog |
| route_policies | routing |
| config_drafts / config_versions / approval_requests | controlplane |
| request_records / audit_events / alerts / alert_rules / tool_call_events | observability |
| usage_events / budget_state / budget_reservations | finops |
| users / org_units / user_org_assignments / cost_centers / tenant_memberships | organization |
| security_events / guardrail_policies | guardrail |
| federation_relationships | federation |

其他模块只能通过 Repository Contract / Query Service / RuntimeSnapshot 使用，不允许跨模块直接修改非本模块表。

V8.5 起该表由 CI 三重校验：

1. **migration owner 校验**（`CheckMigrationOwners`）：每个 `CREATE/ALTER TABLE` 的 migration 文件首行 `-- owner: <module>` 必须与 manifest 一致；
2. **owner 存在性校验**（`CheckTableOwnerModules`）：manifest 中的 owner 必须是 `modules.yaml` 已声明模块；
3. **直接 SQL 引用校验**（`CheckDirectSQL`，V8.5 新增）：非持久层、非 contract-test 的 Go 源码中出现的 `FROM/JOIN/INTO/UPDATE <已知表名>` 字面量，若引用模块不是表 owner 即违规；持久层适配器（`internal/platform/storage/**`）与 contract-test 豁免。


## 2.8 Production HA 核心不变量

1. **Data Plane Active-Active**：无单一 Gateway Leader；
2. **Control Plane Failure Isolation**：CP/metadata DB 故障不清空已加载 Runtime；
3. **Atomic Config**：配置以 RuntimeBundle 原子 Prepare/Activate；
4. **LKG Boot**：Data Plane 可在 CP 不可达时从本地签名 LKG 启动；
5. **Dependency-specific Degradation**：Redis/DB/KMS/Provider/Analytics 各自定义故障语义，不使用一个全局 fail mode；
6. **Durable Accounting**：Provider 已产生 usage 后，DB 写失败必须进入 Durable Spool；
7. **Noisy Neighbor Isolation**：Tenant/Project/Principal 有容量与并发隔离；
8. **Streaming-aware Upgrade**：Rolling Upgrade 必须先 Drain，再终止长流；
9. **Multi-AZ First**：Standard/Enterprise 的 HA 首先在 Region 内解决 AZ 故障，再讨论 Multi-Region；
10. **DR is Tested**：RTO/RPO、Failover、Restore、Region DR 必须通过演练，不以架构图代替验证；
11. **No Fake Exactly-Once**：Usage/Event 采用 At-least-once + Idempotent Dedupe；
12. **No Global Strong Consistency by Default**：跨 Region Budget/Rate 需要显式选择 Regional / Global Soft / Global Hard 语义。

---

## 2.9 HTTP 服务微内核与三层服务架构（V8.5）

### 2.9.1 WebKit 微内核

Admin API 与 Gateway 的 HTTP 服务统一构建在 `internal/platform/webkit` 之上。决策：**不引入 Echo 等第三方 Web 框架**——Go 1.22 `http.ServeMux`（方法+路径模式、`{param}` 通配）已满足路由需求，第三方框架只会增加依赖面而不增加治理价值。

内核 API（冻结）：

```go
type Handler    func(c *Context) error
type Middleware func(next Handler) Handler
type ErrorHandler func(c *Context, err error)

type Engine struct{ /* ... */ }
func (e *Engine) Use(...Middleware)            // 洋葱：后注册者更外层
func (e *Engine) Handle("GET /path/{id}", h)   // 方法 + 模式路由
func (e *Engine) Group(prefix, middlewares...) // 前缀分组 + 组中间件
func (e *Engine) HandleHTTP(prefix, http.Handler) // 挂载外部 Handler（插件）
func (e *Engine) ErrorHandler(fn)              // 统一错误渲染
func (e *Engine) ServeHTTP(w, r)               // 标准 net/http 兼容

type Context struct{ /* ... */ }
func (c *Context) Param(name) string
func (c *Context) JSON(status, v) error        // 幂等：首次写入后拒绝二次写入
func (c *Context) Bind(v, maxBytes) error      // 带上限的 body 解码
func (c *Context) Status/Set/Get/Written/Path  // 状态与请求作用域值

type APIError struct{ Status int; Code string; Params map[string]any; Err error }
func NewAPIError(status, code, params) *APIError
func (e *APIError) Wrap(err) *APIError
```

约束：

- 中间件顺序固定为：全局（Recover 最外 → SecurityHeaders）→ drainGate（运维中间件）→ group 中间件（requireAuth/requireRole/requirePermission）→ route handler；
- 安全头基线：`Cache-Control: no-store`、`X-Content-Type-Options: nosniff`、Admin 附加严格 CSP（`default-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'`）；
- handler 返回 `*APIError` 或领域错误，由 `ErrorHandler` 统一渲染 + 5xx 日志；handler 不得手写错误 JSON；
- panic 由 `Recover()` 兜底为 500，不泄漏堆栈；
- 内核对业务包零依赖（`webkit-is-implementation-free` CI 规则）；新增内核能力必须保持标准库实现。

### 2.9.2 三层服务职责

```text
表示层  internal/controlplane/adminapi
  ├─ Server：webkit Engine + 安全中间件组（requireAuth/requireRole/requirePermission）
  ├─ routes_*.go：薄 handler（取参 → 调窄能力 → jsonResult 渲染）
  ├─ session.go / login.go / oidc_login.go / users.go：会话、本地登录、OIDC、用户管理
  └─ stream.go：SSE 帧（Live Tail / Playground Stream）

业务编排层  internal/controlplane/backend
  ├─ ControlBackend：跨域编排（Dashboard/FinOps 聚合、Recommendation、Playground、
  │   SecurityEvents/ToolCalls 投影、Federation Overlay、Simulator、LiveBus）
  ├─ capabilities.go：12 窄能力接口 + Backend 组合超集
  └─ views_*.go：全部视图契约类型（DashboardView、FinOpsView、KeyResource、
      RuntimeResources、SecurityEventView、ApprovalView、FederationView、
      SimulateRequest/Result、ProjectSummary、DraftSummary、MeView、
      WizardSetupResponse、PlaygroundRequest/Response、StreamEventView …）

持久层  internal/platform/storage
  ├─ sqlrepo.Store：唯一仓库构造点（Open(db, Deps) 一次装配 18 类仓库）
  ├─ sqlrepo 适配器：tenancy/bootstrap/config/apikeys/accounting/security/
  │   guardrail/localauth/pepper/audit/budgetaudit/organization/alerts/
  │   secretvault/approvals/federation + SecretResolver
  ├─ sqlite：纯连接适配器（Open + WAL/busy_timeout 参数），零仓库构造
  └─ postgres：连接适配器 + RLS，仓库实现委托 sqlrepo
```

装配根 `internal/app`（组合根，可依赖全部层）：

```text
sqlite.Open → migrations.Apply → sqlrepo.Open(db, Deps{IDs})
→ identity.EnsurePepper（版本化 pepper 引导）
→ ControlBackend 构造（注入窄依赖：config.Service / accounting / alert / registry / …）
→ adminapi.New(backend, sessionManager)（表示层只接装配好的 Backend）
→ drainGate 中间件挂到根 engine 最外层
```

### 2.9.3 窄能力接口契约

`Backend`（41 方法）拆分为 12 个按路由域划分的窄接口；handler 依赖最窄能力，`Server` 按域持有窄字段：

| 能力接口 | 方法 | 服务路由域 |
|---|---|---|
| SetupService | Setup | 初始化向导 |
| ProjectService | Projects / CreateProject | 项目 |
| DashboardService | Dashboard / FinOps / Recommendations / Health / LiveTail | 运营视图 |
| KeyService | CreateKey / RevokeKey / ListKeys / RevealKey / DiscoverMCPTools | 密钥与凭据 |
| ConfigService | Drafts / CreateDraft / GetDraft / UpdateDraft / Diff / Publish / Rollback / Versions / Rebase | 配置生命周期 |
| RuntimeService | Runtime / ResourceCatalog | 运行态 |
| PlaygroundService | Playground | 预演 |
| RequestService | Request / Requests / Usage / Audit / ToolCalls | 请求与审计 |
| AlertService | Alerts / AlertRules / CreateAlertRule / AlertAction / ResetCircuit | 告警与熔断 |
| SecurityService | SecurityEvents / Simulate | 安全事件与模拟 |
| FederationService | Federation / Approvals / ApprovalAction / FederationSuspend / AgentGraph | 联邦与审批 |
| IdentityService | Me | 会话自身 |

契约规则（冻结）：

1. **签名禁止 `any` 返回**：所有能力方法必须返回具体视图类型或领域类型（`ProjectSummary`、`DraftSummary`、`[]config.Version`、`*config.Draft`、`WizardSetupResponse`、`MeView` …）；新增方法沿用该规则；
2. **`Backend` 保留为组合超集**（`type Backend interface { SetupService; ProjectService; … }`）用于既有实现的渐进迁移；新装配直接依赖窄接口；
3. 类型断言扩展点（`PlaygroundStreamer`、`CredentialOperator`、`GuardrailPublisher`）允许保留在超集上，但断言接口必须声明在业务编排层；
4. 视图类型的 JSON tag 即线格式契约；变更必须伴随 e2e 回归。

### 2.9.4 领域类型归属

| 类型 | 归属包 | 说明 |
|---|---|---|
| LocalCredential / LocalUser / LocalCredentialStore / PasswordVerifier | `internal/identity` | 本地管理员凭据领域；`adminapi.LocalVerifier` 是表示层校验器 |
| PepperStore / Crypter / `Pepper.Resolve` / `EnsurePepper` | `internal/identity` | Pepper 领域逻辑；SQL 适配在 `sqlrepo` |
| AuditRecord / `audit.Repository` | `internal/observability/audit` | 审计记录领域；零内部依赖 |
| ResourceCatalogView / Provider / Model / Region CatalogItem | `internal/catalog` | 资源目录视图；零内部依赖 |
| TenantLister / FirstTenant | `internal/tenancy` | 租户窄查询接口 |

规则：领域包只定义接口与领域类型；SQL 实现一律在 `platform/storage/sqlrepo`；表示层以类型别名（`type X = domain.X`）兼容既有引用。

### 2.9.5 HTTP 错误契约（冻结）

```text
Admin API（401/403/404/409/422/500）：
  { "error": { "code": "UNAUTHORIZED|ROLE_FORBIDDEN|NOT_FOUND|REVISION_CONFLICT|NO_PUBLISHED_VERSION|INVALID_REQUEST|ALREADY_INITIALIZED|NOT_IMPLEMENTED|INTERNAL_ERROR", "params": {...}|null } }

Gateway（协议层，OpenAI 兼容）：
  { "error": { "message": "...", "type": "liteaig_error", "code": "INVALID_REQUEST|..." } }

Drain（运维中间件，逐字节固定）：
  503 { "error": { "code": "DRAINING", "message": "server is draining" } }
```

- `code` 是稳定枚举，前端按 code 分支，不解析 message；
- 领域哨兵错误 → 状态码映射集中在表示层 `mapError`（`tenancy.ErrNotFound/config.ErrDraftNotFound`→404、`ErrRevisionConflict`→409 …）；
- 5xx 必须记录原因日志；4xx 不记录。

### 2.9.6 API Key 生命周期即时生效（V8.5）

- Key Secret 创建时生成 256-bit CSPRNG 值，`api_keys.key_ciphertext`（envelope 加密，owner=identity）支持 Console Reveal 与一次性完整展示；数据面验证仍走 §6.3 HMAC 路径，不依赖明文 secret；
- Key 创建/吊销/轮换后触发 Tenant Runtime 即时重编译（`refreshTenantRuntime`）：用最新 Key 集合重编译该 Tenant 当前 active 版本并原子替换快照，**不产生新配置版本**，数据面立即生效；
- 旧格式历史 Key（迁移前创建）无密文备份，Reveal 明确返回不可用，走 Rotation 替换。

---

# 3. 固定七阶段 Data Plane Pipeline

Pipeline 顺序编译期固定，不开放用户任意重排。

```text
1. Admission
2. Input Guardrail
3. Policy & Cost Preflight
4. Resolution
5. Execution & Resilience
6. Output / Stream Guardrail
7. Accounting & Telemetry
```

## 3.1 Stage 1 — Admission

执行：

1. 生成/接受 Request ID；
2. 协议解析与 Normalize；
3. Body Size Guard；
4. Tenant Status + Virtual Key 认证；
5. Tenant / Project / Principal 解析；
6. 可信身份归属解析（API Key / Application / Agent / User / Service Account）；
7. Model ACL；
8. IP / Network Policy；
9. Cheap local coarse RPM；
10. Context Window 基础校验。

禁止在 Admission 执行外部网络 Guardrail、数据库查询或昂贵 ML 推理。

## 3.2 Stage 2 — Input Guardrail

默认顺序：

```text
Fast deterministic guard
  → DLP / PII / Secret
  → Prompt Injection / Jailbreak
  → Semantic / External Guardrail
```

检查对象：

- user/system/developer message；
- 显式标记的 RAG context；
- file/url metadata；
- tool result；
- external document provenance。

并行规则由 Guardrail Planner 自动编排；管理员只选策略，不编辑执行 DAG。

## 3.3 Stage 3 — Policy & Cost Preflight

执行：

- 精确 RPM / TPM；
- Budget Reservation；
- Project / Key Quota；
- Data Residency；
- Cache Policy；
- Exact Cache Lookup；
- Semantic Cache Lookup（P1）。

Cache Hit 后：

```text
Stage 1 → Stage 2 → Stage 3 HIT
→ 跳过 Stage 4/5
→ Stage 6 Output Guardrail
→ Stage 7 Accounting
```

缓存结果必须执行当前版本 Output Guardrail。

## 3.4 Stage 4 — Resolution

生成 `RoutePlan`：

- Logical Model 解析；
- Capability Filter；
- Region / Data Residency Filter；
- Project Policy Filter；
- Deployment Health / Circuit Filter；
- Soft Score；
- Credential Pool 候选；
- Fallback Chain；
- Route Explain Trace。

## 3.5 Stage 5 — Execution & Resilience

```text
Acquire Concurrency Lease
  → Select Credential
  → Invoke / Stream
  → Normalize Error
     ├─ retry same credential
     ├─ rotate credential
     ├─ retry deployment
     └─ fallback next deployment
```

释放 Lease 必须使用 `defer`，流式请求额外续租。

## 3.6 Stage 6 — Output / Stream Guardrail

非流式：

```text
Response / Cached Response
→ PII / Secret / Content / Topic
→ Reliability Guard(optional)
→ Tool Call Guard
→ Client
```

流式按第 14 章三层 Guardrail 执行。

## 3.7 Stage 7 — Accounting & Telemetry

通过顶层 `defer finalizeRequest()` 保证无条件执行：

- Budget Reconcile / Release；
- Usage Ledger；
- Provider Cost / Charge；
- Cache Usage；
- Retry / Fallback；
- Route Decision；
- Guardrail Event；
- Metrics / OTel；
- Request Log。

生产模式下 Stage 7 的财务事实写入采用 Durable Accounting：

```text
Unified Usage / Cost
  → append Durable Accounting Spool
  → mark local durable
  → async/sync batch ingest
  → PostgreSQL / Event Sink
  → idempotent ACK
  → truncate committed segment
```

原则：

- Provider 已返回可计费 usage 后，不允许因为 PostgreSQL 暂时不可用而静默丢弃 Usage；
- 采用 At-least-once delivery，依赖 `event_id` 幂等去重；
- Spool 是本地恢复机制，不是长期 Analytics Store；
- `hard_accounting` Tenant 可在 Spool 达硬上限后拒绝新的可计费调用；
- `soft_accounting` 可继续服务，但必须 Critical Alert；
- Spool flush 不进入正常 Provider TTFT 热路径。

---

## 3.8 Agentic Interaction 在七阶段中的映射

| Stage | Agentic 处理 |
|---|---|
| Admission | 解析 InteractionKind、Agent Identity、Task、Delegation；校验 Tenant/Project/Protocol |
| Input Guardrail | `AGENT_REQUEST` / `TOOL_REQUEST` / Message/Artifact/参数检查 |
| Policy & Cost Preflight | Agent/Tool ACL、Delegation Scope、Task Budget、Hop/Call/Duration Limit |
| Resolution | Model Route / Tool Endpoint / Agent Endpoint；P2 可做 Capability Routing |
| Execution & Resilience | Provider/Tool/Agent 调用、Timeout、Circuit、Fallback；Task 状态推进 |
| Output Guardrail | `AGENT_RESPONSE` / `TOOL_RESULT` / Artifact/Result 检查 |
| Accounting & Telemetry | Request/AgentCall/ToolCall/Task Usage、Cost、Delegation、Graph Edge、Audit |

固定七阶段不增加第八阶段；Agentic 能力通过已有 Engine 组合完成。

---

# 4. RequestContext 与扩展类型系统

## 4.1 核心强类型上下文

```go
type RequestContext struct {
    RequestID      string
    ReceivedAt     time.Time
    Snapshot       *TenantRuntimeSnapshot

    Interaction    *InteractionContext
    Request        *UnifiedRequest
    Response       *UnifiedResponse
    Principal      *Principal
    Delegation     *DelegationContext
    Task           *TaskContext
    Policy         *ResolvedPolicy
    RoutePlan      *RoutePlan
    Reservation    *BudgetReservation
    Usage          *UnifiedUsage
    Cost           *CostResult
    Cache          *CacheDecision
    Guardrail      *GuardrailTrace

    Ext            *ExtBag
}
```

Core Pipeline 不允许通过 ExtBag 传递核心状态。

## 4.2 类型安全 ExtBag

```go
type ExtKey[T any] struct { name string }

func NewExtKey[T any](name string) ExtKey[T] {
    return ExtKey[T]{name: name}
}

type ExtBag struct {
    mu     sync.RWMutex
    values map[string]any
}

func SetExt[T any](bag *ExtBag, key ExtKey[T], v T) {
    bag.mu.Lock()
    defer bag.mu.Unlock()
    bag.values[key.name] = v
}

func GetExt[T any](bag *ExtBag, key ExtKey[T]) (T, bool) {
    bag.mu.RLock()
    defer bag.mu.RUnlock()
    raw, ok := bag.values[key.name]
    if !ok {
        var zero T
        return zero, false
    }
    v, ok := raw.(T)
    return v, ok
}
```

Extension 必须使用注册的 `ExtKey[T]`；禁止裸字符串访问。

---

# 5. Access Engine

## 5.1 API 协议

### P0-Core

- OpenAI `/v1/chat/completions`
- OpenAI `/v1/embeddings`
- OpenAI `/v1/models`
- Anthropic `/v1/messages`

### P0-Commercial

- OpenAI `/v1/responses`
- tools / tool_choice
- vision/multimodal
- structured output / JSON schema

### P1

Agentic Protocol：

- MCP `2026-07-28`：Streamable HTTP、`server/discover`、`Mcp-Method/Mcp-Name`、Tasks extension；
- A2A `1.0.0`：Agent Card、SendMessage/Task、Streaming、Push Notification；

其它模型能力：

- image generation
- audio
- rerank
- batch
- Gemini-native compatibility

## 5.2 Unified Request

```go
type RequestKind string

const (
    KindChat       RequestKind = "chat"
    KindResponses  RequestKind = "responses"
    KindEmbedding  RequestKind = "embedding"
    KindImage      RequestKind = "image"
    KindAudio      RequestKind = "audio"
    KindRerank     RequestKind = "rerank"
)

type UnifiedRequest struct {
    Kind          RequestKind
    Model         string
    Stream        bool
    Metadata      map[string]string
    Tags          map[string]string
    ClientEndUserID string // 客户端自报标签，不作为可信身份
    SessionID     string
    TaskID        string
    RootTaskID    string
    ParentTaskID  string

    Chat          *ChatPayload
    Responses     *ResponsesPayload
    Embedding     *EmbeddingPayload
    Image         *ImagePayload
    Audio         *AudioPayload
    Rerank        *RerankPayload
}
```

## 5.3 Principal 与可信身份归属

```go
type Principal struct {
    Type             string // user|application|service_account|agent|api_key|system
    TenantID         string
    ProjectID        string
    APIKeyID         string

    ApplicationID    string
    AgentID          string
    ServiceAccountID string

    UserID           string
    OrgUnitID        string
    CostCenterID     string

    AuthMethod       string // api_key|delegated_jwt|oidc|service_account|federated_mtls|federated_oidc|federated_jws|registry_attested
    AttributionTrust string // verified|key_bound|delegated|federated_verified|unverified|none
    DelegationChainID string

    TrustBoundary            string // internal|external_federated
    FederationRelationshipID string
    ExternalSubject          string
}
```

身份归属规则：

1. API Key 固定解析 `tenant_id/project_id`，不可由请求 Header 覆盖；
2. API Key 可绑定 Application / Agent / Service Account；
3. 用户归属优先使用可信 Delegated User JWT / OIDC 映射；
4. 普通 `user`/`end_user` 字段只作为业务标签，不得用于权限、预算或正式 Chargeback；
5. Backend 代表用户调用时，使用 `X-LiteAIG-User-Context: <signed-jwt>`，JWT 必须来自 Tenant 配置的受信任 Issuer/Audience；
   - JWT 必须含 `iss/aud/sub/exp/iat/jti`；
   - `tenant_ref/project_id` 必须与 API Key Scope 匹配；
   - 高风险场景对 `jti` 做短 TTL 防重放；
6. 解析出 `user_id` 后，从当前 Tenant Snapshot 取得用户调用时的主 OrgUnit / Cost Center，并写入 Usage Event 快照；
7. 无可信用户身份时 `user_id/org_unit_id` 允许为空，用量仍可归属 Tenant/Project/API Key/Application/Agent。
8. Inbound External Agent 必须由 transport/HTTP 层的受信任身份机制解析到 `FederationRelationshipID`；远端 JSON/A2A Payload 中自报的 `user_id/tenant_id/delegation` 不构成可信身份。
9. 外部 Agent 可以作为 `Principal.Type=agent`，但 `TrustBoundary=external_federated`；其本地权限来自 Federation Relationship Grant，而不是本 Tenant 的普通 Agent Membership。
10. 若未来支持可验证跨组织 Delegation Token，应作为独立扩展验证链处理；在未支持前禁止把远端声称的 Delegation Chain 直接并入本地权限计算。

---

## 5.4 参数兼容策略

Provider 声明 `SupportedParams()`；Project 可配置：

- `strict=true`：不支持参数返回 `400 UNSUPPORTED_PARAMETER`；
- `strict=false`：安全丢弃并记录 warning；
- 未知 provider-specific 参数仅允许在显式 passthrough allowlist 中透传。

---

## 5.5 Agentic Ingress 与协议适配

- Model API 继续兼容 OpenAI/Anthropic；
- MCP Adapter 只负责 Wire Protocol ↔ Unified Interaction，不直接执行 Policy；
- A2A Adapter 只负责 Agent Card/Task/Message/Artifact ↔ Unified Interaction，不直接决定 Agent 选择；
- A2A Agent Card 默认从 `/.well-known/agent-card.json`、受信任 Registry 或管理员指定 URL 发现；**发现只创建 Candidate，不自动授予 Trust**；
- INTERNAL Agent：存在 Agent Card JWS 时校验；失败按 Tenant Policy 拒绝或标记 untrusted；
- EXTERNAL_FEDERATED Agent：Relationship 进入 `active` 前必须至少存在一个有效 Verified Trust Anchor。JWS 是可选的验证方式之一；也可使用 mTLS/SPKI Pin、OIDC Issuer/Subject、受信任 Registry Attestation 等企业信任方式；
- 外部 Agent Card 的 endpoint/auth/capability/publisher/trust material 发生实质变化时生成 Pending Review，不自动覆盖已批准版本；
- A2A Push Notification URL 必须经过 SSRF/egress policy 校验；
- MCP 2026-07-28 新部署禁止依赖协议级 Session；业务状态显式使用 Task/Handle；Legacy SSE Adapter 仅兼容旧端点。

---

# 6. Virtual API Key 与认证

## 6.1 Key 格式

```text
sk-lia-v1_<tenant_ref>_<public_id>_<secret>
```

- `tenant_ref`：128-bit 随机租户公开标识，用于 O(1) 定位 Tenant Runtime；
- `public_id`：128-bit 随机 Key 标识，可公开，用于 Tenant 内 O(1) 查找；
- `secret`：至少 256-bit CSPRNG 随机值，仅创建时展示一次。

## 6.2 存储

```sql
CREATE TABLE api_keys (
    id UUID PRIMARY KEY,
    public_id TEXT NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,

    hmac_digest BYTEA NOT NULL,
    pepper_version INT NOT NULL,
    fingerprint TEXT NOT NULL,

    application_id UUID NULL,
    agent_id UUID NULL,
    service_account_id UUID NULL,

    status TEXT NOT NULL CHECK (status IN ('active','disabled','revoked')),
    expires_at TIMESTAMPTZ NULL,
    model_allowlist TEXT[] NULL,
    ip_allowlist TEXT[] NULL,
    budget_policy_id UUID NULL,
    rate_policy_id UUID NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ NULL,

    UNIQUE(tenant_id, public_id)
);
CREATE INDEX idx_api_keys_tenant_project ON api_keys(tenant_id, project_id);
```

V8.5 增量（migration 028，owner=identity）：

```sql
ALTER TABLE api_keys ADD COLUMN key_ciphertext BYTEA;
```

- `key_ciphertext` 保存 Key Secret 的 envelope 加密密文，仅用于 Console Reveal / 一次性完整展示；
- 数据面验证路径不变（HMAC digest，§6.3），不依赖密文列；
- 迁移前创建的历史 Key 无密文，Reveal 返回不可用，通过 Rotation 替换；
- 密文列属于 identity 模块私有，其他模块禁止读写（table owner CI 校验）。

## 6.3 验证

```text
1. 从 token 解析 tenant_ref / public_id / secret
2. 通过 RuntimeRegistry 按 tenant_ref O(1) 取得 TenantRuntimeSnapshot
3. 在该 Tenant 的 APIKeyIndex 中按 public_id O(1) 查 Key
4. 根据 pepper_version 读取对应 pepper
5. digest = HMAC-SHA256(pepper, tenant_ref || "." || public_id || "." || secret)
6. constant-time compare
7. 校验 tenant/key status / expire / IP / scope
```

Data Plane 不每请求查询数据库。

## 6.4 Pepper 版本化轮换

```sql
CREATE TABLE key_pepper_versions (
    version INT PRIMARY KEY,
    pepper_ref TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active','retiring','retired')),
    activated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

规则：

1. 新 Key 永远用 `active` pepper；
2. 旧 Key 按自身 `pepper_version` 继续验证；
3. **不能后台把旧 HMAC 直接迁移到新 pepper**，因为服务端不保存 Key secret 明文；
4. Key 迁移必须通过 Key Rotation：生成新 Key，旧 Key 保留可配置 Grace Period 后自动 revoke；
5. 当旧 pepper 版本不再存在 active Key 时，才可 `retired` 并销毁；
6. Pepper 泄漏时执行 Emergency Rotation：批量生成替代 Key / 通知应用 / 按策略缩短 Grace Period，最终撤销旧 Key。

## 6.5 Console 用户认证

- Lite：local admin，Argon2id；
- Standard：OIDC 优先；
- Enterprise：OIDC/SAML，MFA 交给 IdP，SCIM P1。

---

## 6.6 Agent Identity 与 Delegation

Agent 是一等 Principal，但不等价于 User。`INTERNAL` Agent 必须有稳定 `agent_id`，并绑定 Tenant/Project、Owner、Service Account、版本和风险级别；`EXTERNAL_FEDERATED` Agent 是 Tenant 级受控投影，绑定外部 Subject/Trust Relationship，通过 Project Grant 被本地 Project 使用，不伪装成本地 Project 资产。

```go
type DelegationContext struct {
    ChainID       string
    RootPrincipal PrincipalRef
    Delegator     PrincipalRef
    Delegatee     PrincipalRef
    Scopes        []string
    Resources     []string
    TaskID        string
    IssuedAt      time.Time
    ExpiresAt     time.Time
    Depth         int
}
```

权限解析分成两种目标：

### Internal Agent

```text
EffectiveInternalPermission =
  SystemPolicy
  ∩ TenantPolicy
  ∩ ProjectPolicy
  ∩ DelegatorPermission
  ∩ DelegationGrant
  ∩ DelegateePolicy
```

### External / Federated Agent

外部 Agent 的远端 Policy 不在 LiteAIG Runtime 内可见，因此**不把不存在的远端 Policy 假装加入本地交集**：

```text
EffectiveFederatedEgressPermission =
  SystemPolicy
  ∩ TenantPolicy
  ∩ ProjectPolicy
  ∩ CallerPermission
  ∩ LocalDelegationGrant(if delegated)
  ∩ FederationRelationshipGrant
  ∩ ProjectGrant
  ∩ CapabilityGrant
  ∩ DataBoundaryPolicy
```

远端 Agent 在收到请求后仍应执行它自己的认证/授权；LiteAIG 只对本地可证明的授权事实负责。

不变量：

- Delegation 不能放大权限；
- Grant 必须有 TTL，并可绑定 Task/Resource/Audience；
- Agent Handoff 每增加一跳都记录不可变事件；
- 超过 `max_agent_hops` 直接拒绝；
- 高风险跨 Agent Delegation 可要求 Human Approval；
- 跨组织调用保留本地 Delegation Chain 用于“谁授权发起这次外部调用”的审计，但 External Target 不作为普通 `delegatee_agent_id` 参与本地 DelegateePolicy 解析；
- Federation Relationship 不允许通过远端 Agent Card 自声明扩大本地 Capability Grant。

---

# 7. Provider / Model / Deployment 模型

```text
Provider Instance
  ├─ SYSTEM_SHARED
  └─ TENANT_PRIVATE
       └─ Credential Pool
            └─ Credential

Physical Model Catalog

Deployment
  = Provider + CredentialPool + PhysicalModel + Region + Endpoint

Logical Model (Tenant scoped)
  └─ Route Policy
       └─ Deployment Set
```

## 7.1 Provider / Credential Scope

Provider Instance 支持两种所有权：

```text
SYSTEM_SHARED
TENANT_PRIVATE
```

规则：

- `SYSTEM_SHARED`：由平台管理员维护，可被允许的 Tenant 引用；Provider Credential 由平台持有，Tenant 永远不可读取 Secret；
- `TENANT_PRIVATE`：必须绑定 `tenant_id`，仅该 Tenant 的 Project/Route 可引用；
- Credential Scope 必须继承 Provider Scope，禁止 Tenant A 的 Credential 被 Tenant B 的 Deployment 引用；
- Deployment 必须显式带 `tenant_id`；引用 SYSTEM_SHARED Provider 时仍属于当前 Tenant；
- Control Plane Validator 在 Publish 阶段执行 Scope Referential Integrity；
- Data Plane 编译 Tenant Snapshot 时只装入该 Tenant 可见的 Provider/Credential/Deployment；
- Tenant 删除/暂停不影响 SYSTEM_SHARED Provider，但其 Tenant Snapshot 不再路由到任何 Provider。

---

## 7.2 Provider 契约

```go
type Provider interface {
    ID() string
    Invoke(context.Context, *UnifiedRequest, Credential) (*UnifiedResponse, error)
    Stream(context.Context, *UnifiedRequest, Credential, StreamWriter) error
    Capabilities(context.Context) CapabilitySet
    Health(context.Context, Credential) HealthStatus
    SupportedParams() ParamSupportMatrix
    NormalizeError(error) *UpstreamError
}
```

约束：

- Provider 不读取 Budget / Routing / Guardrail / Tenant 状态；
- Provider 不写 Usage Ledger；
- `Stream` 必须监听客户端取消；
- `Health` 默认 2s timeout；
- 每个 Provider 必须通过 normal / timeout / 429 / 5xx / stream-drop 契约测试。

## 7.3 Connector 优先级

P0：

1. OpenAI
2. OpenAI-Compatible
3. Anthropic
4. DeepSeek / Qwen / Moonshot 走 OpenAI-Compatible 验证
5. vLLM / Ollama compatible

P1：Gemini / Azure OpenAI / Bedrock / Vertex AI。

长尾优先配置化 OpenAI-Compatible，不追求维护大量原生 Provider。

---

# 8. Smart Routing Engine

## 8.1 Hard Constraints

候选 Deployment 必须满足：

- enabled；
- circuit != open / forced_open；
- capability；
- context window；
- model allowlist；
- region / data residency；
- tool / structured output；
- provider policy / tenant scope；
- endpoint health；
- credential pool 至少有可用 credential。

## 8.2 Soft Score

每个候选计算：

```text
score =
    W_priority * priority_score
  + W_latency  * latency_score
  + W_cost     * cost_score
  + W_load     * load_score
  + W_quality  * quality_score
  + W_cache    * cache_affinity
```

### 稳定归一化

为避免纯 Min-Max 在候选差异极小时放大噪声，采用“候选集合归一化 + 差异下限”：

```text
normalize_lower_better(values):
  min_v = min(values)
  max_v = max(values)
  if max_v - min_v < noise_floor:
      all scores = 0.5
  else:
      score(v) = 1 - (v - min_v) / (max_v - min_v)
```

默认：

- latency `noise_floor = max(5ms, 0.05 * median_latency)`；
- cost `noise_floor = max(1e-8, 0.03 * median_cost)`；
- load `score = clamp(1 - inflight/capacity, 0, 1)`；
- quality `score = clamp(quality_rating/5, 0, 1)`；
- priority `score = 1 / (1 + priority)`。

动态 latency/success 指标使用 EWMA，禁止使用单次请求瞬时值。

## 8.3 Route Policy 模板

| Template | Priority | Latency | Cost | Load | Quality | Cache |
|---|---:|---:|---:|---:|---:|---:|
| balanced | 0.25 | 0.25 | 0.20 | 0.20 | 0 | 0.10 |
| lowest_cost | 0.10 | 0.10 | 0.65 | 0.10 | 0 | 0.05 |
| lowest_latency | 0.10 | 0.65 | 0.10 | 0.10 | 0 | 0.05 |
| availability_first | 0.45 | 0.15 | 0.05 | 0.30 | 0 | 0.05 |
| highest_quality(P2) | 0.15 | 0.10 | 0.10 | 0.10 | 0.50 | 0.05 |

`manual_weighted` 由 Console 自动归一化为和 1。

## 8.4 冷启动探索

无历史数据的 Deployment：

- latency/cost 使用同 Provider 同模型基线；
- 无基线时给 0.5；
- 前 50 个请求或 10 分钟给 `exploration_bonus=0.1`；
- 任何 hard constraint / unhealthy / circuit open 不享受探索。

## 8.5 选择策略

默认 `argmax(score)`；需要流量分散时使用 `weighted_rendezvous`：

```text
hash(request_or_session_key, deployment_id) × score_weight
```

优点：

- 对同一 session 稳定；
- Routing Simulator 可复现；
- Deployment 增删时迁移流量比例可控。

## 8.6 Fallback Plan

Fallback 只包含已通过 hard constraints 的 Deployment。

触发：

- connect error；
- timeout；
- 429；
- 5xx；
- circuit transition；
- credential exhausted。

默认不对 4xx 业务错误 fallback。

## 8.7 Routing Simulator

必须调用生产 `PlanRoute()`，并允许传固定 seed/session ID。

输出：

- 候选列表；
- 每个过滤原因；
- 每项 score；
- 最终选择；
- fallback 顺序；
- policy/snapshot version。

## 8.8 Adaptive Governance Recommendations

系统可基于 Tenant 自身历史数据生成 Routing / Guardrail 优化建议，但**永不自动修改生产配置**。

数据来源：

```text
Usage Ledger / Routing Decision
  → latency / cost / success / retry / fallback / cache

Guardrail Event + Guardrail Event Review
  → confirmed / false_positive / uncertain

Replay-eligible Test Sample
  → Impact Preview
```

禁止：

- 跨 Tenant 聚合训练；
- 未经授权读取历史 Prompt/Response；
- 直接执行自动调权、自动改阈值；
- 使用无法解释的黑盒模型直接输出生产配置。

建议结构：

```go
type GovernanceRecommendation struct {
    ID             string
    TenantID       string
    ProjectID      string
    Kind           string // routing|guardrail
    TargetType     string
    TargetID       string
    ProposedPatch  json.RawMessage
    Evidence       EvidenceSummary
    Confidence     float64
    SampleCount    int64
    WindowStart    time.Time
    WindowEnd      time.Time
    Status         string // open|accepted|dismissed|expired
}
```

生成门槛：

- `sample_count >= policy.min_observations`；
- 数据覆盖率、异常值比例和缺失率满足 Recommendation Policy；
- Guardrail 阈值建议必须有人工 review 标签或 replay-eligible 样本；仅有“规则命中数”不能自动推导阈值修改；
- 建议必须包含当前值、建议值、证据、预期收益、风险和回滚点。

接受建议：

```text
Recommendation
  → Create Config Draft(source=system_recommendation)
  → Validate
  → Impact/Diff
  → Human Review
  → Publish
```

首个版本仅生成建议，不实现 Auto Apply。

---

# 9. Resilience Engine

## 9.1 Timeout

支持：

- connect timeout；
- TLS handshake timeout；
- first-byte / TTFT timeout；
- total timeout；
- stream idle timeout。

## 9.2 Retry Budget

```text
network retry max_attempts = 2 (default)
guardrail regeneration max_attempts = 1 (default)
max_total_provider_calls_per_request = 4 (default)
```

三者独立计数，但受总 Provider Call 上限约束。

退避：

```text
backoff(n) = min(base * 2^n, cap) + jitter(0, base)
base=200ms, cap=4s
```

流式请求在首 chunk 已提交客户端后禁止完整自动 Retry。

## 9.3 Circuit Breaker

粒度：

```text
provider + deployment + credential
```

状态：

```text
Closed → Open → HalfOpen → Closed/Open
```

默认：

- 最小样本 20；
- error rate > 50% 打开；
- initial cooldown 30s；
- cooldown 指数增长至 10min；
- HalfOpen 并发探测默认 1。

## 9.4 Credential Pool

支持：

- round-robin；
- weighted；
- least-inflight；
- quota-aware；
- 429 credential rotate；
- credential 独立 circuit / health / cost。

---

# 10. Concurrency Lease

Redis/Valkey 使用 ZSET，不使用 HGETALL 扫描全部 Lease。

## 10.1 Acquire

```lua
-- KEYS[1] = concurrency:{scope}:{id}:leases
-- ARGV[1] = lease_id
-- ARGV[2] = capacity
-- ARGV[3] = now_ms
-- ARGV[4] = expires_at_ms

redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[3])
local active = redis.call('ZCARD', KEYS[1])
if active >= tonumber(ARGV[2]) then
  return {0, active}
end
redis.call('ZADD', KEYS[1], ARGV[4], ARGV[1])
redis.call('PEXPIRE', KEYS[1], math.max(60000, tonumber(ARGV[4]) - tonumber(ARGV[3]) + 60000))
return {1, active + 1}
```

复杂度约 `O(log N)`。

## 10.2 Renew

长流式请求每 `ttl/3`：

```lua
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  redis.call('ZADD', KEYS[1], 'XX', ARGV[2], ARGV[1])
  return 1
end
return 0
```

## 10.3 Release

```lua
return redis.call('ZREM', KEYS[1], ARGV[1])
```

进程 SIGKILL 时 Lease 到期后由下一次 acquire 或后台轻量 cleanup 自动清除。

Circuit Open 的 Deployment 在 Resolution 已被过滤，不申请 Lease；HalfOpen probe 仍须申请 Lease。

---

# 11. Cache Engine

## 11.1 Exact Cache

后端：

- Lite：Memory LRU；
- Standard：Memory L1 + Valkey L2；
- Enterprise：Valkey Cluster。

Cache Key：

```text
hash(
  tenant_id,
  project_id,
  logical_model,
  normalized_request,
  semantic_affecting_params,
  cache_namespace_version
)
```

默认禁止跨 Project 共享。

默认缓存条件：

- deterministic / low-temperature；
- embeddings；
- classification / extraction / translation；
- 明确 `cache=true`。

默认不缓存：

- tool call；
- 高 temperature；
- Guardrail block；
- 请求命中高敏感 PII/secret policy；
- 用户显式 `Cache-Control: no-cache`。

## 11.2 Semantic Cache（P1）

```text
Prompt → Embedding → tenant-scoped vector search
→ threshold → candidate → current Output Guardrail → return
```

约束：

- Tenant/Project namespace 强隔离；
- embedding model + dimension + cache version 写入 namespace；
- tool call 默认禁用；
- security policy 不通过时禁止返回；
- 可选 pgvector / Redis Vector / 外部 backend。

## 11.3 Provider Prompt Cache Awareness

记录：

- cache read/write tokens；
- provider cache hit；
- provider cache cost；
- prefix/session affinity。

Routing 可把 `cache_affinity` 作为软评分。

## 11.4 Cache 安全不变量

1. Input Guardrail 在 Cache Lookup 前；
2. Cache Hit 必须执行当前 Output Guardrail；
3. cache entry 不保存 Provider Secret；
4. 租户不可共享缓存 namespace；
5. Guardrail Policy 变更不要求全量删缓存，但每次命中重新执行 Output Guardrail；
6. Cache Hit 仍写 Usage Event；
7. Cache Hit 的 Provider Cost=0，Gateway Charge 可按策略配置。

---

# 12. Token Metering 与 Context 管理

## 12.1 Unified Usage

```go
type UnifiedUsage struct {
    InputTokens       int64
    OutputTokens      int64
    CacheReadTokens   int64
    CacheWriteTokens  int64
    CachedInputTokens int64
    ReasoningTokens   int64
    AudioInputTokens  int64
    AudioOutputTokens int64
    ImageInputUnits   int64
    ImageOutputUnits  int64
    ToolCalls         int64
    TotalTokens       int64 // text/reasoning token口径；image/audio另按unit记录

    Source             string // provider|count_api|local_estimate
    Estimated          bool
    EstimationMethod   string
}
```

## 12.2 Tokenizer Registry

优先级：

1. Provider 响应 usage；
2. Provider 官方 count-tokens endpoint；
3. 已知模型本地 tokenizer；
4. conservative estimate。

### OpenAI / compatible

- 维护 model → encoding 映射；
- encoding 资源随 LiteAIG Release 固化并版本化；
- 未知新模型不猜精确 tokenizer，走 conservative estimate。

### Anthropic

- 预算预扣需要精确时可调用官方 token count；
- 小请求默认本地估算，避免额外网络 RTT；
- 最终计费以响应 usage 为准。

### HF / vLLM / Ollama

- 可配置 tokenizer metadata；
- P1 可接 sidecar tokenizer service；
- 不把 Python/CGO 作为默认单二进制硬依赖。

### Fallback

```text
estimate = ceil(utf8_runes / 3.3) * safety_factor
safety_factor default = 1.2
```

估算只用于预扣/TPM/Context Guard；最终计费尽量使用 Provider usage。

## 12.3 Context Guard

请求前：

- 计算/估算 input tokens；
- 检查 model context window；
- 计算最大可用 output token；
- strict mode 超限直接拒绝；
- optional truncate/summarize 不属于 Gateway Core，默认不自动修改业务语义。

---

# 13. Pricing、Cost 与 Billing

## 13.1 Pricing Rule

```sql
CREATE TABLE pricing_rules (
    id UUID PRIMARY KEY,
    provider_type TEXT NOT NULL,
    model_name TEXT NOT NULL,
    region TEXT NULL,
    modality TEXT NOT NULL DEFAULT 'text',
    context_tier TEXT NULL,
    input_price NUMERIC(18,9) NULL,
    output_price NUMERIC(18,9) NULL,
    cache_read_price NUMERIC(18,9) NULL,
    cache_write_price NUMERIC(18,9) NULL,
    reasoning_price NUMERIC(18,9) NULL,
    image_price NUMERIC(18,9) NULL,
    audio_price NUMERIC(18,9) NULL,
    currency TEXT NOT NULL,
    source TEXT NOT NULL,
    version INT NOT NULL,
    valid_from TIMESTAMPTZ NOT NULL,
    valid_to TIMESTAMPTZ NULL
);
```

## 13.2 两套金额

必须同时记录：

- `provider_cost`：模型 Provider 上游采购成本；
- `external_agent_cost`：调用外部 Federated Agent 产生的第三方采购/合同成本；
- `customer_charge`：租户/部门 Showback/Chargeback。

支持 discount / markup / internal margin。

## 13.3 多币种

Usage Event 保留 Provider 原始币种与原始成本。

账单规则：

- Tenant 配置 `settlement_currency`；
- 账期内统一使用账期结束日/财务指定汇率；
- 账单生成时记录 `fx_rate_version`；
- 明细原始 Provider Cost 不回写覆盖；
- 外汇 API 为 P1，可使用企业固定汇率。

## 13.4 Tenant Chargeback / Showback

Provider Cost 与内部成本归属分离：

```text
Provider Cost
  → Tenant
     → Project
        → Application / Agent / API Key
     → OrgUnit / User
```

Chargeback Policy 可按 Tenant 配置：

```yaml
chargeback:
  settlement_currency: CNY
  markup_percent: 8
  allocation:
    cache_hit: gateway_fee_only
    retry_cost: request_owner
    guardrail_regeneration_cost: request_owner
  trusted_user_attribution_required: true
```

规则：

- `provider_cost` 始终记录上游真实采购成本；
- `customer_charge`/内部 Showback 可包含 markup、平台服务费、缓存费；
- 部门/人员统计只做归属聚合，不改变 Provider 原始成本；
- 未验证的 `ClientEndUserID` 不参与正式部门/人员 Chargeback；
- 人员调岗以 Usage Event 的组织快照为准；
- Tenant 可定义 Cost Center 映射，财务导出按账期固定汇率结算；
- P0-Commercial 支持 Tenant/Project/API Key Showback；
- P1 支持 OrgUnit/User/Application/Agent Chargeback 与部门预算。

External Agent Cost 规则：

- 外部 Agent 成本**必须计入 Task `max_total_cost`**，否则一个 Task 可以通过外部 Agent 绕过总成本上限；
- 同一笔外部 Agent 成本同时进入独立 `External Procurement Budget`，形成“Task 总预算 + 外部供应商预算”的双重约束；
- A2A 不定义统一计费格式，外部成本来源必须显式标记为 `contract|per_call|reported|estimated|unknown`；
- `hard` 成本策略下，若 External Agent 定价为 `unknown`，默认禁止调用或要求显式风险例外；不能假装能够严格执行金额上限；
- Task 总成本聚合以 leaf accounting event 为事实源，父级 `downstream_*` rollup 不重复计费。


---

# 14. Budget Reservation、Rate Limit 与 Quota

## 14.1 多窗口原子 Reservation Ledger

Standard/Enterprise 的预算正确性由 Redis/Valkey 原子 Ledger 保证，不依赖异步 PostgreSQL 记录。

所有同一 Tenant 下预算 Key 使用同一 Redis Cluster hash tag：

```text
budget:{budget:<tenant_id>}:tenant:<id>:1d
budget:{budget:<tenant_id>}:project:<id>:1d
budget:{budget:<tenant_id>}:key:<id>:1h
budget:{budget:<tenant_id>}:external_agent:<relationship_id>:1d
budget:{budget:<tenant_id>}:reservation:<reservation_id>
budget:{budget:<tenant_id>}:reservation_expiry
```

单个 Lua 一次完成：

1. 检查 Tenant / Project / Key，以及适用的 External Agent Procurement Budget 全部窗口；
2. 任一窗口超限则不修改任何计数；
3. 全部通过则全部 `INCRBYFLOAT`；
4. 写 Reservation Hash；
5. 把 reservation_id 写入 expiry ZSET；
6. 返回 reserved。

伪代码：

```text
for each budget_window:
    current = GET(counter)
    if current + estimate > limit:
        return REJECT

for each budget_window:
    INCRBYFLOAT(counter, estimate)
    set ttl to window boundary + grace

HSET reservation:{id} status=reserved estimate=... windows=...
ZADD reservation_expiry expires_at reservation_id
return RESERVED
```

PostgreSQL `budget_reservations` 仅用于审计、查询和恢复辅助，允许异步写，不参与准入正确性。

## 14.2 Reconcile

Reconcile 使用 idempotent Lua：

```text
if reservation.status != reserved:
    return ALREADY_FINALIZED

delta = actual_cost - estimated_cost
for each window:
    INCRBYFLOAT(counter, delta)

reservation.status = reconciled
ZREM expiry reservation_id
```

失败/取消但 Provider 未产生可计费 usage：释放全部 estimate。

## 14.3 Reservation Sweeper

每个 Tenant 的 expiry ZSET：

```text
ZRANGEBYSCORE ... -inf now LIMIT 0 500
```

对每条调用幂等 `expire_reservation.lua`：

- 若 status=reserved：释放 estimate；
- 标记 expired；
- 从 ZSET 删除；
- 若已 reconciled/released：只删除 expiry entry。

Control Plane job 默认 10s 一次；Enterprise 可按 tenant 分片。

该设计即使 Gateway 在 PostgreSQL 异步记录落盘前 SIGKILL，也不会永久卡住预算。

## 14.4 Redis 不可用

预算策略按 Project 声明：

- `hard`：fail_closed；
- `soft`：fail_open + alert；
- Lite：SQLite transaction / local counter。

P0-Commercial 默认 Production Budget = hard。

## 14.5 Rate Limiting

P0 推荐 GCRA / Token Bucket：

- Key RPM；
- Project RPM；
- User RPM；
- OrgUnit RPM（P1）；
- Model RPM；
- TPM；
- concurrent requests；
- concurrent streams。

严格滑动窗口仅在审计场景使用 Redis ZSET。

TPM 请求前按 estimate 预记，响应后 reconcile。

P0-Commercial 强制预算层级为 Tenant → Project → Key；P1 在存在可信身份归属时可增加 OrgUnit/User 预算。

---

# 15. Guardrail Engine

## 15.1 Checkpoints

```text
REQUEST_IN
CONTEXT_IN
PRE_PROVIDER
STREAM_OUT
RESPONSE_OUT
TOOL_REQUEST
TOOL_RESULT
AGENT_REQUEST
AGENT_RESPONSE
AGENT_HANDOFF
APPROVAL_REQUEST
ASYNC_EVAL
```

## 15.2 Guardrail 类型

### Built-in Fast Guard

- body/token size；
- keyword / deny phrase；
- regex；
- secret pattern；
- basic PII；
- URL policy；
- JSON schema；
- model/tool allowlist。

### AI Security Guard

- Prompt Injection；
- Jailbreak；
- Prompt Leakage；
- harmful/toxic content；
- semantic topic；
- document injection。

### Data Guard / DLP

- PII；
- credentials；
- source code / business secret patterns；
- custom entity；
- mask / redact / block / pseudonymize(P1)。

### Reliability Guard

- groundedness；
- hallucination；
- response policy；
- structured output validation；
- business rule check；
- LLM-as-Judge(P2)。

### Agentic Guard

- MCP/A2A allowlist；
- Agent ACL / Tool ACL；
- Delegation scope / hop limit；
- parameter schema；
- risk level；
- task adherence；
- tool-result injection；
- human approval handoff；
- audit。

## 15.3 Provider Interface

```go
type GuardrailProvider interface {
    ID() string
    Evaluate(context.Context, GuardrailRequest) (*GuardrailResult, error)
    Capabilities() GuardrailCapabilities
    Health(context.Context) HealthStatus
}

type GuardrailResult struct {
    Verdict          Verdict
    RiskScore        float64
    Findings         []Finding
    SanitizedContent *Content
    SuggestedAction  Action
    Latency          time.Duration
    Metadata         map[string]string
}
```

## 15.4 Action Model

```text
ALLOW
FLAG
MASK
REDACT
BLOCK
REWRITE
RETRY
FALLBACK
ALERT
AUDIT
REQUIRE_APPROVAL
```

Guardrail Provider 只给 verdict/finding；Action 最终由 LiteAIG Policy Engine 执行。

## 15.5 聚合

默认优先级：

```text
BLOCK > REQUIRE_APPROVAL > REDACT/MASK > REWRITE > FLAG > ALLOW
```

支持：

- `any_block`；
- `all_must_pass`；
- `weighted_risk`；
- threshold。

## 15.6 Sync / Async

每条规则：

```text
mode = enforce | observe
```

- `enforce`：同步影响请求；
- `observe`：异步 Security Event，不阻断。

## 15.7 Fail Mode

External Guardrail 每条 policy 明确：

- `fail_closed`；
- `fail_open + alert`。

禁止全局一个 Fail Mode。

---

# 16. Streaming Guardrail

默认不允许远程 External Guardrail 逐 SSE chunk 同步调用。

## 16.1 Layer 1 — Inline Fast Guard

- 每个 chunk 写客户端前执行；
- local keyword/regex/secret/basic PII；
- 跨 chunk rolling window；
- 预算目标 ≤0.3ms/chunk；
- 可 MASK / BLOCK。

## 16.2 Layer 2 — Buffered Local Guard

按：

- sentence boundary；
- newline；
- 或每 64 token；

组成释放窗口。

P0-Commercial 只允许本地 deterministic / local classifier；P1 可启用嵌入式本地模型，但必须声明延迟预算。

默认窗口额外预算 ≤20ms。

## 16.3 Layer 3 — Async Shadow Guard

- 已释放内容异步送 Remote Guard；
- 不计入 TTFT；
- 若违规结果在流仍进行中返回：终止后续输出；
- 已发内容无法撤回，记录 `BLOCK_RETROACTIVE` 并进入事后处置。

## 16.4 Strict Remote Buffered Mode

对监管场景可显式开启：

```text
stream.remote_mode = strict_buffered
```

行为：按句段甚至完整响应缓冲后调用 External Guardrail，通过后才释放。

该模式明确牺牲 TTFT，不属于 Core SLO。

## 16.5 流式 BLOCK

- cancel upstream；
- 停止后续 chunk；
- 发送协议兼容终止/错误事件；
- 结算已产生 usage；
- Security Event 记录 `released_bytes/released_tokens`。

---

# 17. Guardrail Retry / Fallback 与事后处置

## 17.1 Output RETRY / FALLBACK

- `RETRY`：相同 Deployment 新建一次 Provider 调用；
- `FALLBACK`：使用 RoutePlan 下一个 Deployment 新建调用；
- 两者产生新 Cost；
- 执行前再次检查剩余 Budget；
- 默认 `guardrail_regeneration_max_attempts=1`；
- 总 Provider Call 仍受 `max_total_provider_calls_per_request` 限制。

若第二次仍违规：BLOCK。

### 流式限制

当违规内容已提交客户端后，禁止自动 RETRY/FALLBACK 替换已输出结果；只能终止剩余流并记录事件。

## 17.2 ASYNC_EVAL 事后违规

```text
1. guardrail_event verdict=BLOCK_RETROACTIVE
2. security_incident alert
3. retroactive_action:
   - flag_only(default)
   - notify_downstream(P1)
   - revoke_session(P1,需业务配合)
4. 计入 async miss / retroactive block 指标
```

网关不承诺撤回已经通过 HTTP/SSE 发送给客户端的内容。

---

# 18. Context / Document / Agentic Interaction Security

## 18.1 Content Provenance

内容可标记：

- user_input；
- system_instruction；
- retrieved_context；
- external_document；
- tool_result；
- external_agent_response。

外部文档、Tool Result 与 External Agent Response 永远按不可信输入处理；`external_agent_response` 单独保留来源语义，不能简单伪装成普通 `external_document`，以便 Security Event / Audit / Agent Graph 明确识别跨组织边界。

## 18.2 External Guard 数据出境

在把原文发送给第三方 Guardrail 前：

1. DLP pre-scan；
2. Residency / Provider policy 检查；
3. 可配置 mask 后再调用第三方。

## 18.3 Tool Governance（MCP First）

```text
Agent
  → LiteAIG Tool Gateway
      → TOOL_REQUEST Guard
      → MCP / Function Tool
      → TOOL_RESULT Guard
  → Agent
```

### Phase 2 基础闭环

- MCP Server Registry；
- Tool discovery / schema snapshot；
- Tenant/Project/Agent Tool ACL；
- parameter schema validation；
- tool timeout / rate limit；
- DLP / Secret / URL policy；
- risk classification；
- Content Provenance；
- `tool_call_events` runtime ledger；
- Usage Attribution；
- Request Explorer 单请求/单会话关联。

### Phase 4 增强

- Human Approval Handoff / one-time Approval Grant；
- Task Adherence；
- advanced Tool Result injection detection；
- cross-server session graph；
- approval audit / expiry / escalation。

### Policy 解析

```text
System Mandatory Tool Policy
  → Tenant Tool Policy
  → Project Tool Policy
  → Agent Tool Policy
  → EffectiveToolPolicy
```

子级只能收紧父级强制 Deny/Approval 约束。

### 安全顺序

```text
Tool Request
  → ACL
  → Schema
  → DLP
  → Risk/Approval
  → Invoke
  → Tool Result
  → Provenance=tool_result
  → Input Guardrail
  → Agent
```

默认不持久化 Tool 参数/结果正文，只保存 hash、脱敏摘要和决策元数据。LiteAIG 不执行 Agent Workflow，也不托管 Tool Runtime。

---

## 18.4 A2A Gateway（P1）

A2A 仅作为 Agent-to-Agent 互操作协议，LiteAIG 不实现 Agent 内部业务逻辑。

能力：

- Agent Card discovery/cache/signature verification；
- public/extended Agent Card 的 Tenant/Project 可见性控制；
- A2A 1.0 Message / Task / Artifact / Streaming / Push Proxy；
- Agent Endpoint health/circuit/timeout；
- Agent ACL / Capability allowlist；
- `AGENT_REQUEST` 与 `AGENT_RESPONSE` Guardrail；
- Task/Session/Cost/Trace 关联；
- A2A extension allowlist；
- Push Notification SSRF/secret policy。
- Inbound Federated A2A Caller 的身份解析与 Relationship Grant；
- Outbound Federated Agent 的 Trust/Data Boundary/External Budget；
- Agent Card/JWKS/Registry Cache 与 Reverification；
- Trust Relationship suspend/revoke 触发 Security Epoch Fast Publish。

## 18.5 Agent Registry 与 Capability

Agent Registry 只保存“如何发现和治理 Agent”的元数据：

```text
Agent
  ├─ Identity / Owner Scope / Project(or Tenant)
  ├─ Trust Boundary: INTERNAL | EXTERNAL_FEDERATED
  ├─ Endpoint(s)
  ├─ Protocol(s)
  ├─ Capability / Skill
  ├─ Risk Level
  ├─ Health
  └─ Agent Card / Signature Metadata
```

LiteAIG 不保存 Agent Memory、Prompt Chain 或 Planner State。

## 18.6 Task Governance & Loop Control

Task 是跨 Model/Tool/Agent 调用的计费、预算、审计和生命周期聚合单元。

```yaml
task_policy:
  consistency: regional       # regional | global_soft | global_hard
  max_agent_hops: 8
  max_model_calls: 30
  max_tool_calls: 20
  max_agent_calls: 20
  max_total_tokens: 500000
  max_total_cost: 5.00
  max_duration: 10m
  loop_detection: true
```

Loop Detection P1 先采用确定性规则：

- `(caller_agent, callee_agent, capability/tool)` 重复边计数；
- 相同调用签名短窗口重复；
- hop 上限；
- cost/token/call 数上限；
- 超限触发 `TASK_GOVERNANCE_BLOCK`，取消后续调用并完成已产生用量结算；
- External Agent 调用同样计入 `max_agent_hops/max_agent_calls/max_total_cost/max_duration`；
- 对同一 External Agent 的重复边默认使用更低阈值，因为其可能产生真实第三方成本和不可控副作用；
- `crosses_trust_boundary=true` 一旦置位不可逆，并提高 Audit/Retention 级别。

### Task Counter 一致性

Task 首次创建记录 `home_region` 与 `consistency_mode`：

#### `regional`（默认）

- Task Counter Authority 固定在 `home_region`；
- 非 Home Region Gateway 不允许独立维护同一 Task 的权威计数；
- 跨 Region Agent Endpoint 可以从 Home Region 直接调用；如请求从其他 Region 进入，应代理 Counter Check 到 Home Region 或返回明确 `TASK_REGION_MISMATCH`；
- 在 Home Region 可用时不允许因为多 Region 本地计数产生静默超发。

#### `global_soft`

- 允许多个 Region 本地处理；
- 复用统一 `Counter/Quota Slice` 基础设施分配 hops/calls/cost/token slice；
- 异步 Reconcile，允许**有界超发**，必须可度量；
- 不为 Task 单独实现第二套分布式一致性协议。

#### `global_hard`

- 使用 Single Task Counter Authority 或具备强一致语义的外部 Counter Authority；
- 所有硬上限在下一个下游调用前完成权威检查；
- 接受额外跨 Region RTT / Availability Trade-off。

`max_duration` 使用 Root Task 创建时固化的绝对 `deadline_at` 判断，不通过 Quota Slice 近似；Multi-Region 节点必须使用受监控的时间同步并设置允许的 clock-skew budget，超过 skew 阈值的节点不得执行 Global Hard Task Counter。

P2 可加入基于历史图的异常建议，但不得自动放宽限制。

## 18.7 Capability Routing（P2）

Capability Routing 只解决“在**已被 Orchestrator 决定要调用某类 Agent 能力**后，选择哪个合规 Agent Endpoint”，不负责 Task Planning。

Hard Constraints：

- Tenant/Project ACL；
- Delegation Scope；
- Agent Capability/Skill；
- Region/Residency；
- Agent Card / Federation Trust；
- External Agent 的 Project/Capability Grant 与 Data Boundary；
- Health/Circuit；
- Task Budget + External Procurement Budget（如适用）。

Soft Score 可复用 Routing Engine：latency / cost / load / quality / availability。每次 Agent Route 必须可解释。

## 18.8 Human Approval Traffic

Human Approval 是第四类 Interaction。高风险 Tool/Agent Action 可生成 `approval_request`，批准后签发短期一次性 Approval Grant。

要求：

- Approval 与原 Task/Request/Agent/Action 绑定；
- 有 TTL、approver、decision、reason；
- 禁止批准后扩大原 Delegation Scope；
- 重放必须失败；
- 审批本身写 Audit/Event，并可在 Agent Graph 中显示。

## 18.9 Agent Versioning

Agent Registry 区分稳定身份和可变版本：

```text
Agent Identity
  └─ Agent Version
       └─ Endpoint / Capability Snapshot
```

规则：

- `agent_id` 长期稳定；
- 每次发布生成不可变 `agent_version`；
- 版本状态：`draft → active → draining → retired`；
- 新 Task 默认解析当前 `active` 版本；
- 已启动 Task 可按 Policy 固定 `resolved_agent_version`；
- `draining` 版本不接收新 Task，但可完成已绑定 Task；
- `retired` 版本不得接收新调用；
- 回滚创建新的版本，不修改历史版本；
- 若下游 Runtime 无法同时保留旧 Endpoint，长任务必须返回 `AGENT_VERSION_UNAVAILABLE`，禁止静默切换语义不兼容版本。

Version Pin 不意味着 LiteAIG 托管 Agent 进程，只是治理/路由解析语义。

## 18.10 Capability Index

Model / Tool / Agent 的能力事实源分别是其 Resource Manifest：

```text
Model Deployment.capabilities
MCP Tool.schema / risk metadata
Agent Version.capabilities / Agent Card skills
```

Runtime 在编译 Tenant Snapshot 时生成 `CapabilityIndex`，用于查找、Policy Match、Capability Routing、Console 浏览和 Routing Simulator。

`CapabilityIndex` 是**派生索引，不是第二事实源**。任何能力修改必须先修改所属 Resource，再重新编译 Snapshot。

## 18.11 Team / Mesh 的范围

V8.2 不把 `Agent Team` 和 `Agent Mesh` 设为 Core 一级运行对象。

P2 前优先采用：

```text
Agent Label / Agent Group
Task Graph
Delegation Graph
Project / Policy Scope
```

形成团队/协作拓扑视图。

只有真实客户场景证明需要独立 Team/Mesh 生命周期、专属 ACL 或预算后，才允许引入持久化 `agent_groups`；优先使用统一 Group 抽象，而不是同时维护 Team/Mesh 两套平行模型。

## 18.12 External / Federated Agent Trust

### 18.12.1 资源模型

LiteAIG 统一使用 `Agent` 资源，但必须显式区分：

```text
Agent.owner_scope
  INTERNAL
  EXTERNAL_FEDERATED
```

- `INTERNAL`：本 Tenant 自有/可完整治理 Agent，Tenant/Project/Owner/Version/Delegation Policy 均可解析；
- `EXTERNAL_FEDERATED`：本 Tenant 对外部组织 Agent 的**受控投影**，LiteAIG 不拥有、不运行、不假设可见其内部 Policy/Memory/Workflow。

External Agent 默认是 Tenant 级信任资源，不由某个 Project 单方面“拥有”；哪些 Project 可以使用由独立 Project Grant 控制。

对于 `bidirectional` Relationship，Project/Capability Grant 必须显式标记 `inbound/outbound`；禁止“允许我们调用对方”自动推导成“允许对方调用我们”。

### 18.12.2 Discovery 与 Trust 分离

```text
Discover Agent Card / Registry Entry
  → Candidate
  → Verify Trust Anchor
  → Review Metadata / Data Boundary / Cost
  → Grant Projects / Capabilities
  → Validate / Diff
  → Activate Relationship
```

**Discovery != Trust**。

自动发现不得：

- 自动激活外部 Agent；
- 自动接受 Agent Card 新 Capability；
- 自动接受新 Endpoint/Auth Scheme；
- 自动接受 Publisher/Trust Key 变化。

Material Change 至少包括：

```text
endpoint
protocol/auth scheme
capability/skill
publisher identity
trust anchor / key material
data processing declaration
pricing model
```

Material Change 生成 Pending Review；旧批准版本在其 Trust TTL 内按 Policy 继续或暂停，不能被新 Card 静默覆盖。

### 18.12.3 Trust Anchor

A2A Signed Agent Card 可以提供 JWS 完整性/来源验证，但 LiteAIG 不把 JWS 设为唯一企业信任机制。

支持：

```text
JWS/JWK Thumbprint
mTLS CA/SPKI Pin
OIDC Issuer + Subject/Audience
Trusted Registry Attestation
Composite Profile
```

Active External Relationship 不允许“无验证信任”：

```text
relationship.active
  ⇒ at least one active verified trust anchor
```

Trust Anchor 支持 `active / retiring / revoked` 与有效期，允许密钥轮换重叠窗口。任何本地 `revoked` 立即优先于远端缓存。

### 18.12.4 Outbound

```text
Internal User/Agent
  → Local Permission / Delegation
  → Federation Relationship
  → Project Grant
  → Capability Grant
  → DLP / Data Boundary
  → External Budget
  → Approved Endpoint
  → External Agent
```

默认：

- 只调用 Approved Agent Version / Card Snapshot 中的 Endpoint；
- 外部 Card 新增 Endpoint 不自动进入 Failover；
- 非幂等/副作用未知操作默认不 Retry；
- 只有操作明确 idempotent，或协议/业务提供可靠 idempotency key 时才允许配置 Retry；
- External Response 标记 `provenance=external_agent_response`，再次经过 `AGENT_RESPONSE`/Context Guard。

### 18.12.5 Inbound

```text
External Agent
  → TLS/Auth
  → Federation Relationship Resolution
  → External Principal
  → Project/Capability Grant
  → Guardrail / Budget / Rate
  → Internal Agent/Tool/Model
```

规则：

- 远端自报的内部 `user_id/tenant_id/org_unit/delegation` 一律不可信；
- 外部 Caller 只能获得 Relationship 显式授予的本地 Capability；
- 默认不接受“跨组织可传递 Delegation”；未来如支持可验证 Delegation Token，必须作为独立扩展；
- Inbound External Agent 可独立 RateLimit/Budget/Concurrency；
- 外部输入默认更高风险级别并完整进入 Guardrail。

### 18.12.6 Data Processing Boundary

Federation Relationship 至少维护：

```text
data_boundary_status: unknown | declared | contractually_bound
processing_regions
retention_class
training_use: unknown | prohibited | allowed
subprocessor_summary
contract_or_dpa_ref
last_reviewed_at
```

`Project.residency_enforcement=strict`：

- `unknown` 默认 Hard Reject；
- 明确 Region 不满足 Project Allowed Region 时 Hard Reject；
- 风险例外必须通过独立 Config Draft / 高风险审批并有过期时间，不能通过普通 Runtime Header 临时绕过。

### 18.12.7 Revocation

`relationship.suspend/revoke` 属于 Security Tighten：

- 触发 Tenant `security_epoch`；
- Fast Publish；
- 新请求立即拒绝；
- 已建立长流是否中断由 Policy 决定，高风险默认取消；
- LKG 不允许绕过已知的本地 revocation；
- 如果 Data Plane 在 Revocation 发生前已经与 Control/Security Channel 隔离，它无法知道未来的撤销事实，因此 Strict Federation 必须配置 `max_security_epoch_staleness`；超过 freshness 阈值后仅停止新的 Federated Traffic，而普通 Model Traffic 可继续按 LKG 策略运行；
- Security Channel 恢复并确认最新 Epoch 后再恢复 Federated Traffic。

---

# 19. Guardrail Policy 与安全策略发布

## 19.1 Policy 模型

```yaml
name: prod-customer-service
scope:
  project: customer-service

input:
  pii:
    mode: enforce
    action: mask
  secrets:
    mode: enforce
    action: block
  prompt_injection:
    provider: builtin
    threshold: 0.80
    action: block

output:
  pii:
    action: redact
  harmful_content:
    action: block

stream:
  layer1: enabled
  layer2:
    boundary_tokens: 64
  layer3:
    enabled: true
    provider: external-security

external:
  fail_mode: fail_closed
```

Console 以 Wizard / Template 为主，不把 YAML 作为主要操作入口。

## 19.2 Tighten / Loosen 判定

每种 Rule Type 声明自己的 `risk_direction` 和 action severity，不能用统一“阈值变小就是收紧”规则。

```go
type PolicyDiffClass string
const (
    DiffTighten   PolicyDiffClass = "tighten"
    DiffLoosen    PolicyDiffClass = "loosen"
    DiffAmbiguous PolicyDiffClass = "ambiguous"
)
```

- 明确 Tighten：允许 Fast Publish；
- Loosen：二次确认，Enterprise 可双人审批；
- Ambiguous：按 Loosen 处理；
- Federated Relationship `suspend/revoke`、Trust Anchor `revoke`、Project/Capability Grant 收紧：Tighten；
- 新增/扩大 External Trust、Grant、Endpoint、Data Boundary Exception：High/Critical Loosen/Expansion。

## 19.3 Security Epoch

安全配置单独维护 `security_epoch`。

Fast Publish：

- Redis Pub/Sub / process channel 立即推送；
- 常规 Snapshot poll 兜底；
- Data Plane 暴露 `active_security_epoch`；
- Console 显示所有副本收敛状态。

Enterprise 可配置：

```text
security.max_epoch_staleness = 2
```

超过阈值的副本 readiness=false；默认 Standard 不因 Control Plane 临时不可用自动下线。

---

# 20. Multi-Tenancy / Organization / Policy

## 20.1 统一层级模型

LiteAIG 固定采用：

```text
Platform
  └─ Tenant
       ├─ Project                ← AI 资源治理轴
       │   ├─ Application
       │   ├─ INTERNAL Agent
       │   ├─ Service Account
       │   └─ API Key
       │
       ├─ Federated Agent Trust  ← Tenant 级外部信任边界
       │   └─ Project / Capability Grants
       │
       └─ Organization           ← 组织/成本归属轴
           └─ OrgUnit
               └─ User
```

核心定义：

- **Tenant**：最高业务隔离、数据隔离、安全、身份、Provider Credential、预算和结算边界；
- **Project**：Tenant 内 AI 应用/业务场景的资源治理边界；
- **OrgUnit**：部门/组织单元，用于组织权限、Showback/Chargeback 和部门用量统计；
- **User**：人员身份，可使用多个 Project；
- **Application / INTERNAL Agent / Service Account / API Key**：Project 内调用主体或资源主体；
- **EXTERNAL_FEDERATED Agent**：Tenant 级外部信任投影，不直接归属单一 Project，由 Project/Capability Grant 控制使用范围；
- Project 与 OrgUnit 不做父子绑定，二者是正交维度。

禁止设计为 `Tenant → Department → Project → User`，因为部门和 Project 是多对多关系。

## 20.2 Tenant 生命周期

```text
provisioning → active → suspended → deleting → deleted
```

规则：

- `active`：正常调用；
- `suspended`：数据面拒绝新调用，Control Plane 只允许恢复/审计/导出；
- `deleting`：禁止创建资源，按 retention/合规流程清理；
- `deleted`：逻辑删除完成，保留法定财务/审计数据；
- Tenant Provision 自动创建 `Default Project`；
- 单租户部署 UI 可隐藏 Tenant 切换器，但数据库和 Runtime 仍保持 Tenant 边界。

## 20.3 Project 资源治理

Project 绑定：

```text
Route Policy
Guardrail Policy
Cache Policy
Budget Policy
Rate Policy
Model Allowlist
Data Residency
Application / INTERNAL Agent / Service Account / API Key
Federated Agent Project/Capability Grant
```

Project 在存储层必须存在；小团队默认只使用自动创建的 `Default Project`，避免增加使用复杂度。

## 20.4 Organization / User

OrgUnit 支持树状层级：

```text
Tenant
├─ 研发中心
│  ├─ 平台研发部
│  └─ AI研发部
├─ 市场部
└─ 客服部
```

User 与 OrgUnit 关系具有有效期，人员调岗时保留历史。

一次模型调用的 Usage Event 记录**调用发生当时**的：

```text
user_id
org_unit_id
org_path_snapshot
cost_center_id
```

后续人员调岗不能改变历史账期的部门成本归属。

## 20.5 两条正交治理轴

### Resource Axis

```text
Tenant → Project → Application/INTERNAL Agent/API Key
Tenant → Federated Agent Trust → Project/Capability Grant
```

用于：

- 模型授权；
- Provider/Deployment；
- 路由；
- Guardrail；
- Cache；
- Rate Limit；
- Budget；
- SLA。

### Identity / Cost Axis

```text
Tenant → OrgUnit → User
```

用于：

- 用户身份；
- 部门归属；
- 成本中心；
- 人员/部门 Token 与成本统计；
- 用户/部门预算（P1）；
- Chargeback。

一次 Request 同时带两条轴的 ID，在 Usage Ledger 汇合。

## 20.6 Policy 继承

基础策略：

```text
System Policy
    ↓
Tenant Policy
    ↓
Project Policy
    ↓
Principal Policy
    ↓
EffectivePolicy
```

Principal Policy 可来自 API Key / Application / Agent / User。

合并规则：

- allowlist：子级只能取交集；
- limit：子级只能更严格；
- security mandatory rule：子级不能关闭；
- route/cache/guardrail binding：允许在父级授权范围内覆盖；
- Data Residency：子级只能收紧；
- Provider Scope：Tenant 只能使用 SYSTEM_SHARED 或本 Tenant 私有资源；
- 无可信 `user_id` 时，不执行 User/OrgUnit 强制预算，只做 Project/Key 级治理。

## 20.7 Data Residency

```sql
ALTER TABLE model_deployments
  ADD COLUMN data_region TEXT NOT NULL DEFAULT 'unspecified',
  ADD COLUMN data_processing_boundary TEXT NOT NULL DEFAULT 'unspecified';

ALTER TABLE projects
  ADD COLUMN allowed_data_regions TEXT[] NULL,
  ADD COLUMN residency_enforcement TEXT NOT NULL DEFAULT 'advisory'
    CHECK (residency_enforcement IN ('advisory','strict')),
  ADD COLUMN external_data_assurance_min TEXT NOT NULL DEFAULT 'declared'
    CHECK (external_data_assurance_min IN ('declared','contractually_bound'));
```

Strict 模式：不满足地域要求的 Deployment 在 Routing Hard Constraints 阶段直接淘汰。

External / Federated Agent 同样属于 Data Residency Hard Constraint：

```text
strict project
  AND external relationship.data_boundary_status = unknown
    → reject

strict project
  AND external processing_regions not compatible with allowed_data_regions
    → reject

strict project
  AND relationship assurance below project.external_data_assurance_min
    → reject
```

`acknowledged risk` 不作为数据模型中的永久“通行证”。如企业确需例外，应生成有 Scope/Reason/Expiry 的高风险 Config Change，并进入 Audit/Evidence Export。

## 20.8 数据隔离不变量

所有 Tenant 数据必须满足：

1. 业务表具有 `tenant_id`；
2. Repository API 必须显式接收 `TenantScope`，禁止无 Scope 查询 Tenant 表；
3. PostgreSQL Standard/Enterprise 默认启用 RLS；
4. Redis/Valkey Key 必须包含 Tenant namespace；
5. Cache / Vector / Analytics / Object Storage 路径必须 Tenant 隔离；
6. Runtime Snapshot 按 Tenant 独立；
7. Provider Credential 默认 Tenant 私有；
8. Admin API 的 Tenant Scope 来自服务端授权上下文，不能信任客户端任意传入的 `tenant_id`；
9. Audit / Security Event / Usage Event 全部记录 `tenant_id`；
10. 跨 Tenant 聚合仅允许平台授权角色执行。

部署约束：Lite/SQLite 仅提供 Repository 层逻辑隔离，适合单企业/可信团队；互不信任客户的多租户 SaaS 必须使用 Standard/Enterprise，并启用 PostgreSQL RLS、独立 Secret Scope 与租户隔离测试。

### PostgreSQL RLS

应用连接使用受 RLS 限制的数据库角色：

```sql
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_projects
ON projects
USING (
  tenant_id = current_setting('liteaig.tenant_id', true)::uuid
)
WITH CHECK (
  tenant_id = current_setting('liteaig.tenant_id', true)::uuid
);
```

事务开始时由服务端设置：

```sql
SET LOCAL liteaig.tenant_id = '<authorized-tenant-id>';
```

平台级运维使用单独受控 DB Role，不通过普通 Tenant API 连接绕过 RLS。

## 20.9 Redis / Valkey Namespace

禁止使用一个全局 `{tenant_id}` Hash Tag 包住所有状态，避免单 Tenant 把全部数据压在一个 Redis Cluster Slot。

规范：

```text
Budget:
  budget:{budget:<tenant_id>}:tenant:<id>:1d
  budget:{budget:<tenant_id>}:project:<id>:1d
  budget:{budget:<tenant_id>}:key:<id>:1h

Rate:
  rate:{rate:<tenant_id>:<scope_type>:<scope_id>}:rpm

Concurrency:
  concurrency:{lease:<tenant_id>:<deployment_id>}:leases

Idempotency:
  idem:{idem:<tenant_id>:<project_id>}:<idempotency_key>

Cache:
  cache:<tenant_id>:<project_id>:<hash>
```

只有需要同一 Lua 原子操作的 Key 才共享 Hash Tag。

## 20.10 Cache / Semantic Cache 隔离

Cache Key 至少包含：

```text
tenant_id
project_id
logical_model
normalized_request
semantic_affecting_params
cache_namespace_version
```

默认禁止：

- 跨 Tenant 共享 Exact Cache；
- 跨 Tenant Vector Search；
- 跨 Project Semantic Cache。

Enterprise 即使使用共享 Vector Backend，也必须使用 Tenant+Project namespace/filter 双重隔离。

## 20.11 Tenant Runtime Snapshot

Tenant Publish 只替换本 Tenant Snapshot：

```text
Tenant A v18 → v19
Tenant B v27 保持不变
Tenant C v6  保持不变
```

System Shared Provider / Pricing / Mandatory Security Policy 变化时，由依赖索引找出受影响 Tenant 并重编译，不执行全平台无差别热重载。

## 20.12 部门/人员 Token 统计

Usage Ledger 必须支持：

```text
Tenant
Project
Application
Agent
Federation Relationship
Trust Boundary / Direction
API Key
OrgUnit
User
Logical Model
Provider
Deployment
Region
Tag
```

典型查询：

```sql
-- 某人员一个账期的 Token/成本
SELECT
  user_id,
  SUM(total_tokens) AS total_tokens,
  SUM(provider_cost) AS provider_cost,
  SUM(customer_charge) AS charge
FROM usage_events
WHERE tenant_id = $1
  AND user_id = $2
  AND ts >= $3 AND ts < $4
GROUP BY user_id;

-- 某部门一个账期的 Token/成本
SELECT
  org_unit_id,
  SUM(total_tokens) AS total_tokens,
  SUM(provider_cost) AS provider_cost,
  SUM(customer_charge) AS charge
FROM usage_events
WHERE tenant_id = $1
  AND org_unit_id = $2
  AND ts >= $3 AND ts < $4
GROUP BY org_unit_id;
```

部门包含下级部门的统计由 `org_path_snapshot` 或组织闭包索引计算。

## 20.13 身份归属可信度

正式财务统计按以下优先级：

```text
verified delegated user JWT / OIDC
  > API Key 固定绑定主体
  > trusted application mapping
  > client-supplied business label
```

只有 `verified` / `key_bound` 身份可用于强制 User Budget、正式 Chargeback 或审计追责。

---

# 21. Observability Engine

## 21.1 四类数据分离

1. Usage Ledger：计量计费；
2. Request Log：诊断；
3. Security Event：Guardrail；
4. Audit Log：配置/管理操作。

## 21.2 Request Explorer

单请求展示：

```text
Tenant / Project / Resource Identity
User / OrgUnit / Agent / Delegation Trust
Session / Task / Root Task / Agent Hop
Policy / Snapshot Version
Input Guardrail
Budget / Rate Limit
Cache Decision
Route Candidates & Scores
Selected Deployment/Credential
Retry / Fallback
Provider Timing / TTFT
Stream Guardrail
Output Guardrail
Usage
Provider Cost / Charge
Final Status
```

时间线：

```text
Admission               0.3 ms
Fast Input Guard        1.0 ms
External Guard         80.0 ms   [separate]
Budget/RateLimit         0.7 ms
Cache                    0.2 ms
Routing                  0.4 ms
Provider               920.0 ms  [external]
Output Fast Guard        1.1 ms
Accounting sync          0.2 ms
```

External Guardrail / Semantic Embedding / Provider 网络不计入 Core Overhead。

## 21.3 Agent / Task Graph

Agentic 请求支持从单 Request Timeline 升级为 Task Graph：

```text
User
  ↓ delegation
Agent A
  ├─ Model Call
  ├─ Tool Call
  └─ Agent B
       ├─ Tool Call
       └─ Agent C
```

每条 Edge 至少记录：protocol、decision、latency、tokens、cost、guardrail、status、delegated_scope。图数据来自 Request/AgentCall/ToolCall/Task Event，不依赖 Prompt 正文。

## 21.4 Metrics

至少：

- request_total / success_rate；
- gateway_core_latency；
- provider_latency / ttft；
- token_total；
- provider_cost / charge；
- cache_hit；
- retry / fallback；
- circuit_state；
- rate_limit_reject；
- budget_reject；
- guardrail_block / mask / fail_open；
- config_drift；
- reservation_sweeper_expired；
- lease_expired_cleanup。

Prometheus / OTel Metrics 只使用低基数标签（provider/model/deployment/status/cache/guardrail 等）。  
`tenant_id/project_id/user_id/org_unit_id/api_key_id` 等高基数 FinOps 维度进入 Usage Ledger / Analytics，不作为默认 Prometheus Label，避免指标系统基数爆炸。

## 21.5 OTel

使用 OpenTelemetry GenAI 兼容语义字段，输出 OTLP。

LiteAIG 不建设完整 Trace Storage；Console 支持跳转 Tempo/Jaeger/第三方平台。

## 21.6 HA & Resilience Metrics

至少：

```text
gateway_ready_replicas
gateway_active_requests
gateway_active_streams
gateway_drain_state
gateway_dependency_degraded

control_plane_ready_replicas
control_plane_leader_task

runtime_bundle_active_version
runtime_bundle_last_good_version
runtime_bundle_age_seconds
runtime_bundle_load_failures
security_epoch_staleness

postgres_primary_up
postgres_replication_lag_seconds
postgres_failover_total

valkey_primary_up
valkey_replication_lag
valkey_failover_total

accounting_spool_bytes
accounting_spool_events
accounting_spool_oldest_age_seconds
accounting_flush_lag_seconds
accounting_replay_total

secret_cache_age_seconds
secret_provider_errors

zone_ready_replicas
region_ready_replicas
dr_failover_state
```

Console `Operations → HA & Resilience` 展示：

- Region / AZ / Node / Pod 健康；
- RuntimeBundle 收敛与 LKG；
- PostgreSQL / Valkey Failover；
- Accounting Spool；
- Secret Provider；
- Drain / Rolling Upgrade；
- 当前 Dependency Degradation；
- 最近 Chaos/DR Drill 结果。

---

# 22. Alert Engine 与成本异常

## 22.1 Alert 生命周期

```text
pending → firing → acknowledged → resolved
```

支持：

- dedupe；
- silence；
- maintenance window；
- escalation；
- acknowledgment。

## 22.2 通知

P0：Webhook + Console。  
P1：Email / Slack / Teams / PagerDuty。

失败重试：

```text
30s → 1m → 2m → 5m → 10m
最多 5 次
```

全部失败后：

- 写 `alert_delivery_failures`；
- 产生 `notification_channel_down` 元告警；
- Console 必须始终可见。

## 22.3 成本异常

检测：

- EWMA + 同小时季节性；
- token/request 归一化；
- Provider unit price change；
- retry/fallback spike；
- cache hit drop；
- budget risk；
- agent loop / excessive hops；
- task cost/token/duration risk；
- agent delegation deny spike。

告警类型：

```text
COST_SPIKE
TOKEN_SPIKE
CACHE_HIT_DROP
PROVIDER_PRICE_CHANGE
RETRY_COST_SPIKE
TENANT_BUDGET_RISK
PROJECT_BUDGET_RISK
AGENT_LOOP_DETECTED
TASK_BUDGET_RISK
AGENT_DELEGATION_DENY_SPIKE
ORG_COST_SPIKE
USER_COST_SPIKE
```

---

# 23. Config Plane

## 23.1 配置对象与 ChangeSet

所有会改变生产数据面行为的配置修改必须进入服务端持久化 `Config Draft / ChangeSet`，不允许 Web Console 通过普通 Toggle 直接绕过发布流程。

配置修改分为两类：

```text
Configuration Change
  → Provider/Deployment/Logical Model/Route/Guardrail/Cache/Budget/Rate/Data Residency 等
  → Draft → Validate → Diff → Publish → Activate

Operational Action
  → Disable/Revoke Key、Disable Deployment、Reset Circuit、Ack Alert、Suspend Tenant 等
  → Immediate Action API → Impact Confirm/Re-auth(按风险) → Audit
```

两者必须在 API、审计事件和 UI 交互上明确区分。

`ConfigDraft`：

```go
type ConfigDraft struct {
    ID            string
    ScopeType     string // system|tenant
    TenantID      string // system draft 时为空
    BaseVersion   int64
    Revision      int64
    Status        string // editing|pending_approval|published|discarded|expired
    CreatedBy     string
    UpdatedBy     string
    CreatedAt     time.Time
    UpdatedAt     time.Time
    ExpiresAt     *time.Time
}
```

Draft 保存资源 Patch/ChangeSet，不保存 Provider Secret 明文。

## 23.2 发布流程

```text
Create/Open Draft
→ Edit ChangeSet
→ Static Validate
→ Compile Candidate TenantRuntimeSnapshot / GlobalRuntime
→ Diff / Dependency Impact / Risk Analysis
→ Optional Playground / Policy Preview
→ Publish Version N
→ Data Plane Prepare
→ Activate
→ Observe
→ Rollback if needed
```

## 23.3 并发编辑与 Rebase

Draft 必须携带 `base_version` 与 `revision`：

- Patch 使用乐观锁 `If-Match: draft_revision`；
- 若当前 Active Version 已超过 Draft Base Version，Publish 前必须执行 Rebase；
- 自动 Rebase 仅允许无冲突字段；
- 同一资源同一字段被双方修改时标记 Conflict，禁止静默覆盖；
- Diff Viewer 必须展示 Rebase 后的最终差异，而不是旧 Base 上的差异。

## 23.4 Validator

至少：

- credential reference；
- route cycle；
- duplicate alias；
- deployment availability；
- budget reference；
- guardrail schema；
- policy conflict；
- residency conflict；
- tenant/provider/credential scope conflict；
- org/user membership integrity；
- EXTERNAL_FEDERATED Agent owner_scope/project/external_subject consistency；
- Federated Relationship active → verified Trust Anchor；
- Relationship Project/Capability Grant 与 Tenant Scope、Direction；
- 同一 External Agent 不允许存在语义重叠的 Active `bidirectional` 与单向 Relationship；
- Inbound Grant 不能由 Outbound Grant 隐式继承，反之亦然；
- External Data Boundary / Project assurance conflict；
- approved Agent Version/Card Hash/Endpoint material-change conflict；
- unknown External Pricing 与 hard cost policy conflict；
- cache policy safety；
- pricing validity；
- integration dependency；
- mandatory tenant/system security policy cannot be weakened；
- test principal scope validity。

Validator 结果：

```text
ERROR   → 禁止发布
WARNING → 可发布，但必须确认
INFO    → 仅说明影响
```

## 23.5 Risk / Impact Analysis

发布前必须生成：

- Risk Level：Low / Medium / High / Critical；
- 受影响 Tenant/Project/Logical Model/Deployment；
- 可能中断的 API Key / Application；
- Route 主流量变化；
- Guardrail Tighten / Loosen 分类；
- Budget/Rate Limit 变化；
- Data Residency 变化；
- Shared Provider 依赖 Tenant 数；
- 是否需要 Re-auth / 双人审批。

Secret 值不进入 Diff；只显示 `credential_ref changed`、fingerprint 变化和资源引用变化。

## 23.6 Guardrail Fast Publish

安全策略使用独立 `SecurityEpoch`：

- 明确 Tighten：允许“立即收紧并发布”，仍必须生成审计与版本；
- Loosen：必须经过标准 Diff/确认；Enterprise 可要求双人审批；
- Ambiguous：按 Loosen 处理；
- Console 中普通 ON/OFF 开关只修改 Draft，不自动生产生效。

## 23.7 分发

- Lite：进程内同步；
- Standard：PostgreSQL LISTEN/NOTIFY + 30s poll；
- Enterprise：Redis Pub/Sub + version poll；
- Guardrail Fast Publish：额外 Security Epoch channel；
- Tenant 配置发布仅替换该 Tenant Runtime Snapshot；
- System 共享资源变更按依赖关系重编译受影响 Tenant。

目标常规收敛 ≤3s。

## 23.8 Drift

新版本 compile 失败：

1. 当前请求继续旧 snapshot；
2. 节点标记 `config_drift=true`；
3. 告警；
4. 超过 `drift_grace_period` 后 readiness=false；
5. Console 显示节点 active version / security epoch / drift reason。

## 23.9 Rollback

历史版本回滚生成新版本，不覆盖历史：

```text
v9 → v10(bad) → rollback(v9) => v11
v11.rollback_of = 9
```

Rollback 属于 Configuration Change；High Risk 回滚必须展示当前版本与目标版本的反向 Diff，并按风险要求 Re-auth/审批。

---

# 24. 数据模型与存储

## 24.1 核心关系

```text
Platform
  └─ Tenant
       ├─ Project
       │   ├─ Application
       │   ├─ INTERNAL Agent
       │   ├─ Service Account
       │   └─ API Key
       │
       ├─ Federated Agent Trust
       │   ├─ EXTERNAL_FEDERATED Agent Projection
       │   ├─ Trust Anchor
       │   └─ Project / Capability Grant
       │
       ├─ Organization
       │   └─ OrgUnit
       │       └─ User Membership
       │
       ├─ Tenant-private Provider/Credential
       └─ Policies

System Shared Provider
  └─ Tenant Deployment Reference

Logical Model
  └─ Route Policy
       └─ Deployment Set
```

## 24.2 Tenant / Project

```sql
CREATE TABLE tenants (
    id UUID PRIMARY KEY,
    public_ref TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
      CHECK (status IN ('provisioning','active','suspended','deleting','deleted')),
    settlement_currency TEXT NOT NULL DEFAULT 'USD',
    default_project_id UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    suspended_at TIMESTAMPTZ NULL,
    deleted_at TIMESTAMPTZ NULL
);

CREATE TABLE projects (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,

    route_policy_id UUID NULL,
    guardrail_policy_id UUID NULL,
    cache_policy_id UUID NULL,
    budget_policy_id UUID NULL,
    rate_policy_id UUID NULL,

    allowed_data_regions TEXT[] NULL,
    residency_enforcement TEXT NOT NULL DEFAULT 'advisory'
      CHECK (residency_enforcement IN ('advisory','strict')),
    external_data_assurance_min TEXT NOT NULL DEFAULT 'declared'
      CHECK (external_data_assurance_min IN ('declared','contractually_bound')),

    status TEXT NOT NULL DEFAULT 'active'
      CHECK (status IN ('active','disabled','deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ NULL,

    UNIQUE(tenant_id, name)
);
CREATE INDEX idx_projects_tenant ON projects(tenant_id) WHERE deleted_at IS NULL;
```

## 24.3 用户、组织与成员关系

```sql
CREATE TABLE users (
    id UUID PRIMARY KEY,
    display_name TEXT NOT NULL,
    email TEXT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tenant_memberships (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'active',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    left_at TIMESTAMPTZ NULL,
    PRIMARY KEY (tenant_id, user_id)
);

CREATE TABLE external_identities (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NOT NULL REFERENCES users(id),
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    email_claim TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, issuer, subject)
);

CREATE TABLE cost_centers (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    UNIQUE(tenant_id, code)
);

CREATE TABLE org_units (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    parent_id UUID NULL REFERENCES org_units(id),
    name TEXT NOT NULL,
    code TEXT NULL,
    cost_center_id UUID NULL REFERENCES cost_centers(id),
    path TEXT NOT NULL, -- 稳定ID路径，如 /<root-id>/<dept-id>
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, parent_id, name)
);
CREATE INDEX idx_org_units_tenant_parent ON org_units(tenant_id, parent_id);
CREATE INDEX idx_org_units_tenant_path ON org_units(tenant_id, path);

CREATE TABLE user_org_assignments (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID NOT NULL REFERENCES users(id),
    org_unit_id UUID NOT NULL REFERENCES org_units(id),
    is_primary BOOLEAN NOT NULL DEFAULT true,
    valid_from TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_to TIMESTAMPTZ NULL
);
CREATE INDEX idx_user_org_active
  ON user_org_assignments(tenant_id, user_id, valid_from DESC);
```

`user_org_assignments` 使用有效期而不是覆盖更新，保证历史组织归属可追溯。

`external_identities` 以 `(tenant_id, issuer, subject)` 唯一映射企业 IdP 身份；同一自然人可加入多个 Tenant，但任何 IdP Subject 的解析必须先限定 Tenant。

## 24.4 Application / Agent / Service Account

```sql
CREATE TABLE applications (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, project_id, name)
);

CREATE TABLE agents (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NULL REFERENCES projects(id),

    owner_scope TEXT NOT NULL DEFAULT 'INTERNAL'
      CHECK (owner_scope IN ('INTERNAL','EXTERNAL_FEDERATED')),
    external_subject TEXT NULL,

    application_id UUID NULL REFERENCES applications(id),
    service_account_id UUID NULL,
    name TEXT NOT NULL,
    owner_user_id UUID NULL REFERENCES users(id),
    risk_level TEXT NOT NULL DEFAULT 'medium',
    status TEXT NOT NULL DEFAULT 'active',
    active_version INT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (
      (owner_scope = 'INTERNAL' AND project_id IS NOT NULL AND external_subject IS NULL)
      OR
      (owner_scope = 'EXTERNAL_FEDERATED' AND project_id IS NULL AND external_subject IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_internal_agent_name
  ON agents(tenant_id, project_id, name)
  WHERE owner_scope = 'INTERNAL';

CREATE UNIQUE INDEX uq_external_agent_subject
  ON agents(tenant_id, external_subject)
  WHERE owner_scope = 'EXTERNAL_FEDERATED';

CREATE TABLE agent_versions (
    agent_id UUID NOT NULL REFERENCES agents(id),
    version INT NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    spec JSONB NOT NULL,
    schema_version TEXT NOT NULL DEFAULT 'v1',
    status TEXT NOT NULL
      CHECK (status IN ('draft','pending_review','active','draining','retired')),
    compatibility_class TEXT NULL,
    published_at TIMESTAMPTZ NULL,
    drain_until TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(agent_id, version)
);
CREATE INDEX idx_agent_versions_tenant_status
  ON agent_versions(tenant_id, status, agent_id);

CREATE TABLE service_accounts (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    application_id UUID NULL REFERENCES applications(id),
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## 24.5 Provider / Credential / Deployment

```sql
CREATE TABLE providers (
    id UUID PRIMARY KEY,
    tenant_id UUID NULL REFERENCES tenants(id),
    owner_scope TEXT NOT NULL
      CHECK (owner_scope IN ('SYSTEM_SHARED','TENANT_PRIVATE')),
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'enabled',

    CHECK (
      (owner_scope = 'SYSTEM_SHARED' AND tenant_id IS NULL)
      OR
      (owner_scope = 'TENANT_PRIVATE' AND tenant_id IS NOT NULL)
    )
);

CREATE TABLE provider_credentials (
    id UUID PRIMARY KEY,
    provider_id UUID NOT NULL REFERENCES providers(id),
    tenant_id UUID NULL REFERENCES tenants(id),
    owner_scope TEXT NOT NULL
      CHECK (owner_scope IN ('SYSTEM_SHARED','TENANT_PRIVATE')),
    label TEXT NOT NULL,
    secret_ref TEXT NOT NULL,
    weight INT NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'enabled',
    fingerprint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE credential_pools (
    id UUID PRIMARY KEY,
    tenant_id UUID NULL REFERENCES tenants(id),
    provider_id UUID NOT NULL REFERENCES providers(id),
    owner_scope TEXT NOT NULL,
    name TEXT NOT NULL,
    strategy TEXT NOT NULL DEFAULT 'weighted'
);

CREATE TABLE credential_pool_members (
    pool_id UUID NOT NULL REFERENCES credential_pools(id),
    credential_id UUID NOT NULL REFERENCES provider_credentials(id),
    weight INT NOT NULL DEFAULT 1,
    PRIMARY KEY(pool_id, credential_id)
);

CREATE TABLE model_deployments (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    provider_id UUID NOT NULL REFERENCES providers(id),
    credential_pool_id UUID NOT NULL REFERENCES credential_pools(id),

    upstream_model TEXT NOT NULL,
    endpoint TEXT NULL,
    region TEXT NULL,
    data_region TEXT NOT NULL DEFAULT 'unspecified',
    data_processing_boundary TEXT NOT NULL DEFAULT 'unspecified',

    capabilities JSONB NOT NULL DEFAULT '{}',
    context_window INT NULL,
    max_output_tokens INT NULL,
    priority INT NOT NULL DEFAULT 0,
    capacity INT NULL,
    status TEXT NOT NULL DEFAULT 'enabled'
);
CREATE INDEX idx_deployments_tenant ON model_deployments(tenant_id, status);

CREATE TABLE logical_models (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    alias TEXT NOT NULL,
    route_policy_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, alias)
);
```

Control Plane Validator 必须检查 Provider/Credential/Pool/Deployment Scope 一致性。

## 24.6 API Key

```sql
CREATE TABLE api_keys (
    id UUID PRIMARY KEY,
    public_id TEXT NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,

    hmac_digest BYTEA NOT NULL,
    pepper_version INT NOT NULL,
    fingerprint TEXT NOT NULL,

    application_id UUID NULL REFERENCES applications(id),
    agent_id UUID NULL REFERENCES agents(id),
    service_account_id UUID NULL REFERENCES service_accounts(id),

    status TEXT NOT NULL CHECK (status IN ('active','disabled','revoked')),
    expires_at TIMESTAMPTZ NULL,
    model_allowlist TEXT[] NULL,
    ip_allowlist TEXT[] NULL,
    budget_policy_id UUID NULL,
    rate_policy_id UUID NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ NULL,

    UNIQUE(tenant_id, public_id)
);
CREATE INDEX idx_api_keys_tenant_project ON api_keys(tenant_id, project_id);
```

## 24.7 Guardrail 数据

```sql
CREATE TABLE guardrail_policies (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    active_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE guardrail_policy_versions (
    policy_id UUID NOT NULL,
    version INT NOT NULL,
    spec JSONB NOT NULL,
    security_epoch BIGINT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(policy_id, version)
);

CREATE TABLE guardrail_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY,
    ts TIMESTAMPTZ NOT NULL,
    request_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    project_id UUID NOT NULL,
    user_id UUID NULL,
    org_unit_id UUID NULL,

    checkpoint TEXT NOT NULL,
    provider TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    category TEXT NULL,
    risk_score DOUBLE PRECISION NULL,
    verdict TEXT NOT NULL,
    action TEXT NOT NULL,
    latency_ms INT NOT NULL,
    released_tokens INT NULL,
    content_hash TEXT NULL,
    masked_preview TEXT NULL,

    PRIMARY KEY(id, ts)
) PARTITION BY RANGE(ts);
```

默认不保存完整敏感内容。

Guardrail 人工复核用于误报分析和 Adaptive Governance：

```sql
CREATE TABLE guardrail_event_reviews (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    guardrail_event_id BIGINT NOT NULL,
    label TEXT NOT NULL
      CHECK (label IN ('confirmed','false_positive','uncertain')),
    reviewer_id UUID NOT NULL REFERENCES users(id),
    comment TEXT NULL,
    reviewed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_guardrail_reviews_tenant_event
  ON guardrail_event_reviews(tenant_id, guardrail_event_id);
```

复核操作写 Audit Log；评论不得保存未脱敏的 Prompt/Response 正文。

## 24.8 Usage Event / FinOps Ledger

```sql
CREATE TABLE usage_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY,
    event_id UUID NOT NULL,
    ts TIMESTAMPTZ NOT NULL,
    request_id UUID NOT NULL,
    session_id TEXT NULL,
    task_id UUID NULL,
    root_task_id UUID NULL,
    parent_task_id UUID NULL,
    parent_request_id UUID NULL,
    root_agent_id UUID NULL,
    caller_agent_id UUID NULL,
    agent_hop_count INT NOT NULL DEFAULT 0,

    federation_relationship_id UUID NULL,
    trust_boundary TEXT NOT NULL DEFAULT 'internal'
      CHECK (trust_boundary IN ('internal','external_federated')),
    interaction_direction TEXT NOT NULL DEFAULT 'internal'
      CHECK (interaction_direction IN ('internal','inbound','outbound')),

    -- Tenant / Resource Axis
    tenant_id UUID NOT NULL,
    project_id UUID NOT NULL,
    application_id UUID NULL,
    agent_id UUID NULL,
    service_account_id UUID NULL,
    key_id UUID NULL,

    -- Identity / Organization Axis
    user_id UUID NULL,
    org_unit_id UUID NULL,
    org_path_snapshot TEXT NULL,
    cost_center_id UUID NULL,
    attribution_trust TEXT NOT NULL DEFAULT 'none',

    -- Model / Provider
    logical_model TEXT NOT NULL,
    provider_id UUID NULL,
    deployment_id UUID NULL,
    credential_id UUID NULL,
    region TEXT NULL,

    -- Usage
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    cache_write_tokens BIGINT NOT NULL DEFAULT 0,
    reasoning_tokens BIGINT NOT NULL DEFAULT 0,
    audio_input_tokens BIGINT NOT NULL DEFAULT 0,
    audio_output_tokens BIGINT NOT NULL DEFAULT 0,
    image_input_units BIGINT NOT NULL DEFAULT 0,
    image_output_units BIGINT NOT NULL DEFAULT 0,
    tool_call_count BIGINT NOT NULL DEFAULT 0,
    agent_call_count BIGINT NOT NULL DEFAULT 0,
    external_agent_call_count BIGINT NOT NULL DEFAULT 0,
    tool_cost NUMERIC(18,9) NOT NULL DEFAULT 0,
    downstream_agent_cost NUMERIC(18,9) NOT NULL DEFAULT 0,
    external_agent_cost NUMERIC(18,9) NOT NULL DEFAULT 0,
    external_cost_source TEXT NULL,
    total_tokens BIGINT NOT NULL DEFAULT 0,

    usage_source TEXT NOT NULL,
    estimated BOOLEAN NOT NULL DEFAULT false,

    -- Cost
    provider_cost NUMERIC(18,9) NOT NULL DEFAULT 0,
    provider_currency TEXT NOT NULL DEFAULT 'USD',
    customer_charge NUMERIC(18,9) NOT NULL DEFAULT 0,
    charge_currency TEXT NOT NULL DEFAULT 'USD',
    pricing_rule_version INT NOT NULL,

    -- Runtime
    latency_ms INT NOT NULL,
    ttft_ms INT NULL,
    cache_status TEXT NULL,
    guardrail_status TEXT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    fallback_count INT NOT NULL DEFAULT 0,
    guardrail_regeneration_count INT NOT NULL DEFAULT 0,
    route_policy_version INT NULL,
    tenant_snapshot_version BIGINT NOT NULL,
    security_epoch BIGINT NOT NULL,
    status TEXT NOT NULL,
    error_type TEXT NULL,

    PRIMARY KEY(id, ts)
) PARTITION BY RANGE(ts);

CREATE UNIQUE INDEX uq_usage_event_id_ts
  ON usage_events(event_id, ts);
CREATE INDEX idx_usage_tenant_ts
  ON usage_events(tenant_id, ts DESC);
CREATE INDEX idx_usage_project_ts
  ON usage_events(tenant_id, project_id, ts DESC);
CREATE INDEX idx_usage_user_ts
  ON usage_events(tenant_id, user_id, ts DESC)
  WHERE user_id IS NOT NULL;
CREATE INDEX idx_usage_org_ts
  ON usage_events(tenant_id, org_unit_id, ts DESC)
  WHERE org_unit_id IS NOT NULL;
```

Usage Event 是不可变财务事实记录。`event_id + ts` 用于 Durable Spool 重放时幂等去重；Spool 必须保留原始 `event_id/ts`。人员调岗后不得回写历史 `org_unit_id/org_path_snapshot`。

`external_agent_cost` 是独立采购成本维度，但 Task 总成本计算仍必须包含它；`downstream_agent_cost` 作为 rollup 展示字段时不得与 leaf `external_agent_cost/provider_cost/tool_cost` 重复求和。

## 24.9 Usage Rollup

Standard 使用后台 Rollup 降低 Dashboard 扫明细成本：

```sql
CREATE TABLE usage_rollup_hourly (
    bucket TIMESTAMPTZ NOT NULL,
    tenant_id UUID NOT NULL,
    dimension_type TEXT NOT NULL, -- project|user|org_unit|application|agent|federation_relationship|model|provider
    dimension_id TEXT NOT NULL,

    requests BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    provider_cost NUMERIC(18,6) NOT NULL DEFAULT 0,
    customer_charge NUMERIC(18,6) NOT NULL DEFAULT 0,
    errors BIGINT NOT NULL DEFAULT 0,

    PRIMARY KEY(bucket, tenant_id, dimension_type, dimension_id)
);
```

复杂交叉分析（如“部门 × Project × Model”）：

- Standard：查询热 `usage_events` + 索引；
- Enterprise：推荐 ClickHouse Analytics Sink；
- 不在 PostgreSQL 中预生成高维笛卡尔 Cube。

### 24.9.1 Runtime Bundle Manifest

Control Plane 保存 Bundle 元数据用于审计与恢复：

```sql
CREATE TABLE runtime_bundle_manifests (
    tenant_id UUID NOT NULL,
    config_version BIGINT NOT NULL,
    security_epoch BIGINT NOT NULL,
    schema_version TEXT NOT NULL,
    checksum TEXT NOT NULL,
    signature_key_id TEXT NULL,
    object_ref TEXT NULL,
    status TEXT NOT NULL
      CHECK (status IN ('prepared','active','superseded','rejected')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ NULL,
    PRIMARY KEY(tenant_id, config_version)
);
```

Bundle Payload 可存 PostgreSQL、Object Storage 或由 Control Plane 重建；Data Plane 的 LKG 文件是本地恢复副本，不以此表作为请求热路径。

## 24.10 Request / Audit

Request Log 默认只记录 metadata，不记录 prompt/body。

必须包含：

```text
tenant_id
project_id
application_id/agent_id
federation_relationship_id / trust_boundary / interaction_direction
key_id
user_id
org_unit_id
request_id
```

完整 body 日志必须 Project 显式开启：

- sampling；
- PII mask；
- encryption at rest；
- 独立 retention，默认 7d。

Audit Log 记录管理操作，并强制带 `tenant_id`；平台级操作 `tenant_id` 可为空但必须标记 `scope=system`。

## 24.11 RLS 与 Repository Scope

所有 Tenant Repository 统一接口：

```go
type TenantScope struct {
    TenantID string
}

func (r *ProjectRepo) Get(ctx context.Context, scope TenantScope, id string) (*Project, error)
```

禁止：

```go
GetProject(ctx, id) // Tenant 表不允许此类无 Scope API
```

Standard/Enterprise 生产连接默认启用 PostgreSQL RLS 作为第二道防线。

## 24.12 数据保留

- usage hot：默认 90d；
- request body log：默认 7d；
- security event：默认 90d；
- audit：默认 365d；
- config versions：普通 30d+，高风险安全回滚点 90d+；
- Tenant 删除不立即删除法定财务/审计事实；
- `crosses_trust_boundary=true` 的 Task/Agent Call 元数据保留期不得短于普通 Agentic Event；若 Relationship/合规策略要求更长，则取更严格保留期；
- 超期可导出 Parquet/Object Storage，路径必须包含 Tenant namespace。

---

## 24.13 Console 支撑数据

### Config Draft

```sql
CREATE TABLE config_drafts (
    id UUID PRIMARY KEY,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('system','tenant')),
    tenant_id UUID NULL,
    base_version BIGINT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    status TEXT NOT NULL CHECK (status IN ('editing','pending_approval','published','discarded','expired')),
    changes JSONB NOT NULL DEFAULT '[]',
    source_type TEXT NOT NULL DEFAULT 'manual'
      CHECK (source_type IN ('manual','system_recommendation')),
    source_ref UUID NULL,
    created_by UUID NOT NULL,
    updated_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NULL
);
CREATE INDEX idx_config_drafts_tenant_status ON config_drafts(tenant_id, status, updated_at DESC);
```

`changes` 只保存非 Secret 配置 Patch；Secret 修改保存 `secret_ref`。

### Playground Saved Case

Playground 默认不持久化 Prompt/Response；只有用户显式点击“保存为测试用例”时才创建：

```sql
CREATE TABLE playground_cases (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    request_blob_ref TEXT NOT NULL,
    response_blob_ref TEXT NULL,
    labels JSONB NOT NULL DEFAULT '{}',
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`request_blob_ref/response_blob_ref` 指向 Tenant Scope 的 envelope-encrypted blob；默认不保存 Secret、Provider Credential、Authorization Header。

### Integration Instance

```sql
CREATE TABLE integration_instances (
    id UUID PRIMARY KEY,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('system','tenant')),
    tenant_id UUID NULL,
    integration_type TEXT NOT NULL,
    provider_key TEXT NOT NULL,
    name TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    secret_ref TEXT NULL,
    status TEXT NOT NULL DEFAULT 'enabled',
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Integration 只允许注册 §35 列出的有限能力类型。

## 24.14 Tool Governance 数据

```sql
CREATE TABLE mcp_servers (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    protocol_version TEXT NOT NULL DEFAULT '2026-07-28',
    transport TEXT NOT NULL CHECK (transport IN ('streamable_http','legacy_sse','stdio_proxy')),
    endpoint TEXT NOT NULL,
    auth_secret_ref TEXT NULL,
    status TEXT NOT NULL DEFAULT 'enabled',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, name)
);

CREATE TABLE mcp_tools (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    server_id UUID NOT NULL REFERENCES mcp_servers(id),
    name TEXT NOT NULL,
    schema JSONB NOT NULL,
    schema_hash TEXT NOT NULL,
    risk_level TEXT NOT NULL DEFAULT 'medium',
    status TEXT NOT NULL DEFAULT 'enabled',
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(server_id, name)
);

CREATE TABLE tool_call_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY,
    ts TIMESTAMPTZ NOT NULL,
    tenant_id UUID NOT NULL,
    project_id UUID NOT NULL,
    request_id UUID NOT NULL,
    session_id TEXT NULL,
    parent_request_id UUID NULL,
    application_id UUID NULL,
    agent_id UUID NULL,
    user_id UUID NULL,
    key_id UUID NULL,

    transport TEXT NOT NULL,
    server_id UUID NULL,
    tool_name TEXT NOT NULL,
    policy_id UUID NULL,
    decision TEXT NOT NULL
      CHECK (decision IN ('allow','deny','approval_required')),
    risk_level TEXT NULL,

    arguments_hash TEXT NULL,
    masked_arguments_preview TEXT NULL,
    result_hash TEXT NULL,
    result_status TEXT NULL,
    latency_ms INT NULL,
    tool_cost NUMERIC(18,9) NOT NULL DEFAULT 0,
    approval_id UUID NULL,

    PRIMARY KEY(id, ts)
) PARTITION BY RANGE(ts);

CREATE INDEX idx_tool_calls_tenant_session
  ON tool_call_events(tenant_id, session_id, ts DESC);
CREATE INDEX idx_tool_calls_tenant_agent
  ON tool_call_events(tenant_id, agent_id, ts DESC)
  WHERE agent_id IS NOT NULL;
```

`tool_call_events` 是运行时事实记录，不替代管理操作 `audit_logs`。默认只保存 hash/脱敏摘要。

## 24.15 Agentic Governance 数据

```sql
CREATE TABLE agent_endpoints (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    agent_id UUID NOT NULL REFERENCES agents(id),
    agent_version INT NOT NULL,
    protocol TEXT NOT NULL CHECK (protocol IN ('a2a','http','grpc')),
    protocol_version TEXT NULL,
    endpoint TEXT NOT NULL,
    region TEXT NULL,
    data_region TEXT NOT NULL DEFAULT 'unspecified',
    status TEXT NOT NULL DEFAULT 'enabled',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY(agent_id, agent_version) REFERENCES agent_versions(agent_id, version)
);

CREATE TABLE agent_capabilities (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    agent_id UUID NOT NULL REFERENCES agents(id),
    agent_version INT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NULL,
    schema JSONB NULL,
    risk_level TEXT NOT NULL DEFAULT 'medium',
    status TEXT NOT NULL DEFAULT 'enabled',
    PRIMARY KEY(agent_id, agent_version, name),
    FOREIGN KEY(agent_id, agent_version) REFERENCES agent_versions(agent_id, version)
);

CREATE TABLE agent_cards (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    agent_id UUID NOT NULL REFERENCES agents(id),
    endpoint_id UUID NOT NULL REFERENCES agent_endpoints(id),
    card JSONB NOT NULL,
    content_hash TEXT NOT NULL,
    signature_status TEXT NOT NULL DEFAULT 'unknown',
    etag TEXT NULL,
    last_modified TEXT NULL,
    verified_anchor_id UUID NULL,
    verified_at TIMESTAMPTZ NULL,
    material_change_status TEXT NOT NULL DEFAULT 'none'
      CHECK (material_change_status IN ('none','pending_review','approved','rejected')),
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NULL,
    PRIMARY KEY(agent_id, endpoint_id)
);

CREATE TABLE federated_agent_relationships (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    external_agent_id UUID NOT NULL REFERENCES agents(id),

    direction TEXT NOT NULL
      CHECK (direction IN ('inbound','outbound','bidirectional')),
    discovery_source TEXT NOT NULL
      CHECK (discovery_source IN ('agent_card_url','manual_registration','partner_directory','trusted_registry')),
    agent_card_url TEXT NULL,
    publisher_name TEXT NULL,

    status TEXT NOT NULL DEFAULT 'pending_review'
      CHECK (status IN ('pending_review','active','suspended','revoked')),
    assurance_level TEXT NOT NULL DEFAULT 'unverified'
      CHECK (assurance_level IN ('unverified','registered','verified','high_assurance')),

    data_boundary_status TEXT NOT NULL DEFAULT 'unknown'
      CHECK (data_boundary_status IN ('unknown','declared','contractually_bound')),
    processing_regions TEXT[] NULL,
    data_processing_profile JSONB NOT NULL DEFAULT '{}',

    auth_profile JSONB NOT NULL DEFAULT '{}',
    secret_ref TEXT NULL,

    procurement_budget_policy_id UUID NULL,
    pricing_mode TEXT NOT NULL DEFAULT 'unknown'
      CHECK (pricing_mode IN ('unknown','per_call','contract','reported','estimated')),
    unit_price NUMERIC(18,9) NULL,
    currency TEXT NULL,
    max_cost_per_call NUMERIC(18,9) NULL,

    approved_agent_version INT NULL,
    last_verified_at TIMESTAMPTZ NULL,
    reverify_after TIMESTAMPTZ NULL,

    reviewed_by UUID NULL REFERENCES users(id),
    reviewed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    FOREIGN KEY(external_agent_id, approved_agent_version)
      REFERENCES agent_versions(agent_id, version),
    UNIQUE(tenant_id, external_agent_id, direction)
);
CREATE INDEX idx_federated_rel_tenant_status
  ON federated_agent_relationships(tenant_id, status, external_agent_id);

CREATE TABLE federated_agent_trust_anchors (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    relationship_id UUID NOT NULL REFERENCES federated_agent_relationships(id),

    anchor_type TEXT NOT NULL
      CHECK (anchor_type IN ('jws_jwk','mtls_spki','oidc_issuer','registry_attestation')),
    key_id TEXT NULL,
    fingerprint TEXT NULL,
    issuer TEXT NULL,
    subject TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',

    status TEXT NOT NULL DEFAULT 'active'
      CHECK (status IN ('active','retiring','revoked')),
    valid_from TIMESTAMPTZ NULL,
    valid_until TIMESTAMPTZ NULL,
    verified_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_federated_anchor_relationship
  ON federated_agent_trust_anchors(tenant_id, relationship_id, status);

ALTER TABLE agent_cards
  ADD CONSTRAINT fk_agent_cards_verified_anchor
  FOREIGN KEY (verified_anchor_id)
  REFERENCES federated_agent_trust_anchors(id);

CREATE TABLE federated_agent_project_grants (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    relationship_id UUID NOT NULL REFERENCES federated_agent_relationships(id),
    direction TEXT NOT NULL CHECK (direction IN ('inbound','outbound')),
    project_id UUID NOT NULL REFERENCES projects(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(relationship_id, direction, project_id)
);

CREATE TABLE federated_agent_capability_grants (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    relationship_id UUID NOT NULL REFERENCES federated_agent_relationships(id),
    direction TEXT NOT NULL CHECK (direction IN ('inbound','outbound')),
    capability TEXT NOT NULL,
    requires_approval BOOLEAN NOT NULL DEFAULT false,
    risk_level TEXT NOT NULL DEFAULT 'medium',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(relationship_id, direction, capability)
);

CREATE TABLE delegation_grants (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    delegator_type TEXT NOT NULL,
    delegator_id UUID NOT NULL,
    delegatee_agent_id UUID NOT NULL REFERENCES agents(id),
    scopes TEXT[] NOT NULL,
    resources TEXT[] NULL,
    task_id UUID NULL,
    max_depth INT NOT NULL DEFAULT 1,
    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL
);

CREATE TABLE agent_tasks (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    external_task_ref TEXT NULL,
    session_id TEXT NULL,
    root_task_id UUID NULL,
    parent_task_id UUID NULL,
    root_agent_id UUID NULL REFERENCES agents(id),
    resolved_root_agent_version INT NULL,

    home_region TEXT NULL,
    consistency_mode TEXT NOT NULL DEFAULT 'regional'
      CHECK (consistency_mode IN ('regional','global_soft','global_hard')),
    crosses_trust_boundary BOOLEAN NOT NULL DEFAULT false,

    status TEXT NOT NULL,
    max_cost NUMERIC(18,9) NULL,
    max_tokens BIGINT NULL,
    max_agent_hops INT NULL,
    max_model_calls INT NULL,
    max_tool_calls INT NULL,
    max_agent_calls INT NULL,
    deadline_at TIMESTAMPTZ NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ NULL
);
CREATE INDEX idx_agent_tasks_tenant_root ON agent_tasks(tenant_id, root_task_id, started_at DESC);

CREATE TABLE agent_call_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY,
    ts TIMESTAMPTZ NOT NULL,
    tenant_id UUID NOT NULL,
    project_id UUID NOT NULL,
    request_id UUID NOT NULL,
    session_id TEXT NULL,
    task_id UUID NULL,
    root_task_id UUID NULL,
    caller_agent_id UUID NULL,
    caller_agent_version INT NULL,
    callee_agent_id UUID NOT NULL,
    callee_agent_version INT NULL,
    endpoint_id UUID NULL,

    trust_boundary TEXT NOT NULL DEFAULT 'internal'
      CHECK (trust_boundary IN ('internal','external_federated')),
    direction TEXT NOT NULL DEFAULT 'internal'
      CHECK (direction IN ('internal','inbound','outbound')),
    federation_relationship_id UUID NULL REFERENCES federated_agent_relationships(id),

    protocol TEXT NOT NULL,
    capability TEXT NULL,
    delegation_grant_id UUID NULL,
    hop INT NOT NULL DEFAULT 0,
    decision TEXT NOT NULL CHECK (decision IN ('allow','deny','approval_required')),
    status TEXT NOT NULL,
    input_hash TEXT NULL,
    output_hash TEXT NULL,
    latency_ms INT NULL,
    token_count BIGINT NOT NULL DEFAULT 0,
    cost NUMERIC(18,9) NOT NULL DEFAULT 0,
    external_agent_cost NUMERIC(18,9) NOT NULL DEFAULT 0,
    cost_source TEXT NULL,
    PRIMARY KEY(id, ts)
) PARTITION BY RANGE(ts);
CREATE INDEX idx_agent_calls_tenant_task ON agent_call_events(tenant_id, root_task_id, ts DESC);

CREATE TABLE approval_requests (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NOT NULL REFERENCES projects(id),
    request_id UUID NOT NULL,
    task_id UUID NULL,
    actor_agent_id UUID NULL REFERENCES agents(id),
    requested_by_type TEXT NULL,
    requested_by_id UUID NULL,
    action_type TEXT NOT NULL,
    action_ref TEXT NOT NULL,
    requested_scope JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','approved','denied','expired','cancelled')),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    decided_by UUID NULL REFERENCES users(id),
    decided_at TIMESTAMPTZ NULL,
    reason TEXT NULL
);
```

Agent Card 只保存公开/授权可见能力元数据，不保存 Agent Secret 或内部 Memory。`agent_call_events` 与 `tool_call_events` 是 Agent Graph 的事实边。

Agent Version 约束：

- `agents.active_version` 仅指向当前 active version；
- Control Plane 发布版本时必须在一个事务内校验 endpoint/capability spec；
- `agent_endpoints/agent_capabilities` 绑定具体 `agent_version`；
- Task/Agent Call Ledger 保存解析后的版本，历史分析不得按当前版本反推；
- RuntimeSnapshot 只包含 active + still-referenced draining versions。

Capability Index 在 Snapshot 编译阶段从 Model/Tool/Agent 资源声明生成，不单独维护可写事实表。

Federated Agent 数据不变量：

- `EXTERNAL_FEDERATED` Agent 只是 Tenant 内受控投影，Relationship 才是“允许如何交互”的授权事实；
- Relationship `active` 时 Control Plane Validator 必须确认至少一个有效 Verified Trust Anchor；
- `approved_agent_version` 必须属于同一个 `external_agent_id`，并且 Card/Endpoint/Capability Snapshot 已通过 Review；
- `allowed_projects/allowed_capabilities` 不存 UUID/Text Array，使用独立带 Direction 的 Grant 表保证 FK、审计与差异发布；
- External Agent Card 的 Material Change 创建 `pending_review` Agent Version/Card Snapshot；不得直接覆盖 Approved Version；
- Federation suspend/revoke 属于 Security Tighten，必须进入 Security Epoch；
- Relationship/Trust Anchor/Grant 均为 Tenant-owned table，必须纳入 RLS/Repository TenantScope。

## 24.16 Adaptive Governance 数据

```sql
CREATE TABLE governance_recommendations (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    project_id UUID NULL REFERENCES projects(id),
    kind TEXT NOT NULL CHECK (kind IN ('routing','guardrail')),
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    proposed_patch JSONB NOT NULL,
    evidence JSONB NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    sample_count BIGINT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'open'
      CHECK (status IN ('open','accepted','dismissed','expired')),
    linked_draft_id UUID NULL REFERENCES config_drafts(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_by UUID NULL REFERENCES users(id),
    reviewed_at TIMESTAMPTZ NULL
);
CREATE INDEX idx_governance_reco_tenant_status
  ON governance_recommendations(tenant_id, status, created_at DESC);
```

接受 Recommendation 时创建标准 Config Draft，并回填 `linked_draft_id`。Recommendation 本身不能直接进入 RuntimeSnapshot。

---

# 25. Secret 与基础安全

## 25.1 Envelope Encryption

Provider Credential / Pepper / Sensitive Config：

```text
DEK = random AES-256 key
ciphertext = AES-256-GCM(DEK, secret)
encrypted_dek = KMS/Vault/MasterKey(DEK)
```

Master Key 来源：

- env/file（Lite）；
- KMS/Vault（Standard/Enterprise）。

UI 永不返回 Secret 明文。

### 25.1.1 Secret Provider HA

KMS/Vault 不允许成为每个 AI Request 的同步热路径。

Data Plane 采用：

```text
Secret Provider
  → authorized decrypt
  → in-memory credential cache
  → short-lived sealed local cache(optional)
```

配置：

```text
secret_cache_ttl
secret_stale_grace
rotation_overlap
```

语义：

- 已成功解密并仍在 TTL/grace 内的 Credential 可在 Secret Provider 暂时不可用时继续使用；
- 新 Credential、首次解密、Rotation 仍要求 Secret Provider 可用；
- `stale_grace` 到期后按 Credential Policy fail-closed；
- 本地持久化 Secret Cache 如启用，必须使用节点密钥/Envelope Encryption，且与 RuntimeBundle 分离；
- Secret Provider outage 必须可观测和告警，禁止无限期使用过期 Secret。

## 25.2 Egress / SSRF

- Provider host allowlist；
- private CIDR deny；
- DNS rebinding protection；
- redirect 限制；
- URL scheme allowlist；
- content-size / content-type 限制；
- 外部文件下载必须走独立 Safe Fetcher。

## 25.3 Admin Security

- HTTPS；
- HttpOnly/Secure/SameSite Cookie；
- CSRF；
- OIDC/SAML；
- RBAC；
- High Risk 操作 re-auth；
- 全量 Audit。

---

# 26. RBAC 与 Scope Authorization

## 26.1 Scope 层级

授权 Scope：

```text
SYSTEM
TENANT:<tenant_id>
PROJECT:<project_id>
```

角色分配不把用户永久绑定到单一 Tenant；同一 User 可以在不同 Tenant/Project 拥有不同角色。

## 26.2 预置角色

- System Admin；
- System Operator；
- Tenant Admin；
- Tenant Operator；
- Project Admin；
- Developer；
- Finance Viewer；
- Viewer。

## 26.3 权限矩阵

| Resource | System Admin | System Operator | Tenant Admin | Tenant Operator | Project Admin | Developer | Finance | Viewer |
|---|---|---|---|---|---|---|---|---|
| tenant.write | ✓ |  | own |  |  |  |  |  |
| tenant.members.write | ✓ |  | own |  |  |  |  |  |
| org.write | ✓ |  | own |  |  |  |  |  |
| system_provider.write | ✓ | ✓ |  |  |  |  |  |  |
| tenant_provider.write | ✓ |  | own | own |  |  |  |  |
| route.write | ✓ |  | own | own | own |  |  |  |
| project.write | ✓ |  | own |  | own |  |  |  |
| key.create | ✓ |  | own | own | own | constrained |  |  |
| budget.write | ✓ |  | own |  | own |  |  |  |
| guardrail.write | ✓ |  | own | own | own |  |  |  |
| usage.read | ✓ | scoped | own | own | own | own | own | limited |
| usage.cross_tenant | ✓ | scoped |  |  |  |  |  |  |
| alert.ack | ✓ | ✓ | own | own | own |  |  |  |
| circuit.reset | ✓ | ✓ | own | own |  |  |  |  |
| config.publish | ✓ | ✓ | own | own |  |  |  |  |
| audit.read | ✓ | scoped | own | own |  |  |  |  |
| agent.register | ✓ |  | own | own | own |  |  |  |
| agent.version.publish | ✓ |  | own |  | own |  |  |  |
| delegation.grant | ✓ |  | own |  | own |  |  |  |
| delegation.revoke | ✓ | ✓ | own | own | own |  |  |  |
| approval.decide | ✓ |  | own | own |  |  |  |  |
| external_agent.review | ✓ |  | own |  |  |  |  |  |
| external_agent.suspend | ✓ | ✓ | own | own |  |  |  |  |
| external_agent.trust.rotate | ✓ |  | own |  |  |  |  |  |
| external_agent.trust.revoke | ✓ | ✓ | own | own |  |  |  |  |

`own` 表示仅当前授权 Scope 内资源。

## 26.4 Role Assignment

```sql
CREATE TABLE role_assignments (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('SYSTEM','TENANT','PROJECT')),
    tenant_id UUID NULL,
    project_id UUID NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

授权时先验证 Scope，再验证 Action。客户端传入的 Tenant/Project 仅用于资源定位，不构成授权依据。

## 26.5 安全不变量

- Project/Developer 不能通过子级配置放宽 Tenant 父级限制；
- Tenant Admin 不能访问其他 Tenant 的 Provider Credential / Usage / Audit；
- Finance Viewer 可以看成本但不能读取 Provider Secret 或 Prompt Body；
- System Operator 的跨 Tenant 权限应最小化并全量审计；
- High Risk 安全策略放宽、Secret Rotation、Tenant Suspend 必须 re-auth。

高风险职责分离：

- `external_agent.review` 是 Tenant 级信任边界引入，不下放给 Project Admin；
- `external_agent.suspend/revoke` 是紧急安全操作，可下放 Tenant/System Operator，但必须 Re-auth + Audit；
- `approval.decide` 默认禁止审批人批准自己发起的 High/Critical Request；Enterprise 可配置双人审批；
- `external_agent.review` 若涉及 Residency Risk Exception、未知定价或 High Risk Capability，Enterprise 默认要求第二审批人；
- `config.publish` 不能隐式替代 `approval.decide` 或 `external_agent.review`。

---

# 27. Admin API 与 SDK

## 27.1 Admin API

### 27.1.0 服务架构（V8.5 收口）

Admin API 服务由三层构成（见 §2.9）：

```text
webkit Engine
  └─ Use(Recover, SecurityHeaders+CSP)          全局
  └─ Use(drainGate)                             运维（排水 503）
  └─ Group /api/admin [requireAuth]             会话（cookie + CSRF）
       ├─ Group [requireRole(tenant_admin)]     管理写操作
       └─ Group [requirePermission(perm)]       细粒度权限（rbac 矩阵）
            └─ handler（薄）：c.Bind → 窄能力调用 → jsonResult
```

- 路由按域拆分文件（`routes_keys / routes_config / routes_resources / routes_ops / routes_security`），全部注册在 `registerRoutes` 一处可审计；
- handler 只允许访问其路由域的窄能力字段（`s.keySvc`、`s.configSvc` …），跨域能力必须经 `Backend` 超集显式组合；
- 会话端点（login/logout/oidc）与用户管理端点以**插件形式**注册到 Engine（`SessionEndpoints.Register(engine)`、`OIDCLogin.Register(engine)`），表示层内核不感知其内部；
- 错误渲染唯一入口 `handleError/mapError`（§2.9.5 契约）；
- 安全头：全部 JSON 端点 `Cache-Control: no-store` + CSP（§2.9.1）。

前缀（实施基线）：

```text
/api/admin                 # Lite 单租户 Profile 实际实现前缀
/admin/api/v1              # 多租户 System API 扩展预留前缀（V8.2 契约）
```

Lite Profile 当前实现 `/api/admin` 下的 Tenant 级资源子集（projects/keys/credentials/config/runtime/requests/audit/playground/alerts/governance/federation/users …）；System 级 `/tenants`、`/org-units`、`/role-assignments` 等按 V8.2 契约在 Standard 档位随多租户管理面交付。

主要资源：

```text
/tenants
/org-units
/users
/tenant-memberships
/role-assignments
/projects
/applications
/agents
/service-accounts
/api-keys
/providers
/credentials
/models
/deployments
/logical-models
/route-policies
/cache-policies
/guardrail-policies
/budgets
/rate-policies
/pricing
/usage
/requests
/security-events
/alerts
/integrations
/config/drafts
/config/versions
/audit
/system
```

### Tenant Scope 规则

- System Admin 可通过 `/tenants/{tenant_id}/...` 管理指定 Tenant；
- Tenant 用户的 `tenant_id` 从登录 Session/JWT 权限上下文解析，不允许仅凭 Query/Header 切换；
- Tenant Selector 只能在服务端授权集合内切换；切换后重新签发/刷新 Console Session Scope；
- Project API 必须验证 `project.tenant_id == authorized_tenant_id`；
- `/usage`、`/requests`、`/security-events`、`/live` 默认强制当前 Tenant Filter；
- 跨 Tenant 汇总使用独立 System API 权限，不复用 Tenant 级查询；
- 所有分页列表默认服务端过滤、排序、分页。

## 27.2 Config Draft / ChangeSet API

```text
POST   /config/drafts
GET    /config/drafts/{id}
PATCH  /config/drafts/{id}
POST   /config/drafts/{id}/validate
POST   /config/drafts/{id}/rebase
GET    /config/drafts/{id}/diff
POST   /config/drafts/{id}/publish
POST   /config/drafts/{id}/discard
POST   /config/versions/{version}/rollback
```

Patch 必须携带 `If-Match`/revision，避免多标签页静默覆盖。

## 27.3 Playground API

Playground 不要求管理员选择/读取真实业务 API Key Secret。Console 请求短期 Test Principal：

```text
POST /playground/test-principals
POST /playground/execute
GET  /playground/cases
POST /playground/cases
DELETE /playground/cases/{id}
```

`TestPrincipal`：

- TTL 默认 15min；
- 固定 Tenant/Project/调用主体 Scope；
- 不暴露长期 Key Secret；
- 使用签名短期凭据进入同一 Admission/Policy/Pipeline；
- `source=playground`；
- Provider Cost 必须记录；Customer Charge 是否计入正式 Chargeback 由 Tenant Policy 决定；
- 可指定 `active_snapshot` 或拥有权限的 `draft_id`；
- Draft 执行也必须经过完整 Guardrail/Budget/Rate/Resilience，只有 Snapshot 来源不同。

支持 `guardrail_only=true`：只执行 Guardrail/Policy，不调用 Provider、不产生 Provider Cost。

## 27.4 Guardrail Impact Preview API

```text
POST /guardrail-policies/{id}/impact-preview
```

样本来源只允许：

1. 用户显式保存的 Playground Test Case；
2. Project 已开启 Request Content Retention 且标记 `replay_eligible=true` 的脱敏样本；
3. 用户上传/粘贴的临时测试样本。

默认 Request Log 不保存完整 Prompt，因此 Console 不得承诺“自动重放全部历史请求”。

Impact Preview：

- 不调用 LLM Provider；
- 只执行 Guardrail 判定；
- 显示样本数、覆盖范围和数据来源；
- 输出新增 Block/Mask/Flag、解除 Block、规则命中变化；
- 对需要外部 Guardrail 的样本调用必须遵守 Tenant Residency/DLP Policy，并明确显示额外费用/延迟（若第三方 Guardrail 自身收费）。

## 27.5 Request Live Tail API

```text
GET /requests/live  (SSE)
```

服务端强制 Tenant/Project/RBAC 过滤，只发送摘要：

```text
request_id / ts / project / source / endpoint / model / status
latency / ttft / token_total / provider / cache / guardrail_summary
```

不通过 Live Tail 推送 Prompt/Response Body。

SSE：

- 支持 `Last-Event-ID`；
- 心跳；
- 断线重连；
- 服务端 backpressure；
- 高流量时支持 sampling/aggregation，默认最大 50 event/s/浏览器连接；
- Tenant 切换时旧连接必须由客户端立即关闭。

## 27.6 Integration API

```text
GET    /integrations/catalog
GET    /integrations
POST   /integrations
PATCH  /integrations/{id}
POST   /integrations/{id}/test
DELETE /integrations/{id}
```

Catalog 是 LiteAIG 发布版本内置/签名认可的能力目录，不等同于任意插件上传市场。

## 27.7 Agentic Governance API

```text
GET/POST   /admin/api/v1/agents
GET/PATCH  /admin/api/v1/agents/{id}
GET        /admin/api/v1/agents/{id}/versions
POST       /admin/api/v1/agents/{id}/versions
POST       /admin/api/v1/agents/{id}/versions/{version}/activate
POST       /admin/api/v1/agents/{id}/versions/{version}/drain
GET/POST   /admin/api/v1/agents/{id}/endpoints
POST       /admin/api/v1/agents/{id}/discover-card
GET        /admin/api/v1/agents/{id}/capabilities

GET/POST   /admin/api/v1/federation/relationships
GET/PATCH  /admin/api/v1/federation/relationships/{id}
POST       /admin/api/v1/federation/relationships/{id}/review
POST       /admin/api/v1/federation/relationships/{id}/suspend
POST       /admin/api/v1/federation/relationships/{id}/revoke
POST       /admin/api/v1/federation/relationships/{id}/refresh-card
GET/POST   /admin/api/v1/federation/relationships/{id}/trust-anchors
POST       /admin/api/v1/federation/relationships/{id}/trust-anchors/{anchor_id}/revoke
GET/PUT    /admin/api/v1/federation/relationships/{id}/project-grants
GET/PUT    /admin/api/v1/federation/relationships/{id}/capability-grants
GET        /admin/api/v1/agent-calls
GET        /admin/api/v1/tasks
GET        /admin/api/v1/tasks/{id}/graph
GET/POST   /admin/api/v1/delegations
POST       /admin/api/v1/delegations/{id}/revoke
GET        /admin/api/v1/approvals
POST       /admin/api/v1/approvals/{id}/approve
POST       /admin/api/v1/approvals/{id}/deny

GET/POST   /admin/api/v1/mcp/servers
GET/PATCH  /admin/api/v1/mcp/servers/{id}
POST       /admin/api/v1/mcp/servers/{id}/discover
GET        /admin/api/v1/mcp/servers/{id}/tools
GET        /admin/api/v1/tool-calls
GET        /admin/api/v1/tool-calls/{id}
POST       /admin/api/v1/tool-policies/test
```

所有接口由服务端从 Session Scope 解析 `tenant_id`；禁止客户端通过 body/query 覆盖 Tenant Scope。Approval/Delegation 的写操作必须 Audit。

Federation API 规则：

- `review` 属于 High Risk Config Change，进入 Validate/Diff/Publish；
- `suspend/revoke` 属于 Operational Security Tighten，允许 Fast Publish + Security Epoch；
- Trust Anchor Secret/Private Key 不通过 Admin API 返回；
- Inbound/Outbound Relationship 的 Credential 只保存 `secret_ref`；
- `refresh-card` 只刷新 Candidate Snapshot，不自动激活 Material Change。
- `project-grants/capability-grants` 的 PUT 必须携带 `direction=inbound|outbound`；服务端不得在 bidirectional Relationship 中自动复制反向 Grant。

## 27.8 Governance Recommendation API

```text
GET  /admin/api/v1/governance/recommendations
GET  /admin/api/v1/governance/recommendations/{id}
POST /admin/api/v1/governance/recommendations/{id}/accept
POST /admin/api/v1/governance/recommendations/{id}/dismiss
POST /admin/api/v1/guardrail-events/{id}/review
```

`accept` 只创建 Config Draft，不直接 Publish。

## 27.9 API Versioning

- 数据面兼容 OpenAI/Anthropic 官方协议，不自造 `/v2`；
- Admin API 破坏性变更使用 `/admin/api/v2`；
- v1 至少维护 12 个月；
- `Deprecation` / `Sunset` header。

## 27.10 SDK

数据面直接使用官方 OpenAI / Anthropic SDK，只改 `base_url`。

管理侧：

- OpenAPI 3.1；
- TypeScript Admin Client；
- Python Admin SDK；
- Terraform Provider P1。

Web Console 的 API 类型由 OpenAPI 生成，禁止手写重复 DTO。

---

# 28. Web Console / Responsive PWA

## 28.1 产品目标

Web Console 是 LiteAIG 的统一运营与运维控制面，必须形成以下闭环：

```text
Connect
  → Configure
  → Preview/Test
  → Validate/Diff
  → Publish
  → Observe
  → Diagnose
  → Optimize/Rollback
```

UI 不暴露底层 Pipeline 编排复杂度；管理员操作面向 `Tenant / Project / Model / Key / Policy / Cost / Request / Alert` 等业务对象。

## 28.2 三层信息密度

```text
Level 1 — Overview
  回答“现在怎么样”
  Dashboard / Summary / Needs Attention

Level 2 — Manage
  回答“我要改什么”
  Table / Detail / Wizard / Policy Form

Level 3 — Debug
  回答“为什么这样”
  Route Explain / Guardrail Finding / JSON / Raw Error / Runtime Version
```

规则：

- 默认页面只展示 Level 1/2；
- Level 3 默认折叠；
- JSON/YAML 不能成为普通用户的主要入口；
- 高级模式不得绕过服务端 Validator。

## 28.3 App Shell 与全局上下文

### Desktop Shell

```text
┌──────────────────────────────────────────────────────────────────────┐
│ Logo  Tenant ▾  Project ▾     Search/Cmd+K        Time ▾  Help  User│
├──────────────┬───────────────────────────────────────────────────────┤
│ Sidebar      │ Breadcrumb / Page Header                             │
│              │                                                       │
│ Overview     │ Main Content                                          │
│ Gateway      │                                                       │
│ Policies     │                                                       │
│ Observe      │                                                       │
│ FinOps       │                                                       │
│ Organization │                                                       │
│ System       │                                                       │
└──────────────┴───────────────────────────────────────────────────────┘
```

全局上下文：

- Tenant Selector：仅多 Tenant 且当前用户有多个 Tenant 权限时显示；
- Project Selector：支持 `All Projects` 与当前 Project；
- Time Range：1h / 24h / 7d / 30d / Custom；
- Environment/Tag Filter：按需显示；
- `Cmd/Ctrl+K`：搜索 Tenant、Project、Provider、Logical Model、Key、User、Policy、Request ID；
- 当前 Scope 必须写入 URL，可复制链接；Server 仍以 Session/RBAC 校验为准。

`OrgUnit/User` 不作为全局 Project Scope 的父级上下文，只在 Organization/FinOps/Requests 中作为过滤维度。

## 28.4 导航信息架构

为控制管理端认知负担，Console 固定采用 7 个导航组：

```text
Overview
  └─ Dashboard

Gateway
  ├─ Projects
  ├─ Models & Providers
  ├─ Routing
  ├─ API Keys
  └─ Agents & Tools

Policies
  ├─ Guardrails
  ├─ Cache
  └─ Limits & Budgets

Observe
  ├─ Playground
  ├─ Requests
  ├─ Alerts
  └─ Health & Circuits

FinOps
  ├─ Usage & Cost
  └─ Pricing

Organization
  ├─ Departments & Users
  └─ Roles & Memberships

System
  ├─ Integrations
  ├─ Config Changes
  ├─ Audit
  └─ Settings
```

平台角色额外：

```text
Platform
  ├─ Tenants
  └─ Shared Providers
```

Enterprise 按授权显示：

```text
Observe
  └─ Agent / Task Graph
System
  └─ Compliance
```

导航必须由后端 Capability/RBAC 返回决定；前端隐藏菜单只用于体验，真正授权仍由 Admin API 强制执行。

## 28.5 页面容器规范

页面分三类：

1. **List Page**：过滤器 + Server-side Table + Batch Actions；
2. **Resource Detail**：Header + Summary + Tabs；
3. **Workflow Page**：Wizard / Full-page Editor / Diff / Simulator。

交互容器原则：

- 简单编辑（名称、阈值、标签）用 Drawer；
- 多步骤创建用 Wizard；
- Guardrail Policy、Route Policy、Diff、Routing Simulator 等复杂任务使用完整页面；
- 禁止把所有复杂配置都塞进 Drawer；
- 表格支持 Cursor Pagination、Server-side Sort/Filter，禁止浏览器聚合大规模 Usage 明细。

## 28.6 Dashboard

Dashboard 默认回答三个问题：

1. 网关是否健康；
2. 今天用了多少、花了多少；
3. 有没有需要立即处理的问题。

### 首屏 KPI

最多 6 个：

```text
Requests
Success Rate
P95 / TTFT
Total Tokens
Provider Cost
Budget Utilization
```

### 二级区块

- Usage & Cost Trend；
- Provider/Deployment Health；
- Cache Hit / Savings；
- Retry/Fallback；
- Guardrail Block/Mask；
- Top Projects / Departments / Users；
- Cost Anomaly；
- `Needs Attention`：Firing Alert、Open Circuit、Budget >80%、Config Drift、Security Incident。

Dashboard 默认作用于当前 Tenant；选定 Project 后所有可适用卡片联动。

## 28.7 Project Center（新增核心页面）

Project 是 AI 资源治理边界，因此必须提供“360° Project Detail”，减少管理员在多个菜单之间来回跳转：

```text
Project: coding-assistant
[Overview] [Access] [Models & Routing] [Guardrails]
[Limits & Budget] [Cache] [Usage] [Applications & Agents]
```

Overview：

- Status；
- Logical Models；
- API Keys；
- Budget 使用；
- Requests / Tokens / Cost；
- Guardrail 状态；
- Data Residency；
- Recent Alerts。

Project 中的策略开关修改只进入当前 Config Draft；页面顶部显示 Draft 状态。

## 28.8 Models & Providers

### Provider List

列：

```text
Name / Scope / Type / Health / Deployments / Requests / Cost / Last Error
```

Scope 明确显示：

- `Shared`：SYSTEM_SHARED；
- `Private`：TENANT_PRIVATE。

行操作：

- View；
- Test Connection；
- Edit（进入 Draft）；
- Disable（Operational Action，展示影响）；
- Credentials；
- Usage。

### Provider Detail

Tabs：

```text
Overview
Credentials
Models
Deployments
Usage & Cost
Events
```

Credential：只显示 label、fingerprint、scope、status、last_used_at；Secret 永不回显。

### Add Provider Wizard

```text
1. Choose Provider Type
2. Credential / Endpoint
3. Test Connection
4. Discover Models (best-effort)
5. Review & Create
```

自动发现不是能力事实来源。Model Catalog 中能力字段必须标记来源：

```text
VERIFIED     LiteAIG 契约/实测
DECLARED     管理员/Provider 配置声明
DISCOVERED   Provider API 返回
UNKNOWN      未确认
```

如果 `/models` 不提供 vision/tools/context 等能力，不得根据名称猜测为 VERIFIED；允许管理员手动覆盖并记录 Audit。

### Model Catalog

按：

- Provider；
- Capability；
- Modality；
- Context Window；
- Region；
- Imported/Available；
- Verification Status。

搜索“支持 Vision + Tool Calling”的模型不要求用户先知道 Provider。

### Deployment

列表直接显示：

- Health；
- Circuit State；
- Region/Data Region；
- P50/P95/TTFT；
- Unit Cost；
- Inflight/Capacity；
- Credential availability。

点击 Circuit 状态打开事件时间线。

Disable Deployment 前必须展示受影响 Logical Models/Fallback。

## 28.9 Routing

### Logical Models

每个 Logical Model 显示：

- Alias；
- 当前 Route Policy；
- Primary Candidate；
- Candidate 数；
- 24h Success/P95/Cost；
- Project Scope。

### Route Policy Editor

不采用自由拖拽图画布；使用结构化表格，保证简单、可审计：

```text
Deployment | Hard Eligible | Priority | Weight | Health | P95 | Cost | Fallback
```

Route Template：

- balanced；
- lowest_cost；
- lowest_latency；
- availability_first；
- manual_weighted；
- highest_quality(P2)。

修改权重/模板进入 Draft。

### Routing Simulator

必须复用生产 `PlanRoute()`：

输入：

- Tenant/Project；
- Logical Model；
- Request Capabilities；
- Region；
- Session ID/Seed；
- Tags；
- Active 或 Draft Snapshot。

输出：

```text
Candidate
  → Hard Constraint PASS/FAIL + reason
  → priority/latency/cost/load/cache/quality scores
  → final score
  → selected deployment
  → credential pool
  → fallback plan
```

提供“为什么选择它”摘要，不要求管理员读 JSON。

## 28.10 API Keys

列表列：

```text
Name / Fingerprint / Project / Bound Principal / Status
Last Used / Requests / Tokens / Cost / Budget / RPM / Expires
```

高频动作：

- Create；
- Disable；
- Rotate；
- Revoke；
- View Usage；
- Copy public metadata。

创建 Key：

1. Project；
2. Bound Principal（Application/Agent/Service Account，可选）；
3. Model Allowlist；
4. Budget/Rate Override；
5. Expiry/IP；
6. Create。

Secret 仅创建成功时展示一次：

- Copy button；
- 明确“关闭后无法再次查看”；
- UI/日志不保存完整 Secret；
- 浏览器无法阻止系统截图，因此不得宣称“禁止截图”；
- Copy 行为可写管理审计事件，但不能替代 Secret 管理责任。

Rotate：

```text
Generate replacement → Grace Period → migrate client → revoke old key
```

Budget 行内展示 `used / limit` 及状态，不仅依赖颜色，同时显示百分比和文本。

## 28.11 Policies：统一编辑模型

### 生产变更不“Toggle 即生效”

策略卡片仍可采用 `Switch + Summary + Configure` 视觉模式，但 Switch 行为固定为：

```text
Toggle
  → modify server-side Draft
  → Dirty indicator
  → Sticky Draft Bar: “3 changes pending”
  → Preview Changes
  → Validate
  → Publish
```

页面底部/顶部常驻 `Draft Bar`：

```text
Draft based on v41 · 3 changes · Last saved 10s ago
[Discard] [View Changes] [Validate] [Publish]
```

例外仅有明确的 `Guardrail Tighten Fast Publish`，按钮文案必须是“立即收紧并发布”，不能伪装成普通 Toggle。

### Effective Policy

任何 Policy 页面均可查看：

```text
System Mandatory
  ↓
Tenant Policy
  ↓
Project Policy
  ↓
Principal Override
  ↓
Effective Policy
```

UI 必须显示某条规则“来自哪一级”，父级强制规则以锁图标表示，子级不可关闭。

## 28.12 Guardrails

一级内容：

```text
Overview
Policies
Security Events
Test Cases
Integrations
```

Policy Editor：

```text
Input
  PII / Secret / Prompt Injection / Jailbreak / Topic / URL
Context
  Document Injection / Provenance / DLP
Output
  PII / Content Safety / Topic / Groundedness
Stream
  Layer1 / Layer2 / Layer3 / Strict Buffered
Tool & MCP
  Tool ACL / Schema / Risk / Task Adherence
```

每条 Rule 展示：

- Enabled in Draft；
- Mode：Enforce/Observe；
- Action；
- Provider；
- Execution Class：Inline / Buffered / Async；
- Expected latency class；
- Fail Mode（External Guard）；
- Source Policy Level。

不只使用绿色/橙色表达执行层级；必须同时显示文本 Badge，满足可访问性。

### Guardrail Rule Drawer

Drawer 只编辑单条规则：阈值、Action、Provider、Fail Mode、Scope；复杂 Policy 总体关系仍留在完整编辑页。

### Guardrail Impact Preview

不能假设历史 Prompt 一定可回放。

数据源选择器：

```text
Saved Test Cases
Replay-eligible retained samples
Temporary uploaded samples
```

结果：

```text
Sample Coverage: 128 cases (Saved 80 / Retained 48)
New Blocks: 12
New Masks: 5
New Flags: 3
No Longer Blocked: 0
Unchanged: 108
```

必须展示 `Coverage`，避免管理员把有限样本结果误认为全量生产影响。

### Security Events

默认只展示：

- category；
- checkpoint；
- action/verdict；
- project/user；
- risk score；
- time；
- request_id。

`masked_preview` 需要点击展开且受权限控制；完整敏感内容默认不存在。

## 28.13 Cache

页面：

```text
Overview
Policies
Entries(仅 metadata)
Semantic Cache(P1)
Savings
```

重点指标：

- Hit Rate；
- Saved Provider Calls；
- Estimated Cost Saved；
- L1/L2 命中；
- Eviction；
- Semantic match score(P1)。

Cache Policy 修改进入 Draft；Cache Purge 属于 Operational Action，必须明确 Scope：Tenant/Project/Logical Model/Namespace。

禁止在 Console 普通页面展示完整缓存 Prompt/Response；有调试权限时也默认展示 hash/metadata。

## 28.14 Limits & Budgets

统一管理：

- Tenant Budget；
- Project Budget；
- Key Budget；
- RPM/TPM；
- Concurrent Request/Stream；
- Department/User Budget(P1)。

Budget 页面必须同时展示：

```text
Limit
Used
Reserved
Remaining
Window reset
Over-budget action
Attribution trust requirement
```

修改 Budget/Rate 进入 Draft；Emergency Key Disable 为 Operational Action。

## 28.15 Playground

Playground 是正式功能，不是“绕开网关的测试页”。

### 模式

```text
Live
  → Active Tenant Snapshot
  → real Provider call
  → Provider Cost recorded

Draft Preview
  → selected Config Draft Candidate Snapshot
  → real Provider call unless Guardrail-only
  → Provider Cost recorded

Guardrail-only
  → no model provider call
  → no provider cost
```

### Identity

管理员选择：

- Project；
- Logical Model；
- Execution Identity（Application/Agent/Service Account/User if authorized）。

Console 后端签发短期 `TestPrincipal`，不要求管理员选择或看到某个真实 API Key Secret。

### Layout

```text
┌─────────────────────────────┬────────────────────────────────────┐
│ Request                      │ Response                           │
│ Snapshot: Active/Draft       │ Streaming output                   │
│ Project                      │                                    │
│ Identity                     │ Decision Timeline                  │
│ Logical Model                │ Route Explain                      │
│ Messages                     │ Guardrail Findings                 │
│ Tools                        │ Cache Decision                     │
│ Parameters                   │ Usage / Provider Cost / Charge     │
│ [Send] [Guardrail Only]      │ Raw metadata (Advanced)            │
└─────────────────────────────┴────────────────────────────────────┘
```

Playground 调用写 Usage Event：

```text
source = playground
```

Provider Cost 永远真实记录；Customer Charge 可以根据 Tenant Policy 选择是否进入正式内部 Chargeback。

### Saved Test Case

默认不自动保存 Prompt/Response。用户显式保存时：

- Tenant/Project Scope；
- encryption at rest；
- RBAC；
- 可用于 Guardrail Impact Preview；
- 可复制为 CI regression case。

## 28.16 Requests：History + Live Tail

### History

筛选：

```text
request_id / source / project / application / agent / key
org_unit / user / model / provider / deployment
status / error / guardrail / cache / time
```

详情：

```text
Identity & Attribution
Snapshot / Security Epoch
Input Guardrail
Budget / Rate Limit
Cache Decision
Routing Candidates & Scores
Selected Deployment/Credential
Retry/Fallback
Provider Timing/TTFT
Stream Guardrail
Output Guardrail
Usage
Provider Cost / Charge
Final Status
```

### Live Tail

```text
[History] [Live]

Filter: Project / Source / Status / Model / Error
Pause / Resume / Sampling
```

SSE 只推摘要，不推 Body。点击一条后按 request_id 拉取权限允许的完整详情。

高流量时 UI 自动提示：

```text
Incoming rate 320 req/s; Live Tail is sampling at 50 events/s.
```

### Decision Timeline

Playground、History Detail、Live Tail Drawer 三处复用同一组件：

```text
Admission           0.4ms
Input Guardrail     1.2ms
Coordination        0.8ms
Cache               0.1ms  MISS
Routing             0.3ms  Deployment B
Provider          780.0ms
Output Guardrail    0.9ms
Accounting          async
```

可视化采用对数/分段尺度，同时直接显示数值，避免 Provider 网络耗时把 Core Stage 完全压缩不可见。

## 28.17 Alerts

### Rule Builder

使用结构化“条件句”而不是巨型表单：

```text
WHEN [Provider Error Rate]
SCOPE [Provider: OpenAI]
WINDOW [5m]
OPERATOR [>]
THRESHOLD [10%]
SEVERITY [Critical]
NOTIFY [Webhook: ops-primary]
```

指标分组：

- Reliability：error rate / latency / circuit / fallback；
- FinOps：budget / cost spike / token spike / cache savings drop；
- Security：guardrail block / retroactive block / fail-open；
- Platform：DB/Redis/config drift/security epoch/provider health。

保存前可执行历史窗口预览，显示“过去 24h 若规则已存在将触发几次”，但不发送通知。

### Alert Inbox

以 `Firing` 为默认优先视图：

- Firing；
- Acknowledged；
- Silenced；
- Resolved。

动作：Ack / Silence / Maintenance Window / Open Resource / Open Requests。

不依赖脉冲动画传达 Critical；使用文本、图标和状态色共同表达。

## 28.18 Health & Circuits

统一展示：

- Control/Data Plane nodes；
- active Tenant Snapshot Version；
- Security Epoch；
- Config Drift；
- DB/Redis；
- Providers/Credentials/Deployments；
- Circuit States；
- Reservation Sweeper；
- Lease cleanup；
- Event Sink health。

Circuit Reset 属于 Operational Action；必须展示最近失败原因和影响范围。

## 28.19 Usage & Cost（AI FinOps）

### 全局维度

```text
Tenant
Project
Application
Agent
API Key
OrgUnit
User
Logical Model
Provider
Deployment
Region
Tag
Attribution Trust
Time Range
```

### Tabs

```text
Overview
Projects
Departments
Users
Applications & Agents
Models & Providers
Chargeback
Budgets
```

### Overview

- Input/Output/Reasoning/Cache Token；
- Provider Cost；
- Customer Charge；
- Cost per 1K Requests；
- Cost per User/Project；
- Cache Savings；
- Retry/Fallback Cost；
- Cost Anomaly。

### Cross Analysis

支持矩阵：

```text
Project × Department
Project × User
Department × Model
Application/Agent × Model
```

示例：

```text
Coding Assistant
├─ 研发一部   $5,600
├─ 研发二部   $3,800
└─ 测试部     $1,500
```

身份归属必须显示 `verified / key_bound / trusted_app / business_label`。正式 Chargeback 默认排除低可信 `business_label`，除非 Tenant 明确配置。

所有 Dashboard 图表读取后端 Rollup/Aggregation API，不在浏览器聚合 Usage 明细。

## 28.20 Organization

页面：

```text
Departments
Users
Memberships
Roles
Cost Centers
Identity Sync(P1)
```

Departments：树 + 成员数 + Token/Cost + Budget(P1)。

User Detail：

- 当前 OrgUnit；
- 调岗历史；
- Tenant/Project Roles；
- Identity Provider mapping；
- 使用 Project；
- Token/Cost；
- Attribution Trust；
- Recent Requests（按权限）。

Project 与 OrgUnit 不在 UI 中做强父子嵌套。

## 28.21 Integrations（取代“插件市场”默认定位）

`Extensions Marketplace` 收敛为 **Integrations Catalog**，只管理受支持集成，不提供任意插件运行时。

分类：

```text
Guardrail Integrations
Notification Channels
Observability / Analytics Sinks
Secret Providers
```

Provider Connector 仍在 `Models & Providers` 管理，不在 Integrations 中重复出现。

每个 Integration Card：

- Capability；
- Scope（System/Tenant）；
- Status/Health；
- Configured By；
- Version；
- [Configure] [Test] [Disable]。

“Enable”只代表启用一个已有受支持 Integration Instance，不允许上传任意 Wasm/JS 代码。

P2 `gRPC Extension Bridge` 放在：

```text
System → Integrations → Advanced Extension Runtime
```

仅 System Admin 可见；必须是签名/allowlisted endpoint，并受 mTLS、timeout、health、scope 与 audit 约束。

## 28.22 Config Changes：Draft / Diff / Publish / Versions

### Draft Center

显示：

- My Drafts；
- Tenant Drafts；
- Pending Approvals；
- Base Version；
- Revision；
- Changed Resources；
- Conflict/Rebase 状态。

### Sticky Draft Bar

任何页面修改配置后出现：

```text
Draft d-184 · Based on Tenant v41 · 3 changes
[Discard] [Review Changes] [Validate] [Publish]
```

### Diff Viewer

默认语义 Diff，而不是大段 Raw JSON：

```text
Routing
  balanced → lowest_cost
  deployment-a weight 50 → 30
  deployment-b weight 50 → 70

Guardrail
  Prompt Injection threshold 0.80 → 0.75  [Tighten]

Budget
  daily $500 → $300
```

完整 JSON/Protobuf diff 仅在 Advanced 展开。

Diff 页面同时显示：

- Validator；
- Risk；
- Dependency Impact；
- Route Impact；
- Guardrail Tighten/Loosen；
- Approval Requirement；
- Rebase Conflict。

### Versions

历史版本不可编辑：

- View；
- Diff against current；
- Create Draft from Version；
- Rollback（生成新版本）。

## 28.23 Audit

Audit 重点回答：

```text
Who
When
From where
Changed what
Before/After
Result
Request/Config Version
```

Secret 值永不出现在 Audit Diff。

筛选：actor / tenant / project / action / resource / risk / result / time。

## 28.23.1 Agents & Tools（P1）

Tenant/Project 范围内提供：

```text
Agent Registry
Agent Endpoints / Agent Cards
Agent Capabilities
Federated Agents / Trust Relationships
Trust Anchors / Project Grants / Capability Grants
Delegations
MCP Servers
Tool Catalog
Agent / Tool Policies
Agent Calls / Tool Calls
Task Graph / Session Trace
Approvals
```

默认列表仅展示 Agent/Tool、调用关系、决策、风险、耗时、成本和归属，不展示 Message/参数/结果正文。

Tool Policy 编辑仍走 Config Draft/ChangeSet；Disable MCP Server 属于 Operational Action。

Federated Agent Detail 至少显示：

```text
Owner Scope / Direction
Relationship Status / Assurance
Publisher / External Subject
Approved Card Hash / Candidate Card Diff
Trust Anchors / Last Verified / Reverify
Allowed Projects / Capabilities
Data Boundary / Processing Regions / Contract Ref
Pricing Mode / External Procurement Budget
Inbound/Outbound Calls / Cost / Security Events
```

`Review/Activate Relationship` 进入 High Risk Workflow；`Suspend/Revoke` 提供紧急操作入口并触发 Security Epoch。

## 28.23.2 Optimization Recommendations（P2）

不新增一级导航，放在 `Routing`、`Guardrails` 与 `Config Changes` 的 Recommendation Tab 中。

每条建议必须展示：

- 当前配置；
- 建议变更；
- observation window / sample count；
- evidence summary；
- confidence；
- expected impact；
- risks；
- `Accept as Draft` / `Dismiss`。

接受后进入标准 Diff/Validate/Publish，不提供“一键自动应用”。

## 28.24 Tenant / Platform 管理

System Admin 才显示：

- Tenants；
- Shared Providers；
- Platform Health；
- System Policies；
- Global Pricing；
- Cross-tenant Usage（独立权限）；
- System Integrations。

Tenant List：

- Status；
- Requests/Tokens/Cost；
- Budget；
- Runtime Version；
- Security Epoch；
- Drift；
- Provider Health；
- Data Region。

Suspend Tenant 是 Critical Operational Action：展示影响 + 输入 Tenant 名称 + Re-auth + Audit；Enterprise 可选双人审批。

## 28.25 危险操作分级

不对所有动作一律“输入名称 + re-auth”，避免高频操作过度摩擦。

```text
Low
  简单确认或直接执行
  Ack Alert / Test Connection

Medium
  Impact Summary + Confirm
  Disable non-primary Credential / Purge scoped Cache

High
  Impact Summary + type-to-confirm
  Revoke Key / Disable primary Deployment / Config Rollback

Critical
  type-to-confirm + Re-auth + optional dual approval
  Suspend Tenant / Loosen mandatory Guardrail / OIDC critical change
```

Re-auth：

- OIDC `prompt=login` / `max_age=0`；
- WebAuthn/Passkey（若部署启用）；
- Local Admin password（Lite）。

PWA 不直接假设“设备生物识别”可用；生物识别仅通过 WebAuthn/平台认证器间接实现。

## 28.26 Design System

### Theme

- 默认 `system`，跟随操作系统；
- 支持 Light / Dark；
- 不把 Dark 作为强制默认；
- Theme/Accent 仅影响视觉，不影响状态语义。

### Status Semantics

固定：

```text
Healthy/Active      Success
Degraded/Warning    Warning
Failed/Open/Critical Error
Disabled/Unknown    Neutral
```

不得只依赖颜色；必须配 Text/Icon/Badge。

### Density

支持：

- Comfortable（默认）；
- Compact（大规模表格管理员可选）。

### Layout Tokens

- 8px spacing grid；
- Sidebar 240px / collapsed 64px；
- Form label/field 一致宽度；
- 详情页使用 Tabs，不无限堆叠长表单；
- 长 JSON/错误日志使用 monospace + copy，默认折叠。

## 28.27 共享组件

必须先实现：

| Component | Usage |
|---|---|
| TenantProjectScope | Tenant/Project 全局上下文 |
| GlobalTimeRange | Dashboard/FinOps/Requests |
| ServerDataTable | 所有资源列表 |
| ResourceStatusBadge | Provider/Deployment/Key/Tenant |
| DraftBar | 所有配置编辑页面 |
| SemanticDiffViewer | Config/Route/Guardrail/Budget |
| DecisionTimeline | Playground/Requests |
| RouteExplain | Routing Simulator/Request Detail |
| PolicyInheritanceView | Guardrail/Budget/Rate/Cache/ACL |
| BudgetMeter | Dashboard/Project/Key/OrgUnit |
| RiskConfirm | 高风险动作 |
| SecretOneTimeView | Key/Credential create |
| AlertRuleBuilder | Alerts |
| AttributionBadge | User/OrgUnit FinOps |
| EmptyState | 所有首次使用页面 |
| AsyncJobStatus | Export/Impact Preview/Long task |

## 28.28 Empty / Loading / Error State

### Empty State

禁止只有“无数据”。必须提供：

```text
What: 这个页面管理什么
Why: 为什么需要
Next: 下一步按钮
```

示例：

```text
还没有 Guardrail Policy
保护输入、输出和 Tool 调用，避免 Prompt Injection 与敏感数据泄漏。
[从推荐模板创建] [打开 Playground]
```

### Loading

- 表格 Skeleton，不整页 Spinner；
- 图表保留布局避免跳动；
- SSE 显示 Connecting/Reconnecting 状态。

### Error

错误必须包含：

- 用户可读摘要；
- error code；
- request_id；
- retry/diagnose action；
- 高级模式 Raw detail。

## 28.29 Frontend Security

强制：

- Console Session 使用 Secure/HttpOnly/SameSite Cookie；
- access token/refresh token 不存 localStorage；
- Provider/API Key Secret 不进入前端日志、Analytics、Error Reporter；
- Service Worker 只缓存静态资源，不离线缓存 Admin API、Usage、Prompt、Response、Security Event；
- Playground/Request 中模型输出默认按纯文本/安全 Markdown 渲染，禁用 Raw HTML；
- 外部链接 `rel=noopener noreferrer`；
- CSP；
- 防 CSV Formula Injection：导出字符串若以 `=,+,-,@` 等危险前缀开始时转义；
- Tenant 切换必须清理 Query Cache、SSE、临时 Draft state；
- 前端 RBAC 只用于显示，后端授权为最终准则；
- Clipboard Secret 展示页面超时自动遮蔽；
- 浏览器 console 不打印请求 Body/Secret。

## 28.30 Frontend 技术栈

固定建议：

| Layer | Choice |
|---|---|
| Framework | React + TypeScript + Vite |
| Component System | Ant Design 5 + ProComponents + LiteAIG Design Tokens |
| Routing | React Router |
| Server State | TanStack Query |
| Local UI State | Zustand（仅跨组件 UI 状态） |
| Forms | React Hook Form + Zod；类型/基础 Schema 从 OpenAPI 生成 |
| Charts | ECharts |
| Realtime | SSE；暂不引入 WebSocket 作为默认依赖 |
| i18n | react-i18next，中文/英文 |
| Build | route-level code split + embed.FS into Go binary |
| Test | Vitest + Testing Library + Playwright + axe |

选择 Ant Design 的原因：LiteAIG 是数据密集型企业管理控制台，Table/Form/Drawer/Modal/Tree/Steps 等成熟度优先于完全自定义视觉。通过 Design Token 控制品牌化，不维护第二套基础组件系统。

## 28.31 Frontend 状态边界

- TanStack Query：API 数据；
- URL：Tenant/Project/Time/Filter/Tab 等可分享状态；
- Zustand：Sidebar、Command Palette、临时 UI 偏好；
- React Hook Form：未提交表单；
- Config Draft：服务端持久化，不只存在浏览器内存；
- Secret：仅组件瞬时内存，不进入 Query Cache/Persistence。

Tenant 切换：

```text
close SSE
→ cancel inflight query
→ clear tenant-scoped query cache
→ reset project selection
→ fetch authorized tenant context
→ reconnect
```

## 28.32 Realtime 实现

统一 `useSSEStream()`：

- Playground Stream；
- Request Live Tail；
- Alert Inbox optional live update；
- Config convergence status。

支持：

- AbortController；
- Last-Event-ID；
- exponential reconnect + jitter；
- heartbeat timeout；
- visibility change 降频；
- Tenant/Project scope change cleanup。

不为了实时 UI 单独引入 WebSocket Gateway。

## 28.33 PWA

### Mobile Navigation

窄屏：

```text
Dashboard | Alerts | Requests | More
```

`More`：Usage、Keys、Health、Security Events、Config Versions。

### Mobile 能力边界

高频、低输入复杂度：

- Dashboard；
- Alert Ack/Silence；
- Security Event；
- Usage/Cost summary；
- Disable Key；
- Disable Deployment；
- Reset Circuit；
- Rollback（High Risk）；
- Tenant Suspend（System Admin/Critical）。

复杂 Route/Guardrail/Integration 编辑默认只读或引导桌面端完成，不在手机上复制复杂表单。

### Mobile Requests

默认：最近 20/50 条请求 + Pull to Refresh；Live SSE 可手动启用，不作为默认，减少移动网络/电量开销。

### PWA Security

- Service Worker 只缓存静态 shell；
- Admin API `no-store`；
- 不离线保存 Secret/Prompt/Response；
- High/Critical Action 强制 Re-auth。

## 28.34 UI 性能预算

目标：

| Metric | Target |
|---|---|
| Dashboard LCP | ≤2.5s（受控企业网络/标准数据集） |
| SPA route transition | ≤300ms（缓存命中） |
| Table filter response | ≤500ms UI feedback；数据 SLO 由 API 决定 |
| Live event received→row rendered | ≤100ms P95 |
| Command Palette open | ≤100ms |
| Lighthouse Accessibility | ≥90 |
| WCAG | AA |

工程约束：

- Route lazy-load；
- ECharts 按页面加载；
- Usage/Cost 必须使用聚合 API；
- 大表格 Server-side pagination；
- >1,000 可见行才考虑 virtualization；
- 前端不得一次拉取 90 天 usage_events 明细。

## 28.35 UI 验收标准

### Scope Isolation

```gherkin
Given 用户同时属于 Tenant A 与 Tenant B
When Console 从 A 切换到 B
Then A 的 Query Cache/SSE/Project Context 被清理
And B 页面不能短暂显示任何 A 的资源行
```

### Draft Safety

```gherkin
Given 管理员在 Guardrail 页面关闭一条规则
When Switch 被切换
Then 只更新服务端 Draft
And Active Tenant Snapshot 不发生变化
And Draft Bar 显示待发布变更
```

### Playground Consistency

```gherkin
Given Playground 使用 Draft d12 执行请求
When 请求完成
Then Request Explorer 中相同 request_id 的 Route/Guardrail/Usage 与 Playground 完全一致
And source=playground
And Provider Cost 被记录
```

### Guardrail Impact Preview

```gherkin
Given Tenant 未启用 Request Content Retention
When 打开 Impact Preview
Then UI 不声称可重放历史流量
And 只提供 Saved Cases / Temporary Samples 数据源
```

### Diff

```gherkin
Given Validator 返回 1 error + 2 warnings
When 打开 Diff Viewer
Then Publish 禁用
And error 修复后允许进入 warning acknowledgement
```

### Live Tail Privacy

```gherkin
Given 用户打开 Request Live Tail
Then SSE payload 不包含 prompt/response body
And Server 强制当前 Tenant RBAC Scope
```

### Secret

```gherkin
Given API Key 创建成功
When 用户关闭 One-time Secret View
Then 任何 Console API 都无法重新获取完整 secret
```

### High Risk Mobile

```gherkin
Given 用户在 PWA 执行 Config Rollback
Then 系统要求 type-to-confirm 与 Re-auth
And 未成功 Re-auth 前后端均不执行操作
```

---

## 28.25 HA & Resilience

位置：`Operations → HA & Resilience`。

页面包含：

```text
Overview
Runtime & LKG
Gateway Replicas / Drain
Control Plane
PostgreSQL
Valkey/Redis
Accounting Spool
Secret Provider
AZ / Region
DR Drills
```

只显示低基数全局/Scope 聚合，不在 Prometheus 中放 Tenant 高基数标签。

高风险操作：

- Force Drain；
- Force Bundle Rollback；
- Promote DR Region；
- Override dependency fail mode；

必须 Re-auth + Audit；Enterprise 可要求双人审批。

---

# 29. Setup Wizard 与默认体验

默认：

```bash
docker run -p 8080:8080 \
  -v liteaig-data:/data \
  -e LITEAIG_ADMIN_PASSWORD=... \
  liteaig/liteaig:v6.9
```

Setup Wizard 目标：用户不需要先理解 Tenant/Project/Deployment/Route Policy 才能完成首次调用。

流程：

```text
1. Create Admin
2. Add Provider
3. Test Connection
4. Discover/Select Model
5. Create Default Tenant + Default Project
6. Create first Deployment
7. Auto-create Logical Model: default-chat
8. Configure Default Route
9. Create Virtual Key
10. Run first call in Playground
11. Show SDK example
12. Open Dashboard
```

默认策略：

- 单 Tenant 隐藏 Tenant Selector；
- Default Project 自动创建；
- Route 使用简单 Priority；
- Guardrail 只启用 P0 Fast Guard 安全默认值；
- Cache 默认关闭；
- Budget 可跳过但明确提示生产建议；
- 不强迫用户进入 YAML/JSON；
- 第一次调用完成后显示 `request_id / token / cost / latency / selected deployment`。

Setup Wizard 中 Provider Secret 提交后立即转为后端 `secret_ref`，前端不再读取明文。

验收：首次安装后 5 分钟内完成真实调用，并能从 Playground 跳转到同一 `request_id` 的 Request Explorer。

---

# 30. Production HA、灾备与部署拓扑

## 30.1 HA 设计目标

高可用不是“Gateway 多副本”的同义词。LiteAIG 将连续性拆分为：

1. Data Plane HA；
2. Control Plane HA；
3. Config/Runtime HA；
4. Distributed State HA；
5. Metadata/PostgreSQL HA；
6. Accounting HA；
7. Secret/KMS HA；
8. AZ/Region HA；
9. Upgrade/Migration HA；
10. Tenant Fault Isolation。

核心原则：

> **配置面可以停，数据面不能因配置面故障同时停；状态依赖可以降级，但每一种降级必须有显式语义。**

## 30.2 Lite

```text
liteaig(all)
  ├─ SQLite
  ├─ memory cache
  ├─ local Runtime LKG
  └─ embedded Web/PWA
```

Lite 提供进程恢复、LKG 与 graceful shutdown，不承诺 Multi-Node/AZ HA。

## 30.3 Standard HA

推荐最小生产拓扑：

```text
                     Regional Load Balancer
                               │
              ┌────────────────┼────────────────┐
              ↓                ↓                ↓
          Gateway-1        Gateway-2        Gateway-3
            AZ-A             AZ-B             AZ-A/B
              │                │                │
              └──────────── Coordination ───────┘
                               │
                     Valkey/Redis HA
                               │
                     PostgreSQL HA
                               │
                 Control Plane x2+ (Active)
```

基线：

- Gateway ≥3；
- Control Plane ≥2；
- ≥2 Nodes，推荐 ≥2 AZ；
- PostgreSQL Primary + Standby / Managed HA；
- Valkey/Redis Sentinel/Managed HA 或等价方案；
- Runtime LKG；
- Durable Accounting Spool；
- PDB + TopologySpread + Anti-affinity；
- Graceful Drain；
- HPA。

## 30.4 Enterprise Multi-AZ

```text
                       Regional LB
                           │
        ┌──────────────────┼──────────────────┐
        ↓                  ↓                  ↓
      AZ-A               AZ-B               AZ-C
   Gateway x2+        Gateway x2+        Gateway x2+
        └──────────────────┼──────────────────┘
                           │
                Control Plane x3+
                           │
             PostgreSQL Multi-AZ HA
                           │
              Valkey/Redis Cluster
                           │
          Optional Analytics/Event Sink
```

要求：

- Gateway/Control 跨至少 3 AZ 分散；
- 任一 AZ 丢失后，剩余容量仍满足最低生产负载；
- PostgreSQL/Valkey Replica 不与 Primary 全部落在同一 AZ；
- Analytics Store 不作为 Data Plane Ready 强依赖；
- Zone 级故障必须通过 Chaos Drill。

## 30.5 Data Plane Active-Active

Gateway 没有 Leader。

LB Health Check 使用：

```text
/livez   → 进程存活
/readyz  → 是否接受新流量
/healthz → 依赖和运行详情，不用于简单摘流
```

`/readyz` 不要求：

- Control Plane 可达；
- PostgreSQL 可达；
- 所有 Provider 健康；
- Analytics/OTel Sink 可达。

Provider/Dependency 故障由 Routing/Degradation Policy 处理，避免依赖故障导致“所有 Pod 同时 NotReady”。

## 30.6 Control Plane HA

普通 CRUD / Query：

- Active-Active；
- 共享 PostgreSQL；
- Admin LB 任意分发。

单例/协调任务：

```text
Config Publish Coordinator
Reservation Sweeper
Retention/Rollup
Alert Evaluation
Scheduled Recommendation
Migration Coordinator
```

采用 PostgreSQL advisory lock、Valkey Lease 或分片 Ownership。所有任务必须可重入或幂等。

Control Plane 全部不可用时：

- Data Plane 使用 LKG；
- Admin 修改/Publish 暂停；
- 新 Secret/新 Tenant provisioning 暂停；
- 已运行请求继续按当前 Runtime 工作。

## 30.7 PostgreSQL HA

LiteAIG 不自研数据库复制协议。

推荐：

- Cloud Managed PostgreSQL；
- CloudNativePG / Patroni / 等价成熟 PostgreSQL HA 方案。

数据分级：

| 数据 | 重要性 | 故障策略 |
|---|---|---|
| Tenant/API Key/RBAC/Policy/Config/Pricing | 强持久 | HA + 备份 + PITR |
| Usage Ledger | 财务事实 | Durable Spool + DB HA |
| Audit/Security Event | 审计事实 | 持久化 + Event Buffer |
| Dashboard Rollup | 可重建 | 可延迟/重算 |
| Cache | 可重建 | Bypass |

PostgreSQL Primary Failover 期间 Data Plane 不应因 metadata DB 不可达而退出；新增配置/管理写入可暂时失败。

## 30.8 Valkey/Redis HA 与故障语义

Redis/Valkey 中的状态必须按用途定义，不允许一个全局 `redis.fail_mode`：

| 能力 | Redis/Valkey 故障行为 |
|---|---|
| Exact Cache | bypass |
| Semantic Cache | bypass / disable semantic |
| Config PubSub | version poll / existing LKG |
| Routing dynamic metric | local EWMA / conservative |
| Soft Budget | fail-open + alert |
| Hard Budget | fail-closed |
| Rate Limit | policy: fail-open / local conservative / fail-closed |
| Concurrency Lease | policy: reject or local conservative；禁止假装分布式上限仍严格 |
| Reservation Ledger | hard policy fail-closed；恢复后 Sweeper/Reconcile |
| Leader/Job Lease | job pause / re-elect |

Standard 可使用 Sentinel/Managed HA；Enterprise 可使用 Cluster/Managed Redis。复制故障语义必须承认异步复制可能存在短窗口数据损失，Hard Budget 正确性仍依赖 Reservation/Reconcile，而不是“有副本就等于零丢失”。

## 30.9 Durable Accounting Spool

路径示例：

```text
/data/spool/accounting/
  segment-000001.wal
  segment-000002.wal
```

每条记录至少：

```text
event_id
request_id
tenant_id
event_ts
usage
cost
pricing_version
checksum
```

写入：

```text
Provider usage finalized
  → local WAL append + fsync policy
  → enqueue flush
  → PostgreSQL/Event Sink
  → idempotent ingest
  → checkpoint/truncate
```

默认：

```text
spool.warn_threshold = 70%
spool.critical_threshold = 90%
spool.hard_threshold = 100%
```

Tenant/Platform 可选择：

- `hard_accounting`：Spool 无法保证新事件持久化时拒绝新的可计费请求；
- `soft_accounting`：继续服务 + Critical Alert，必须暴露未结算风险。

Spool 必须限制磁盘使用并防止单 Tenant 占满整个节点，可按 Tenant/Shard 设置配额。

## 30.10 Graceful Drain 与 Streaming

SIGTERM：

```text
state=DRAINING
  → readyz=false
  → LB 停止新流量
  → 普通请求完成
  → SSE/A2A/MCP 长流继续
  → drain timeout
  → cancel remaining upstream
  → finalize usage/accounting
  → exit
```

配置：

```text
drain_timeout
stream_drain_timeout
force_shutdown_timeout
terminationGracePeriodSeconds
```

Drain 时禁止接收新长流；已有 Budget/Lease/Usage 必须正常 Reconcile。

## 30.11 Kubernetes HA 基线

Standard/Enterprise Helm 默认提供：

- `PodDisruptionBudget`；
- `topologySpreadConstraints`；
- Pod anti-affinity；
- HPA；
- PriorityClass（可选）；
- startup/readiness/liveness probes；
- Graceful termination；
- rolling update `maxUnavailable/maxSurge`；
- resource requests/limits；
- zone-aware placement。

建议：

```text
Gateway PDB: minAvailable >= 2
Control PDB: minAvailable >= 1/2（按副本规模）
```

具体数值必须与副本数、AZ 数和容量计划联动，Chart Validator 禁止明显矛盾配置。

## 30.12 Autoscaling

HPA 不只看 CPU。

Gateway 推荐：

```text
CPU
Memory
RPS
inflight_requests
active_streams
gateway_core_latency
queue_depth
```

重点：

```text
active_streams_per_pod
inflight_requests_per_pod
```

Scale-down 必须考虑 Drain，避免大量 Stream 被同时终止。

## 30.13 Noisy Neighbor Isolation

容量控制层级：

```text
Global Capacity
  → Tenant
      → Project
          → Principal / Agent / Key
```

至少支持：

- max concurrent requests；
- max concurrent streams；
- RPM/TPM；
- Task/Agent call limits；
- Budget；
- queue/admission policy。

单 Tenant 流量异常不得耗尽整个 Gateway Worker、连接池、Spool 或 Redis 热 Slot。

## 30.14 Multi-Region / DR

推荐先实现 Region 内 Multi-AZ，再实现 Region DR。

```text
                       Global DNS / GSLB
                              │
             ┌────────────────┴────────────────┐
             ↓                                 ↓
         Region A                          Region B
      Active / Primary                  DR / Active
             │                                 │
        Regional LB                       Regional LB
             │                                 │
        Gateway xN                         Gateway xN
        LKG Bundle                         LKG Bundle
             │                                 │
       Regional State                    Regional State
```

原则：

- Gateway/RuntimeBundle 可多 Region 分发；
- Request 热路径尽量使用 Regional State；
- 不默认使用跨 Region 单一 Redis 热路径；
- PostgreSQL DR 根据业务选择跨 Region replica / managed global DB / backup restore；
- Region B 可以是 Warm Standby 或 Active-Active。

## 30.15 Multi-Region Budget 一致性

支持三种模式：

### Regional

每个 Region 独立上限。低延迟、最简单。

### Global Soft

```text
Global Budget
  → Region Quota Slice A
  → Region Quota Slice B
```

各 Region 在本地 Slice 内原子扣减，后台协调 Slice 再分配。适合大多数全球业务。

### Global Hard

需要 Single Budget Authority 或具备跨 Region 一致性的外部系统。此模式明确接受更高延迟和可用性权衡，不作为默认。

禁止宣称“全球双活 + 零额外延迟 + 全局强一致 Budget”同时无代价成立。

### 30.15.1 Multi-Region Task Governance

Task Counter 与 Budget 面临同类的跨 Region “上限 + 原子计数”问题，因此复用统一 `Counter/Quota Authority` 基础设施，但不把 Task 强行实现成 Budget 子模块。

#### Regional

- `agent_tasks.home_region` 是权威 Region；
- Task Counter 只在 Home Region 权威修改；
- 非 Home Region 不能独立接受同一 Task 的硬计数更新；
- 可以从 Home Region 调用位于其他 Region 的 Agent Endpoint；
- Home Region 不可达时，严格 Task 默认 fail-closed，避免静默形成第二个 Task Authority。

#### Global Soft

- 每个 Region 获得 Calls/Hops/Tokens/Cost Slice；
- 本地原子检查；
- 后台 Reconcile；
- 允许可观测、有界的超发；
- External Agent Procurement Budget 与 Task Cost Slice 都必须纳入 Reconcile。

#### Global Hard

- Task 与其硬 Budget 使用同一类 Strong Counter Authority；
- 不另建 Task 专用 Consensus 系统；
- 所有 Region 在发起下一下游调用前完成权威检查；
- 适用于高风险/高成本 Agentic Task，接受额外 RTT。

Loop Detection：

- Regional：Home Region Graph 为权威；
- Global Soft：区域局部检测 + Root Task 汇总，跨 Region 重复边使用更保守阈值；
- Global Hard：关键 Hop/Call Counter 强一致；图分析仍可异步，不把 Graph Store 放进热路径。

## 30.16 DR、Backup 与 Restore

至少覆盖：

- PostgreSQL Base Backup + WAL/PITR；
- Config/Bundle Manifest 备份；
- KMS/Vault recovery procedure；
- Object Storage versioning（若使用）；
- RuntimeBundle 可由 metadata 重编译；
- Accounting Spool 节点故障场景的风险与恢复边界；
- Helm/Config/Secret Provider 的 Infrastructure-as-Code 恢复。

DR Runbook 必须包含：

```text
detect
declare
traffic switch
restore dependency
validate runtime
reconcile accounting/budget
return traffic
postmortem
```

## 30.17 故障降级矩阵

| 故障 | 预期行为 |
|---|---|
| 单 Gateway Pod | LB 摘除；其他 Active 实例继续 |
| Node | Kubernetes 重调度 |
| 单 AZ | 其他 AZ 承担；容量告警 |
| Control Plane 全 Down | DP 使用 LKG；Config/Admin 写暂停 |
| PostgreSQL Down | DP 继续；Config 写暂停；Usage 进入 Spool |
| Redis Cache Down | Cache bypass |
| Redis PubSub Down | poll/LKG |
| Redis Hard Budget Down | fail-closed |
| Redis Soft Budget Down | fail-open + alert |
| KMS/Vault Down | 已缓存 Credential 在 TTL/grace 内继续；新解密失败 |
| 单 Provider Down | Circuit → Fallback |
| Credential 失效 | Credential Pool rotate |
| 单 MCP Server Down | Tool 标记不可用；默认不做语义 Fallback；只有显式 `equivalent_tool_group` + schema/idempotency 兼容时才允许受控切换 |
| INTERNAL Agent Endpoint Down | 同 Agent Version/Compatibility Class 的已批准 Endpoint 可 Failover；否则当前 Hop 失败并结算 |
| EXTERNAL Agent Endpoint Down | 默认直接失败；只有操作明确幂等且目标仍属于同一 Approved Relationship/Version 时才允许有限 Retry/Endpoint Failover |
| External Agent Card/JWKS/Registry 不可达 | 已缓存且仍在 Trust TTL/Anchor Validity 内的已批准关系可继续；新增/续期/Material Change Review fail-closed；不得跳过验证 |
| Federated Inbound IdP/JWKS 不可达 | 只允许有效缓存窗口内身份验证；缓存过期后 fail-closed |
| Federated Relationship Suspend/Revoke | Security Epoch Fast Publish；新 Federated Traffic 立即拒绝，Strict 节点不得用旧 LKG 绕过已知 Revocation |
| Analytics/OTel Down | 本地请求不阻断；buffer/drop 按 sink policy |
| Config Compile/Bundle NACK | 保持旧 Bundle |
| Gateway Rolling Upgrade | Drain；已有流完成 |
| Region Down | GSLB/Runbook 切换 DR Region |

该矩阵必须在 Tenant/Platform Policy 中对安全关键项允许覆盖，但默认值必须安全、明确且可观测。

---

# 31. 性能、可用性与 SLO

## 31.1 Core Overhead 定义

Core 不包含：

- Provider 网络；
- External Guardrail；
- Semantic Cache embedding；
- LLM-as-Judge；
- strict remote stream guard。

Standard P99 目标：≤6ms。

### Standard 分阶段预算

| Component | P99 Budget |
|---|---:|
| Admission | 0.5ms |
| Built-in Input Guard | 1.5ms |
| Local Policy Preflight | 1.0ms |
| Routing | 0.5ms |
| Execution scheduling | 0.3ms |
| Built-in Output Guard | 1.2ms |
| Accounting sync | 0.3ms |
| Total | ≤5.3ms target / ≤6ms SLO |

Redis 网络 RTT 作为独立 `coordination_latency` 记录；如果开启 hard distributed budget/rate limit，其网络 RTT不伪装成本地 CPU Core 时间，但计入 end-to-end gateway latency。

## 31.2 其它 SLO

| SLI | Target |
|---|---|
| Standard Data Plane Availability architecture target | ≥99.95% |
| Enterprise Data Plane Availability architecture target | ≥99.99% |
| Standard Control Plane Availability target | ≥99.9% |
| CP outage: already-loaded DP forwarding | Continue on valid LKG |
| Config convergence | ≤3s |
| Security Fast Publish best-effort | <1s typical |
| Client disconnect upstream cancel | ≤500ms |
| Request Explorer hot query | ≤2s |
| Hard budget overrun target | <1% |
| Hard budget acceptance ceiling | <5% |
| Layer1 Stream Guard | ≤0.3ms/chunk |
| Agent/Tool Policy local decision P99 | ≤1ms target（不含外部 Auth/Approval） |
| Task Governance counter update P99 | ≤1ms target（不含 Redis RTT） |

External Guardrail 有独立 SLI：latency / timeout / fail-open / fail-closed。

## 31.3 HA SLI

| SLI | Target / Rule |
|---|---|
| Single Gateway failure traffic recovery | LB health convergence + retry policy；不承诺单请求零失败 |
| Rolling Upgrade new-request interruption | 0 target |
| Existing Stream graceful completion | ≥99.9% target under planned upgrade |
| RuntimeBundle activation | atomic per process |
| LKG boot | CP unreachable 时可启动 valid LKG |
| Accounting accepted-but-not-ingested loss | 0 target for acknowledged local-spool events |
| Accounting spool replay | idempotent |
| Config partial activation | 0 |
| Cross-tenant failure propagation | 0 target |
| AZ failure | Enterprise/qualified Standard within SLO after capacity failover |

## 31.4 RTO / RPO 设计目标

以下是架构目标，必须经过实际环境 DR Drill 后才能对外承诺：

| Failure Domain | Standard Target | Enterprise Target |
|---|---:|---:|
| Gateway Pod | RTO < 1 min | RTO < 30s |
| Node | RTO < 5 min | RTO < 2 min |
| PostgreSQL Primary | RTO < 5 min | RTO < 2 min |
| Redis/Valkey Primary | RTO < 5 min | RTO < 2 min |
| Single AZ | best effort / capacity dependent | RTO < 5 min |
| Region | Runbook dependent | RTO 15–60 min target |
| Config metadata RPO | ≤ backup/replication policy | near-zero regional / defined cross-region |
| Durable Accounting local RPO | 0 for fsync-acknowledged spool event | 0 for fsync-acknowledged spool event |

跨 Region DR 的 RPO 取决于 PostgreSQL/KMS/Object Storage 的实际复制能力，LiteAIG 不在软件层虚假承诺零 RPO。

## 31.5 Capacity Headroom

生产 HA 要求在设计故障域丢失后仍保留容量：

```text
normal_utilization_target <= 60~70%
```

具体阈值按 Provider 延迟、Streaming 比例、CPU/Memory/FD/连接数压测决定。Enterprise 三 AZ 场景建议按“丢一 AZ 后剩余实例仍能承载关键负载”进行容量规划。

---

# 32. 测试策略

## 32.1 单元测试

- route hard filter / score；
- circuit state machine；
- HMAC key verify；
- budget reservation/reconcile/expire；
- lease acquire/renew/release；
- guardrail aggregation；
- pricing calculation；
- config validator。

## 32.2 Provider Contract

每个 Provider：

- normal；
- timeout；
- 429；
- 5xx；
- stream drop；
- client cancel。

## 32.3 Guardrail Contract

- allow；
- block；
- mask/redact；
- timeout；
- unavailable；
- fail_open；
- fail_closed；
- no sensitive log。

## 32.4 Cache Tests

- tenant isolation；
- policy update + old cache；
- cache hit output guard；
- tool-call exclusion；
- eviction；
- backend unavailable。

## 32.5 Budget Tests

- multi-window all-or-nothing；
- Tenant + Project + Key same-slot atomicity；
- duplicate reconcile idempotent；
- process crash before PG audit write；
- sweeper release；
- Redis fail behavior。

## 32.6 Lease Tests

- capacity boundary；
- SIGKILL simulation；
- expired cleanup；
- stream renew；
- release idempotent。

## 32.7 Stream Guard

- cross-chunk sensitive token；
- UTF-8 boundary；
- Layer1 mask；
- Layer2 buffered block；
- Layer3 retroactive event；
- client disconnect；
- provider disconnect。

## 32.8 Multi-Tenancy Tests

- Tenant A API Key 不能解析 Tenant B Project；
- Tenant Private Credential 不能被其他 Tenant Deployment 引用；
- PostgreSQL RLS 横向越权测试；
- Redis namespace 交叉污染测试；
- Exact/Semantic Cache Tenant leakage；
- User 调岗后历史 Usage 仍归属原 OrgUnit；
- Signed User Context 伪造/重放/跨 Tenant 测试；
- Tenant Snapshot 更新不改变其他 Tenant active version；
- Suspended Tenant 新请求全部拒绝；
- System Shared Provider 不泄露 Credential Secret。

## 32.9 FinOps Attribution Tests

- API Key-only 请求可归属 Tenant/Project/Key；
- Application/Agent 绑定正确落 Usage；
- verified user JWT 正确写 user/org_unit；
- unverified user label 不参与强制 User Budget；
- 部门/人员 Token 聚合与明细总和一致；
- 人员调岗跨账期统计正确；
- Project × OrgUnit 交叉聚合正确。

## 32.10 Security Tests

- SSRF；
- DNS rebinding；
- cache tenant leakage；
- RBAC escalation；
- MCP Tool ACL bypass；
- Prompt Injection against Tool ACL；
- Config loosen approval。

## 32.11 Chaos / HA / DR

### Process / Node

- kill single Gateway；
- kill 50% Gateway；
- SIGKILL during active SSE；
- graceful node drain；
- Control Plane process kill；
- Control Plane all unavailable。

### Config

- PubSub lost；
- stale config node；
- invalid signature/checksum；
- partial distribution；
- Bundle Prepare NACK；
- active bundle corruption → previous LKG recovery；
- restart DP while CP unreachable。

### PostgreSQL / Valkey

- PostgreSQL primary loss / failover；
- PostgreSQL unavailable while requests continue；
- Redis/Valkey primary loss；
- Redis/DB network partition；
- Hard Budget Redis outage；
- Cache Redis outage；
- Reservation/Lease crash recovery。

### Accounting

- kill Gateway after Provider success but before DB ingest；
- PostgreSQL outage with Spool growth；
- restart and replay Spool；
- duplicate replay idempotency；
- disk full / spool hard threshold；
- single Tenant attempts to exhaust Spool。

### Secret

- KMS/Vault outage；
- cached credential TTL/grace；
- rotation during Secret Provider degradation。

### Capacity / Fault Domain

- request flood；
- active stream flood；
- noisy Tenant；
- single AZ loss；
- scale up/down under streaming load；
- PDB + node maintenance；
- Enterprise Region DR drill（release/milestone gate）。

验收必须检查：请求正确性、Budget/Lease、Usage/Cost、LKG、Security Epoch、Tenant 隔离、恢复后的 Reconcile，而不仅是“服务最终恢复”。

---

## 32.12 Web Console Component / E2E

组件测试：

- TenantProjectScope；
- DraftBar；
- SemanticDiffViewer；
- DecisionTimeline；
- PolicyInheritanceView；
- RiskConfirm；
- SecretOneTimeView；
- AlertRuleBuilder；
- AttributionBadge。

E2E：

- Setup Wizard → First Playground Call → Request Explorer；
- Provider Create/Test/Disable Impact；
- Draft Edit → Validate → Diff → Publish → Runtime Convergence；
- Draft Conflict/Rebase；
- Guardrail Tighten Fast Publish；
- Guardrail Loosen Re-auth/Approval；
- API Key create/rotate/revoke；
- Live Tail reconnect/tenant switch；
- Usage Project×Department drill-down；
- RBAC menu visibility + backend deny；
- PWA High Risk Re-auth。

## 32.13 Frontend Security Tests

- XSS payload in LLM output；
- malicious Markdown/URL；
- Secret absent from localStorage/IndexedDB/console/error reporter；
- Tenant switch cache leakage；
- SSE cross-tenant leakage；
- CSV formula injection；
- CSRF；
- clickjacking/CSP；
- unauthorized deep-link access；
- Draft resource scope escalation。

## 32.14 Accessibility / Visual Regression

- axe automated checks；
- keyboard-only navigation；
- focus trap for Modal/Drawer；
- screen-reader labels；
- Light/Dark visual regression；
- Compact/Comfortable density；
- Desktop/Tablet/Mobile breakpoints；
- 图表表格替代视图。

---

## 32.15 Agentic Governance Tests

- cross-tenant MCP server access denied；
- Project/Agent Tool ACL inheritance；
- denied call does not reach MCP server；
- Tool Result re-enters Guardrail with `provenance=tool_result`；
- argument/result raw content not persisted by default；
- session trace links LLM request and Tool Call；
- Prompt Injection cannot bypass Tool ACL；
- approval-required call cannot execute before approval（Phase 4）；
- A2A Agent Card signature/trust policy；
- Delegation cannot escalate parent permission；
- max_agent_hops/task_budget blocks before next downstream call；
- deterministic loop detection terminates repeated agent edges；
- A2A push callback URL passes SSRF policy；
- Task Graph edges equal agent/tool/request ledger facts。
- External Agent Candidate cannot become active without verified Trust Anchor；
- A2A Agent Card JWS verification matches A2A canonicalization when JWS mode is used；
- mTLS/OIDC/Registry Attestation can satisfy configured Trust Profile without falsely requiring JWS；
- Material Agent Card endpoint/auth/capability/key change creates pending review and does not replace approved version；
- local Delegation + Federation Grant cannot escalate caller permission；
- inbound external caller cannot self-assert internal user/delegation/tenant identity；
- outbound Capability Grant never authorizes inbound access and vice versa；
- strict residency rejects unknown/incompatible external data boundary；
- external response uses `provenance=external_agent_response` and re-enters Guardrail；
- external cost is included in Task total and separately constrained by Procurement Budget；
- unknown external pricing cannot satisfy hard cost enforcement without explicit policy；
- Relationship revoke increments Security Epoch and blocks new federated calls；
- external non-idempotent call is not automatically retried。

## 32.16 Adaptive Governance Tests

- Recommendation never mutates active RuntimeSnapshot；
- accept creates Config Draft only；
- cross-tenant data cannot enter evidence；
- insufficient sample produces no actionable patch；
- Guardrail recommendation without review/replay evidence is informational only；
- evidence/confidence/sample window displayed consistently in API and UI；
- dismissed recommendation does not reappear until evidence window materially changes。

## 32.17 Trust & Assurance Release Tests

- Guardrail benchmark report is reproducible from release artifact；
- benchmark report includes corpus/version/threshold/model version；
- Evidence Export excludes Secret and Prompt/Response body by default；
- External Guardrail destination and policy are traceable；
- data processing opt-in/out path covered by E2E test。

---

## 32.18 Modular Architecture Tests

- forbidden import graph；
- Protocol Adapter purity；
- Data Plane / Control Plane dependency separation；
- Agent Identity single-owner；
- Capability Index is derived from Resource source；
- migration table owner manifest；
- Agent Version active/draining/retired state machine；
- Task version pin / drain behavior；
- Connector cannot bypass Policy/Accounting。
- Federated Agent relationship cannot be written by Catalog repository；
- External Agent cannot have both INTERNAL project ownership and EXTERNAL_FEDERATED owner_scope；
- Protocol Adapter cannot treat Agent Card discovery as authorization；
- Federation Trust Anchor/Grant tables have a single module owner。

V8.5 新增（由 `cmd/architecture-test` + `go test ./internal/architecture` 执行）：

- 全部 Go 包必须有 `modules.yaml` 声明的属主模块（无属主包即失败）；
- forbidden-imports 规则全集（24+ 条）：含 `storage→adminapi`、`backend→adminapi/gateway/app`、`webkit/catalog/audit 零业务依赖`、`tenancy/identity→表示层`；
- 规则有效性负向测试：注入违规 import 必须导致检查失败（防止规则空转）；
- migration `-- owner:` 注释与 `table-owners.yaml` 一致（`CheckMigrationOwners`）；
- 非持久层 Go 源码的跨模块表引用违规（`CheckDirectSQL`，contract-test 豁免）；
- 能力接口契约测试：`Backend` 组合超集 = 12 窄接口并集；服务签名无 `any` 返回（编译期）；
- 视图类型线格式契约：e2e management-loop（setup→login→dashboard→playground→requests→config）作为 JSON 契约回归门禁；
- webkit 内核单测：路由参数/方法、具体度匹配、中间件洋葱顺序、短路语义、group 作用域、APIError 渲染、自定义 ErrorHandler、Bind 上限、二次写入防护、Recover、安全头覆盖。

## 32.19 HA Invariant Tests

- `/readyz` does not fail only because CP/PostgreSQL/one Provider is down；
- no Gateway leader dependency；
- RuntimeBundle atomic activate；
- invalid Bundle never replaces active LKG；
- LKG boot preserves Tenant/Policy/Security version；
- Config Publish concurrent coordinator is single-owner/idempotent；
- Drain stops new traffic before process exit；
- Stream finalization executes Accounting；
- Dependency degradation matches §30.17 matrix；
- Multi-region hard/soft/regional budget modes have explicit tests；
- Multi-region Task regional/global_soft/global_hard modes have explicit counter/overshoot tests；
- Regional Task has a single Home Region Authority；
- Global Soft Task overshoot remains within allocated slice bound and reconciles；
- External Trust cache outage follows §30.17 fail-closed/TTL behavior；
- Tenant overload does not exhaust another Tenant's reserved capacity in isolation test。

---

# 33. CI/CD

PR：

```text
lint
→ architecture-test
→ unit
→ provider contract
→ MCP/A2A protocol contract
→ guardrail contract
→ routing/budget golden
→ integration(Postgres+Redis+Mock Provider)
→ ha-invariant
→ lkg/spool recovery
→ benchmark gate
→ UI schema compatibility
```

Main：

```text
migration dry-run
→ image build
→ SBOM
→ image signing
→ security scan
→ guardrail benchmark report
→ evidence export smoke
→ migration expand-contract check
→ canary
→ smoke
→ graceful-drain smoke
→ rolling release
→ post-rollout HA probe
```

Migration：

采用 **Expand → Migrate/Dual Read-Write → Contract**：

- forward-compatible；
- 新字段优先 nullable/default；
- 先发布兼容新旧 Schema 的代码，再执行数据回填；
- 删除字段必须经过 deprecate 窗口；
- 扩缩容混部期间新旧版本 schema 必须兼容；
- destructive migration 禁止与同一次 Gateway rolling upgrade 同步直接执行；
- Migration Coordinator 使用 DB lock/lease，禁止多 Control Plane 重复执行；
- 迁移失败必须有恢复/继续语义，不允许留下不可启动的中间状态。

Benchmark gate：Stage P99 超预算 >10% 阻断合并。

---

# 34. 代码结构

V8.5 采用 **Modular Monolith + Contract Boundary + 三层服务架构**。目录体现职责，不代表独立进程；包属主由 `architecture/modules.yaml` 声明并 CI 校验。以下为实施基线实际布局（★ = V8.5 收口新增/重构）：

```text
liteaig/
├── cmd/
│   ├── liteaig/                          # 组合入口（--mode all|gateway|control）
│   └── architecture-test/ ★             # 架构守卫检查器（make check 门禁）
│
├── architecture/ ★                       # 可执行边界 manifest（CI 事实源）
│   ├── modules.yaml                      # 23 个模块 → 包属主
│   ├── forbidden-imports.yaml            # 24+ 条依赖禁则
│   └── table-owners.yaml                 # 33 张表 → owner 模块
│
├── internal/
│   ├── architecture/ ★                   # 检查器实现（import/module/table/direct-SQL）
│   │
│   ├── kernel/                           # 最稳定层
│   │   ├── pipeline/
│   │   ├── runtime/
│   │   ├── interaction/
│   │   ├── contracts/
│   │   └── errors/
│   │
│   ├── access/
│   │   ├── auth/
│   │   ├── params/
│   │   └── protocol/                     # openai / anthropic / mcp / a2a
│   │
│   ├── gateway/                          # Data Plane 编排（七阶段）
│   │   ├── server/                       # webkit 网关（Config.Middleware 注入）
│   │   ├── admission/  execution/  model/  tool/
│   │   ├── guardrail/  cache/  approval/
│   │   ├── accounting/                   # Stage 7 + Durable Spool
│   │   └── playground/
│   │
│   ├── tenancy/                          # 领域：租户/项目 + Repository/Transactor 契约
│   │   └── contracttest/                 # 仓库适配器契约测试（共享 harness）
│   │
│   ├── identity/                         # 领域：Principal / API Key / 本地凭据 / Pepper
│   │   ├── apikey/  password/  oidc/  scim/
│   │   ├── localauth.go ★                # LocalCredential/Store 领域接口
│   │   └── pepper.go ★                   # Pepper 领域（Resolve/EnsurePepper）
│   │
│   ├── catalog/ ★                        # 领域：资源目录视图（零内部依赖）
│   ├── federation/ ★                     # 领域：联邦关系/信任锚/Grant
│   │
│   ├── policy/        # rate / resolver / acl
│   ├── guardrail/     # builtin / judge / groundedness / benchmark
│   ├── cache/
│   ├── routing/       # model
│   ├── resilience/    # circuit / retry / leasegate / credentialpool
│   ├── finops/        # accounting / budget / pricing / aggregate
│   ├── organization/  # 用户/组织/成本中心
│   ├── agentic/       # analytics / recommend / routing（跨域建议）
│   │
│   ├── connectors/
│   │   ├── model/  tool/  agent/
│   │
│   ├── controlplane/
│   │   ├── adminapi/        # 表示层 ★（瘦身后：HTTP/会话/rbac/错误渲染）
│   │   │   ├── server.go             # Engine + 中间件 + registerRoutes
│   │   │   ├── capabilities.go ★     # 12 窄能力接口别名（源在 backend）
│   │   │   ├── routes_{keys,config,resources,ops,security}.go
│   │   │   └── session/login/oidc/users/stream
│   │   ├── backend/ ★             # 业务编排层（CI 禁止依赖表示层）
│   │   │   ├── backend.go            # ControlBackend + LiveBus
│   │   │   ├── capabilities.go       # 12 窄能力接口 + Backend 超集
│   │   │   └── views_*.go            # 全部视图契约类型
│   │   ├── config/              # Draft/Validator/Compiler/Publish（Service）
│   │   ├── setup/               # Setup Wizard + contracttest
│   │   ├── rbac/
│   │   └── approval/
│   │
│   ├── observability/
│   │   ├── audit/ ★                   # AuditRecord/Repository（零内部依赖）
│   │   ├── alert/  sink/  agentgraph/
│   │
│   └── platform/
│       ├── webkit/ ★                  # HTTP 微内核（零业务依赖）
│       │   ├── engine.go  context.go  error.go  middleware.go
│       ├── storage/
│       │   ├── sqlrepo/ ★             # Store 统一入口 + 全部 SQL 适配器
│       │   ├── sqlite/                # 纯连接适配器（Open + WAL 参数）
│       │   ├── postgres/              # 连接 + RLS
│       │   └── spool/                 # Durable Accounting Spool
│       ├── bundle/  lkg/  dr/         # RuntimeBundle / Last Known Good / DR
│       ├── coordination/  secrets/  egress/  clock/  id/
│   │
│   └── app/ ★                          # 组合根（唯一可依赖全部层的包）
│       ├── lite.go                     # 装配：Store → ControlBackend → HTTP
│       ├── lifecycle.go                # drainGate 中间件
│       ├── readiness.go                # readyz（webkit engine）
│       ├── governance.go / playground.go  # 业务编排（零 adminapi 引用）
│
├── web/
│   └── console/                        # React + TS + Vite + AntD + TanStack + SSE
│       ├── e2e/management-loop.spec.ts # Playwright 契约回归（setup→…→config）
│       └── ...
│
├── migrations/                          # 001–028，首行 -- owner: <module>
├── data/                                # Lite 档 SQLite（file: DSN）
└── tests/
    ├── contract/  golden/  chaos/  benchmark/  soak/  kubernetes/
```

与 V8.2 规划布局的差异说明：

- V8.2 规划的 `internal/agentic/{delegation,task,loop}`、`internal/controlplane/admin` 等在实施中按真实职责落位为 `federation/`、`controlplane/{backend,adminapi,approval}` 等，模块属主以 `modules.yaml` 23 项为准；
- `pkg/` 暂不创建（当前无对外承诺的稳定扩展契约，Provider/Guardrail/Sink 扩展点先落 internal）；
- `internal/architecture` + `cmd/architecture-test` + `architecture/*.yaml` 是 V8.5 新增的边界执行设施。

## 34.1 包依赖规则

1. `kernel/` 不 import `access/catalog/policy/guardrail/finops/connectors/controlplane/platform`；
2. `access/protocol/*` 只依赖 Kernel Contract，不直接依赖 Policy/Guardrail/FinOps；
3. `connectors/*` 不 import Routing/Guardrail/FinOps；
4. `gateway/` 是主要编排层，可通过 interface 使用各 Domain；
5. Domain 包不依赖 PostgreSQL/Redis concrete implementation；
6. Data Plane 不 import `controlplane/`；
7. `controlplane/config/compiler` 可以读取 Domain 配置模型并生成 Snapshot，Data Plane 不能反向读取 Draft；
8. `observability` 通过 Event/Telemetry Contract 接入，业务包禁止 import exporter/storage 实现；
9. `identity` 是唯一 Principal 事实模型；禁止 `agentic/identity`；
10. `catalog/agent` 管 Agent 元数据/版本/能力，`agentic` 管 Delegation/Task/Approval/Federation Trust；Federation Relationship 不复制 Agent Resource 元数据；
11. `pkg/` 只放明确承诺稳定兼容的外部扩展接口，内部 Agent API 不因目录方便而提前公开；
12. Budget/Accounting/Pipeline 不开放第三方覆盖；
13. `platform/ha` 只提供 readiness/LKG/lease/degradation 基础能力，不承担业务 Policy；
14. Accounting Spool 只能接收已经 Normalize 的 immutable accounting event；
15. Data Plane readiness 不允许通过 import Control Plane client 形成隐式强依赖。

V8.5 新增（forbidden-imports.yaml 执行，节选）：

16. `platform/storage/*` → `controlplane/adminapi` 禁止：存储层只实现领域包接口（`tenancy.Repository`、`identity.LocalCredentialStore`、`audit.Repository` …），不得引用表示层类型；
17. `controlplane/backend` → `controlplane/adminapi / gateway / app` 禁止：业务编排层是纯业务层，视图契约自持（`backend/views_*.go`），不反向取表示层符号；
18. `platform/webkit / catalog / observability/audit` 业务零依赖：三包仅允许标准库（catalog 允许引用 catalog 领域类型），新增业务 import 即 CI 失败；
19. `tenancy / identity` → `controlplane/adminapi / gateway / app` 禁止：基础领域包不得反向依赖服务层；
20. `controlplane/config` 等控制面领域包 → `adminapi` 禁止：Config Service 只面向领域类型（`config.Draft/Version`）；
21. 仓库构造唯一入口：`sqlrepo.Open(db, Deps)`；`app` 之外的包不得调用 `sqlrepo.New*` 单仓库构造器（测试直构除外，属白名单豁免）；
22. 装配根 `app` 允许依赖全部层（组合根豁免）；`app` 内业务文件（非装配函数）不得引用表示层类型——当前以代码评审 + `grep` 基线（governance/playground 零 adminapi 引用）维持。

## 34.2 架构边界 CI

V8.5 起 `architecture-test` 已落地为 `cmd/architecture-test`（`make check` 门禁，与前端检查串联），五类检查器：

| 检查器 | 输入 | 违规语义 |
|---|---|---|
| `CheckModules` | `go list` 包图 + `modules.yaml` | 无属主模块的 Go 包 |
| `CheckImports` | `go list` 包图 + `forbidden-imports.yaml`（24+ 条） | 命中禁则的 import 边 |
| `CheckTableOwnerModules` | `table-owners.yaml` + `modules.yaml` | 表 owner 不是已声明模块 |
| `CheckMigrationOwners` | `migrations/*.sql` 首行 `-- owner:` + `table-owners.yaml` | migration 建表 owner 与 manifest 不一致 |
| `CheckDirectSQL` ★ | 非持久层 Go 源码 `FROM/JOIN/INTO/UPDATE <已知表>` + `table-owners.yaml` | 跨模块直接 SQL 引用（持久层/contract-test 豁免） |

检查器自身有单测（`internal/architecture/check_test.go`），并采用**负向注入验证**：规则必须能抓住故意注入的违规，防止规则空转。

```text
architecture/
├─ modules.yaml           # module_path + 23 模块属主
├─ forbidden-imports.yaml # 依赖禁则（V8.5 起含服务分层禁则）
└─ table-owners.yaml      # 33 表 → owner 模块
```

模块化不是代码审查约定，而是 CI 可执行约束。后续规划项（未落地）：禁止新建无 `TenantScope` 的 Tenant Repository 方法、`pkg/` API breaking change 检测（随 `pkg/` 出现时启用）。

---

# 35. Integrations 与扩展机制

## 35.1 产品层：Integrations

Console 默认只暴露受支持的 Integration Catalog：

- Guardrail Provider Integration；
- Notification Channel；
- Observability / Analytics Sink；
- Secret Provider。

Provider Connector 属于 Gateway Models/Providers 域，不在 Integration Catalog 中重复建模。

Integration Catalog 的条目由 LiteAIG 版本内置元数据或管理员认可的签名目录提供，不支持普通 Tenant 用户上传执行代码。

## 35.2 Core 扩展点

后端只开放有限接口：

- Provider Connector；
- MCP Protocol Adapter；
- A2A Protocol Adapter；
- Guardrail Provider；
- Notification Sink；
- Analytics/Event Sink；
- Secret Provider。

不开放：

- 任意 Pipeline Stage；
- Stage 重排；
- 自定义 Budget/Accounting 核心逻辑；
- Extension 直接读取数据库内部表；
- Browser-side 插件执行；
- 任意 Wasm/JS 上传后直接挂载生产 Pipeline。

## 35.3 gRPC Extension Bridge（P2）

仅作为 Advanced Extension Runtime：

- System Admin 注册；
- mTLS；
- signed/allowlisted endpoint；
- capability manifest；
- request timeout；
- health check；
- tenant/system scope；
- circuit/failure isolation；
- full audit。

Extension 失效不得拖垮 Gateway 主进程；按对应 Integration Policy 的 fail-open/fail-closed 处理。

## 35.4 Provider Bridge 约束

LiteLLM Bridge 或其他 Provider Aggregator 以普通 Provider Connector 的形式存在，并遵循以下约束：

- 默认关闭，按 Tenant/System Provider 配置启用；
- 不向 Bridge 下放 Tenant API Key、Budget、Guardrail、RBAC 决策；
- Bridge 返回的 usage/error 必须 Normalize 为 LiteAIG Unified Usage / UpstreamError；
- LiteAIG 仍执行 Budget、Cache、Guardrail、Accounting 与 Request Explorer；
- Bridge 故障进入正常 Circuit/Fallback；
- Bridge 后的真实 Provider/Model 若可识别，应写入 `upstream_provider` / `upstream_model` 以便成本和故障分析；
- Bridge 不作为 P0-Core 的强依赖。

---

# 36. 实施阶段与 Definition of Done

## 36.0 开发任务优先级规则

从 v8.2 起继续执行“禁止平均建设所有模块”的原则。任何 Epic/Feature 进入开发前必须回答以下问题：

1. 是否直接服务于本章定义的七个真实验证场景之一？
2. 是否强化六个核心能力之一？
3. 该能力是否已有成熟生态可以集成而不是自研？
4. 是否有可量化的 DoD，而不是“页面存在/接口存在”？
5. 是否会扩大长期兼容、插件或运维负担？

任务按以下标签管理：

- `DIFFERENTIATOR`：六个核心能力及 Adaptive Governance，最高优先；
- `CORE`：数据面正确性、可靠性、安全基础；
- `INTEGRATION`：用成熟生态补齐非核心能力；
- `EXPERIMENT`：尚未证明价值，必须有验证期限；
- `DEFERRED`：不影响当前真实场景，暂缓；
- `DO_NOT_BUILD`：落入 §1.6 禁止范围。

开发资源优先级：`CORE correctness > DIFFERENTIATOR > INTEGRATION > EXPERIMENT > DEFERRED`。

## 36.1 六个必须持续回归的真实企业场景

### Scenario A — 多模型统一出口

模拟：10 个应用通过 LiteAIG 使用至少 OpenAI-compatible + Anthropic 两类协议和多个模型。

验收：

- 新应用 5 分钟内接入；
- 业务只依赖 Logical Model，不绑定 Provider Secret；
- 模型切换不改业务代码；
- 每次调用可追踪 Tenant/Project/Key/Model/Cost；
- Provider Bridge 不是完成该场景的强依赖。

### Scenario B — Provider 故障与成本优化

持续注入：429、timeout、5xx、slow TTFT、credential exhausted、circuit open。

验收：

- Retry/Fallback/Circuit 按策略执行；
- 无重复流式输出和明显 Budget 泄漏；
- Request Explorer 能解释每一次 fallback；
- Routing Simulator 能复现策略决策；
- 可量化 Retry/Fallback 带来的额外成本；
- Cost/Latency 策略切换效果可通过基准流量比较。

### Scenario C — 企业 FinOps 归因

模拟：3 个部门、20 个用户、5 个 Project、10 个 Agent/Application，包含人员跨项目使用和调岗历史。

验收：

- 1 分钟内查询任意部门/用户/项目/Agent 的 Token 与成本；
- Project×Department、Project×User 交叉分析准确；
- Attribution Trust 可区分 verified/key_bound/untrusted；
- 人员调岗不改写历史账期；
- Dashboard 与 Usage Ledger SQL 对账一致；
- Cache Savings、Retry Cost、Fallback Cost 可查询。

### Scenario D — 企业 AI 安全治理

持续运行：Prompt Injection、Jailbreak、PII、Secret、Stream Cross-chunk、Document Injection、Tool/MCP Injection 样本。

验收：

- Input/Output/Stream/Tool 检查点均能识别并执行正确 Action；
- Guardrail 规则可在 Playground/Test Case 中复现；
- Impact Preview 不使用未授权历史内容；
- Tighten/Loosen 发布规则正确；
- 安全事件可追踪到 Policy/Rule/Request；
- Guardrail 延迟、误拦截与 retroactive block 可度量；
- Tool ACL 不可被 Prompt Injection 绕过。

### Scenario E — Agent / Tool 治理与审计

模拟：3 个 Agent 分属不同 Project，通过多个 MCP Server 调用 Tool，混入越权调用、参数敏感数据、Prompt Injection 诱导调用和高风险 Tool。

验收：

- 越权 Tool Call 在 `TOOL_REQUEST` 阶段被拒绝且不触达上游；
- Tool Result 按不可信内容重新进入 Guardrail；
- Request Explorer 可按 session 重建 Agent→LLM→Tool→Result；
- Tool Call 正确归属 Tenant/Project/Agent/User，并记录 `tool_call_count/tool_cost`；
- Phase 4 时，高风险 Tool 未审批不得执行且审批链可追溯；
- 不依赖 Agent Runtime，可使用 Mock MCP Server 独立回归。

### Scenario F — 多智能体与跨组织 Federated Agent 协作治理

模拟：User→Internal Agent A→Internal Agent B / External Federated Agent C→Tool/Model 的多跳任务，至少两个不同 Runtime/Framework，并包含一个独立 Mock Partner Organization；通过 A2A 与 MCP 交互，混入 Delegation 越权、未经审核 External Capability、Data Residency 冲突、Agent Loop、Task/External Budget 超限、高风险 Agent Handoff，以及外部 Agent Inbound 调用场景。

验收：

- A2A Agent Card 可发现并按 trust policy 校验；
- Agent A 委托 Agent B 后 Effective Permission 不得超过 A 的原权限；
- Agent Call / Tool Call / Model Call 全部关联同一 Root Task；
- `max_agent_hops/max_total_cost/max_duration` 任一超限时，在下一次下游调用前阻断；
- Request Explorer 可重建 Agent Graph，并解释每条边为何允许/拒绝；
- Task-level Token/Cost 与所有子调用 Ledger 聚合一致；
- 高风险 Handoff/Action 经 Approval 后才执行，Grant 不可重放；
- External Agent 未建立 Active Verified Relationship 时不得调用；
- External Agent 的 Capability/Project Grant、Data Boundary 与 Procurement Budget 均被执行；
- External Response 以不可信 Provenance 重新过 Guardrail；
- External Agent Cost 既计入 Task Total，又可按供应商/Relationship 独立统计；
- Inbound External Agent 不得通过自报身份获得内部 User/Delegation 权限；
- Relationship Suspend/Revocation 能在 Security Epoch 收敛后阻断新调用；
- 跨 Region Task 按 `regional/global_soft/global_hard` 约定执行；
- 全场景不依赖 LiteAIG 自己运行 Agent Workflow。

### Scenario G — Production HA / DR

在持续真实/Mock AI 流量和 SSE Streaming 下依次注入：Gateway kill、CP 全断、PostgreSQL failover、Valkey failover、KMS outage、单 AZ 丢失、Rolling Upgrade、Accounting DB outage。

验收：

- CP/DB 故障时已加载 DP 使用 LKG 继续；
- 单 Gateway/AZ 故障不会导致整个 Region 无 Ready 实例；
- Config 不出现半版本；
- Provider 已产生的可计费 Usage 在 DB 恢复后通过 Spool 完整重放且无重复计费；
- Hard/Soft Budget 按配置执行故障语义；
- Secret Cache 只在 TTL/grace 内继续；
- Rolling Upgrade 先 Drain，已有 Stream 大比例正常完成；
- HA Dashboard 能解释当前 Degraded Dependency 和恢复事件；
- Enterprise DR Drill 能按 Runbook 完成流量切换、验证 Runtime、Reconcile Accounting/Budget。

七个场景作为跨版本 Golden Scenario，Phase 升级、核心重构和 Release Candidate 必须重新跑通。

## 36.2 跨 Phase Agentic Workstreams

V8.2 不新增 Phase 2.5 / 3.5，避免路线图碎片化。Agentic 通过三个跨 Phase Workstream 推进：

### Workstream A — Agent Registry & Versioning

- Phase 2：Agent Identity / Task ID plumbing；
- Phase 3：Agent Version / Endpoint / Capability 数据模型、成本归因准备；
- Phase 4：A2A Registry、Version Pin、Draining、Agent Card trust 完整闭环。

### Workstream B — Agentic Observability

- Phase 2：Tool Call Event / Session linkage；
- Phase 3：Task/RootAgent/AgentHop FinOps；
- Phase 4：Agent/Task Graph + OTel Span correlation；
- Phase 5：Graph analytics / anomaly recommendation。

不另建 `agentic_spans` 作为第二 Trace 事实源；优先复用 OTel Span + `agent_call_events/tool_call_events`。

### Workstream C — Collaboration Governance

- Phase 4：Delegation / Approval / Task Governance；
- Phase 5：Capability Routing；
- Team/Mesh 仅作为 P2 派生视图或统一 Agent Group 实验，不作为 Phase 4 GA 依赖。

Framework-specific LangGraph/CrewAI/AutoGen Adapter 不进入 Core；通过 A2A/MCP/HTTP 标准接入，并可在 `examples/` 提供集成示例。


## Phase 0 — P0-Core

后端：

- OpenAI Chat/Embeddings/Models；
- Anthropic Messages；
- OpenAI-Compatible；
- Virtual Key；
- Tenant/Project + Default Project；
- Tenant Runtime Snapshot；
- Provider/Credential Tenant Scope；
- Provider/Credential/Deployment/Logical Model；
- Priority/Weighted/RoundRobin；
- Timeout/Retry/basic Circuit/Fallback；
- Token usage；
- 单窗口 Budget；
- simple RPM；
- Fast Guard：size/keyword/regex/secret；
- Config Draft/Validate/Diff/Publish/Rollback；
- RuntimeBundle checksum + atomic activate；
- Local LKG basic；
- graceful shutdown basic。

Web Console：

- App Shell + Tenant/Project Scope；
- Setup Wizard；
- Dashboard basic；
- Projects；
- Models/Providers/Deployments；
- API Keys；
- Routing basic；
- Playground Live basic；
- Request Explorer history basic；
- Config Draft Bar + Diff Viewer basic；
- Audit basic。

DoD：

- 5 分钟首次调用；
- Playground 与 Request Explorer 同一 request_id 决策一致；
- Toggle 不绕过 Draft/Publish；
- Provider 契约通过；
- Stage Benchmark 通过；
- Control Plane 挂掉后已加载配置仍能转发；
- invalid RuntimeBundle 不替换 active LKG；
- Lighthouse Accessibility ≥90；
- **Scenario A 多模型统一出口通过。**

## Phase 1 — Production Hardening

后端：

- 多窗口 Reservation Ledger；
- Reservation Sweeper；
- ZSET Concurrency Lease；
- 完整 Circuit State Machine；
- Credential Pool；
- Config Drift；
- User/OrgUnit 基础模型 + Usage Attribution；
- Tenant RLS / Redis Namespace；
- OTel；
- Alert basic；
- Gateway Active-Active / readiness contract；
- Control Plane Active-Active + singleton job lease；
- Durable Accounting Spool；
- PostgreSQL/Valkey dependency degradation；
- graceful drain / streaming shutdown；
- Helm PDB / TopologySpread / HPA baseline；
- Secret cache TTL/grace。

Web Console：

- Request full Decision Timeline；
- Request Live Tail；
- Health & Circuits；
- Alert Inbox + Rule Builder basic；
- Organization basic；
- RBAC Scope Navigation；
- Config Rebase/Conflict；
- PWA Dashboard/Alert/Request summary；
- HA & Resilience basic。

DoD：

- Redis/Postgres/Provider Chaos 通过；
- 24h soak 无持续 Reservation/Lease 泄漏；
- Hard Budget 原子性通过；
- Tenant 切换无 Query Cache/SSE 泄漏；
- Live Tail payload 无 Prompt/Response Body；
- Accounting DB outage 后 Spool replay 对账一致；
- Gateway rolling restart 不造成全局 NotReady；
- **Scenario B 故障注入基础版通过；**
- **Scenario G 单节点/CP/DB/Redis/Accounting 基础 HA 通过。**

## Phase 2 — P0-Commercial Guardrail + Cache

后端：

- Exact Cache；
- Cache Policy；
- basic PII；
- Prompt Injection basic；
- input/output Guardrail；
- Streaming Layer1/Layer2；
- External Guardrail API；
- Security Event；
- Guardrail Policy Fast Publish；
- Agentic Governance 基础版：Agent Identity、MCP 2026-07-28 Registry/Discovery、Tool ACL、Schema/DLP、Content Provenance、Tool Call Event、Task ID plumbing。

Web Console：

- Guardrail Policy Editor；
- Policy Inheritance；
- Guardrail Test Cases；
- Guardrail-only Playground；
- Impact Preview（仅显式可用样本）；
- Security Events；
- Cache Overview/Policies/Savings；
- Integrations basic（Guardrail/Notification）；
- Security Epoch convergence view；
- MCP Servers / Tool Catalog / Tool Policy / Tool Calls basic。

DoD：

- Cache hit 仍 100% 经过 Output Guardrail；
- Stream cross-chunk corpus 通过；
- Layer1/2 TTFT regression ≤20%；
- External Guardrail timeout/fail mode 可观测；
- 未保存 Request Content 时 Impact Preview 不伪造历史覆盖率；
- Tighten/Loosen UI 流程符合安全发布规则；
- **Scenario D 安全治理基础版通过；**
- **Scenario E 的越权拦截、Tool Result Guardrail、归属审计三项通过。**

## Phase 3 — Smart Routing + FinOps

后端：

- latency/cost/load/cache-aware score；
- Routing Simulator；
- Pricing Version；
- Chargeback；
- multi-currency；
- cost anomaly；
- OrgUnit/User/Application/Agent/Task 多维 FinOps；
- Root Task / Parent Task / Agent Hop Usage aggregation；
- Agent Version / Endpoint / Capability 数据模型与 Snapshot 编译索引；
- Data Residency strict；
- cache savings；
- Adaptive Governance Recommendation backend（routing/guardrail，suggest-only）；
- Guardrail Benchmark Harness / Evidence Export basic。

Web Console：

- Routing Simulator full explain；
- Route score/dynamic metrics；
- Usage & Cost full；
- Project×Department Cross Analysis；
- Task / Root Agent / Agent Hop Cost；
- Department/User Ranking；
- Attribution Trust；
- Budget/Chargeback；
- Cost Anomaly；
- Pricing；
- Optimization Recommendations（Accept as Draft / Dismiss）；
- Guardrail Benchmark / Evidence Export entry。

DoD：

- Simulator 与生产 PlanRoute 可复现一致；
- Pricing golden 100% 通过；
- Budget overrun 达成 SLO；
- Data Residency strict 不能绕过；
- Department/User 统计与 Ledger SQL 对账一致；
- Browser 不聚合 Usage 明细；
- **Scenario C 企业 FinOps 归因完整通过；**
- **Scenario B 成本/延迟优化版通过；**
- Recommendation 不得自动改变生产配置，接受建议后必须生成 Config Draft；
- Guardrail Benchmark Report 可复现，Evidence Export 默认不含正文/Secret。

## Phase 4 — Enterprise / Agentic

后端：

- Semantic Cache；
- OIDC/SAML/SCIM；
- ClickHouse Analytics Sink；
- A2A 1.0 Adapter / Agent Card Discovery & Signature；
- Agent Registry / Agent Versioning / Endpoint / Capability；
- External/Federated Agent Relationship / Trust Anchor / Project & Capability Grant；
- Inbound/Outbound Federation Identity Resolution；
- External Data Boundary / Procurement Cost & Budget；
- Agent Card Material Change Review / Trust Rotation / Revocation；
- Version Pin / Draining / Rollback semantics；
- Delegation Grant / Agent ACL；
- Task Governance / Agent Hop / Loop Control；
- MCP/Tool Governance advanced；
- Human Approval Handoff；
- Tool ACL / Task Adherence；
- Groundedness；
- LLM-as-Judge；
- advanced DLP；
- approval workflow；
- gRPC Extension Bridge(P2)；
- Multi-AZ Enterprise HA；
- Region DR RuntimeBundle replication / Runbook；
- Global Soft Budget Slice（可选）；
- DR backup/restore automation。

Web Console：

- Semantic Cache；
- Agents & Tools；
- Federated Agents / Trust Relationships；
- External Agent Review / Card Diff / Trust Anchors；
- Agent / Task Graph；
- Delegations / Approvals；
- Compliance；
- Identity Sync；
- Dual Approval；
- Platform Tenants；
- Shared Providers；
- Advanced Integration Runtime；
- HA & Resilience full / DR Drill view。

DoD：

- Semantic Cache tenant isolation 渗透测试通过；
- Tool ACL 无法通过 Prompt Injection 绕过；
- Enterprise HA/rolling upgrade 验证通过；
- Single-AZ loss / PostgreSQL failover / Valkey failover / CP outage 通过；
- Region DR Runbook 至少完成一次演练；
- High/Critical action approval/re-auth E2E 通过；
- Cross-tenant system view 权限隔离通过；
- **Scenario D Advanced Guard 完整通过；**
- **Scenario E Agent/Tool Governance 完整通过；**
- Federated Agent 未验证关系、越权 Capability、未知 Strict Residency、Trust Revocation E2E 通过；
- External Agent Task Cost / Procurement Budget 对账通过；
- **Scenario F 多智能体与跨组织 Federated Agent 协作治理基础闭环通过；**
- **Scenario G Enterprise Multi-AZ/DR 通过；**
- 七个 Golden Scenario 在 Enterprise HA/rolling upgrade 与 DR Drill 后再次通过。

---

## Phase 5 — Agentic Optimization（P2，非初始 GA 门槛）

后端：

- Capability-based Agent Endpoint Routing；
- Agent Health / Circuit / Cost / Latency-aware Score；
- Task-level Adaptive Governance Recommendation；
- Agent anomaly / loop pattern recommendation；
- Agent Reputation/Quality 仅作为可解释输入，不自动获得高权限；
- Cross-Agent Graph analytics；
- Team/Mesh 派生视图 / Agent Group 实验（非 Core）；
- A2A/MCP 新协议版本兼容矩阵。

Web Console：

- Agent Routing Simulator；
- Agent Capability Explorer；
- Task Cost Breakdown；
- Agent Graph Compare；
- Agentic Optimization Recommendations。

DoD：

- Capability Router 与生产 Agent Route 使用同一 deterministic plan；
- 系统建议只能生成 Config Draft，不自动修改 Agent ACL/Delegation/Budget；
- Agent Quality/Cost/Latency 的任何评分都有证据窗口和解释；
- Scenario F 在跨框架、跨 Runtime 条件下稳定通过。

Phase 5 不允许演化为 Agent Planner/Workflow Runtime。

---

# 37. 最终冻结决策

1. Go 为主实现；
2. 默认单二进制；
3. Control/Data Plane 可合可分；
4. 固定七阶段 Pipeline，不开放任意重排；
5. Provider / Guardrail / Sink 为有限扩展点；
6. **Tenant 是最高业务、数据、安全与计费隔离边界**；
7. **Project 是 Tenant 内 AI 资源治理边界，数据库始终存在；单团队默认自动创建 Default Project**；
8. **OrgUnit/User 是组织与成本归属维度，不嵌入 Project 层级**；
9. **Resource Axis 与 Identity/Cost Axis 正交，并在 Usage Ledger 汇合**；
10. Runtime 采用 GlobalRuntime + TenantRuntimeSnapshot；Tenant 配置变更只替换本 Tenant Snapshot；
11. API Key 格式包含随机 `tenant_ref`，Data Plane 可 O(1) 定位 Tenant Runtime；
12. Provider/Credential 支持 `SYSTEM_SHARED` 与 `TENANT_PRIVATE`，Tenant 私有 Secret 严禁跨租户；
13. Standard/Enterprise Tenant 表默认 Repository TenantScope + PostgreSQL RLS 双层隔离；
14. Redis/Valkey、Cache、Vector、Analytics、Object Storage 必须 Tenant namespace 隔离；
15. 用户身份是全局 Identity，权限通过 Tenant/Project Membership/Role Assignment 建立；
16. 人员组织关系保留有效期，Usage Event 快照调用时 `org_unit_id/org_path/cost_center`，调岗不得改变历史账目；
17. 正式 User/Department Chargeback 只接受可信身份归属；客户端自报 `end_user` 不能作为安全或财务身份；
18. Guardrail Engine 是一级能力；
19. Streaming Guardrail 固定采用 Inline Fast / Buffered Local / Async Shadow 三层模型；
20. External Guardrail 不允许默认逐 chunk 同步调用；
21. Exact Cache P0-Commercial，Semantic Cache P1；
22. Cache Hit 必须执行当前 Output Guardrail；
23. Token / Cost / Budget / Rate Limit 是商用核心能力；
24. FinOps 必须支持 Tenant/Project/Application/Agent/API Key/OrgUnit/User/Model/Provider 多维统计；
25. 多窗口 Budget 使用 Redis/Valkey 原子 Reservation Ledger；
26. Reservation Sweeper 正确性依赖 Redis Ledger，不依赖异步 PostgreSQL 记录；
27. Concurrency 使用 ZSET Lease + TTL/renew，不使用 Hash 全扫描；
28. API Key 使用高熵 Secret + HMAC-SHA256；
29. Pepper 必须版本化；Key 迁移必须真正旋转客户端 Key；
30. Smart Routing 先 hard constraints，再 soft score；归一化必须设置 noise floor；
31. Guardrail Retry/Fallback 有独立 regeneration budget，并受总 Provider Call 上限约束；
32. ASYNC_EVAL 只能事后告警/通知，不能宣称可撤回已发送内容；
33. Data Residency 是路由硬约束且必须有对应数据模型；
34. Web Console + Responsive PWA 为统一管理端，不优先开发原生移动 App；
35. Web Console 固定采用分组信息架构，不使用 13+ 平铺一级导航；
36. 所有生产配置修改进入服务端 Config Draft/ChangeSet；普通 Toggle 不允许绕过 Validate/Diff/Publish；
37. Operational Action 与 Configuration Change 必须分离，按 Low/Medium/High/Critical 风险执行确认/Re-auth/Approval；
38. Project Center 是 Project 资源治理的 360° 聚合入口；跨域页面仍保留用于全局分析；
39. Playground 使用短期 Test Principal，复用生产 Pipeline，不要求读取真实 API Key Secret；
40. Playground Provider Cost 必须真实记账，`source=playground`；Customer Charge 是否纳入正式 Chargeback 由 Tenant Policy 决定；
41. Guardrail Impact Preview 只使用显式 Saved Case、replay-eligible retained sample 或临时样本；默认不假设历史 Prompt 可用；
42. Request Live Tail 只推摘要并由服务端强制 Tenant/RBAC Scope，不推 Prompt/Response Body；
43. Integration Catalog 只暴露有限受支持集成，不演化为任意 Wasm/JS 插件市场；
44. Provider Connector 归 Models/Providers 域，不在 Integrations 中重复管理；
45. Config Diff 默认语义化展示，Secret 值不进入 Diff；
46. Frontend 默认 Theme 跟随系统，支持 Light/Dark；状态不只依赖颜色表达；
47. PWA 只缓存静态资源，不离线缓存 Admin API、Prompt、Response、Secret、Usage 明细；
48. Console access/refresh token 不存 localStorage；Secret 不进入 Query Cache、日志和错误上报；
49. Frontend 采用 React + TypeScript + Vite + Ant Design/ProComponents + TanStack Query + ECharts + SSE，继续保持单前端、非微前端；
50. 数据密集页面使用服务端过滤/分页/聚合，浏览器不扫描 Usage Ledger；
51. Agentic Governance P1/P2，不做 Agent Runtime/Tool Runtime/Planner；
52. PostgreSQL 是 Standard 元数据主库，Valkey/Redis 是分布式协调层；
53. Analytics Store 可插拔但不是最小依赖；
54. 配置必须版本化、可验证、可回滚、可审计；
55. Guardrail 安全策略维护 Tenant Security Epoch；
56. 任何 External Guardrail / Semantic Cache / Provider 网络延迟必须单独观测，不混入 Core Overhead；
57. P0-Core 与 P0-Commercial 分阶段交付；
58. Phase N 验收未通过，不进入 Phase N+1 的生产范围；
59. **产品定位冻结为 Enterprise AI Governance & Traffic Control Plane，不转型为通用 API Gateway；**
60. **Provider 数量、插件数量不是核心 KPI；协议兼容率、接入成功率、治理闭环和真实场景效果才是 KPI；**
61. **六个优先产品能力固定为 AI Access、Explainable Smart Routing、Enterprise AI FinOps、Guardrail Lifecycle、AI Operations、Agentic Governance & Audit；**
62. **长尾 Provider 优先 OpenAI-Compatible / LiteLLM Bridge，不建立 100+ 原生适配器维护负担；**
63. **企业已有 Higress/Envoy/Nginx/API Gateway 时优先共存，不要求 LiteAIG 替代网络网关；**
64. **Prometheus/OTel/APM/Log/IdP/KMS/第三方 Guardrail 等成熟基础设施优先集成，不重复实现其核心系统；**
65. **任何新增一级能力必须证明服务至少一个 Golden Scenario 或核心产品能力，否则默认进入 DEFERRED；**
66. **任何核心决策（Routing/Guardrail/Retry/Fallback/Budget）必须可在 Request Explorer 中解释或审计；**
67. **产品发布标准以 Scenario A/B/C/D/E/F/G 端到端验收为核心，不以功能清单完成率作为发布依据；**
68. **V8.2 继续将 Agentic Governance 定义为现有 Gateway Governance 的对象扩展，而不是新建 Agent Runtime；除该扩展外原则上不再新增新的大能力域；**
69. **Agentic Governance 是核心差异化能力，产品语义不绑定 MCP/A2A；MCP 是 Agent→Tool 首选 Adapter，A2A 是 Agent→Agent 首选 Adapter；**
70. **Trust & Assurance 以可复现 Benchmark、Evidence Export、数据处理透明和审计证据为产品交付；SOC 2/ISO 等认证作为独立合规项目，不作为单个代码 Feature 的 DoD；**
71. **Adaptive Governance 只能生成 Recommendation；任何建议必须经人工接受并转成 Config Draft 后走 Validate/Diff/Publish，禁止自动改写生产配置；**
72. **Adaptive Governance 严格 Tenant 隔离，不跨租户学习；Guardrail 参数建议没有人工 Review 或 replay-eligible 证据时不得生成可执行 Patch；**
73. **Model / Tool / Agent / Human Approval 是四类统一 Interaction；Core Engine 依赖规范化 Interaction，不依赖协议 Wire Type；**
74. **固定七阶段 Pipeline 不因 A2A/MCP 增加新 Stage；Agentic 逻辑必须映射到现有 Admission/Guardrail/Preflight/Resolution/Execution/Accounting；**
75. **Agent 是一等 Principal；每个生产 Agent 必须有稳定 Identity、Tenant/Project Scope、Owner/Service Account 与风险级别；**
76. **Delegation 权限采用交集语义，任何 Agent Handoff 不允许权限放大，Grant 必须有 TTL/Scope/Task/Audience；**
77. **Task 是跨 Model/Tool/Agent 调用的预算、成本、审计和生命周期聚合单元；Usage Ledger 必须支持 Task/RootTask/RootAgent/AgentHop；**
78. **Task Governance 至少支持 max_agent_hops/max_model_calls/max_tool_calls/max_agent_calls/max_tokens/max_cost/max_duration 与确定性 Loop Detection；**
79. **A2A 1.0.0 作为首个 Agent-to-Agent 标准 Adapter，必须支持 Agent Card trust、Task/Message/Artifact、Streaming/Push 的治理与审计；**
80. **MCP 新实现以 2026-07-28 stateless core 为基线，Legacy SSE 仅兼容；Mcp-Method/Mcp-Name 可用于路由/授权，但 Header 与 Body 必须一致校验；**
81. **Capability Routing 只在 Orchestrator 已决定需要某类 Agent 能力之后选择合规 Endpoint，不承担任务规划；**
82. **Human Approval 是独立 Interaction；Approval Grant 一次性、短 TTL、绑定 Task/Action，不能扩大原 Delegation Scope；**
83. **Agent Graph 从 Request/AgentCall/ToolCall/Task Ledger 构建，不依赖保存 Prompt/Response 正文；**
84. **Phase 5 Agentic Optimization 不是初始 GA 门槛，且不得演化为 Agent Planner、Workflow、Memory 或 Multi-Agent Team Runtime。**
85. **V8.2 延续 Modular Monolith；模块边界是代码/数据所有权边界，不默认拆成微服务，单二进制仍为首选部署。**
86. **Kernel 只拥有 Pipeline/Runtime/Interaction/Contracts；Kernel 不拥有业务表，不依赖 Provider SDK、MCP/A2A Wire Type 或 Control Plane。**
87. **Protocol Adapter 只做协议归一化，不允许直接调用 Guardrail/Budget/Routing 等业务实现；所有治理必须经过固定 Pipeline。**
88. **Model/Tool/Agent 上游调用通过 Connector Contract；Connector 负责“怎么调用”，不负责“能不能调用”，不得绕过 Policy/Accounting。**
89. **Identity/Principal 只有一个 Canonical Owner；Agent 是 Principal 类型，不建立第二套 agentic identity。**
90. **Capability Registry 是 Runtime 编译索引而不是事实源；事实源属于 Model/Tool/Agent Resource Manifest。**
91. **Agent Identity 与 Agent Version 分离；Task/Agent Call 可以固定已解析版本，版本支持 active/draining/retired，回滚创建新版本。**
92. **Team/Mesh 在获得真实客户需求前不成为 Core 一级对象；优先使用 Agent Group、Project/Policy Scope 和 Task/Delegation Graph 表达。**
93. **Observability 通过 Domain Event/OTel Contract 横切接入，禁止所有模块直接依赖 Observability concrete implementation。**
94. **One Table, One Owner；跨模块禁止直接修改其他模块持久化表，表所有权由 architecture manifest 与 CI 检查。**
95. **V8.2 不新增 Phase 2.5/3.5；Agentic Foundation/Observability 采用跨 Phase Workstream，避免版本路线图碎片化。**
96. **不在 Core 中维护 LangGraph/CrewAI/AutoGen 等框架专有 Runtime Adapter；框架通过 A2A/MCP/HTTP 接入，框架示例可独立维护。**
97. **Agentic anomaly 的精确率/误报率必须基于版本化 Benchmark Corpus 后再设 Release Gate，不预先承诺无数据支撑的固定 95%/5%。**
98. **架构边界必须由 CI import-graph / table-owner / public-contract 检查执行，而不是仅依赖文档约定。**
99. **Production HA 是 CORE correctness，不作为独立插件或可绕过的外围能力；Standard/Enterprise 必须按明确 HA 基线交付。**
100. **Data Plane 默认 Active-Active / Shared-nothing，无 Gateway Leader；单实例故障不得要求主从接管协议。**
101. **Control Plane / PostgreSQL 不可用不得清空 Data Plane 已加载 Runtime；Data Plane Ready 不以 Control Plane/Metadata DB 可达作为默认条件。**
102. **所有运行配置以不可变 Signed RuntimeBundle 原子 Prepare/Activate；任何节点 NACK 必须继续运行 Last Known Good，不允许半配置。**
103. **Data Plane 本地维护 active/previous Last Known Good；CP 不可达时允许从签名有效 LKG 启动，LKG 不得绕过 suspended/security fail-closed 约束。**
104. **Provider 已产生可计费 Usage 后，数据库写失败必须进入 Durable Accounting Spool；采用 At-least-once + event_id 幂等，不宣称虚假的 Exactly Once。**
105. **Redis/Valkey 不设置一个全局 fail mode；Cache、PubSub、Rate、Budget、Concurrency、Reservation、Job Lease 必须分别定义故障语义。**
106. **Hard Budget/强制安全策略在其必要协调依赖失效时默认 fail-closed；Soft Budget/可重建缓存可 fail-open/bypass，但必须 Alert。**
107. **KMS/Vault 不进入每请求同步热路径；缓存 Credential 只能在 TTL/stale_grace 内继续使用，禁止无限期使用过期 Secret。**
108. **Rolling Upgrade 必须 Streaming-aware Graceful Drain；readyz 先摘新流量，再等待已有请求/Stream，最后 finalize Accounting 后退出。**
109. **Standard/Enterprise Helm 必须支持 PDB、TopologySpread、anti-affinity、HPA、probes 和 graceful termination；HPA 不能只以 CPU 为唯一尺度。**
110. **Standard HA 至少验证多 Gateway + Control Plane 冗余；Enterprise 以 Multi-AZ 为基础，Multi-Region/DR 不得替代 Region 内 HA。**
111. **跨 Region Budget 必须显式选择 Regional / Global Soft / Global Hard；默认禁止用跨 Region Redis 热路径伪装强一致。**
112. **Tenant/Project/Principal 需要 Noisy Neighbor 容量隔离；单 Tenant 不得耗尽全局并发、Stream、Spool 或协调资源。**
113. **PostgreSQL/Valkey 复制与 Failover 优先集成成熟托管/开源 HA 方案，LiteAIG 不自研数据库一致性协议。**
114. **数据库 Migration 固定采用 Expand→Migrate/Dual Read-Write→Contract；滚动升级期间旧/新实例必须 Schema 兼容。**
115. **HA/DR SLO、RTO、RPO 只能在真实部署和 Chaos/DR Drill 验证后对外承诺；架构目标不得直接包装为已达成 SLA。**
116. **Scenario G Production HA/DR 纳入跨版本 Golden Scenario；Release Candidate 必须验证 LKG、Spool Replay、Dependency Degradation、Drain 和核心 Failover。**
117. **Agent 资源显式区分 `INTERNAL / EXTERNAL_FEDERATED`；External Agent 是 Tenant 内受控投影，不代表 LiteAIG 拥有或运行该 Agent。**
118. **External/Federated 不新增第五种 InteractionKind；仍使用 `InteractionAgent`，通过 `TrustBoundary/Direction/FederationContext` 表达跨组织边界。**
119. **Discovery 与 Trust 永久分离；Agent Card/Registry Discovery 只能创建 Candidate，不能自动产生可调用权限。**
120. **Active External Relationship 必须至少有一个有效 Verified Trust Anchor；A2A JWS 是可选 Trust 方法之一，不是唯一方法，允许 mTLS/OIDC/Trusted Registry 等企业验证方式。**
121. **外部 Agent 的远端 Policy 不参与本地 DelegateePolicy 交集；本地授权采用 Caller/Delegation + Federation Relationship + Project/Capability/Data Boundary 的交集。**
122. **Inbound External Agent 不得通过 Payload 自报内部 User/Tenant/Delegation 获得权限；必须通过受信任 Transport/Auth 映射为 Federated Principal。**
123. **External Agent 成本必须计入 Task 总成本，同时进入独立 Procurement Budget/Cost 维度；不得通过“外部预算独立”绕过 Task `max_total_cost`。**
124. **External Agent 定价未知时不得宣称 Hard Cost Budget 可严格执行；Hard Policy 默认拒绝 unknown-cost 调用或要求显式风险例外。**
125. **External Agent Response 使用独立 `external_agent_response` Provenance，并按不可信内容经过 AGENT_RESPONSE/Context Guard。**
126. **External Agent Data Boundary 为 Routing/Preflight Hard Constraint；Strict Project 对 unknown/incompatible processing boundary 默认拒绝。**
127. **Federation Relationship suspend/revoke 属于 Security Tighten，必须增加 Security Epoch；Strict 节点不得用旧 LKG 绕过已知本地 Revocation。**
128. **Task Counter 跨 Region 显式支持 regional/global_soft/global_hard；复用统一 Counter/Quota Authority 基础设施，不为 Task 自研第二套 Consensus。**
129. **Regional Task 只有一个 Home Region Authority；Global Soft 允许有界超发并 Reconcile；Global Hard 接受跨 Region RTT 换取强上限。**
130. **外部 Agent 非幂等调用默认不 Retry/Fallback；只有同一 Approved Relationship/Version 且操作明确幂等时才允许受控重试。**
131. **Agent Card/Trust 依赖故障时默认向更保守方向降级：现有 verified cache 可在 TTL 内继续，新建/续期/Material Change 不得 fail-open。**
132. **`external_agent.review`、`approval.decide`、`delegation.grant`、Agent Version Publish 与 Trust Revoke 必须在 RBAC 显式建模；`config.publish` 不得作为笼统替代权限。**
133. **High/Critical Approval 默认执行 requester/approver separation of duties；Enterprise 可要求 Dual Approval。**
134. **Scenario F 必须覆盖至少一个跨组织 Federated Agent 的 Outbound + Inbound、Trust、Residency、Cost、Revocation 与跨 Region Task 语义。**
135. **EXTERNAL_FEDERATED Agent 默认是 Tenant 级信任资源，不归属单一 Project；Project 使用权限由独立 Project/Capability Grant 表达。**
136. **Usage/Request/Agent Call Ledger 必须显式记录 Federation Relationship、Trust Boundary 与 Direction，保证跨组织成本、数据流和事故可追溯。**
137. **Project 可声明 `external_data_assurance_min`；Strict Residency 不仅校验 Region，还必须满足最低外部数据处理 Assurance。**
138. **Federated Agent Material Change 必须生成 Pending Review；Endpoint/Auth/Capability/Publisher/Trust/Data Boundary/Pricing 的实质变化不得自动覆盖 Approved Version。**
139. **Bidirectional Federation 的 Project/Capability Grant 必须显式区分 inbound/outbound；任何一个方向的授权不得隐式产生反向权限。**
140. **Strict Federated Traffic 必须有 Security Epoch freshness 上限；控制面隔离超过阈值时优先停止新 Federated Traffic，而不是无限期相信旧 LKG。**

V8.5 服务层收口（141–153）：

141. **不引入 Echo 等第三方 Web 框架**；HTTP 服务统一构建在 `internal/platform/webkit` 微内核上（Go 1.22 ServeMux + 洋葱中间件 + APIError + Recover + 安全头），内核对业务包零依赖。
142. **Admin API / Gateway 服务实现固定为三层**：表示层 `adminapi`（薄 handler + 会话/rbac/错误渲染）、业务编排层 `controlplane/backend`（ControlBackend + 视图契约）、持久层 `platform/storage`（sqlrepo + 纯适配器）；依赖只能自上而下，装配根 `app` 除外。
143. **`Backend` God Interface 拆分为 12 个窄能力接口**（Setup/Project/Dashboard/Key/Config/Runtime/Playground/Request/Alert/Security/Federation/Identity）；`Backend` 保留为组合超集用于渐进迁移；新路由接线只依赖最窄能力。
144. **Admin API 服务签名禁止 `any` 返回**：一律具体视图类型（`ProjectSummary`、`DraftSummary`、`MeView`、`WizardSetupResponse`、`[]config.Version` …）；JSON 线格式契约由 e2e management-loop 回归守护。
145. **领域类型归属冻结**：localauth/pepper → `identity`；AuditRecord/Repository → `observability/audit`；Resource Catalog 视图 → `catalog`；Tenant 窄查询 → `tenancy.TenantLister`；表示层以类型别名兼容既有引用，领域包禁止 import 表示层。
146. **`sqlrepo.Store` 是仓库唯一构造点**；`sqlite` 包退化为纯连接适配器（Open + WAL/busy_timeout），不再暴露 `New*Repository`；`postgres` 仅连接 + RLS，仓库实现委托 sqlrepo。
147. **Key Secret 加密存储**（`api_keys.key_ciphertext`，owner=identity）仅用于 Console Reveal / 一次性完整展示；数据面验证仍走 HMAC digest 路径，不依赖明文或密文。
148. **Key 创建/吊销/轮换即时生效**：触发 Tenant Runtime 即时重编译并原子替换快照，不产生新配置版本；迁移前历史 Key 无密文时 Reveal 不可用，走 Rotation。
149. **HTTP 错误契约冻结**：Admin `{"error":{"code","params"}}`（code 稳定枚举）、Gateway OpenAI 风格 `{"error":{"message","type":"liteaig_error","code"}}`、Drain 固定 503 `{"error":{"code":"DRAINING","message":"server is draining"}}`；错误渲染唯一入口在表示层，handler 不手写错误 JSON。
150. **表所有权三重 CI 校验**：migration `-- owner:` 一致性、owner 模块存在性、非持久层直接 SQL 跨模块引用（`CheckDirectSQL`）；`table-owners.yaml` 是表属主唯一事实源。
151. **架构守卫纳入 `make check` 门禁**：五类检查器（import graph / module owner / table owner / migration owner / direct SQL）+ 规则负向注入验证；检查失败即 CI 失败，不允许“下版本再修”。
152. **Admin API 前缀实施基线为 `/api/admin`**（Lite 单租户 Profile）；`/admin/api/v1` 作为多租户 System API 扩展预留前缀，两者契约在 §27 分别描述。
153. **部署/运维基线**：单二进制 `liteaig --mode all`（readyz/admin/gateway 三端口）；`setsid` 守护 + PID 文件；进程替换必须“先 kill 确认消失再启动”，禁止后台化竞态；Playwright management-loop e2e + 冒烟契约（SPA 指纹 / 401 / 400 / setup-status）为每次部署的发布门禁。

---

# 38. 后续开发任务拆解原则

## 38.1 Epic 必须围绕用户结果命名

禁止使用纯模块型 Epic：

```text
“实现 Routing Engine”
“实现 FinOps 页面”
“实现 Guardrail”
```

推荐：

```text
“Provider 故障时在不重复流式输出的前提下完成自动切换并解释原因”
“让财务管理员按部门/人员/项目核对 AI 月度成本”
“让安全管理员在发布 Prompt Injection 规则前评估误拦截影响”
```

每个 Epic 再拆成 Backend / Data / UI / Test / Observability 子任务，保证端到端闭环。

## 38.2 每个功能的 Definition of Done

至少同时包含：

1. **Functional**：功能正确；
2. **Failure Mode**：依赖失败时行为明确；
3. **Tenant Isolation**：无跨租户泄漏；
4. **Accounting**：Token/Cost/Usage 是否正确；
5. **Security**：是否改变 Guardrail/RBAC 边界；
6. **Explainability**：Request Explorer/Audit 能否解释；
7. **Observability**：Metrics/Trace/Event 可观测；
8. **Performance**：不突破对应 SLO；
9. **UX**：Console 能否完成配置/验证/回滚；
10. **Golden Scenario**：至少映射一个 Scenario A/B/C/D/E/F/G 或明确说明为何不需要。

## 38.3 版本节奏

后续版本优先使用实现迭代号，而不是持续进行大范围架构改版：

```text
v8.2.x   Federated trust / HA hardening / compatibility / production validation
v8.3–v8.4 实现迭代：WebKit 微内核落地、adminapi/gateway 全量迁移、
          持久层统一（sqlrepo.Store）、领域类型下沉、虚拟 Key 加密与即时生效
v8.5     服务层收口基线（本版本）：三层服务架构 + 窄能力接口 + 类型化契约
          + 可执行架构守卫（architecture manifest + cmd/architecture-test）
v8.6+    仅当出现经过生产验证、确需跨模块变更的新需求
```

原则：真实运行数据优先于文档推演。新架构决策必须由生产问题、客户场景或明确性能瓶颈驱动。V8.5 的服务层收口由“表示层/业务层/持久层互相引用导致变更半径失控”的真实工程问题驱动，并已全部通过 CI 固化，后续不再允许回退。


---

# 39. 协议基线与实现参考

V8.2 Agentic Adapter 以以下公开规范为实现基线，核心数据模型不直接复制协议对象：

- MCP `2026-07-28`：https://blog.modelcontextprotocol.io/posts/2026-07-28/
- A2A Protocol `1.0.0`：https://a2a-protocol.org/dev/specification/

实现约束：

- A2A 1.0 Agent Card 可以使用 JWS Signature；协议层的“可选”不等于企业 Federation Trust 可以无验证；
- LiteAIG 外部关系的安全不变量是“至少一个 Verified Trust Anchor”，而不是“所有 External Agent 必须有 JWS”；
- Agent Card 缓存遵守 HTTP cache/ETag 语义，但 Relationship/Trust Anchor 的本地 revoke 优先级高于缓存。

版本升级策略：Adapter 必须有独立 conformance/contract suite；协议 breaking change 通过 Adapter 版本兼容层吸收，不要求修改 Tenant/Policy/Usage/Task 核心数据模型。
