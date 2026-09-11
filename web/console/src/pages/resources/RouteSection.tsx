import { useState, type ReactNode } from "react";
import {
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
  Tooltip,
  Typography,
} from "antd";
import { useTranslation } from "react-i18next";
import { newID } from "./helpers";
import { strategyDescriptions, strategyLabels } from "./constants";
import { ResourceStatusTag } from "./ResourceStatusTag";
import type {
  LogicalModel,
  ResourceSelectOption,
  RoutePolicy,
  RoutePolicyFormValues,
} from "./types";
import type {
  DraftEditor,
  ProjectsQuery,
  RowDraftStateLookup,
  RuntimeQuery,
} from "./sections";
import type { ResourceRows } from "./useResourceRows";

// RouteSection renders the route policies table and the route editor drawer,
// including the deployment multi-select with missing-resource placeholders.
export function RouteSection({
  editor,
  rows,
  canWrite,
  rowDraftState,
  runtime,
  projects,
}: {
  editor: DraftEditor;
  rows: ResourceRows;
  canWrite: boolean;
  rowDraftState: RowDraftStateLookup;
  runtime: RuntimeQuery;
  projects: ProjectsQuery;
}) {
  const { t } = useTranslation();
  const {
    providerDraft,
    startProviderDraft,
    loadDraft,
    persistDraft,
    updateConfig,
  } = editor;
  const [routeForm] = Form.useForm<RoutePolicyFormValues>();
  const [routeDrawerOpen, setRouteDrawerOpen] = useState(false);
  const [editingRouteID, setEditingRouteID] = useState<string>();
  const routeStrategy = Form.useWatch("strategy", routeForm);
  const selectedRouteDeploymentIDs = (Form.useWatch(
    "deploymentIds",
    routeForm,
  ) ?? []) as string[];
  const labels = strategyLabels(t);
  const descriptions = strategyDescriptions(t);
  const routeIsInUse = (route: RoutePolicy) =>
    (providerDraft
      ? (providerDraft.Config.logical_models ?? [])
      : ((runtime.data?.logicalModels ?? []) as LogicalModel[])
    ).some(
      (model) => (model.route_policy_id ?? model.routePolicyId) === route.id,
    );
  const routeDeploymentOptions = [
    ...rows.deploymentSelectOptions,
    ...selectedRouteDeploymentIDs
      .filter((id) => !rows.deploymentByID.has(id))
      .map((id) => ({
        value: id,
        title: t("resources.missingResource", { id }),
        searchText: id,
        label: <Tag color="red">{t("resources.missingResource", { id })}</Tag>,
      })),
  ];

  const openRouteDrawer = async (route?: RoutePolicy) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
      loadDraft(draft);
    }
    const item = route?.id
      ? (draft.Config.route_policies ?? []).find(
          (candidate) => candidate.id === route.id,
        )
      : undefined;
    setEditingRouteID(item?.id);
    routeForm.setFieldsValue({
      id: item?.id ?? newID("route"),
      strategy: item?.strategy ?? "priority",
      projectId: item?.project_id ?? undefined,
      deploymentIds: item?.deployment_ids ?? [],
      scoreLatency: item?.score_weights?.latency ?? 0,
      scoreCost: item?.score_weights?.cost ?? 0,
      scoreLoad: item?.score_weights?.load ?? 0,
      scoreCache: item?.score_weights?.cache ?? 0,
    });
    setRouteDrawerOpen(true);
  };

  const saveRoute = (values: RoutePolicyFormValues) => {
    if (!providerDraft) return;
    const routes = providerDraft.Config.route_policies ?? [];
    const scoreWeights: Record<string, number> = {};
    if (values.strategy === "soft") {
      for (const [key, value] of [
        ["latency", values.scoreLatency],
        ["cost", values.scoreCost],
        ["load", values.scoreLoad],
        ["cache", values.scoreCache],
      ] as const) {
        if (value && value > 0) {
          scoreWeights[key] = value;
        }
      }
    }
    const next: RoutePolicy = {
      id: values.id,
      tenant_id: providerDraft.Config.tenant?.id,
      project_id: values.projectId || undefined,
      strategy: values.strategy,
      deployment_ids: values.deploymentIds,
      score_weights: values.strategy === "soft" ? scoreWeights : undefined,
    };
    const exists = routes.some((item) => item.id === values.id);
    updateConfig((config) => ({
      ...config,
      route_policies: exists
        ? routes.map((item) =>
            item.id === values.id ? { ...item, ...next } : item,
          )
        : [...routes, next],
    }));
    setRouteDrawerOpen(false);
    setEditingRouteID(undefined);
  };

  const deleteRoute = async (route: RoutePolicy) => {
    let draft = providerDraft;
    if (!draft) {
      draft = await startProviderDraft.mutateAsync();
    }
    const inUse = (draft.Config.logical_models ?? []).some(
      (model) => (model.route_policy_id ?? model.routePolicyId) === route.id,
    );
    if (inUse) {
      void message.error(t("resources.routeInUse"));
      return;
    }
    persistDraft({
      ...draft,
      Config: {
        ...draft.Config,
        route_policies: (draft.Config.route_policies ?? []).filter(
          (item) => item.id !== route.id,
        ),
      },
    });
  };

  const routeColumns = [
    { title: t("resources.identifier"), dataIndex: "id", key: "id" },
    {
      title: t("resources.strategy"),
      dataIndex: "strategy",
      key: "strategy",
      render: (value?: string) => (value ? (labels[value] ?? value) : "-"),
    },
    {
      title: t("common.status"),
      key: "status",
      render: (_: unknown, row: RoutePolicy) => (
        <ResourceStatusTag
          resource="routes"
          row={row}
          rowDraftState={rowDraftState}
        />
      ),
    },
    {
      title: t("resources.deployments"),
      dataIndex: "deployment_ids",
      key: "deployment_ids",
      render: (value?: string[], row?: RoutePolicy) =>
        (value ?? row?.deploymentIds ?? [])
          .map((id) => {
            const deployment = rows.deploymentByID.get(id);
            return deployment
              ? rows.deploymentSummary(deployment)
              : t("resources.missingResource", { id });
          })
          .join(", ") || "-",
    },
    {
      title: t("resources.actions"),
      key: "actions",
      render: (_: unknown, row: RoutePolicy) => {
        const inUse = routeIsInUse(row);
        return canWrite ? (
          rowDraftState("routes", row) === "deleted" ? (
            <Typography.Text type="secondary">
              {t("resources.draftPendingDeletionAction")}
            </Typography.Text>
          ) : (
            <Space>
              <Button size="small" onClick={() => void openRouteDrawer(row)}>
                {t("resources.edit")}
              </Button>
              {inUse ? (
                <Tooltip title={t("resources.routeInUse")}>
                  <span>
                    <Button size="small" danger disabled>
                      {t("resources.delete")}
                    </Button>
                  </span>
                </Tooltip>
              ) : (
                <Popconfirm
                  title={t("resources.deleteRoute")}
                  onConfirm={() => void deleteRoute(row)}
                >
                  <Button size="small" danger>
                    {t("resources.delete")}
                  </Button>
                </Popconfirm>
              )}
            </Space>
          )
        ) : null;
      },
    },
  ];

  return (
    <Card>
      {canWrite && (
        <Space wrap>
          <Button type="primary" onClick={() => void openRouteDrawer()}>
            {t("resources.createRoute")}
          </Button>
        </Space>
      )}
      <Table
        className="result-panel"
        rowKey="id"
        columns={routeColumns}
        dataSource={rows.routeRows}
        pagination={{ pageSize: 20 }}
        loading={runtime.isLoading}
        locale={{ emptyText: t("common.empty") }}
      />
      <Drawer
        title={
          editingRouteID ? t("resources.editRoute") : t("resources.createRoute")
        }
        open={routeDrawerOpen}
        onClose={() => setRouteDrawerOpen(false)}
        destroyOnHidden
      >
        <Form<RoutePolicyFormValues>
          form={routeForm}
          layout="vertical"
          onFinish={saveRoute}
        >
          <Form.Item
            name="id"
            label={t("resources.identifier")}
            rules={[{ required: true }]}
          >
            <Input disabled={Boolean(editingRouteID)} />
          </Form.Item>
          <Form.Item
            name="strategy"
            label={t("resources.strategy")}
            rules={[{ required: true }]}
          >
            <Select
              options={[
                { value: "priority", label: labels.priority },
                { value: "weighted", label: labels.weighted },
                { value: "round_robin", label: labels.round_robin },
                { value: "soft", label: labels.soft },
              ]}
            />
          </Form.Item>
          {routeStrategy && descriptions[routeStrategy] && (
            <Typography.Paragraph type="secondary" style={{ marginTop: -12 }}>
              {descriptions[routeStrategy]}
            </Typography.Paragraph>
          )}
          {routeStrategy === "soft" && (
            <>
              <Form.Item
                name="scoreLatency"
                label={t("resources.scoreLatency")}
              >
                <InputNumber min={0} step={0.05} />
              </Form.Item>
              <Form.Item name="scoreCost" label={t("resources.scoreCost")}>
                <InputNumber min={0} step={0.05} />
              </Form.Item>
              <Form.Item name="scoreLoad" label={t("resources.scoreLoad")}>
                <InputNumber min={0} step={0.05} />
              </Form.Item>
              <Form.Item name="scoreCache" label={t("resources.scoreCache")}>
                <InputNumber min={0} step={0.05} />
              </Form.Item>
            </>
          )}
          <Form.Item name="projectId" label={t("resources.projectId")}>
            <Select
              allowClear
              options={(projects.data ?? []).map((project) => ({
                value: project.id,
                label: project.name || project.id,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="deploymentIds"
            label={t("resources.deployments")}
            rules={[{ required: true }]}
          >
            <Select
              mode="multiple"
              options={
                routeDeploymentOptions as unknown as {
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
                const option = routeDeploymentOptions.find(
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
            <Button onClick={() => setRouteDrawerOpen(false)}>
              {t("common.close")}
            </Button>
          </Space>
        </Form>
      </Drawer>
    </Card>
  );
}
