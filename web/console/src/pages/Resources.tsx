import { useMemo, type ReactNode } from "react";
import {
  Alert,
  Button,
  Card,
  Space,
  Statistic,
  Tabs,
  Tag,
  Typography,
} from "antd";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, ResourceCatalogView, RuntimeResources } from "../api/client";
import { isAdminRole, useAuth } from "../state/auth";
import type { ConfigDiagnostic, Project } from "./resources/types";
import {
  draftRowStates,
  useTenantConfigDraft,
} from "./resources/useTenantConfigDraft";
import { useResourceRows } from "./resources/useResourceRows";
import { AuditSection } from "./resources/AuditSection";
import { DeploymentSection } from "./resources/DeploymentSection";
import { KeySection } from "./resources/KeySection";
import { LogicalModelSection } from "./resources/LogicalModelSection";
import { MCPServerSection } from "./resources/MCPServerSection";
import { ProviderSection } from "./resources/ProviderSection";
import { ProjectSection } from "./resources/ProjectSection";
import { RouteSection } from "./resources/RouteSection";

// The Resources page is a thin orchestrator: it owns the shared runtime /
// projects / catalog queries, the tenant-config draft state machine, and the
// draft row badges; every resource table plus its editor drawer lives in a
// dedicated section component under ./resources.
export default function Resources() {
  const { t } = useTranslation();
  const role = useAuth((state) => state.role);
  const canWrite = isAdminRole(role);
  const runtime = useQuery({
    queryKey: ["runtime-resources"],
    queryFn: () => api<RuntimeResources>("/api/admin/runtime"),
  });
  const projects = useQuery({
    queryKey: ["projects"],
    queryFn: () => api<Project[]>("/api/admin/projects"),
  });
  const resourceCatalog = useQuery({
    queryKey: ["resource-catalog"],
    queryFn: () => api<ResourceCatalogView>("/api/admin/resource-catalog"),
  });
  const editor = useTenantConfigDraft({
    canWrite,
    onPublished: () => void runtime.refetch(),
  });
  const {
    providerDraft,
    draftBaseline,
    draftDirty,
    savePending,
    publishDiagnostics,
    publishProviderDraft,
  } = editor;
  const draftStates = useMemo(
    () => draftRowStates(providerDraft, draftBaseline),
    [draftBaseline, providerDraft],
  );
  const pendingChanges = useMemo(() => {
    const totals = { added: 0, modified: 0, removed: 0 };
    for (const map of Object.values(draftStates)) {
      for (const state of map.values()) {
        if (state === "new") totals.added += 1;
        else if (state === "modified") totals.modified += 1;
        else if (state === "deleted") totals.removed += 1;
      }
    }
    return {
      ...totals,
      total: totals.added + totals.modified + totals.removed,
    };
  }, [draftStates]);
  const rowDraftState = (resource: string, row: { id?: string }) =>
    row.id ? draftStates[resource]?.get(row.id) : undefined;
  const rows = useResourceRows({
    editor,
    runtime,
    projects,
    resourceCatalog,
    rowDraftState,
  });
  const titledSection = (title: string, content: ReactNode) => (
    <div>
      <Typography.Title level={4}>{title}</Typography.Title>
      {content}
    </div>
  );
  const items = [
    {
      key: "overview",
      label: t("resources.overview"),
      children: (
        <div className="metric-grid">
          <Card>
            <Statistic
              title={t("resources.providers")}
              value={rows.providerRows.length}
            />
          </Card>
          <Card>
            <Statistic
              title={t("resources.deployments")}
              value={rows.deploymentRows.length}
            />
          </Card>
          <Card>
            <Statistic
              title={t("resources.models")}
              value={rows.logicalModelRows.length}
            />
          </Card>
          <Card>
            <Statistic
              title={t("resources.accessKeys")}
              value={(runtime.data?.keys ?? []).length}
            />
          </Card>
          <Card>
            <Statistic
              title={t("resources.mcpServers")}
              value={rows.mcpServerRows.length}
            />
          </Card>
          <Card>
            <Statistic
              title={t("resources.tools")}
              value={rows.toolRows.length}
            />
          </Card>
        </div>
      ),
    },
    {
      key: "modelAccess",
      label: t("resources.modelAccess"),
      children: (
        <Space direction="vertical" size="large" style={{ width: "100%" }}>
          <Typography.Text type="secondary">
            {t("resources.modelAccessHelp")}
          </Typography.Text>
          {titledSection(
            t("resources.providers"),
            <ProviderSection
              editor={editor}
              rows={rows}
              canWrite={canWrite}
              rowDraftState={rowDraftState}
              runtime={runtime}
              resourceCatalog={resourceCatalog}
            />,
          )}
          {titledSection(
            t("resources.deployments"),
            <DeploymentSection
              editor={editor}
              rows={rows}
              canWrite={canWrite}
              rowDraftState={rowDraftState}
              runtime={runtime}
            />,
          )}
          {titledSection(
            t("resources.routes"),
            <RouteSection
              editor={editor}
              rows={rows}
              canWrite={canWrite}
              rowDraftState={rowDraftState}
              runtime={runtime}
              projects={projects}
            />,
          )}
          {titledSection(
            t("resources.models"),
            <LogicalModelSection
              editor={editor}
              rows={rows}
              canWrite={canWrite}
              rowDraftState={rowDraftState}
              runtime={runtime}
            />,
          )}
        </Space>
      ),
    },
    {
      key: "accessKeys",
      label: t("resources.accessKeys"),
      children: (
        <KeySection
          canWrite={canWrite}
          runtime={runtime}
          projects={projects}
          rows={rows}
        />
      ),
    },
    {
      key: "toolAccess",
      label: t("resources.toolAccess"),
      children: (
        <MCPServerSection
          editor={editor}
          rows={rows}
          canWrite={canWrite}
          rowDraftState={rowDraftState}
          runtime={runtime}
        />
      ),
    },
    {
      key: "publishConfig",
      label: t("resources.publishConfig"),
      children: (
        <Space direction="vertical" size="large" style={{ width: "100%" }}>
          {titledSection(
            t("resources.projects"),
            <ProjectSection
              canWrite={canWrite}
              projects={projects}
              rows={rows}
            />,
          )}
          {titledSection(t("resources.audit"), <AuditSection />)}
        </Space>
      ),
    },
  ];

  const diagnosticResourceLabel = (diagnostic: ConfigDiagnostic) => {
    const match = diagnostic.path?.match(/^\/(\w+)\/(\d+)/);
    if (!match || !providerDraft) return diagnostic.path;
    const [, collection, indexText] = match;
    const index = Number(indexText);
    const item = (providerDraft.Config as Record<string, unknown>)[collection];
    if (!Array.isArray(item)) return diagnostic.path;
    const resource = item[index] as { id?: string; alias?: string } | undefined;
    const label = resource?.alias ?? resource?.id;
    return label ? `${label} · ${diagnostic.path}` : diagnostic.path;
  };

  const draftBar =
    canWrite && providerDraft ? (
      <Card
        size="small"
        style={{
          marginBottom: 16,
          position: "sticky",
          top: 8,
          zIndex: 10,
          background: "#fff",
        }}
      >
        <Space direction="vertical" size="small" style={{ width: "100%" }}>
          <Space wrap>
            <Tag color={draftDirty ? "orange" : "gold"}>
              {draftDirty
                ? t("resources.draftDirty")
                : t("resources.draftEditing")}
            </Tag>
            {pendingChanges.total > 0 && (
              <Typography.Text type="secondary">
                {t("resources.draftChangesSummary", {
                  added: pendingChanges.added,
                  modified: pendingChanges.modified,
                  removed: pendingChanges.removed,
                })}
              </Typography.Text>
            )}
            <Button
              type="primary"
              loading={publishProviderDraft.isPending || savePending}
              disabled={!draftDirty || savePending}
              onClick={() =>
                providerDraft && publishProviderDraft.mutate(providerDraft)
              }
            >
              {t("resources.publishDraft")}
              {pendingChanges.total > 0 ? ` (${pendingChanges.total})` : ""}
            </Button>
          </Space>
          {publishDiagnostics.length > 0 && (
            <Alert
              type="error"
              showIcon
              message={t("resources.publishValidationFailed")}
              description={
                <Space direction="vertical" size={2}>
                  {publishDiagnostics.map((diagnostic, index) => {
                    const code = diagnostic.code ?? "CONFIG_INVALID";
                    return (
                      <Typography.Text key={`${code}-${index}`}>
                        {t(`resources.validation.${code}`, {
                          defaultValue: code,
                          ...diagnostic.params,
                        })}
                        {diagnostic.path
                          ? ` (${diagnosticResourceLabel(diagnostic)})`
                          : ""}
                      </Typography.Text>
                    );
                  })}
                </Space>
              }
            />
          )}
        </Space>
      </Card>
    ) : null;
  return (
    <section>
      <Typography.Title className="page-title">
        {t("resources.title")}
      </Typography.Title>
      <Card loading={runtime.isLoading || projects.isLoading}>
        {draftBar}
        <Tabs items={items} />
      </Card>
    </section>
  );
}
