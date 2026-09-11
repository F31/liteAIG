import { useMemo, useState } from "react";
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
  Tooltip,
  Typography,
} from "antd";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, APIError } from "../../api/client";
import { newID } from "./helpers";
import { providerEndpointDefaults, providerTypeOptions } from "./constants";
import { ResourceStatusTag } from "./ResourceStatusTag";
import type {
  Credential,
  CredentialFormValues,
  Provider,
  ProviderFormValues,
  RotateCredentialValues,
  TenantConfig,
} from "./types";
import type {
  DraftEditor,
  ResourceCatalogQuery,
  RowDraftStateLookup,
  RuntimeQuery,
} from "./sections";
import type { ResourceRows } from "./useResourceRows";

// ProviderSection renders the providers table (with per-provider credential
// sub-table), the provider editor drawer, and the credential create / rotate
// drawers.
export function ProviderSection({
  editor,
  rows,
  canWrite,
  rowDraftState,
  runtime,
  resourceCatalog,
}: {
  editor: DraftEditor;
  rows: ResourceRows;
  canWrite: boolean;
  rowDraftState: RowDraftStateLookup;
  runtime: RuntimeQuery;
  resourceCatalog: ResourceCatalogQuery;
}) {
  const { t } = useTranslation();
  const {
    providerDraft,
    startProviderDraft,
    loadDraft,
    persistDraft,
    updateConfig,
  } = editor;
  const [providerForm] = Form.useForm<ProviderFormValues>();
  const [credentialForm] = Form.useForm<CredentialFormValues>();
  const [rotateForm] = Form.useForm<RotateCredentialValues>();
  const [providerDrawerOpen, setProviderDrawerOpen] = useState(false);
  const [editingProviderID, setEditingProviderID] = useState<string>();
  const [credentialDrawerOpen, setCredentialDrawerOpen] = useState(false);
  const [credentialID, setCredentialID] = useState<string>();
  const runtimeProviderIDs = useMemo(
    () => new Set(rows.runtimeProviders.map((provider) => provider.id)),
    [rows.runtimeProviders],
  );

  const rotateCredential = useMutation({
    mutationFn: (values: RotateCredentialValues) =>
      api(
        `/api/admin/credentials/${encodeURIComponent(credentialID!)}/rotate`,
        {
          method: "POST",
          body: JSON.stringify(values),
        },
      ),
    onSuccess: () => {
      setCredentialID(undefined);
      rotateForm.resetFields();
      void runtime.refetch();
      void message.success(t("resources.credentialRotated"));
    },
  });
  const createCredential = useMutation({
    mutationFn: (values: CredentialFormValues) =>
      api("/api/admin/credentials", {
        method: "POST",
        body: JSON.stringify(values),
      }),
    onSuccess: () => {
      setCredentialDrawerOpen(false);
      credentialForm.resetFields();
      void runtime.refetch();
      void message.success(t("resources.credentialCreated"));
    },
    onError: (error) => {
      if (error instanceof APIError && error.code === "NOT_FOUND") {
        credentialForm.setFields([
          {
            name: "providerId",
            errors: [t("resources.credentialProviderNotFound")],
          },
        ]);
        return;
      }
      void message.error(t("common.error"));
    },
  });
  const disableCredential = useMutation({
    mutationFn: (id: string) =>
      api(`/api/admin/credentials/${encodeURIComponent(id)}/disable`, {
        method: "POST",
      }),
    onSuccess: () => {
      void runtime.refetch();
      void message.success(t("resources.credentialDisabled"));
    },
  });
  const deleteCredential = useMutation({
    mutationFn: (id: string) =>
      api(`/api/admin/credentials/${encodeURIComponent(id)}`, {
        method: "DELETE",
      }),
    onSuccess: () => {
      void runtime.refetch();
      void message.success(t("resources.credentialDeleted"));
    },
  });

  const openCredentialDrawer = (providerID?: string) => {
    if (providerID && !runtimeProviderIDs.has(providerID)) {
      credentialForm.setFields([
        {
          name: "providerId",
          errors: [t("resources.publishProviderBeforeCredential")],
        },
      ]);
      return;
    }
    credentialForm.setFieldsValue({
      providerId: providerID ?? rows.runtimeProviders[0]?.id,
      status: "enabled",
    });
    setCredentialDrawerOpen(true);
  };

  const openProviderDrawer = async (provider?: Provider) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
      loadDraft(draft);
    }
    const item = provider?.id
      ? (draft.Config.providers ?? []).find(
          (candidate) => candidate.id === provider.id,
        )
      : undefined;
    setEditingProviderID(item?.id);
    providerForm.setFieldsValue({
      id: item?.id ?? provider?.id ?? newID("provider"),
      type: item?.type ?? provider?.type ?? "openai-compatible",
      endpoint: item?.endpoint ?? provider?.endpoint ?? "",
      status: item?.status ?? "enabled",
    });
    setProviderDrawerOpen(true);
  };

  const saveProvider = (values: ProviderFormValues) => {
    if (!providerDraft) return;
    const tenantID = providerDraft.Config.tenant?.id;
    const providers = providerDraft.Config.providers ?? [];
    const next: Provider = {
      id: values.id,
      tenant_id: tenantID,
      owner_scope: "TENANT_PRIVATE",
      type: values.type,
      endpoint: values.endpoint,
      status: values.status,
    };
    const exists = providers.some((provider) => provider.id === values.id);
    updateConfig((config) => ({
      ...config,
      providers: exists
        ? providers.map((provider) =>
            provider.id === values.id ? { ...provider, ...next } : provider,
          )
        : [...providers, next],
    }));
    setProviderDrawerOpen(false);
    setEditingProviderID(undefined);
  };

  const disableProvider = async (provider: Provider) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
      loadDraft(draft);
    }
    const providers = draft.Config.providers ?? [];
    persistDraft({
      ...draft,
      Config: {
        ...draft.Config,
        providers: providers.map((item) =>
          item.id === provider.id ? { ...item, status: "disabled" } : item,
        ),
      },
    });
  };

  const deleteProvider = async (provider: Provider) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
    }
    const inUse =
      [
        ...(draft.Config.credentials ?? []),
        ...(draft.Config.deployments ?? []),
      ].some((item) => item.provider_id === provider.id) ||
      rows.runtimeCredentials.some((item) => item.providerId === provider.id);
    if (inUse) {
      void message.error(t("resources.providerInUse"));
      return;
    }
    persistDraft({
      ...draft,
      Config: {
        ...draft.Config,
        providers: (draft.Config.providers ?? []).filter(
          (item) => item.id !== provider.id,
        ),
      },
    });
  };

  const providerColumns = [
    { title: t("resources.identifier"), dataIndex: "id", key: "id" },
    { title: t("resources.type"), dataIndex: "type", key: "type" },
    {
      title: t("resources.endpoint"),
      dataIndex: "endpoint",
      key: "endpoint",
      render: (value?: string) => value || "-",
    },
    {
      title: t("common.status"),
      dataIndex: "status",
      key: "status",
      render: (_: string, row: Provider) => (
        <ResourceStatusTag
          resource="providers"
          row={row}
          rowDraftState={rowDraftState}
        />
      ),
    },
    {
      title: t("resources.credentials"),
      key: "credentialCount",
      render: (_: unknown, row: Provider) =>
        rows.runtimeCredentials.filter((c) => c.providerId === row.id).length ||
        "-",
    },
    {
      title: t("resources.actions"),
      key: "actions",
      render: (_: unknown, row: Provider) =>
        canWrite ? (
          rowDraftState("providers", row) === "deleted" ? (
            <Typography.Text type="secondary">
              {t("resources.draftPendingDeletionAction")}
            </Typography.Text>
          ) : (
            <Space>
              <Button size="small" onClick={() => void openProviderDrawer(row)}>
                {row.status === "catalog"
                  ? t("resources.addToDraft")
                  : t("resources.edit")}
              </Button>
              {row.status !== "catalog" && (
                <>
                  <Button
                    size="small"
                    onClick={() => void disableProvider(row)}
                  >
                    {t("resources.disable")}
                  </Button>
                  <Popconfirm
                    title={t("resources.deleteProvider")}
                    onConfirm={() => void deleteProvider(row)}
                  >
                    <Button size="small" danger>
                      {t("resources.delete")}
                    </Button>
                  </Popconfirm>
                </>
              )}
            </Space>
          )
        ) : null,
    },
  ];
  const credentialColumns = [
    { title: t("resources.identifier"), dataIndex: "id", key: "id" },
    {
      title: t("resources.providerId"),
      dataIndex: "providerId",
      key: "providerId",
    },
    {
      title: t("resources.fingerprint"),
      dataIndex: "fingerprint",
      key: "fingerprint",
    },
    {
      title: t("common.status"),
      dataIndex: "status",
      key: "status",
      render: (_: string, row: Credential) => (
        <ResourceStatusTag
          resource="credentials"
          row={row}
          rowDraftState={rowDraftState}
        />
      ),
    },
    {
      title: t("resources.actions"),
      key: "actions",
      render: (_: unknown, row: Credential) =>
        canWrite ? (
          <Space>
            <Button size="small" onClick={() => setCredentialID(row.id)}>
              {t("resources.rotate")}
            </Button>
            <Popconfirm
              title={t("resources.disableCredential")}
              onConfirm={() => disableCredential.mutate(row.id)}
            >
              <Button size="small" danger loading={disableCredential.isPending}>
                {t("resources.disable")}
              </Button>
            </Popconfirm>
            <Popconfirm
              title={t("resources.deleteCredential")}
              onConfirm={() => deleteCredential.mutate(row.id)}
            >
              <Button size="small" danger loading={deleteCredential.isPending}>
                {t("resources.delete")}
              </Button>
            </Popconfirm>
          </Space>
        ) : null,
    },
  ];
  const credentialSubColumns = credentialColumns.filter(
    (column) => column.key !== "providerId",
  );

  return (
    <Card>
      {canWrite && (
        <Space wrap>
          <Button type="primary" onClick={() => void openProviderDrawer()}>
            {t("resources.createProvider")}
          </Button>
          <Button
            disabled={rows.runtimeProviders.length === 0}
            onClick={() => openCredentialDrawer()}
          >
            {t("resources.createCredential")}
          </Button>
          {rows.runtimeProviders.length === 0 && (
            <Typography.Text type="secondary">
              {t("resources.createProviderFirst")}
            </Typography.Text>
          )}
        </Space>
      )}
      <Table
        className="result-panel"
        rowKey="id"
        columns={providerColumns}
        dataSource={rows.providerRows}
        pagination={{ pageSize: 20 }}
        loading={runtime.isLoading || resourceCatalog.isLoading}
        locale={{ emptyText: t("common.empty") }}
        expandable={{
          rowExpandable: (provider: Provider) => provider.status !== "catalog",
          expandedRowRender: (provider: Provider) => (
            <div style={{ padding: "4px 8px 8px" }}>
              <Space style={{ marginBottom: 8 }}>
                {canWrite && (
                  <Tooltip
                    title={
                      !runtimeProviderIDs.has(provider.id) ||
                      rowDraftState("providers", provider) === "deleted"
                        ? t("resources.publishProviderBeforeCredential")
                        : undefined
                    }
                  >
                    <span>
                      <Button
                        size="small"
                        type="primary"
                        disabled={
                          !runtimeProviderIDs.has(provider.id) ||
                          rowDraftState("providers", provider) === "deleted"
                        }
                        onClick={() => openCredentialDrawer(provider.id)}
                      >
                        {t("resources.createCredential")}
                      </Button>
                    </span>
                  </Tooltip>
                )}
              </Space>
              <Table
                size="small"
                rowKey="id"
                columns={credentialSubColumns}
                dataSource={rows.runtimeCredentials.filter(
                  (credential) => credential.providerId === provider.id,
                )}
                pagination={false}
                loading={runtime.isLoading}
                locale={{ emptyText: t("common.empty") }}
              />
            </div>
          ),
        }}
      />
      <Drawer
        title={
          editingProviderID
            ? t("resources.editProvider")
            : t("resources.createProvider")
        }
        open={providerDrawerOpen}
        onClose={() => setProviderDrawerOpen(false)}
        destroyOnHidden
      >
        <Form<ProviderFormValues>
          form={providerForm}
          layout="vertical"
          onFinish={saveProvider}
        >
          <Form.Item label={t("resources.providerPreset")}>
            <Select
              allowClear
              options={rows.providerPresets.map((preset) => ({
                value: preset.id,
                label: preset.label,
              }))}
              onChange={(value?: string) => {
                const preset = rows.providerPresets.find(
                  (candidate) => candidate.id === value,
                );
                if (!preset) return;
                providerForm.setFieldsValue({
                  id: editingProviderID
                    ? providerForm.getFieldValue("id")
                    : `provider-${preset.id}`,
                  type: preset.type,
                  endpoint: preset.endpoint,
                  status: "enabled",
                });
              }}
            />
          </Form.Item>
          <Form.Item
            name="id"
            label={t("resources.identifier")}
            rules={[{ required: true }]}
          >
            <Input disabled={Boolean(editingProviderID)} />
          </Form.Item>
          <Form.Item
            name="type"
            label={t("resources.type")}
            rules={[{ required: true }]}
          >
            <Select
              options={providerTypeOptions}
              onChange={(value) => {
                const endpoint = providerEndpointDefaults[value];
                if (endpoint) providerForm.setFieldValue("endpoint", endpoint);
              }}
            />
          </Form.Item>
          <Form.Item name="endpoint" label={t("resources.endpoint")}>
            <Input />
          </Form.Item>
          <Form.Item
            name="status"
            label={t("common.status")}
            rules={[{ required: true }]}
          >
            <Select
              options={[
                { value: "enabled", label: t("common.active") },
                { value: "disabled", label: t("resources.disabled") },
              ]}
            />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">
              {t("resources.applyToDraft")}
            </Button>
            <Button onClick={() => setProviderDrawerOpen(false)}>
              {t("common.close")}
            </Button>
          </Space>
        </Form>
      </Drawer>
      <Drawer
        title={t("resources.createCredential")}
        open={credentialDrawerOpen}
        onClose={() => setCredentialDrawerOpen(false)}
        destroyOnHidden
      >
        <Form<CredentialFormValues>
          form={credentialForm}
          layout="vertical"
          onFinish={(values) => createCredential.mutate(values)}
        >
          <Form.Item
            name="providerId"
            label={t("resources.providerId")}
            rules={[{ required: true }]}
          >
            <Select
              options={rows.runtimeProviders.map((provider) => ({
                value: provider.id,
                label: provider.id,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="secret"
            label={t("resources.newSecret")}
            rules={[{ required: true }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="status"
            label={t("common.status")}
            initialValue="enabled"
            rules={[{ required: true }]}
          >
            <Select
              options={[
                { value: "enabled", label: t("common.active") },
                { value: "disabled", label: t("resources.disabled") },
              ]}
            />
          </Form.Item>
          <Space>
            <Button
              type="primary"
              htmlType="submit"
              loading={createCredential.isPending}
            >
              {t("resources.createCredential")}
            </Button>
            <Button onClick={() => setCredentialDrawerOpen(false)}>
              {t("common.close")}
            </Button>
          </Space>
        </Form>
      </Drawer>
      <Drawer
        title={t("resources.rotateCredential")}
        open={Boolean(credentialID)}
        onClose={() => setCredentialID(undefined)}
        destroyOnHidden
      >
        <Form<RotateCredentialValues>
          form={rotateForm}
          layout="vertical"
          onFinish={(values) => rotateCredential.mutate(values)}
        >
          <Form.Item
            name="secret"
            label={t("resources.newSecret")}
            rules={[{ required: true }]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Space>
            <Button
              type="primary"
              htmlType="submit"
              loading={rotateCredential.isPending}
            >
              {t("resources.rotate")}
            </Button>
            <Button onClick={() => setCredentialID(undefined)}>
              {t("common.close")}
            </Button>
          </Space>
        </Form>
      </Drawer>
    </Card>
  );
}
