import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Typography,
} from "antd";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  AgentGraphView,
  APIError,
  ApprovalView,
  DelegationGrantView,
  FederationView,
  SecurityEventView,
  SimulateResult,
  ToolCallView,
  api,
  RuntimeResources,
} from "../api/client";
import { hasPermission, isAdminRole, useAuth } from "../state/auth";
import { useTenantConfigDraft } from "./resources/useTenantConfigDraft";

type PublishInput = {
  id: string;
  change: string;
  rules: GuardrailRule[];
  reauth: string;
};

type GuardrailRule = {
  id: string;
  kind: string;
  pattern: string;
  action: string;
  replacement?: string;
};

export default function Governance() {
  const { t } = useTranslation("governance");
  const role = useAuth((state) => state.role);
  const scopes = useAuth((state) => state.scopes);
  const canWrite = isAdminRole(role);
  const canDecide = hasPermission(scopes, "approval.decide");
  const canSuspend = hasPermission(scopes, "external_agent.suspend");
  const canGrantDelegation = hasPermission(scopes, "delegation.grant");
  const canRevokeDelegation = hasPermission(scopes, "delegation.revoke");
  const runtime = useQuery({
    queryKey: ["governance"],
    queryFn: () => api<RuntimeResources>("/api/admin/governance"),
  });
  const cacheDraft = useTenantConfigDraft({
    canWrite,
    onPublished: () => void runtime.refetch(),
  });
  const semanticDraft = cacheDraft.providerDraft?.Config.cache?.semantic;
  const semanticLive = runtime.data?.cache?.semantic;
  const semanticCache = semanticDraft ?? semanticLive;
  const updateSemanticCache = (
    patch: Partial<{ enabled: boolean; model: string; threshold: number }>,
  ) => {
    cacheDraft.updateConfig((config) => ({
      ...config,
      cache: {
        ...(config.cache ?? {}),
        semantic: { ...(config.cache?.semantic ?? {}), ...patch },
      },
    }));
  };
  const publish = useMutation({
    mutationFn: (input: PublishInput) =>
      api("/api/admin/guardrail/publish", {
        method: "POST",
        headers: { "X-Reauth-Token": input.reauth },
        body: JSON.stringify({
          id: input.id,
          change: input.change,
          rules: input.rules ?? [],
        }),
      }),
    onSuccess: () => void message.success(t("published")),
    onError: (error: unknown) =>
      void message.error(
        error instanceof APIError &&
          error.code !== "INTERNAL_ERROR" &&
          error.code !== "GUARDRAIL_PUBLISH_FAILED"
          ? t(`errors.${error.code}`, { ns: "common" })
          : error instanceof APIError &&
              error.code === "GUARDRAIL_PUBLISH_FAILED"
            ? t("publishFailed")
            : t("error", { ns: "common" }),
      ),
  });
  const securityEvents = useQuery({
    queryKey: ["security-events"],
    queryFn: () => api<SecurityEventView[]>("/api/admin/security-events"),
  });
  const toolCalls = useQuery({
    queryKey: ["tool-calls"],
    queryFn: () => api<ToolCallView[]>("/api/admin/tool-calls"),
  });
  const simulate = useMutation({
    mutationFn: (input: {
      model: string;
      projectId: string;
      metrics: Record<
        string,
        { latencyMS: number; cost: number; load: number }
      >;
    }) =>
      api<SimulateResult>("/api/admin/simulator", {
        method: "POST",
        body: JSON.stringify(input),
      }),
  });
  const federation = useQuery({
    queryKey: ["federation"],
    queryFn: () => api<FederationView>("/api/admin/federation"),
  });
  const approvals = useQuery({
    queryKey: ["approvals"],
    queryFn: () => api<ApprovalView[]>("/api/admin/approvals"),
  });
  const agentGraph = useQuery({
    queryKey: ["agent-graph"],
    queryFn: () => api<AgentGraphView>("/api/admin/agent-graph/root"),
    enabled: false,
  });
  const delegations = useQuery({
    queryKey: ["delegations"],
    queryFn: () => api<DelegationGrantView[]>("/api/admin/delegations"),
  });
  const grantDelegation = useMutation({
    mutationFn: (input: {
      delegatorId: string;
      delegateeId: string;
      permissions: string;
    }) =>
      api<DelegationGrantView>("/api/admin/delegations", {
        method: "POST",
        body: JSON.stringify({
          delegatorId: input.delegatorId,
          delegateeId: input.delegateeId,
          permissions: input.permissions
            .split(",")
            .map((value) => value.trim())
            .filter(Boolean),
        }),
      }),
    onSuccess: () => {
      void message.success(t("delegationGranted"));
      void delegations.refetch();
    },
    onError: () => void message.error(t("error", { ns: "common" })),
  });
  const revokeDelegation = useMutation({
    mutationFn: (id: string) =>
      api(`/api/admin/delegations/${encodeURIComponent(id)}`, {
        method: "DELETE",
      }),
    onSuccess: () => {
      void message.success(t("delegationRevoked"));
      void delegations.refetch();
    },
    onError: () => void message.error(t("error", { ns: "common" })),
  });
  const decide = useMutation({
    mutationFn: (input: { id: string; decision: string }) =>
      api(`/api/admin/approvals/${input.id}/action`, {
        method: "POST",
        body: JSON.stringify({ decision: input.decision }),
      }),
    onSuccess: () => void approvals.refetch(),
  });
  const suspend = useMutation({
    mutationFn: (id: string) =>
      api(`/api/admin/federation/${id}/suspend`, { method: "POST" }),
    onSuccess: () => void federation.refetch(),
  });
  const projects = useQuery({
    queryKey: ["projects"],
    queryFn: () => api<{ id: string }[]>("/api/admin/projects"),
  });
  const discover = useMutation({
    mutationFn: (input: {
      url: string;
      name: string;
      verificationKeys: { alg: string; pem: string }[];
    }) =>
      api<FederationView["relationships"][number]>(
        "/api/admin/federation/discover",
        {
          method: "POST",
          body: JSON.stringify(input),
        },
      ),
    onSuccess: () => {
      void message.success(t("discovered"));
      void federation.refetch();
    },
    onError: () => void message.error(t("discoverFailed")),
  });
  const activate = useMutation({
    mutationFn: (id: string) => {
      const projectId = projects.data?.[0]?.id ?? "";
      return api(`/api/admin/federation/${id}/review`, {
        method: "POST",
        body: JSON.stringify({
          approved: true,
          projectGrants: projectId ? [projectId] : [],
          capabilityGrants: ["chat"],
        }),
      });
    },
    onSuccess: () => {
      void message.success(t("activated"));
      void federation.refetch();
    },
    onError: () => void message.error(t("activationFailed")),
  });
  const relationshipColumns = [
    { title: t("identifier"), dataIndex: "id" },
    { title: t("externalAgentId"), dataIndex: "externalAgentID" },
    { title: t("status"), dataIndex: "status" },
    { title: t("assuranceLevel"), dataIndex: "assuranceLevel" },
    {
      title: t("verifiedAnchor"),
      dataIndex: "hasVerifiedAnchor",
      render: (v: boolean) => (v ? "✓" : "✗"),
    },
    {
      title: "",
      key: "action",
      render: (_: unknown, row: FederationView["relationships"][number]) =>
        row.status === "active" && canSuspend ? (
          <Button
            size="small"
            danger
            loading={suspend.isPending}
            onClick={() => suspend.mutate(row.id)}
          >
            {t("suspend")}
          </Button>
        ) : (row.status === "candidate" || row.status === "pending_review") &&
          canSuspend ? (
          <Button
            size="small"
            type="primary"
            loading={activate.isPending}
            onClick={() => activate.mutate(row.id)}
          >
            {t("activate")}
          </Button>
        ) : null,
    },
  ];
  const agentColumns = [
    { title: t("identifier"), dataIndex: "id" },
    { title: t("name"), dataIndex: "name" },
    { title: t("externalSubject"), dataIndex: "externalSubject" },
    { title: t("status"), dataIndex: "status" },
  ];
  const approvalColumns = [
    { title: t("identifier"), dataIndex: "id" },
    { title: t("requester"), dataIndex: "requester" },
    { title: t("action"), dataIndex: "action" },
    { title: t("target"), dataIndex: "target" },
    { title: t("status"), dataIndex: "status" },
    { title: t("approvers"), dataIndex: "approvers" },
    {
      title: "",
      key: "action",
      render: (_: unknown, row: ApprovalView) =>
        row.status === "pending" && canDecide ? (
          <Space>
            <Button
              size="small"
              type="primary"
              loading={decide.isPending}
              onClick={() => decide.mutate({ id: row.id, decision: "approve" })}
            >
              {t("approve")}
            </Button>
            <Button
              size="small"
              loading={decide.isPending}
              onClick={() => decide.mutate({ id: row.id, decision: "reject" })}
            >
              {t("reject")}
            </Button>
          </Space>
        ) : null,
    },
  ];
  const agentGraphColumns = [
    { title: t("hopOrder"), dataIndex: "order" },
    { title: t("name"), dataIndex: "agentId" },
    { title: t("model"), dataIndex: "model" },
    { title: t("score"), dataIndex: "cost" },
    { title: t("status"), dataIndex: "outcome" },
  ];
  const delegationColumns = [
    { title: t("identifier"), dataIndex: "id" },
    { title: t("delegator"), dataIndex: "delegatorId" },
    { title: t("delegatee"), dataIndex: "delegateeId" },
    {
      title: t("permissions"),
      dataIndex: "permissions",
      render: (values: string[]) => values.join(", "),
    },
    { title: t("createdBy"), dataIndex: "createdBy" },
    {
      title: "",
      key: "action",
      render: (_: unknown, row: DelegationGrantView) =>
        canRevokeDelegation ? (
          <Popconfirm
            title={t("revokeDelegation")}
            onConfirm={() => revokeDelegation.mutate(row.id)}
          >
            <Button size="small" danger loading={revokeDelegation.isPending}>
              {t("revoke")}
            </Button>
          </Popconfirm>
        ) : null,
    },
  ];
  const evidenceColumns = [
    { title: t("identifier"), dataIndex: "deploymentId" },
    {
      title: t("status"),
      dataIndex: "eligible",
      render: (v: boolean) => (v ? "✓" : "✗"),
    },
    { title: t("score"), dataIndex: "score" },
  ];
  const eventColumns = [
    { title: t("identifier"), dataIndex: "id" },
    { title: t("rule"), dataIndex: "ruleId" },
    { title: t("action"), dataIndex: "action" },
    { title: t("occurredAt"), dataIndex: "occurredAt" },
  ];
  const callColumns = [
    { title: t("name"), dataIndex: "toolName" },
    { title: t("session"), dataIndex: "sessionId" },
    { title: t("task"), dataIndex: "taskId" },
    { title: t("occurredAt"), dataIndex: "occurredAt" },
  ];
  const columns = [
    { title: t("identifier"), dataIndex: "id" },
    { title: t("name"), dataIndex: "name" },
    { title: t("status"), dataIndex: "status" },
    { title: t("endpoint"), dataIndex: "url" },
    { title: t("classification"), dataIndex: "dataClassification" },
  ];
  const budgetColumns = [
    { title: t("identifier"), dataIndex: "id" },
    { title: t("projectId"), dataIndex: "projectId" },
    { title: t("budgetWindow"), dataIndex: "windowHours" },
    { title: t("budgetLimit"), dataIndex: "tokenLimit" },
    { title: t("budgetMode"), dataIndex: "mode" },
    {
      title: t("budgetConsistency"),
      dataIndex: "consistency",
      render: (value: RuntimeResources["budgets"][number]["consistency"]) =>
        t(`budgetConsistencyModes.${value}`),
    },
  ];
  const emptyTable = (data: object[] = []) => (
    <Table
      rowKey={(row) =>
        String((row as { id?: string }).id ?? JSON.stringify(row))
      }
      columns={columns}
      dataSource={data}
      pagination={false}
    />
  );
  return (
    <section>
      <Typography.Title className="page-title">{t("title")}</Typography.Title>
      <Tabs
        items={[
          {
            key: "guardrail",
            label: t("guardrail"),
            children: (
              <Space direction="vertical" className="result-panel">
                {canWrite && (
                  <Card title={t("guardrail")}>
                    <Form
                      layout="vertical"
                      onFinish={(values) =>
                        publish.mutate(values as PublishInput)
                      }
                      initialValues={{
                        change: "tighten",
                        rules: [
                          {
                            id: "pii-redact",
                            kind: "regex",
                            pattern: "",
                            action: "redact",
                          },
                        ],
                      }}
                    >
                      <Form.Item
                        name="id"
                        label={t("policyId")}
                        rules={[{ required: true }]}
                      >
                        <Input />
                      </Form.Item>
                      <Form.Item name="change" label={t("change")}>
                        <Select
                          options={[
                            { value: "tighten", label: t("tighten") },
                            { value: "loosen", label: t("loosen") },
                          ]}
                        />
                      </Form.Item>
                      <Form.List name="rules">
                        {(fields, { add, remove }) => (
                          <Space direction="vertical" style={{ width: "100%" }}>
                            <Typography.Text strong>
                              {t("rules")}
                            </Typography.Text>
                            {fields.map((field) => (
                              <Card key={field.key} size="small">
                                <Space
                                  direction="vertical"
                                  style={{ width: "100%" }}
                                >
                                  <Form.Item
                                    {...field}
                                    name={[field.name, "id"]}
                                    label={t("ruleId")}
                                    rules={[{ required: true }]}
                                  >
                                    <Input />
                                  </Form.Item>
                                  <Form.Item
                                    {...field}
                                    name={[field.name, "kind"]}
                                    label={t("ruleKind")}
                                    rules={[{ required: true }]}
                                  >
                                    <Select
                                      options={[
                                        { value: "regex", label: t("regex") },
                                        {
                                          value: "builtin",
                                          label: t("builtin"),
                                        },
                                      ]}
                                    />
                                  </Form.Item>
                                  <Form.Item
                                    {...field}
                                    name={[field.name, "pattern"]}
                                    label={t("pattern")}
                                    rules={[{ required: true }]}
                                  >
                                    <Input />
                                  </Form.Item>
                                  <Form.Item
                                    {...field}
                                    name={[field.name, "action"]}
                                    label={t("ruleAction")}
                                    rules={[{ required: true }]}
                                  >
                                    <Select
                                      options={[
                                        { value: "block", label: t("block") },
                                        { value: "redact", label: t("redact") },
                                        { value: "allow", label: t("allow") },
                                      ]}
                                    />
                                  </Form.Item>
                                  <Form.Item
                                    {...field}
                                    name={[field.name, "replacement"]}
                                    label={t("replacement")}
                                  >
                                    <Input />
                                  </Form.Item>
                                  <Button
                                    danger
                                    onClick={() => remove(field.name)}
                                  >
                                    {t("removeRule")}
                                  </Button>
                                </Space>
                              </Card>
                            ))}
                            <Button
                              onClick={() =>
                                add({
                                  id: "",
                                  kind: "regex",
                                  pattern: "",
                                  action: "block",
                                })
                              }
                            >
                              {t("addRule")}
                            </Button>
                          </Space>
                        )}
                      </Form.List>
                      <Form.Item
                        name="reauth"
                        label={t("reauth")}
                        rules={[{ required: true }]}
                      >
                        <Input.Password autoComplete="off" />
                      </Form.Item>
                      <Button
                        htmlType="submit"
                        type="primary"
                        loading={publish.isPending}
                      >
                        {t("publish")}
                      </Button>
                    </Form>
                  </Card>
                )}
                <Card
                  title={t("advancedCheckpoints")}
                  loading={runtime.isLoading}
                >
                  <Space wrap size="large">
                    <Typography.Text>
                      {t("judgeCheckpoint")}:{" "}
                      {runtime.data?.guardrail?.judge?.enabled
                        ? t("judgeOn", {
                            model: runtime.data?.guardrail?.judge?.model ?? "-",
                            action:
                              runtime.data?.guardrail?.judge?.action || "block",
                          })
                        : t("off")}
                    </Typography.Text>
                    <Typography.Text>
                      {t("groundednessCheckpoint")}:{" "}
                      {runtime.data?.guardrail?.groundedness?.enabled
                        ? t("groundednessOn", {
                            overlap: String(
                              runtime.data?.guardrail?.groundedness
                                ?.minOverlap ?? 0.2,
                            ),
                          })
                        : t("off")}
                    </Typography.Text>
                  </Space>
                </Card>
                <Card title={t("testCases")}>
                  {t("playground")} · {t("impactPreview")} ·{" "}
                  {t("explicitSamples")}
                </Card>
                <Card
                  title={t("securityEvents")}
                  loading={securityEvents.isLoading}
                >
                  <Table
                    rowKey="id"
                    columns={eventColumns}
                    dataSource={securityEvents.data ?? []}
                    pagination={{ pageSize: 10 }}
                    locale={{ emptyText: t("noSensitiveData") }}
                  />
                </Card>
              </Space>
            ),
          },
          {
            key: "cache",
            label: t("cache"),
            children: (
              <Card loading={runtime.isLoading}>
                <Space>
                  <Switch checked={runtime.data?.cache?.enabled} disabled />
                  {t("cacheEnabled")}
                  <InputNumber
                    value={runtime.data?.cache?.ttlSeconds}
                    disabled
                  />
                  {t("cacheTTL")}
                  <InputNumber
                    value={runtime.data?.cache?.namespaceVersion}
                    disabled
                  />
                  {t("cacheNamespace")}
                </Space>
                <Typography.Paragraph>{t("cacheSavings")}</Typography.Paragraph>
                {runtime.data?.cache?.semantic?.enabled ? (
                  <Typography.Paragraph type="secondary">
                    {t("semanticCacheStatus", {
                      state: t("semanticOn"),
                      model: runtime.data?.cache?.semantic?.model ?? "-",
                      threshold: String(
                        runtime.data?.cache?.semantic?.threshold ?? 0.85,
                      ),
                    })}
                  </Typography.Paragraph>
                ) : (
                  <Typography.Paragraph type="secondary">
                    {t("semanticCache")}
                  </Typography.Paragraph>
                )}
                <Space wrap>
                  <Button
                    disabled={!canWrite || Boolean(cacheDraft.providerDraft)}
                    loading={cacheDraft.startProviderDraft.isPending}
                    onClick={() => cacheDraft.startProviderDraft.mutate()}
                  >
                    {t("resources.startDraft", { ns: "common" })}
                  </Button>
                  <Button
                    type="primary"
                    disabled={!canWrite || !cacheDraft.providerDraft}
                    loading={cacheDraft.publishProviderDraft.isPending}
                    onClick={() =>
                      cacheDraft.providerDraft &&
                      cacheDraft.publishProviderDraft.mutate(
                        cacheDraft.providerDraft,
                      )
                    }
                  >
                    {t("resources.publishDraft", { ns: "common" })}
                  </Button>
                  <Typography.Text type="secondary">
                    {cacheDraft.providerDraft
                      ? t("cacheDraftEditing")
                      : t("cacheDraftHelp")}
                  </Typography.Text>
                </Space>
                <Space wrap style={{ marginTop: 16 }}>
                  <Switch
                    checked={semanticCache?.enabled ?? false}
                    disabled={!canWrite || !cacheDraft.providerDraft}
                    onChange={(enabled) => updateSemanticCache({ enabled })}
                  />
                  {t("semanticEnabled")}
                  <Input
                    value={semanticCache?.model ?? ""}
                    disabled={!canWrite || !cacheDraft.providerDraft}
                    placeholder={t("semanticModel")}
                    style={{ width: 220 }}
                    onChange={(event) =>
                      updateSemanticCache({ model: event.target.value })
                    }
                  />
                  <InputNumber
                    value={semanticCache?.threshold ?? 0.85}
                    min={0}
                    max={1}
                    step={0.01}
                    disabled={!canWrite || !cacheDraft.providerDraft}
                    onChange={(threshold) =>
                      typeof threshold === "number" &&
                      updateSemanticCache({ threshold })
                    }
                  />
                  {t("semanticThreshold")}
                </Space>
              </Card>
            ),
          },
          {
            key: "budgets",
            label: t("budgets"),
            children: (
              <Card loading={runtime.isLoading}>
                <Typography.Paragraph type="secondary">
                  {t("budgetConsistencyHelp")}
                </Typography.Paragraph>
                <Table
                  rowKey="id"
                  columns={budgetColumns}
                  dataSource={runtime.data?.budgets ?? []}
                  pagination={false}
                  locale={{ emptyText: t("empty") }}
                />
              </Card>
            ),
          },
          {
            key: "mcp",
            label: t("mcpServers"),
            children: (
              <Tabs
                items={[
                  {
                    key: "servers",
                    label: t("mcpServers"),
                    children: emptyTable(runtime.data?.mcpServers),
                  },
                  {
                    key: "tools",
                    label: t("tools"),
                    children: emptyTable(runtime.data?.tools),
                  },
                  {
                    key: "policies",
                    label: t("toolPolicies"),
                    children: emptyTable(),
                  },
                  {
                    key: "calls",
                    label: t("toolCalls"),
                    children: (
                      <Table
                        rowKey="id"
                        columns={callColumns}
                        dataSource={toolCalls.data ?? []}
                        pagination={{ pageSize: 10 }}
                        loading={toolCalls.isLoading}
                        locale={{ emptyText: t("empty") }}
                      />
                    ),
                  },
                ]}
              />
            ),
          },
          {
            key: "simulator",
            label: t("simulator"),
            children: (
              <Card>
                {canWrite && (
                  <Form
                    layout="vertical"
                    onFinish={(values) =>
                      simulate.mutate(
                        values as {
                          model: string;
                          projectId: string;
                          metrics: Record<
                            string,
                            { latencyMS: number; cost: number; load: number }
                          >;
                        },
                      )
                    }
                  >
                    <Space>
                      <Form.Item
                        name="model"
                        label={t("model")}
                        rules={[{ required: true }]}
                      >
                        <Input style={{ width: 180 }} />
                      </Form.Item>
                      <Form.Item
                        name="projectId"
                        label={t("projectId")}
                        rules={[{ required: true }]}
                      >
                        <Input style={{ width: 180 }} />
                      </Form.Item>
                      <Form.Item
                        name={["metrics", "deployment-a", "latencyMS"]}
                        label={t("latency")}
                      >
                        <InputNumber min={0} />
                      </Form.Item>
                      <Form.Item
                        name={["metrics", "deployment-a", "cost"]}
                        label={t("cost")}
                      >
                        <InputNumber min={0} step={0.01} />
                      </Form.Item>
                      <Button
                        htmlType="submit"
                        type="primary"
                        loading={simulate.isPending}
                      >
                        {t("runSimulator")}
                      </Button>
                    </Space>
                  </Form>
                )}
                <Typography.Paragraph type="secondary">
                  {t("replayMode")}
                </Typography.Paragraph>
                {simulate.data && (
                  <Space direction="vertical" className="result-panel">
                    <Typography.Text>
                      {t("selectedDeployment")}: <b>{simulate.data.selected}</b>
                    </Typography.Text>
                    <Table
                      rowKey="deploymentId"
                      columns={evidenceColumns}
                      dataSource={simulate.data.evidence}
                      pagination={false}
                    />
                  </Space>
                )}
              </Card>
            ),
          },
          {
            key: "agentGraph",
            label: t("agentGraph"),
            children: (
              <Card loading={agentGraph.isLoading}>
                <Space>
                  <Typography.Text>{t("rootTask")}: root</Typography.Text>
                  <Button
                    type="primary"
                    onClick={() => void agentGraph.refetch()}
                  >
                    {t("runSimulator")}
                  </Button>
                </Space>
                {agentGraph.data && (
                  <Space direction="vertical" className="result-panel">
                    <Typography.Text>
                      {t("totalCost")}: {agentGraph.data.totalCost}
                    </Typography.Text>
                    <Table
                      rowKey="order"
                      columns={agentGraphColumns}
                      dataSource={agentGraph.data.hops}
                      pagination={{ pageSize: 10 }}
                      locale={{ emptyText: t("empty") }}
                    />
                  </Space>
                )}
              </Card>
            ),
          },
          {
            key: "delegations",
            label: t("delegations"),
            children: (
              <Space direction="vertical" className="result-panel">
                {canGrantDelegation && (
                  <Card title={t("grantDelegation")}>
                    <Form
                      layout="inline"
                      onFinish={(values) =>
                        grantDelegation.mutate(
                          values as {
                            delegatorId: string;
                            delegateeId: string;
                            permissions: string;
                          },
                        )
                      }
                    >
                      <Form.Item
                        name="delegatorId"
                        label={t("delegator")}
                        rules={[{ required: true }]}
                      >
                        <Input style={{ width: 180 }} />
                      </Form.Item>
                      <Form.Item
                        name="delegateeId"
                        label={t("delegatee")}
                        rules={[{ required: true }]}
                      >
                        <Input style={{ width: 180 }} />
                      </Form.Item>
                      <Form.Item
                        name="permissions"
                        label={t("permissions")}
                        rules={[{ required: true }]}
                      >
                        <Input
                          placeholder={t("permissionsPlaceholder")}
                          style={{ width: 260 }}
                        />
                      </Form.Item>
                      <Form.Item>
                        <Button
                          type="primary"
                          htmlType="submit"
                          loading={grantDelegation.isPending}
                        >
                          {t("grant")}
                        </Button>
                      </Form.Item>
                    </Form>
                  </Card>
                )}
                <Card title={t("delegations")} loading={delegations.isLoading}>
                  <Table
                    rowKey="id"
                    columns={delegationColumns}
                    dataSource={delegations.data ?? []}
                    pagination={{ pageSize: 10 }}
                    locale={{ emptyText: t("empty") }}
                  />
                </Card>
              </Space>
            ),
          },
          {
            key: "federation",
            label: t("federatedAgents"),
            children: (
              <Card loading={federation.isLoading}>
                {canSuspend && (
                  <Space
                    direction="vertical"
                    style={{ width: "100%" }}
                    size="middle"
                  >
                    <Typography.Title level={5}>
                      {t("discoverAgentCard")}
                    </Typography.Title>
                    <Form
                      layout="inline"
                      onFinish={(values: {
                        url: string;
                        name?: string;
                        pem?: string;
                      }) =>
                        discover.mutate({
                          url: values.url,
                          name: values.name ?? "",
                          verificationKeys: values.pem?.trim()
                            ? [{ alg: "RS256", pem: values.pem.trim() }]
                            : [],
                        })
                      }
                    >
                      <Form.Item
                        name="url"
                        label={t("agentCardUrl")}
                        rules={[{ required: true }]}
                      >
                        <Input
                          aria-label={t("agentCardUrl")}
                          placeholder={t("agentCardUrl")}
                          style={{ width: 300 }}
                        />
                      </Form.Item>
                      <Form.Item name="name" label={t("agentCardName")}>
                        <Input
                          aria-label={t("agentCardName")}
                          placeholder={t("agentCardName")}
                          style={{ width: 200 }}
                        />
                      </Form.Item>
                      <Form.Item name="pem" label={t("verificationKey")}>
                        <Input.TextArea
                          aria-label={t("verificationKey")}
                          placeholder={t("verificationKey")}
                          rows={2}
                          style={{ width: 320 }}
                        />
                      </Form.Item>
                      <Form.Item>
                        <Button
                          type="primary"
                          htmlType="submit"
                          loading={discover.isPending}
                        >
                          {t("discover")}
                        </Button>
                      </Form.Item>
                    </Form>
                  </Space>
                )}
                <Typography.Title level={5}>
                  {t("trustRelationships")}
                </Typography.Title>
                <Table
                  rowKey="id"
                  columns={relationshipColumns}
                  dataSource={federation.data?.relationships ?? []}
                  pagination={{ pageSize: 10 }}
                  locale={{ emptyText: t("empty") }}
                />
                <Typography.Title level={5}>
                  {t("federatedAgents")}
                </Typography.Title>
                <Table
                  rowKey="id"
                  columns={agentColumns}
                  dataSource={federation.data?.externalAgents ?? []}
                  pagination={{ pageSize: 10 }}
                  locale={{ emptyText: t("empty") }}
                />
              </Card>
            ),
          },
          {
            key: "approvals",
            label: t("approvals"),
            children: (
              <Card loading={approvals.isLoading}>
                <Table
                  rowKey="id"
                  columns={approvalColumns}
                  dataSource={approvals.data ?? []}
                  pagination={{ pageSize: 10 }}
                  locale={{ emptyText: t("empty") }}
                />
              </Card>
            ),
          },
          {
            key: "integrations",
            label: t("integrations"),
            children: <Card>{t("noSensitiveData")}</Card>,
          },
        ]}
      />
    </section>
  );
}
