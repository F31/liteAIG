import { useState, type ReactNode } from "react";
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
import { useTranslation } from "react-i18next";
import { newID, uniqueAlias } from "./helpers";
import { ResourceStatusTag } from "./ResourceStatusTag";
import type {
  LogicalModel,
  LogicalModelFormValues,
  ResourceSelectOption,
} from "./types";
import type {
  DraftEditor,
  RowDraftStateLookup,
  RuntimeQuery,
} from "./sections";
import type { ResourceRows } from "./useResourceRows";

// LogicalModelSection renders the logical models table and the model editor
// drawer, including the route policy select with missing-resource fallback.
export function LogicalModelSection({
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
  const [logicalModelForm] = Form.useForm<LogicalModelFormValues>();
  const [logicalModelDrawerOpen, setLogicalModelDrawerOpen] = useState(false);
  const [editingLogicalModelID, setEditingLogicalModelID] = useState<string>();
  const selectedLogicalRouteID = Form.useWatch(
    "route_policy_id",
    logicalModelForm,
  ) as string | undefined;
  const logicalRouteOptions = [
    ...rows.routeSelectOptions,
    ...(selectedLogicalRouteID && !rows.routeByID.has(selectedLogicalRouteID)
      ? [
          {
            value: selectedLogicalRouteID,
            title: t("resources.missingResource", {
              id: selectedLogicalRouteID,
            }),
            searchText: selectedLogicalRouteID,
            label: (
              <Tag color="red">
                {t("resources.missingResource", { id: selectedLogicalRouteID })}
              </Tag>
            ),
          },
        ]
      : []),
  ];

  const openLogicalModelDrawer = async (model?: LogicalModel) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
      loadDraft(draft);
    }
    const models = draft.Config.logical_models ?? [];
    const item = model?.id
      ? models.find((candidate) => candidate.id === model.id)
      : undefined;
    setEditingLogicalModelID(item?.id);
    logicalModelForm.setFieldsValue({
      id: item?.id ?? newID("model"),
      alias: item?.alias ?? uniqueAlias("default-chat", models),
      route_policy_id:
        item?.route_policy_id ??
        (draft.Config.route_policies ?? [])[0]?.id ??
        "",
    });
    setLogicalModelDrawerOpen(true);
  };

  const saveLogicalModel = (values: LogicalModelFormValues) => {
    if (!providerDraft) return;
    const models = providerDraft.Config.logical_models ?? [];
    const next: LogicalModel = {
      id: values.id,
      tenant_id: providerDraft.Config.tenant?.id,
      alias: values.alias,
      route_policy_id: values.route_policy_id,
    };
    const duplicateAlias = models.some(
      (item) => item.alias === values.alias && item.id !== values.id,
    );
    if (duplicateAlias) {
      logicalModelForm.setFields([
        { name: "alias", errors: [t("resources.logicalModelAliasInUse")] },
      ]);
      void message.error(t("resources.logicalModelAliasInUse"));
      return;
    }
    const exists = models.some((item) => item.id === values.id);
    updateConfig((config) => ({
      ...config,
      logical_models: exists
        ? models.map((item) =>
            item.id === values.id ? { ...item, ...next } : item,
          )
        : [...models, next],
    }));
    setLogicalModelDrawerOpen(false);
    setEditingLogicalModelID(undefined);
  };

  const deleteLogicalModel = async (model: LogicalModel) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
    }
    persistDraft({
      ...draft,
      Config: {
        ...draft.Config,
        logical_models: (draft.Config.logical_models ?? []).filter(
          (item) => item.id !== model.id,
        ),
      },
    });
  };

  const logicalModelColumns = [
    { title: t("resources.identifier"), dataIndex: "id", key: "id" },
    { title: t("resources.alias"), dataIndex: "alias", key: "alias" },
    {
      title: t("common.status"),
      key: "status",
      render: (_: unknown, row: LogicalModel) => (
        <ResourceStatusTag
          resource="logicalModels"
          row={row}
          rowDraftState={rowDraftState}
        />
      ),
    },
    {
      title: t("resources.routePolicyId"),
      dataIndex: "route_policy_id",
      key: "route_policy_id",
      render: (value: string, row: LogicalModel) =>
        value || row.routePolicyId || "-",
    },
    {
      title: t("resources.actions"),
      key: "actions",
      render: (_: unknown, row: LogicalModel) =>
        canWrite ? (
          rowDraftState("logicalModels", row) === "deleted" ? (
            <Typography.Text type="secondary">
              {t("resources.draftPendingDeletionAction")}
            </Typography.Text>
          ) : (
            <Space>
              <Button
                size="small"
                onClick={() => void openLogicalModelDrawer(row)}
              >
                {t("resources.edit")}
              </Button>
              <Popconfirm
                title={t("resources.deleteLogicalModel")}
                onConfirm={() => void deleteLogicalModel(row)}
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
          <Button type="primary" onClick={() => void openLogicalModelDrawer()}>
            {t("resources.createLogicalModel")}
          </Button>
        </Space>
      )}
      <Table
        className="result-panel"
        rowKey="id"
        columns={logicalModelColumns}
        dataSource={rows.logicalModelRows}
        pagination={{ pageSize: 20 }}
        loading={runtime.isLoading}
        locale={{ emptyText: t("common.empty") }}
      />
      <Drawer
        title={
          editingLogicalModelID
            ? t("resources.editLogicalModel")
            : t("resources.createLogicalModel")
        }
        open={logicalModelDrawerOpen}
        onClose={() => setLogicalModelDrawerOpen(false)}
        destroyOnHidden
      >
        <Form<LogicalModelFormValues>
          form={logicalModelForm}
          layout="vertical"
          onFinish={saveLogicalModel}
        >
          <Form.Item
            name="id"
            label={t("resources.identifier")}
            rules={[{ required: true }]}
          >
            <Input disabled={Boolean(editingLogicalModelID)} />
          </Form.Item>
          <Form.Item
            name="alias"
            label={t("resources.alias")}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="route_policy_id"
            label={t("resources.routePolicyId")}
            rules={[
              { required: true, message: t("resources.selectRoutePolicy") },
            ]}
          >
            <Select
              options={
                logicalRouteOptions as unknown as {
                  value: string;
                  label: ReactNode;
                }[]
              }
              filterOption={(input, option) => {
                const opt = option as ResourceSelectOption | undefined;
                return String(opt?.searchText ?? opt?.title ?? "")
                  .toLowerCase()
                  .includes(input.toLowerCase());
              }}
              labelRender={(props) => {
                const option = logicalRouteOptions.find(
                  (candidate) => candidate.value === props.value,
                );
                return option ? option.title : String(props.value);
              }}
            />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">
              {t("resources.applyToDraft")}
            </Button>
            <Button onClick={() => setLogicalModelDrawerOpen(false)}>
              {t("common.close")}
            </Button>
          </Space>
        </Form>
      </Drawer>
    </Card>
  );
}
