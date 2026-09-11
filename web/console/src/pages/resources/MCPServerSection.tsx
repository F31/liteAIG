import { useState } from "react";
import {
  Button,
  Card,
  Drawer,
  Form,
  Input,
  message,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { newID } from "./helpers";
import { ResourceStatusTag } from "./ResourceStatusTag";
import type { DiscoveredTool, MCPServer, MCPServerFormValues } from "./types";
import type {
  DraftEditor,
  RowDraftStateLookup,
  RuntimeQuery,
} from "./sections";
import type { ResourceRows } from "./useResourceRows";

// MCPServerSection renders the MCP servers + tools tables, the discovered
// tools panel, and the MCP server editor drawer.
export function MCPServerSection({
  editor,
  rows,
  canWrite,
  rowDraftState,
  runtime,
}: {
  editor: DraftEditor;
  rows: ResourceRows;
  canWrite: boolean;
  rowDraftState: RowDraftStateLookup;
  runtime: RuntimeQuery;
}) {
  const { t } = useTranslation();
  const {
    providerDraft,
    startProviderDraft,
    loadDraft,
    persistDraft,
    updateConfig,
  } = editor;
  const [mcpServerForm] = Form.useForm<MCPServerFormValues>();
  const [mcpServerDrawerOpen, setMCPServerDrawerOpen] = useState(false);
  const [editingMCPServerID, setEditingMCPServerID] = useState<string>();
  const [discoveredServerID, setDiscoveredServerID] = useState<string>();
  const [discoveredTools, setDiscoveredTools] = useState<DiscoveredTool[]>([]);

  const discoverMCPTools = useMutation({
    mutationFn: (id: string) =>
      api<DiscoveredTool[]>(
        `/api/admin/mcp-servers/${encodeURIComponent(id)}/discover`,
        { method: "POST" },
      ),
    onSuccess: (tools, id) => {
      setDiscoveredServerID(id);
      setDiscoveredTools(tools);
      void message.success(t("resources.toolsDiscovered"));
    },
  });

  const openMCPServerDrawer = async (server?: MCPServer) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
      loadDraft(draft);
    }
    const item = server?.id
      ? (draft.Config.mcp_servers ?? []).find(
          (candidate) => candidate.id === server.id,
        )
      : undefined;
    setEditingMCPServerID(item?.id);
    mcpServerForm.setFieldsValue({
      id: item?.id ?? newID("mcp"),
      url: item?.url ?? "http://localhost:19093/mcp",
      status: item?.status ?? "active",
    });
    setMCPServerDrawerOpen(true);
  };

  const saveMCPServer = (values: MCPServerFormValues) => {
    if (!providerDraft) return;
    const servers = providerDraft.Config.mcp_servers ?? [];
    const next: MCPServer = {
      id: values.id,
      tenant_id: providerDraft.Config.tenant?.id,
      url: values.url,
      status: values.status,
    };
    const exists = servers.some((item) => item.id === values.id);
    updateConfig((config) => ({
      ...config,
      mcp_servers: exists
        ? servers.map((item) =>
            item.id === values.id ? { ...item, ...next } : item,
          )
        : [...servers, next],
    }));
    setMCPServerDrawerOpen(false);
    setEditingMCPServerID(undefined);
  };

  const disableMCPServer = async (server: MCPServer) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
      loadDraft(draft);
    }
    persistDraft({
      ...draft,
      Config: {
        ...draft.Config,
        mcp_servers: (draft.Config.mcp_servers ?? []).map((item) =>
          item.id === server.id ? { ...item, status: "disabled" } : item,
        ),
      },
    });
  };

  const deleteMCPServer = async (server: MCPServer) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
    }
    const inUse = (draft.Config.tools ?? []).some(
      (tool) => tool.server_id === server.id,
    );
    if (inUse) {
      void message.error(t("resources.mcpServerInUse"));
      return;
    }
    persistDraft({
      ...draft,
      Config: {
        ...draft.Config,
        mcp_servers: (draft.Config.mcp_servers ?? []).filter(
          (item) => item.id !== server.id,
        ),
      },
    });
  };

  const applyDiscoveredTools = () => {
    if (!providerDraft || !discoveredServerID) return;
    const existing = providerDraft.Config.tools ?? [];
    const tenantID = providerDraft.Config.tenant?.id;
    const nextTools = discoveredTools.map((tool) => ({
      id: `${discoveredServerID}-${tool.name}`.replace(/[^A-Za-z0-9_-]/g, "-"),
      tenant_id: tenantID,
      server_id: discoveredServerID,
      name: tool.name,
      status: "active",
      schema: tool.inputSchema,
    }));
    updateConfig((config) => ({
      ...config,
      tools: [
        ...existing.filter(
          (tool) => !nextTools.some((candidate) => candidate.id === tool.id),
        ),
        ...nextTools,
      ],
    }));
    void message.success(t("resources.toolsApplied"));
  };

  const mcpServerColumns = [
    { title: t("resources.identifier"), dataIndex: "id", key: "id" },
    { title: t("resources.url"), dataIndex: "url", key: "url" },
    {
      title: t("common.status"),
      dataIndex: "status",
      key: "status",
      render: (_: string, row: MCPServer) => (
        <ResourceStatusTag
          resource="mcpServers"
          row={row}
          rowDraftState={rowDraftState}
        />
      ),
    },
    {
      title: t("resources.actions"),
      key: "actions",
      render: (_: unknown, row: MCPServer) =>
        canWrite ? (
          rowDraftState("mcpServers", row) === "deleted" ? (
            <Typography.Text type="secondary">
              {t("resources.draftPendingDeletionAction")}
            </Typography.Text>
          ) : (
            <Space>
              <Button
                size="small"
                onClick={() => void openMCPServerDrawer(row)}
              >
                {t("resources.edit")}
              </Button>
              <Button size="small" onClick={() => void disableMCPServer(row)}>
                {t("resources.disable")}
              </Button>
              <Button
                size="small"
                loading={discoverMCPTools.isPending}
                onClick={() => discoverMCPTools.mutate(row.id)}
              >
                {t("resources.discoverTools")}
              </Button>
              <Popconfirm
                title={t("resources.deleteMCPServer")}
                onConfirm={() => void deleteMCPServer(row)}
              >
                <Button size="small" danger>
                  {t("resources.delete")}
                </Button>
              </Popconfirm>
            </Space>
          )
        ) : null,
    },
  ];
  const toolColumns = [
    { title: t("resources.identifier"), dataIndex: "id", key: "id" },
    { title: t("resources.name"), dataIndex: "name", key: "name" },
    {
      title: t("resources.serverId"),
      dataIndex: "server_id",
      key: "server_id",
      render: (value?: string) => value || "-",
    },
    {
      title: t("common.status"),
      dataIndex: "status",
      key: "status",
      render: (value: string) => (
        <Tag color={value === "active" ? "green" : "default"}>{value}</Tag>
      ),
    },
  ];

  return (
    <Card>
      {canWrite && (
        <Space wrap>
          <Button type="primary" onClick={() => void openMCPServerDrawer()}>
            {t("resources.createMCPServer")}
          </Button>
          <Button
            disabled={!providerDraft || !discoveredTools.length}
            onClick={applyDiscoveredTools}
          >
            {t("resources.applyDiscoveredTools")}
          </Button>
        </Space>
      )}
      {discoveredTools.length ? (
        <Card className="result-panel" size="small">
          <Typography.Text strong>
            {t("resources.discoveredTools")}
          </Typography.Text>
          <Table
            rowKey="name"
            size="small"
            columns={[{ title: t("resources.name"), dataIndex: "name" }]}
            dataSource={discoveredTools}
            pagination={false}
          />
        </Card>
      ) : null}
      <Typography.Title level={5}>{t("resources.mcpServers")}</Typography.Title>
      <Table
        className="result-panel"
        rowKey="id"
        columns={mcpServerColumns}
        dataSource={rows.mcpServerRows}
        pagination={{ pageSize: 20 }}
        loading={runtime.isLoading}
        locale={{ emptyText: t("common.empty") }}
      />
      <Typography.Title level={5}>{t("resources.tools")}</Typography.Title>
      <Table
        className="result-panel"
        rowKey="id"
        columns={toolColumns}
        dataSource={rows.toolRows}
        pagination={{ pageSize: 20 }}
        loading={runtime.isLoading}
        locale={{ emptyText: t("common.empty") }}
      />
      <Drawer
        title={
          editingMCPServerID
            ? t("resources.editMCPServer")
            : t("resources.createMCPServer")
        }
        open={mcpServerDrawerOpen}
        onClose={() => setMCPServerDrawerOpen(false)}
        destroyOnHidden
      >
        <Form<MCPServerFormValues>
          form={mcpServerForm}
          layout="vertical"
          onFinish={saveMCPServer}
        >
          <Form.Item
            name="id"
            label={t("resources.identifier")}
            rules={[{ required: true }]}
          >
            <Input disabled={Boolean(editingMCPServerID)} />
          </Form.Item>
          <Form.Item
            name="url"
            label={t("resources.url")}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="status"
            label={t("common.status")}
            rules={[{ required: true }]}
          >
            <Select
              options={[
                { value: "active", label: t("common.active") },
                { value: "disabled", label: t("resources.disabled") },
              ]}
            />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">
              {t("resources.applyToDraft")}
            </Button>
            <Button onClick={() => setMCPServerDrawerOpen(false)}>
              {t("common.close")}
            </Button>
          </Space>
        </Form>
      </Drawer>
    </Card>
  );
}
