import { useState } from "react";
import {
  Button,
  Card,
  Drawer,
  Form,
  Input,
  message,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { APIError, api } from "../../api/client";
import { shortID } from "./helpers";
import type { APIKey, APIKeyCreateResult, APIKeyFormValues } from "./types";
import type { ProjectsQuery, RuntimeQuery } from "./sections";
import type { ResourceRows } from "./useResourceRows";
import { useAuth } from "../../state/auth";

// KeySection renders the access keys table, the one-time key reveal panel,
// and the key creation drawer.
export function KeySection({
  canWrite,
  runtime,
  projects,
  rows,
}: {
  canWrite: boolean;
  runtime: RuntimeQuery;
  projects: ProjectsQuery;
  rows: ResourceRows;
}) {
  const { t } = useTranslation();
  const authMethod = useAuth((state) => state.authMethod);
  const [keyForm] = Form.useForm<APIKeyFormValues>();
  const [keyDrawerOpen, setKeyDrawerOpen] = useState(false);
  const [createdKey, setCreatedKey] = useState<string>();
  const [revokeTarget, setRevokeTarget] = useState<string>();
  const [revokePassword, setRevokePassword] = useState("");
  const keys = useQuery({
    queryKey: ["admin-keys"],
    queryFn: () => api<APIKey[]>("/api/admin/keys"),
  });

  const createKey = useMutation({
    mutationFn: (values: APIKeyFormValues) =>
      api<APIKeyCreateResult>("/api/admin/keys", {
        method: "POST",
        body: JSON.stringify({
          Name: values.name,
          ProjectID: values.projectId,
          ModelAllowlist: values.modelAllowlist ?? [],
          IPAllowlist: values.ipAllowlist ?? [],
        }),
      }),
    onSuccess: (result) => {
      setCreatedKey(result.Key);
      keyForm.resetFields();
      setKeyDrawerOpen(false);
      void runtime.refetch();
      void keys.refetch();
      void message.success(t("resources.keyCreated"));
    },
  });
  const revealKey = useMutation({
    mutationFn: (id: string) =>
      api<{ key: string }>(`/api/admin/keys/${encodeURIComponent(id)}`),
    onSuccess: (result) => {
      setCreatedKey(result.key);
    },
  });
  const revokeKey = useMutation({
    mutationFn: (input: { id: string; password: string }) =>
      api(`/api/admin/keys/${encodeURIComponent(input.id)}/revoke`, {
        method: "POST",
        headers: { "X-Reauth-Token": input.password },
      }),
    onSuccess: () => {
      void runtime.refetch();
      void keys.refetch();
      void message.success(t("resources.keyRevoked"));
    },
    onError: (error: unknown) =>
      void message.error(
        error instanceof APIError
          ? t(`errors.${error.code}`)
          : t("common.error"),
      ),
  });

  const keyColumns = [
    { title: t("resources.name"), dataIndex: "name", key: "name" },
    {
      title: t("resources.keyProject"),
      dataIndex: "projectId",
      key: "projectId",
      render: (value: string) => (
        <Typography.Text copyable={{ text: value }}>
          {shortID(value)}
        </Typography.Text>
      ),
    },
    {
      title: t("resources.fingerprint"),
      dataIndex: "fingerprint",
      key: "fingerprint",
      render: (value: string) => (
        <Typography.Text code>{value}</Typography.Text>
      ),
    },
    {
      title: t("common.status"),
      dataIndex: "status",
      key: "status",
      render: (value: string) => (
        <Tag color={value === "active" ? "green" : "default"}>{value}</Tag>
      ),
    },
    {
      title: t("resources.keyCreatedAt"),
      dataIndex: "createdAt",
      key: "createdAt",
      render: (value: string) =>
        value ? new Date(value).toLocaleString() : "-",
    },
    {
      title: t("resources.actions"),
      key: "actions",
      render: (_: unknown, row: APIKey) =>
        canWrite ? (
          <Space>
            <Tooltip
              title={
                row.revealable ? undefined : t("resources.keyNotRevealable")
              }
            >
              <Button
                size="small"
                loading={revealKey.isPending}
                disabled={!row.revealable}
                onClick={() => revealKey.mutate(row.id)}
              >
                {t("resources.viewKey")}
              </Button>
            </Tooltip>
            {row.status === "active" && (
              <Button
                size="small"
                danger
                loading={revokeKey.isPending}
                disabled={authMethod === "oidc"}
                title={
                  authMethod === "oidc"
                    ? t("errors.REAUTH_UNAVAILABLE")
                    : undefined
                }
                onClick={() => setRevokeTarget(row.id)}
              >
                {t("resources.revoke")}
              </Button>
            )}
          </Space>
        ) : null,
    },
  ];

  return (
    <Card>
      {canWrite && (
        <Space wrap>
          <Button type="primary" onClick={() => setKeyDrawerOpen(true)}>
            {t("resources.createKey")}
          </Button>
        </Space>
      )}
      {createdKey ? (
        <Card className="result-panel" size="small">
          <Space direction="vertical" style={{ width: "100%" }} size={4}>
            <Typography.Text strong>
              {t("resources.keyRevealed")}
            </Typography.Text>
            <Typography.Paragraph
              copyable={{ text: createdKey }}
              style={{ marginBottom: 0 }}
            >
              {createdKey}
            </Typography.Paragraph>
          </Space>
        </Card>
      ) : null}
      <Table
        className="result-panel"
        rowKey="id"
        columns={keyColumns}
        dataSource={keys.data ?? []}
        pagination={{ pageSize: 20 }}
        loading={keys.isLoading}
        locale={{ emptyText: t("common.empty") }}
      />
      <Drawer
        title={t("resources.createKey")}
        open={keyDrawerOpen}
        onClose={() => setKeyDrawerOpen(false)}
        destroyOnHidden
      >
        <Form<APIKeyFormValues>
          form={keyForm}
          layout="vertical"
          onFinish={(values) => createKey.mutate(values)}
        >
          <Form.Item
            name="name"
            label={t("resources.name")}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="projectId"
            label={t("resources.projectId")}
            rules={[{ required: true }]}
          >
            <Select
              options={(projects.data ?? []).map((project) => ({
                value: project.id,
                label: project.name || project.id,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="modelAllowlist"
            label={t("resources.modelAllowlist")}
          >
            <Select
              mode="tags"
              options={rows.logicalModelRows.map((model) => ({
                value: model.alias,
                label: model.alias,
              }))}
            />
          </Form.Item>
          <Form.Item name="ipAllowlist" label={t("resources.ipAllowlist")}>
            <Select mode="tags" />
          </Form.Item>
          <Space>
            <Button
              type="primary"
              htmlType="submit"
              loading={createKey.isPending}
            >
              {t("resources.createKey")}
            </Button>
            <Button onClick={() => setKeyDrawerOpen(false)}>
              {t("common.close")}
            </Button>
          </Space>
        </Form>
      </Drawer>
      <Modal
        title={t("resources.revokeKey")}
        open={Boolean(revokeTarget)}
        confirmLoading={revokeKey.isPending}
        okButtonProps={{ disabled: !revokePassword }}
        onCancel={() => {
          setRevokeTarget(undefined);
          setRevokePassword("");
        }}
        onOk={async () => {
          if (!revokeTarget || !revokePassword) return;
          await revokeKey.mutateAsync({
            id: revokeTarget,
            password: revokePassword,
          });
          setRevokeTarget(undefined);
          setRevokePassword("");
        }}
      >
        <Input.Password
          autoComplete="current-password"
          placeholder={t("governance:reauth")}
          value={revokePassword}
          onChange={(event) => setRevokePassword(event.target.value)}
        />
      </Modal>
    </Card>
  );
}
