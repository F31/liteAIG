import { useEffect, useState } from "react";
import {
  AutoComplete,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { useTranslation } from "react-i18next";
import { newID } from "./helpers";
import type { Deployment, DeploymentFormValues } from "./types";
import type {
  DraftEditor,
  RowDraftStateLookup,
  RuntimeQuery,
} from "./sections";
import type { ResourceRows } from "./useResourceRows";

// DeploymentSection renders the deployments table and the deployment editor
// drawer, including the provider-scoped credential options.
export function DeploymentSection({
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
  const [deploymentForm] = Form.useForm<DeploymentFormValues>();
  const [deploymentDrawerOpen, setDeploymentDrawerOpen] = useState(false);
  const [editingDeploymentID, setEditingDeploymentID] = useState<string>();
  const selectedDeploymentProviderID = Form.useWatch(
    "provider_id",
    deploymentForm,
  ) as string | undefined;
  const deploymentCredentialOptions = rows.runtimeCredentials
    .filter(
      (credential) => credential.providerId === selectedDeploymentProviderID,
    )
    .map((credential) => ({
      value: credential.id,
      label: `${credential.id} · ${credential.fingerprint}`,
    }));

  useEffect(() => {
    if (!deploymentDrawerOpen || !selectedDeploymentProviderID) return;
    const selectedCredentialID = deploymentForm.getFieldValue(
      "credential_id",
    ) as string | undefined;
    if (!selectedCredentialID) return;
    const selectedCredential = rows.runtimeCredentials.find(
      (credential) => credential.id === selectedCredentialID,
    );
    if (selectedCredential?.providerId !== selectedDeploymentProviderID) {
      deploymentForm.setFieldValue("credential_id", undefined);
    }
  }, [
    deploymentDrawerOpen,
    deploymentForm,
    rows.runtimeCredentials,
    selectedDeploymentProviderID,
  ]);

  const openDeploymentDrawer = async (deployment?: Deployment) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
      loadDraft(draft);
    }
    const item = deployment?.id
      ? (draft.Config.deployments ?? []).find(
          (candidate) => candidate.id === deployment.id,
        )
      : undefined;
    setEditingDeploymentID(item?.id);
    deploymentForm.setFieldsValue({
      id: item?.id ?? newID("deployment"),
      provider_id: item?.provider_id ?? draft.Config.providers?.[0]?.id ?? "",
      credential_id: item?.credential_id ?? undefined,
      upstream_model: item?.upstream_model ?? "gpt-4o-mini",
      data_region: item?.data_region ?? "global",
      capabilities: item?.capabilities ?? ["chat"],
      context_window: item?.context_window,
      priority: item?.priority ?? 100,
      status: item?.status ?? "enabled",
    });
    setDeploymentDrawerOpen(true);
  };

  const saveDeployment = (values: DeploymentFormValues) => {
    if (!providerDraft) return;
    const deployments = providerDraft.Config.deployments ?? [];
    const next: Deployment = {
      id: values.id,
      tenant_id: providerDraft.Config.tenant?.id,
      provider_id: values.provider_id,
      credential_id: values.credential_id,
      upstream_model: values.upstream_model,
      data_region: values.data_region,
      capabilities: values.capabilities ?? [],
      context_window: values.context_window,
      priority: values.priority,
      status: values.status,
    };
    const exists = deployments.some((item) => item.id === values.id);
    updateConfig((config) => ({
      ...config,
      deployments: exists
        ? deployments.map((item) =>
            item.id === values.id ? { ...item, ...next } : item,
          )
        : [...deployments, next],
    }));
    setDeploymentDrawerOpen(false);
    setEditingDeploymentID(undefined);
  };

  const disableDeployment = async (deployment: Deployment) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
      loadDraft(draft);
    }
    persistDraft({
      ...draft,
      Config: {
        ...draft.Config,
        deployments: (draft.Config.deployments ?? []).map((item) =>
          item.id === deployment.id ? { ...item, status: "disabled" } : item,
        ),
      },
    });
  };

  const deleteDeployment = async (deployment: Deployment) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
    }
    const inUse = (draft.Config.route_policies ?? []).some((policy) =>
      (policy.deployment_ids ?? []).includes(deployment.id),
    );
    if (inUse) {
      void message.error(t("resources.deploymentInUse"));
      return;
    }
    persistDraft({
      ...draft,
      Config: {
        ...draft.Config,
        deployments: (draft.Config.deployments ?? []).filter(
          (item) => item.id !== deployment.id,
        ),
      },
    });
  };

  const deploymentColumns = [
    { title: t("resources.identifier"), dataIndex: "id", key: "id" },
    {
      title: t("resources.providerId"),
      dataIndex: "provider_id",
      key: "provider_id",
      render: (value: string, row: Deployment) =>
        value || row.providerId || "-",
    },
    {
      title: t("resources.credentialId"),
      dataIndex: "credential_id",
      key: "credential_id",
      render: (value?: string) => value || "-",
    },
    {
      title: t("resources.upstreamModel"),
      dataIndex: "upstream_model",
      key: "upstream_model",
      render: (value: string, row: Deployment) => value || row.model || "-",
    },
    {
      title: t("resources.dataRegion"),
      dataIndex: "data_region",
      key: "data_region",
      render: (value: string, row: Deployment) => value || row.region || "-",
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
      title: t("resources.actions"),
      key: "actions",
      render: (_: unknown, row: Deployment) =>
        canWrite ? (
          rowDraftState("deployments", row) === "deleted" ? (
            <Typography.Text type="secondary">
              {t("resources.draftPendingDeletionAction")}
            </Typography.Text>
          ) : (
            <Space>
              <Button
                size="small"
                onClick={() => void openDeploymentDrawer(row)}
              >
                {t("resources.edit")}
              </Button>
              <Button size="small" onClick={() => void disableDeployment(row)}>
                {t("resources.disable")}
              </Button>
              <Popconfirm
                title={t("resources.deleteDeployment")}
                onConfirm={() => void deleteDeployment(row)}
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

  return (
    <Card>
      {canWrite && (
        <Space wrap>
          <Button type="primary" onClick={() => void openDeploymentDrawer()}>
            {t("resources.createDeployment")}
          </Button>
        </Space>
      )}
      <Table
        className="result-panel"
        rowKey="id"
        columns={deploymentColumns}
        dataSource={rows.deploymentRows}
        pagination={{ pageSize: 20 }}
        loading={runtime.isLoading}
        locale={{ emptyText: t("common.empty") }}
      />
      <Drawer
        title={
          editingDeploymentID
            ? t("resources.editDeployment")
            : t("resources.createDeployment")
        }
        open={deploymentDrawerOpen}
        onClose={() => setDeploymentDrawerOpen(false)}
        destroyOnHidden
      >
        <Form<DeploymentFormValues>
          form={deploymentForm}
          layout="vertical"
          onFinish={saveDeployment}
        >
          <Form.Item
            name="id"
            label={t("resources.identifier")}
            rules={[{ required: true }]}
          >
            <Input disabled={Boolean(editingDeploymentID)} />
          </Form.Item>
          <Form.Item
            name="provider_id"
            label={t("resources.providerId")}
            rules={[{ required: true }]}
          >
            <Select
              options={(providerDraft?.Config.providers ?? []).map(
                (provider) => ({ value: provider.id, label: provider.id }),
              )}
            />
          </Form.Item>
          <Form.Item
            name="credential_id"
            label={t("resources.credentialId")}
            rules={[
              { required: true, message: t("resources.selectCredential") },
            ]}
          >
            <Select allowClear options={deploymentCredentialOptions} />
          </Form.Item>
          <Form.Item
            name="upstream_model"
            label={t("resources.upstreamModel")}
            rules={[{ required: true }]}
          >
            <AutoComplete
              options={rows.presetModelOptions}
              placeholder={t("resources.selectPresetModel")}
              filterOption={(input, option) =>
                String(option?.value ?? "")
                  .toLowerCase()
                  .includes(input.toLowerCase())
              }
            />
          </Form.Item>
          <Form.Item
            name="data_region"
            label={t("resources.dataRegion")}
            rules={[{ required: true }]}
          >
            <Select options={rows.regionOptions} />
          </Form.Item>
          <Form.Item name="capabilities" label={t("resources.capabilities")}>
            <Select
              mode="tags"
              options={[
                { value: "chat", label: "chat" },
                { value: "embeddings", label: "embeddings" },
                { value: "tools", label: "tools" },
              ]}
            />
          </Form.Item>
          <Form.Item name="context_window" label={t("resources.contextWindow")}>
            <InputNumber min={1} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item name="priority" label={t("resources.priority")}>
            <InputNumber min={0} style={{ width: "100%" }} />
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
            <Button onClick={() => setDeploymentDrawerOpen(false)}>
              {t("common.close")}
            </Button>
          </Space>
        </Form>
      </Drawer>
    </Card>
  );
}
