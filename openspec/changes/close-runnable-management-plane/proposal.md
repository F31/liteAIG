# close-runnable-management-plane

## Why

V8.2 §34（代码结构）与 §37（最终冻结决策）要求“默认单二进制”，且 Phase 0 DoD 为“5 分钟首次调用”。当前 `cmd/liteaig/main.go` 只启动 readiness server：`internal/app.Compose` 仅返回 Plan，未构造任何业务 HTTP 服务；`adminapi.New`、Gateway、`console.Assets` 均无调用方；`ControlBackend` 有 12 处 `errNotWired`（Setup/CreateKey/Playground/Audit/Projects 等）。Console 已有一批页面（Setup/Playground/Config/Requests/Health/Alerts/Governance）但无后端可连（vite 无 proxy、主二进制不 serve `/api/admin`、不 serve 静态资源）。结果是浏览器无法执行任何管理操作。

本 change 建立 **Lite 可运行管理面**：单二进制组装 SQLite + 迁移 + pepper + repositories + services + Admin API + 会话 + Console 静态服务，并闭环 Setup 向导（创建 Tenant/Project/Admin/Provider/Credential/Deployment/LogicalModel/Key + 首次调用）。

## What Changes

- `internal/app` 新增组合根：打开 SQLite、应用迁移、seed key pepper、构造全部 sqlite repositories 与领域 services，组装 `adminapi.Server` + `SessionManager` + `SessionEndpoints` + `LocalVerifier`，serve Console 静态资源，并挂载 drain 生命周期。
- `cmd/liteaig/main.go` 接入组合根：`--db`（SQLite DSN）、`--admin-addr`（管理面监听）等 flag；未指定 `--db` 时保持只起 readiness（不破坏现有 `main_test`）。
- Setup 向导闭环：实现 `setup.ProviderSetup`（mock provider）与 `setup.FirstCallRunner`（playground pipeline），wizard 返回 `virtualKey`/`SDKExample`/`requestId`。
- 会话闭环：`POST /api/admin/session`、`DELETE /api/admin/session`、`GET /api/admin/oidc/config`（禁用态）、`/me` 返回 scopes；CSRF + Secure cookie。
- Console 静态服务：serve `console.Assets`（embed dist）含 SPA fallback；`/api/admin/*` 路由到 Admin API。
- Governance/Playground/Request Explorer 的只读数据面（Runtime/Health/Requests/Usage/Simulator/Federation）经由 registry + repositories 提供真实数据；写操作（Setup/CreateKey/Playground）闭环。

## Capabilities

### New Capabilities
- `runnable-management-plane`: 单二进制 Lite 管理面（SQLite 持久化 + Admin API + 会话 + Console 静态服务 + Setup 向导闭环）。

### Modified Capabilities
- `multimodel-protocol-access`: Admin API 从“接口存在”变为“可运行可连”。

## Impact

- **Backend**: `internal/app` 组合根；`cmd/liteaig` 扩展 flags；`internal/controlplane/adminapi` 后端接线（Setup/CreateKey/Playground/Projects/Audit 消除 `errNotWired`）。
- **APIs/Console**: `/api/admin/*` 全链路可访问；Console 页面连接到后端。
- **Dependencies**: 复用现有 `modernc.org/sqlite`；无新增第三方依赖。
- **Tests**: 组合根集成测试（Setup→snapshot 激活→session 登录→runtime 查询→console 可访问）；保持现有 `main_test` 绿。

### Non-Goals
- Gateway `/v1` 数据面服务、Live Tail 事件发布、Governance 写操作（approval/federation-suspend/agent-graph 的完整后端接线）留待后续 change。
- PostgreSQL/Redis 生产档组合（本 change 仅 SQLite Lite 档）。

**Golden Scenario:** Scenario A（5 分钟首次调用）与 Console 管理闭环的起点。
