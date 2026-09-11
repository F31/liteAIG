import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Space, Tag, Typography } from "antd";
import type {
  Credential,
  Deployment,
  LogicalModel,
  MCPServer,
  Provider,
  RoutePolicy,
  Tool,
} from "./types";
import { shortID } from "./helpers";
import { strategyLabels } from "./constants";
import type {
  DraftEditor,
  ProjectsQuery,
  ResourceCatalogQuery,
  RowDraftStateLookup,
  RuntimeQuery,
} from "./sections";

// useResourceRows derives every table row set and cross-resource select
// option for the Resources page from the runtime snapshot plus the active
// draft, so the per-resource section components only need to render and
// mutate.
export function useResourceRows(options: {
  editor: DraftEditor;
  runtime: RuntimeQuery;
  projects: ProjectsQuery;
  resourceCatalog: ResourceCatalogQuery;
  rowDraftState: RowDraftStateLookup;
}) {
  const { t } = useTranslation();
  const { editor, runtime, projects, resourceCatalog, rowDraftState } = options;
  const { providerDraft, draftBaseline } = editor;
  const labels = strategyLabels(t);
  const providerPresets = resourceCatalog.data?.providers ?? [];
  const runtimeProviders = (runtime.data?.providers ?? []) as Provider[];
  const runtimeCredentials = (runtime.data?.credentials ?? []) as Credential[];
  const draftUnionRows = <T extends { id: string }>(
    draftItems: T[],
    runtimeItems: T[],
  ): T[] =>
    providerDraft
      ? [
          ...draftItems,
          ...runtimeItems.filter(
            (item) => !draftItems.some((candidate) => candidate.id === item.id),
          ),
        ]
      : runtimeItems;
  const providerRows = providerDraft
    ? draftUnionRows(
        (providerDraft.Config.providers ?? []) as Provider[],
        (draftBaseline?.providers ?? runtimeProviders) as Provider[],
      )
    : runtimeProviders.length > 0
      ? runtimeProviders
      : providerPresets.map((provider) => ({
          id: provider.id,
          type: provider.type,
          endpoint: provider.endpoint,
          status: "catalog",
        }));
  const deploymentRows = providerDraft
    ? draftUnionRows(
        (providerDraft.Config.deployments ?? []) as Deployment[],
        (draftBaseline?.deployments ??
          runtime.data?.deployments ??
          []) as Deployment[],
      )
    : ((runtime.data?.deployments ?? []) as Deployment[]);
  const providerByID = useMemo(
    () => new Map(providerRows.map((provider) => [provider.id, provider])),
    [providerRows],
  );
  const deploymentByID = useMemo(
    () =>
      new Map(deploymentRows.map((deployment) => [deployment.id, deployment])),
    [deploymentRows],
  );
  const deploymentProviderID = (deployment: Deployment) =>
    deployment.provider_id ?? deployment.providerId ?? "";
  const deploymentCredentialID = (deployment: Deployment) =>
    deployment.credential_id ?? "";
  const deploymentModel = (deployment: Deployment) =>
    deployment.upstream_model ?? deployment.model ?? "-";
  const deploymentRegion = (deployment: Deployment) =>
    deployment.data_region ?? deployment.region ?? "-";
  const deploymentSummary = (deployment: Deployment) => {
    const providerID = deploymentProviderID(deployment);
    const provider = providerByID.get(providerID);
    return `${provider?.type ?? (providerID || "-")} · ${deploymentModel(deployment)} · ${deploymentRegion(deployment)}`;
  };
  const deploymentDetails = (deployment: Deployment) =>
    [
      `${t("resources.identifier")}: ${shortID(deployment.id)}`,
      `${t("resources.providerId")}: ${shortID(deploymentProviderID(deployment))}`,
      `${t("resources.credentialId")}: ${shortID(deploymentCredentialID(deployment))}`,
      `${t("common.status")}: ${deployment.status ?? "-"}`,
      `${t("resources.priority")}: ${deployment.priority ?? "-"}`,
    ].join(" · ");
  const deploymentSelectOptions = deploymentRows
    .filter(
      (deployment) => rowDraftState("deployments", deployment) !== "deleted",
    )
    .sort((left, right) => {
      const leftDisabled = left.status === "disabled" ? 1 : 0;
      const rightDisabled = right.status === "disabled" ? 1 : 0;
      if (leftDisabled !== rightDisabled) return leftDisabled - rightDisabled;
      return deploymentSummary(left).localeCompare(deploymentSummary(right));
    })
    .map((deployment) => {
      const state = rowDraftState("deployments", deployment);
      const summary = deploymentSummary(deployment);
      const details = deploymentDetails(deployment);
      const searchText = [
        deployment.id,
        deploymentProviderID(deployment),
        deploymentCredentialID(deployment),
        deploymentModel(deployment),
        deploymentRegion(deployment),
        deployment.status,
        String(deployment.priority ?? ""),
      ].join(" ");
      return {
        value: deployment.id,
        title: summary,
        searchText,
        label: (
          <Space direction="vertical" size={0}>
            <Space size={6}>
              <span>{summary}</span>
              {state && state !== "unchanged" && (
                <Tag color={state === "new" ? "blue" : "orange"}>
                  {state === "new"
                    ? t("resources.draftNew")
                    : t("resources.draftModified")}
                </Tag>
              )}
            </Space>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {details}
            </Typography.Text>
          </Space>
        ),
      };
    });
  const logicalModelRows = providerDraft
    ? draftUnionRows(
        (providerDraft.Config.logical_models ?? []) as LogicalModel[],
        (draftBaseline?.logical_models ??
          runtime.data?.logicalModels ??
          []) as LogicalModel[],
      )
    : ((runtime.data?.logicalModels ?? []) as LogicalModel[]);
  const routeRows = providerDraft
    ? draftUnionRows(
        (providerDraft.Config.route_policies ?? []) as RoutePolicy[],
        (draftBaseline?.route_policies ??
          runtime.data?.routes ??
          []) as RoutePolicy[],
      )
    : ((runtime.data?.routes ?? []) as RoutePolicy[]);
  const routeByID = useMemo(
    () => new Map(routeRows.map((route) => [route.id, route])),
    [routeRows],
  );
  const projectByID = useMemo(
    () =>
      new Map((projects.data ?? []).map((project) => [project.id, project])),
    [projects.data],
  );
  const routeDeploymentIDs = (route: RoutePolicy) =>
    route.deployment_ids ?? route.deploymentIds ?? [];
  const routeProjectID = (route: RoutePolicy) =>
    route.project_id ?? route.projectId ?? "";
  const routeSummary = (route: RoutePolicy) => {
    const deploymentCount = routeDeploymentIDs(route).length;
    const projectID = routeProjectID(route);
    const project = projectByID.get(projectID);
    return `${route.strategy ? (labels[route.strategy] ?? route.strategy) : "-"} · ${deploymentCount} ${t("resources.endpointCount", { count: deploymentCount })} · ${project?.name ?? (projectID || t("resources.allProjects"))}`;
  };
  const routeDetails = (route: RoutePolicy) => {
    const deployments = routeDeploymentIDs(route)
      .map((id) => {
        const deployment = deploymentByID.get(id);
        return deployment
          ? deploymentSummary(deployment)
          : t("resources.missingResource", { id });
      })
      .join("; ");
    return [
      `${t("resources.identifier")}: ${shortID(route.id)}`,
      `${t("resources.deployments")}: ${deployments || "-"}`,
    ].join(" · ");
  };
  const routeSelectOptions = routeRows
    .filter((route) => rowDraftState("routes", route) !== "deleted")
    .sort((left, right) =>
      routeSummary(left).localeCompare(routeSummary(right)),
    )
    .map((route) => {
      const state = rowDraftState("routes", route);
      const summary = routeSummary(route);
      const details = routeDetails(route);
      const deploymentSearchText = routeDeploymentIDs(route)
        .map((id) => {
          const deployment = deploymentByID.get(id);
          return deployment
            ? [
                deployment.id,
                deploymentProviderID(deployment),
                deploymentModel(deployment),
                deploymentRegion(deployment),
              ].join(" ")
            : id;
        })
        .join(" ");
      return {
        value: route.id,
        title: summary,
        searchText: [
          route.id,
          route.strategy,
          route.strategy ? (labels[route.strategy] ?? "") : "",
          routeProjectID(route),
          projectByID.get(routeProjectID(route))?.name,
          deploymentSearchText,
        ].join(" "),
        label: (
          <Space direction="vertical" size={0}>
            <Space size={6}>
              <span>{summary}</span>
              {state && state !== "unchanged" && (
                <Tag color={state === "new" ? "blue" : "orange"}>
                  {state === "new"
                    ? t("resources.draftNew")
                    : t("resources.draftModified")}
                </Tag>
              )}
            </Space>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {details}
            </Typography.Text>
          </Space>
        ),
      };
    });
  const regionOptions = (resourceCatalog.data?.regions ?? []).map((region) => ({
    value: region.id,
    label: region.label,
  }));
  const mcpServerRows = providerDraft
    ? draftUnionRows(
        (providerDraft.Config.mcp_servers ?? []) as MCPServer[],
        (draftBaseline?.mcp_servers ??
          runtime.data?.mcpServers ??
          []) as MCPServer[],
      )
    : ((runtime.data?.mcpServers ?? []) as MCPServer[]);
  const toolRows = providerDraft
    ? (providerDraft.Config.tools ?? [])
    : ((runtime.data?.tools ?? []) as Tool[]);
  const presetModelOptions = (resourceCatalog.data?.models ?? []).map(
    (model) => {
      const provider = providerPresets.find(
        (candidate) => candidate.id === model.providerId,
      );
      return {
        value: model.model,
        label: `${provider?.label ?? model.providerId} · ${model.model}`,
      };
    },
  );

  return {
    providerPresets,
    runtimeProviders,
    runtimeCredentials,
    providerRows,
    deploymentRows,
    logicalModelRows,
    routeRows,
    mcpServerRows,
    toolRows,
    providerByID,
    deploymentByID,
    routeByID,
    projectByID,
    deploymentSummary,
    deploymentSelectOptions,
    routeSelectOptions,
    regionOptions,
    presetModelOptions,
  };
}

export type ResourceRows = ReturnType<typeof useResourceRows>;
