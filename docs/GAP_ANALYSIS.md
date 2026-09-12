# LiteAIG 企业特性差距分析（Gap Analysis）

> 版本：1.0
> 日期：2026-09
> 方法：以 `docs/ROADMAP_V86_COMPLIANCE.md` 的 Stage 7–15 逐项核对当前代码库（grep/file:line 佐证），再对企业级能力面做一次补充扫描，输出「未闭环」与「值得推进」清单。

## 一、总览

| 批次 | 主题 | 状态 | 备注 |
|---|---|---|---|
| Stage 7 | 函数调用 / 结构化输出 / Tool 治理 | ✅ 已实现 | `interaction.ToolCalls/ToolChoice/ResponseFormat`、OpenAI `tool_calls`、路由 `tools_not_supported` |
| Stage 8 | Token 计量 / TPM / Context Guard | ✅ 已实现 | `tokenizer/registry`、`EstimationMethod`、`usage_events` 缓存/工具/推理列、`--gateway-tpm` |
| Stage 9 | 流式三级护栏接线 | ✅ 已实现 | `pipeline.nFor` 装配 Inline/Buffered/Shadow；`stream_guardrail_e2e_test.go` |
| Stage 10 | 实时路由指标与健康 | ✅ 已实现 | `routingMetrics` + `recordRoutingMetrics` 滚动窗口进 `routing.Input` |
| Stage 11 | RuntimeBundle 投递闭环 | ✅ 已实现 | `bundlesync.go`、`split_mode_e2e_test.go`、LKG 兜底 |
| Stage 12 | Standard 档协调 | ✅ 已实现 | `coordination_leases` 单例租约、`--coordinator redis://`、双网关 exactly-once 冒烟 |
| Stage 13 | 私有云 Provider + Prompt-Cache 感知 | ✅ 已实现 | 连接器齐（Bedrock/Vertex/Azure）；Anthropic 出站 `cache_control` 透传；OpenAI/Anthropic 缓存 usage 计量 |
| Stage 14 | 企业证据与事件化 | ✅ 已实现 | Evidence Export、Webhook、OTLP traces、Prometheus |
| Stage 15 | `/v1/responses` 与多模态 | ✅ 已实现 | `POST /v1/responses`、content parts（text/image） |

**结论：路线图 Stage 7–15 主体全部落地；真正未闭环的是「明确延期项」与「已实现区域内的半截」两类。**

**2026-09-08 更新**：B1（Prompt-Cache 出站 `cache_control` 注入）已通过 connector 级测试闭环；B2（A2A 1.0 `SendStreamingMessage` 官方 SDK streaming parser 互通断言）已闭环；B5（Delegation grant/revoke API + Console）已闭环；B6（OpenAI/Anthropic 官方 SDK 互通 harness）已闭环。B4（Standard chaos/soak CI 常态化）已由 `ci.yml` 的 Postgres/Redis-backed `quality` job 覆盖并通过目标测试。

**2026-09-09 更新**：B3（真实 K8s 多副本 Standard 集群 e2e）已本地全链路跑通。修复过程中同步合入三个 Postgres-only 缺陷与一个 HA 收敛机制：(1) setup 向导写 `config_drafts.created_by/updated_by` 与 publish actor 用非 UUID 的 `"setup"`，违反 `005_controlplane.sql` 的 `UUID NOT NULL`（SQLite 不校验故测试全绿，Postgres 报 `22P02`），改用固定 sentinel UUID；(2) 记账 `key_id` 写 key 的 `PublicID`（hex 片段）到 `request_records`/`usage_events` 的 UUID 列，同样 `22P02`，改用 `request.Key.ID`；(3) `migrations.Apply` 并发启动竞态（两副本同时建 `schema_migrations` 触发 `pg_type_typname_nsp_index` 冲突），加 Postgres advisory lock 串行化（SQLite 无锁、不探测）；(4) 多副本 publish 只激活本进程 registry，peer 副本永不收敛导致持续 `503`，新增 `configReconcileInterval`（5s）周期 `ReconcileAll`，每个共享同库的副本都会追上最新已发布版本。

**2026-09-09 更新（C6）**：多租户就绪第一步已落地 system-scope 租户生命周期最小切片：`tenancy.TenantAdmin` + SQL repository 支持租户列表、租户+默认 Project 原子创建、租户状态软更新（active/suspended/deleted）；Admin API 增加 `GET /api/admin/tenants`、`POST /api/admin/tenants`、`PATCH /api/admin/tenants/{id}/status`；Lite composition 已接线，SQLite/Postgres conformance 与 Admin API 权限测试覆盖。A4 已补 system role 门禁、持久化支持、membership-backed 租户切换与 System→Tenant→Project→Agent 策略继承第一步：`local_admins.role` 允许 operator-seeded `system_admin`，system admin 登录不再被隐式绑定到最早 active tenant，租户生命周期接口现在要求 `system_admin`，`POST /api/admin/session/tenant` 可将 system admin 当前 session 切入任意 active tenant；非 system admin 只能切入 `tenant_memberships` 中自己的 active membership；租户内用户目录只能授予 tenant-local roles；编译器级 `SystemConfig.tenant_defaults.allowed_data_regions/residency_enforcement` 可作为 Tenant 默认值，tenant 显式值优先；`tenant.allowed_data_regions`/`tenant.residency_enforcement` 可作为 Project 默认值，Project 显式值优先，并参与 route residency validation；Project `agent_defaults.require_approval` 可被 Agent `approval_mode:"inherit_project"` 继承，Agent `approval_mode:"always"|"never"` 可显式覆盖。

**2026-09-09 更新（C4）**：语义缓存可观测/可调第一步已闭环：FinOps 增加 `cacheHitRate` 与 `semanticHitRate`，Console FinOps 卡片展示总体缓存命中率与语义缓存命中率；Governance 的 Cache 面板可通过现有 config draft 生命周期调整语义缓存 enabled/model/threshold，并发布后生效。后续若需要更深成本节省估算，可基于 provider avoided-cost 模型继续扩展。

**2026-09-09 更新（C5）**：控制面防护第一步已落地：Admin API 增加进程内滑动窗口限流（默认每 IP 600/min、每 admin 120/min），覆盖所有已认证 `/api/admin` read/admin/permission-gated 路由；超限返回 `429 RATE_LIMITED` 与 `retryAfterSeconds`。高危操作 step-up reauth 已落地：`X-Reauth-Token`（session 账户当前本地密码）现在同样要求 `PUT /api/admin/system/config`（全局 policy 写入）与 tenant `suspended`/`deleted` 状态变更，比原先仅 guardrail fast-publish 覆盖更广；IdP-only 无本地凭据会话返回 `REAUTH_UNAVAILABLE`。登录与密码重置原有专用 limiter 保持不变；完整 reauth 策略矩阵仍可后续推进。

**2026-09-09 更新（C7）**：审计保留/轮转第一步已落地：新增 `audit_retention` 表（租户级保留天数）与 `GET/PUT /api/admin/audit/retention`（read 组可读、tenant_admin 可写），Lite 每副本每小时跑一次幂等清理循环，仅清理显式配置了留存窗口的租户，未配置租户永不删除。清理按租户在 Go 侧计算 cutoff 后逐租户 DELETE，SQLite/Postgres 共用同一实现；Postgres RLS 与 conformance 已覆盖。

**2026-09-09 更新（C1）**：通知渠道扩展第一步已落地：`notification_settings` 增加 `targets`（多 Webhook 目标，每目标带严重级别门槛 low/medium/high/critical）与 `dedup_seconds`（同 kind+severity+rule 去重窗口，默认 300s）；`notificationSink` 按事件 severity 路由到满足门槛的目标，去重后再投递；legacy `webhook_url` 自动合成 low 门槛单目标，历史配置零迁移成本。Backend/Admin API/Lite reload 全部改为承载完整 settings 对象。

**2026-09-09 更新（C2）**：备份/恢复与 DR 演练已落地：新增 `scripts/backup-liteaig.sh`（SQLite 用 `sqlite3 .backup` 在线热备 + `*.masterkey` sidecar 打包，Postgres 用 `pg_dump -Fc`，支持 `PG_DUMP`/`PG_RESTORE` 覆盖以匹配 server 主版本）与 `scripts/restore-liteaig.sh`（按 MANIFEST 还原 SQLite 文件+sidecar 或 `pg_restore` 进目标库），新增 `docs/DR_RUNBOOK.md` 含 RPO/RTO、SQLite/Postgres 损毁演练与校验清单。SQLite 与 Postgres 往返均已在本地实机验证。

**2026-09-09 更新（C3）**：网关性能基准已落地：新增 `internal/loadtest` 与 `cmd/loadbench`，可对真实 Gateway `/v1/chat/completions` 执行非流式 RPS/P50/P95/P99 与流式 TTFT/P99 压测，并通过 `--rps-floor`、`--p99-ms-floor`、`--max-error-rate` 作为 release SLO gate。报告支持 JSON 与文件落盘，错误按 `errorSummary` 聚合；本地 SQLite + mock OpenAI provider 实测：chat 746 RPS / P99 62ms / 0 错误，stream 1721 RPS / TTFT P99 8ms / 0 错误（benchmark 实例需显式提高 `--gateway-rpm/--gateway-burst`，避免默认 600 RPM 干扰压测）。

**2026-09-09 更新（A2）**：MCP Tasks 治理最小切片已落地：Gateway `/mcp` 现在接受 `tasks/*` JSON-RPC 方法，将其作为同名 Tool resource 进入现有 auth、tool policy、approval、rate、budget、resilience、tool-call ledger 与 accounting 闭环；MCP connector 对 `tasks/*` 保留原始 method/params 转发，并把任意 JSON-RPC `result` 原样返回。该切片让 LiteAIG 成为 MCP Tasks 的治理点，但不内置任务存储/调度器；完整 Tasks 生命周期仍由上游 MCP server 承担。

**2026-09-09 更新（C8）**：OTLP 完整度第一步已落地：在原有 traces exporter 基础上新增 opt-in OTLP/HTTP metrics 与 logs exporter，分别支持 `--otlp-metrics-endpoint` / `LITEAIG_OTLP_METRICS_ENDPOINT`、`--otlp-logs-endpoint` / `LITEAIG_OTLP_LOGS_ENDPOINT`；traces 也新增 `--otlp-traces-endpoint` 并兼容旧 `LITEAIG_OTLP_ENDPOINT`。三类 exporter 均复用 Lite egress 校验、禁代理、防 ambient OTel headers/resource 注入、无压缩/禁重试/有界 shutdown。Metrics 先接入低基数请求 counters（source/outcome、tokens kind/source、latency total），logs 先导出安全启动运维日志。

**2026-09-09 更新（C9）**：公开 API 兼容契约第一步已落地：Gateway 与 Admin API 统一输出 `LiteAIG-API-Version: 2026-09-09`，请求可显式 pin 当前版本；旧版本 `2026-09-01` 暂时接受并返回 `Deprecation: true` 与 `Sunset`，未知版本在进入业务逻辑前返回 `400 UNSUPPORTED_API_VERSION`。新增 `docs/API_COMPATIBILITY.md` 固化允许的兼容变更、禁止的破坏性变更和废弃窗口要求。

**2026-09-09 更新（A3）**：官方 Client SDK 第一步已落地：新增 dependency-free Go SDK `pkg/liteaig`，覆盖 Gateway 稳定面 `GET /v1/models`、`POST /v1/chat/completions`、`POST /v1/chat/completions` streaming、`POST /v1/responses`、`POST /v1/embeddings`、`POST /mcp` JSON-RPC（含 `tools/call` helper）与 `POST /a2a` A2A `SendMessage`/`SendStreamingMessage` helper，默认发送 `LiteAIG-API-Version: 2026-09-09` 并支持显式版本 pin。新增 `docs/GO_SDK.md` 记录用法和范围。JavaScript/TypeScript SDK 第一步也已落地：`packages/liteaig-js` 提供 dependency-free ESM client、TypeScript declarations、chat/A2A SSE async iterator、MCP/A2A helpers 与 focused Node tests。Python SDK 第一步已落地：`packages/liteaig-python` 提供 dependency-free 标准库 client、chat/A2A SSE generator、MCP/A2A helpers 与 focused unittest。SDK packaging/compatibility gate 已落地：`scripts/check-sdks.sh` 统一跑 Go/JS/Python SDK 测试、JS dry-run pack 与 Python 本地安装 smoke，CI `sdk-packages` job 接线；`docs/SDK_COMPATIBILITY.md` 固化语言版本、覆盖矩阵与验证命令。

**2026-09-09 更新（A1）**：Gemini-native 接入第一步已落地：新增 `internal/connectors/model/gemini`，支持 Google Gemini 原生 `generateContent` chat/vision 调用（`x-goog-api-key`、`/v1beta/models/{model}:generateContent`、usageMetadata 映射）与 `streamGenerateContent?alt=sse` 流式增量输出；支持统一 Tool/function calling 映射到 Gemini `functionDeclarations`/`functionCallingConfig`，并将 `functionCall` 结果映射回统一 `ToolCall`。provider resolver 支持 `provider.type=gemini`；setup/provider health probe 使用 Gemini 原生 `/v1beta/models` 与模型 catalog 解析；资源 catalog 通过 migration 将 `google-gemini` 从 OpenAI-compatible endpoint 切到 native endpoint，并声明 stream/tools 能力。Rerank 第一切片也已落地：新增统一 `RequestRerank`、Gateway `POST /v1/rerank`、OpenAI-compatible `/v1/rerank` connector 转发与 `rerank` capability 路由约束。Audio transcription 第一切片已落地：新增统一 `RequestAudio`、Gateway `POST /v1/audio/transcriptions` multipart 入站、OpenAI-compatible `/v1/audio/transcriptions` multipart connector 转发与 `audio` capability 路由约束。Batch lifecycle 第一切片已落地：新增统一 `RequestBatch` lifecycle operation、Gateway `POST /v1/batches` create、`GET /v1/batches/{id}` retrieve、`POST /v1/batches/{id}/cancel` 与 `GET /v1/batches?model=...` list；create 成功后通过 `batch_mappings` 持久化 batch-id→logical-model 映射，retrieve/cancel 复用映射回到同一治理路径，list 使用 LiteAIG 扩展 `model` query 参数路由。

**2026-09-10 更新（A5）**：多区域 DR drill 自动化第一步已落地：新增 `cmd/drdrill`，可在 cron/CI 中执行只读 DR checklist，输入 `--region`、`--tenant`、`--budget-mode`、`--node-ready`，并可选 `--lkg-root` 校验 Region-scoped LKG bundle（`regions/<region>/<tenant>/active.bundle` 或 `previous.bundle`）是否可恢复；输出 JSON 并支持 `--out` 保存报告。`docs/DR_RUNBOOK.md` 已补自动化命令。报告归档第一切片已补：`--archive-dir` 按 `drdrill-<region>-<tenant>-<UTC时间戳>.json` 落盘并保留最近 `--archive-keep`（默认 30）份，便于 cron 长期归档与监控消费。完整多 AZ traffic switch 与云资源级演练仍需真实环境接线。

**2026-09-10 更新（A4）**：System config lifecycle 第一步已落地：新增 `system_config` 单例表（`041_system_config.sql`）与 `SystemConfigRepository`（Get/Upsert）；`config.Service` 通过可选 `SystemConfigRepository` 在 tenant publish/rollback/reconcile/compileAndActivate 路径统一读取系统默认值，把 `SystemConfig.tenant_defaults` 作为 System→Tenant→Project 继承的持久化事实源；Admin API 新增 `GET/PUT /api/admin/system/config`（仅 `system_admin`，无 tenant scope），`PUT` 校验 `residency_enforcement` 只允许 `advisory`/`strict`。`SetSystemConfig` 持久化后会立即 `ReconcileAll`，让已发布租户的 active snapshot 尽快按新系统默认值重编译；无法在新默认值下通过校验的租户保留原 active snapshot。System config 成功更新会写入 `audit_events(scope='system', tenant_id=NULL, action='system_config.update')`，并发出 redacted `system_config.update` DomainEvent（webhook allowlist 仅保留 `action`/`severity` 等安全元数据，不外发配置内容或 actor ID）。继承矩阵测试已补：同一编译用例覆盖 System→Tenant defaults、Tenant→Project defaults、Project explicit override、Project→Agent approval default、Agent always/never/legacy override。持久 DomainEvent outbox 已闭环：新增 `domain_event_outbox`（`043_domain_event_outbox.sql`）与 `DomainEventOutbox.Enqueue`；`durableEventSink` 先持久化再立即转发既有 analytics/notification sink（成功即 `MarkDelivered`），新增 outbox delivery worker（`platform:event_outbox` singleton 租约）在 crash/失联场景按 claim 宽限窗口（5 分钟）回收未交付事件并重投，失败按平方退避重试至 5 次上限；outbox 写入失败只记录日志不阻断请求；`GET /metrics` 暴露 `liteaig_event_outbox_events{status=queued|claiming|sent|failed}` 与最早 retry 时间戳 gauge 用于投递监控。SQLite/Postgres 共用同一 migration；SQLite conformance 已验证默认值穿透到编译后的 runtime snapshot，并验证 system config 变更会传播到多个已发布租户。

**2026-09-10 更新（C5/C9）**：高危操作 step-up reauth 已从 guardrail fast-publish 泛化为共享 `Server.requireReauth`，并扩展到 `PUT /api/admin/system/config`（全局 policy 写入）与 tenant `suspended`/`deleted` 状态变更（`active` 不需要）。module API 稳定性门禁已落地：`cmd/api-snapshot` 将 `pkg/liteaig` 等公开包的导出面（func/method/type/var/const，含接收者方法）写入 `architecture/exported.json`，`cmd/architecture-test` 对比 live surface 并判「移除已导出符号」为 breaking 违规（新增允许）；CI 的 architecture boundary job 因此自动覆盖 SDK 兼容回归，变更公开 API 后需 `go run ./cmd/api-snapshot -write && go run ./cmd/architecture-test`。

**2026-09-10 更新（C4）**：语义缓存 avoided-cost 估算已补：FinOps 记账对 exact/semantic cache 命中请求（`ProviderCost` 为空但记录完整 would-be input/output token）累计 `savedTokens`，并按文档化 blended `$2/M` token 报价估算 `cacheSavings`；FinOps cache 建议带出「规避 ~N provider tokens、估算 $X」详情与 Impact。估算值明确标注非 provider 账单。

**2026-09-10 更新（C1）**：alert 邮件通知通道已落地：新增 `alert.EmailNotifier` 与确定性邮件模板（subject/body 只含 redacted fact set：rule/severity/status/message/evidence，不含请求体），Lite 在 SMTP relay 配置且设置 `--smtp-alert-to` 时接线该 notifier。

**2026-09-10 更新（C7）**：审计保留清理指标已暴露：`/metrics` 新增 `liteaig_audit_events_purged_total` 与 `liteaig_audit_purge_last_timestamp_seconds`，由 retention sweeper 每次完成（含 0 清理）更新 process-local 计数器，方便告警「清理停滞/历史未降」。

**2026-09-10 更新（C8）**：结构化安全事件日志桥已落地：`observability.StructuredEventLog` 将 redacted DomainEvent 转 OTLP LogRecord（固定属性白名单 action/rule_id/severity/status/operation/file_id/logical_model/deployment_id/outcome/content_hash/reason/decision/account_id + tenant/project/request/event 上下文，kind 前缀派生 security severity；未白名单的属性与敏感值一律丢弃）；`OTLPLogSink` 作为 `contracts.EventSink`，Lite 在 `otlpLogger` 配置时把它包进事件链最外层（`logThenEventSink`，best-effort，导出失败不阻断请求也不阻断持久 outbox）。adminapi 侧第二切片已接：reauth 缺 token/密码不匹配/rate-limit 429 通过 `Server.WithEventSink` 发出 `admin.security.reauth_required`/`admin.security.reauth_failed`/`admin.security.rate_limited` 结构化 DomainEvent（含 account_id），并入同一 OTLP 导出。自此 guardrail/file/system_config/alert/admin 等全部已事件化安全信号可经 OTLP 送入 SIEM。

**2026-09-11 更新（B3/稳定闭环）**：K8s 多副本 Standard e2e 已在本地 kind 实机全链路跑通（`kind standard e2e passed`）。期间修复三处缺口以让脚本在受限网络/本地代理环境可复跑且仍是 CI 等价：`sqlite.Open` 对 in-memory DSN 固定单连接，消除 `TestSingletonReleasesLeaseOnShutdown` 全量偶发 `no such table`；`038_gemini_native_catalog.sql` 补 `-- owner: catalog`，修复 `TestEveryMigrationDeclaresOwner`；`kind-standard-e2e.sh` 给 Postgres 加 `pg_isready` startup/readiness probe（此前 `rollout status` 在 DB 未就绪时放行，gateway 与 DB init 竞态导致 setup 失败）并为 setup/readyz 的 `curl` 加 `--noproxy '*'`（本地存在 HTTP(S)_PROXY 时被劫持，CI 无代理时 no-op）。验证：`go test ./... -count=1` 98 包全绿、0 失败；`scripts/check-k8s.sh` helm lint/template 通过；`cmd/architecture-test` boundaries ok。

**2026-09-12 更新（B3 正式关闭）**：GitHub Actions `ci` 与 `sdk-conformance` run #19 在 commit `8a42490` 全部成功，`quality`（含 Postgres/Redis-backed 全仓 race）、`k8s-kind-standard`、image、Console、SDK packages、secrets、K8s manifest 与官方 SDK conformance 均 green。最终 race flaky 的第一处根因通过新增的 `race-diagnostics` artifact 定位为多个 Lite test fixture 复用固定 SQLite named-memory DSN：前一 fixture 的后台连接仍在清理时，后一 fixture 可读到由不同 master key 加密的 persisted pepper，触发 `cipher: message authentication failed`。`testMemoryDSN` 现在为每次 Lite fixture 启动生成唯一 shared-cache DSN，并覆盖所有 19 个非持久化测试入口；restart/durability/two-process 等有意共享数据库的测试保持不变。run #20 随后暴露第二处生命周期缺陷：`Lite.Close()` 未取消 event outbox worker，且关闭数据库前未等待 singleton/reconcile/retention 等后台循环退出，导致 `t.TempDir` 清理期间目录仍被写入；commit `c04003a` 统一跟踪后台 goroutine，按“全部 cancel → wait → 关闭通知/telemetry/spool → 关闭协调器/DB”顺序停机，run #21 再次全绿。验证：Responses E2E race 20/20、关闭路径 race 100/100、`internal/app` race、CI 等价 `go test -race -count=1 ./...` 与 GitHub Actions #19/#21/#22 均通过。CI 失败时会保留完整 `race.jsonl`、还原后的 output/stderr、环境与失败列表 artifact，后续 runner-only 故障无需依赖管理员日志权限。

**2026-09-12 更新（下一批企业增强）**：C9 已增加 `architecture/openapi.json`（OpenAPI 3.1）与无外部依赖的 `cmd/openapi-compat`，PR CI 从 base SHA 读取历史快照，阻断稳定 Gateway path/method 删除及 request/response schema 收窄；C8 已将无认证登录失败与 login limiter 拒绝事件化为 `admin.security.login_failed`/`admin.security.rate_limited`，只保留固定 `reason`，不输出用户名、密码或 IP；C5 step-up reauth 已扩展至 API key revoke 与 federation approve（reject 不要求），relationship review 权限也从 suspend 更正为 admin-only review，Console 提供当前密码确认且 `REAUTH_REQUIRED` 不再触发全局登出；A1 的 `file_mapping_retention_days` 为 system-scope opt-in 策略（0 表示永久保留），每小时只清理本地授权映射，绝不自动调用上游文件删除，并暴露清理累计数/时间戳指标；C4 已用 `lite-reference-v1` per-model input/output 价格替代 blended `$2/M`，仅对可从相同 snapshot 唯一解析的 upstream model 估价，歧义/未知模型计入 `unpricedSavedTokens`，Console 按 Project/Model 展示 cache hit rate 与参考节省。

**2026-09-10 更新（A1）**：Batch/files artifact 治理第一步已落地：OpenAI Batch response now parses `output_file_id`/`error_file_id`；Gateway 在 batch create/retrieve 成功后把这些 file IDs 写入新增 `file_mappings`（`042_batch_artifact_mappings.sql`），`GET /v1/files/{id}/content` 只允许访问已学习到的 file ID，并通过 `file_id -> logical_model` 回到同一 governed pipeline/connector 路径；未知 file ID 返回 404，避免客户端任意选择上游 files。Files upload/list/retrieve/delete 第一切片也已接入：`POST /v1/files` 使用 LiteAIG 扩展 multipart `model` 字段做 logical-model 路由并在上游转发时剥离；上传成功后保存 `file_id -> logical_model`；`GET /v1/files?model=...` 通过显式 model 列表；`GET /v1/files/{id}`/`DELETE /v1/files/{id}` 通过持久映射回同一治理路径；delete 成功后撤销本地 file mapping，防止 stale mapping 继续授权后续 retrieve/content。OpenAI connector 新增 `/v1/files` 与 `/v1/files/{id}`/`content` 原生转发。文件 upload/retrieve/delete/content 成功完成后会发出 redacted `file.access` DomainEvent（operation/file_id/logical_model/deployment_id/outcome），不包含文件内容或文件名。Batch input artifact scanning 已接入输入护栏：`RequestFile{operation:"upload"}` 会扫描上传 bytes，block 命中直接阻断，redact 命中会改写上传内容后再转发。本地 mapping retention foundation 已补 `PurgeFileMappingsOlderThan`，后续 sweeper/策略可按保留窗口清理本地授权映射；当前不自动删除上游文件，避免无产品策略时误删。

---

## 二、A 类 —— 路线图明确延期（§1.6，需产品决策后立项）

| # | 特性 | 现状 | 推进价值 |
|---|---|---|---|
| A1 | image / audio / rerank / batch / **Gemini-native** 接入 | **全部主要能力已有第一切片**：Gemini-native chat/vision/streaming/tools、rerank、audio transcription、batch lifecycle 与 files 治理路由均已通过统一治理路径；artifact 映射、redacted `file.access`、input guardrail scanning 已接入；system-scope `file_mapping_retention_days` 已驱动本地映射自动清理并暴露指标，默认永久保留且不会删除上游文件 | 后续按产品需要定义并实现上游 artifact 归档/删除策略与更深文件内容分类/报告 |
| A2 | **MCP Tasks 治理最小切片已落地**：`tasks/*` 方法可作为同名 Tool resource 接入既有治理链并转发到 MCP server | `protocol/mcp`；`connectors/tool/mcp`；`/mcp` | 后续若产品需要，可补内置 task store、任务订阅/取消语义与 Console 视图 |
| A3 | 官方 **Client SDK 第一步已落地**：Go、JavaScript/TypeScript 与 Python SDK 覆盖稳定 Gateway 非流式面、chat streaming、MCP JSON-RPC 与 A2A SendMessage/SendStreamingMessage，并默认 pin API contract version；SDK package gate 与兼容矩阵已接入 | `pkg/liteaig`；`packages/liteaig-js`；`packages/liteaig-python`；`scripts/check-sdks.sh`；`docs/SDK_COMPATIBILITY.md` | 后续按发布需要补真实 registry publishing 与签名 provenance |
| A4 | 完整**多租户 SaaS**（System→Tenant→Project→Agent 策略继承、租户 CRUD） | **system role + membership-backed tenant switch + policy inheritance + system config lifecycle 已落地**：`system_admin` 可持久化并登录为无 tenant scope，可访问全局租户生命周期接口，并可通过 `POST /api/admin/session/tenant` 切入任意 active tenant；非 system admin 只能切入自己的 active `tenant_memberships`；租户内用户目录禁止授予 `system_admin`；`GET/PUT /api/admin/system/config` 持久化 `tenant_defaults`，publish/rollback/reconcile 编译时注入 System→Tenant data-residency defaults、Tenant→Project data-residency defaults、Project→Agent approval default；继承矩阵测试覆盖显式 override/default/legacy 组合；system config 变更后立即触发全租户 ReconcileAll，传播到已发布 active snapshots，并写入 system-scoped audit event + redacted DomainEvent/notification；持久 DomainEvent outbox 已接入（先持久化再同步转发，delivery worker 按 5 分钟 claim 窗口 crash recovery + 平方退避重试），`/metrics` 暴露投递状态 gauge | 后续补更多产品策略字段继承 |
| A5 | **多区域 Active / DR drill** 自动化 | **自动化 drill CLI 第一步已落地**：`cmd/drdrill` 输出 read-only checklist JSON，可选校验 Region LKG readiness；runbook 已记录命令；`--archive-dir` 报告归档与保留窗口已接入 | 后续接入真实多 AZ traffic switch、云资源探测与定时归档的监控消费 |

---

## 三、B 类 —— 已实现区域内仍留的半截（可直接推进）

| # | 差距 | 证据 | 建议动作 |
|---|---|---|---|
| B3 | ✅ **真实 K8s 多副本 Standard 集群 e2e 已关闭**：`scripts/kind-standard-e2e.sh` 覆盖 Helm 2 副本 mode=all、setup、配置收敛、pod 杀恢复与 `/readyz`；本地 kind 实机通过，GitHub Actions run #19 的 `k8s-kind-standard` 与完整 `ci`/`sdk-conformance` 全绿；race 测试固定 named-memory DB 污染已用 per-fixture `testMemoryDSN` 系统性消除，失败诊断 artifact 已固化 | `scripts/kind-standard-e2e.sh`；`.github/workflows/ci.yml`；`internal/app/testdb_test.go` | 保持常态化门禁；后续 flaky 直接基于 `race-diagnostics` artifact 定位 |

---

## 四、C 类 —— 值得推进的企业级增强（超越路线图）

| # | 增强 | 现状 | 建议 |
|---|---|---|---|
| C1 | **通知渠道扩展第一步已落地**：多 Webhook 目标 + 按严重级别路由 + 同事件去重窗口；legacy 单目标兼容；**邮件通知通道已补**：`alert.EmailNotifier` 将 firing alert 渲染为确定性纯文本邮件（severity/rule/evidence，redacted fact set），Lite 在 SMTP relay + `--smtp-alert-to` 配置时接线为该 alert channel | `notification_settings.targets/dedup_seconds`；`notificationSink` 路由；`alert/email.go`；`--smtp-alert-to` | 后续可补 Slack 模板与归档目标 |
| C2 | **备份/恢复与 DR 演练已落地**：SQLite 在线热备 + masterkey 打包、Postgres pg_dump 往返，附 RPO/RTO runbook 与校验清单 | `scripts/backup-liteaig.sh`；`scripts/restore-liteaig.sh`；`docs/DR_RUNBOOK.md` | 后续可补云对象存储轮转与自动保留策略 |
| C3 | **网关性能基准已落地**：支持真实 Gateway chat/stream 压测，输出 RPS、P50/P95/P99、TTFT P99、错误率与错误摘要，可用 SLO flag 作 release gate | `internal/loadtest`；`cmd/loadbench`；`docs/GATEWAY_BENCHMARK.md` | 后续可按 Standard/Enterprise 拓扑沉淀分档基线与 CI 长跑 |
| C4 | **语义缓存可观测与参考价估算已落地**：FinOps 暴露总体/语义及 Project/Model 维度命中率；`lite-reference-v1` 按 upstream model 的 input/output rate 估算 avoided cost，只对相同 snapshot 下可唯一解析的模型计价，未知/歧义 token 单列为 unpriced，明确不是 provider 账单 | `pricing.LiteReferencePriceVersion`；`FinOpsView.*SavedTokens/cacheSavings/estimateVersion`；Console FinOps | 后续可接外部可更新价格源与历史 snapshot 价格版本归档 |
| C5 | **控制面防护持续扩展**：Admin API 与登录/密码重置均有限流；step-up reauth 已覆盖 guardrail fast-publish、system config、tenant suspend/delete、API key revoke 与 federation approve；Console 对新增高危动作要求当前密码，OIDC-only 会话 fail-closed 且禁用不可完成的操作 | `AdminRateLimiter`；`Server.requireReauth`；`Session.AuthMethod`；KeySection/Governance reauth modal | 后续按产品决策覆盖 credential rotation/deletion、federation suspend、workflow approval 等剩余高危动作，并设计 IdP 原生 step-up |
| C6 | **多租户基础切片已落地**：Postgres RLS、system-scope tenant CRUD、持久 `system_admin`、membership-backed tenant switch 与首批 System→Tenant→Project→Agent 策略继承均已完成 | `rls.go`；`tenancy.TenantAdmin`；`/api/admin/tenants`；`/api/admin/session/tenant` | 后续只按 A4 增加明确选定的产品策略字段继承 |
| C7 | **审计保留第一步已落地**：租户级 `audit_retention` 留存窗口 + 每副本后台清理循环 + Admin API GET/PUT；Evidence Export 已有；**清理指标已暴露**：`/metrics` 含 `liteaig_audit_events_purged_total`（累计清理计数）与 `liteaig_audit_purge_last_timestamp_seconds`（最近一次 sweep 完成时间） | `audit_events`；`036_audit_retention.sql`；`/api/admin/audit/retention`；`writeAuditRetentionMetrics` | 后续可补保留前导出归档（合规轮转） |
| C8 | **OTLP traces/metrics/logs 与结构化安全事件桥已落地**：DomainEvent 经固定白名单进入 OTLP logs；reauth、admin rate-limit 以及本地登录失败/login limiter 拒绝均输出 redacted `admin.security.*`，无认证登录事件不包含用户名、密码、IP 或 account_id | `observability/otlp.go`；`eventlog.go`；`SessionEndpoints.EventSink`；`Server.WithEventSink` | 后续可扩展更多低基数 gauges/histograms |
| C9 | **HTTP 与 module API 兼容门禁均已落地**：Gateway/Admin 统一版本头；公开 Go 导出面由 `architecture/exported.json` 守护；稳定外部 Gateway contract 由 OpenAPI 3.1 `architecture/openapi.json` 描述，PR CI 用 `cmd/openapi-compat` 与 base SHA 比较 path/method 和 request/response schema 兼容性 | `webkit.APICompatibility`；`architecture/{exported,openapi}.json`；`cmd/{api-snapshot,openapi-compat}`；`docs/API_COMPATIBILITY.md` | 后续可扩展 OpenAPI baseline 到 Admin API，并在 wire DTO 规范化后引入生成类型 |

---

## 五、建议执行顺序

1. **C 类其余**：B3 已正式关闭；C1/C2/C3/C4/C5/C6/C7/C8/C9 与 A2 最小切片均已落地；完整 SaaS 能力并入 A4。
2. **A 类**：A2/A3 可作为产品候选；A1/A4/A5 需要明确需求与投资决策。

---

*本文件与 `docs/WHITEPAPER.md`、`docs/ROADMAP_V86_COMPLIANCE.md` 配套使用；差距与证据均以当前代码库为准。*
