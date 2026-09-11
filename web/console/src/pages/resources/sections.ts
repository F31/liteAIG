import type { UseQueryResult } from "@tanstack/react-query";
import type { ResourceCatalogView, RuntimeResources } from "../../api/client";
import type { Project } from "./types";
import type { useTenantConfigDraft } from "./useTenantConfigDraft";
import type { DraftRowState } from "./constants";

// Shared prop contracts for the per-resource section components rendered on
// the Resources page.

export type DraftEditor = ReturnType<typeof useTenantConfigDraft>;

export type RowDraftStateLookup = (
  resource: string,
  row: { id?: string },
) => DraftRowState | undefined;

export type RuntimeQuery = UseQueryResult<RuntimeResources, Error>;

export type ProjectsQuery = UseQueryResult<Project[], Error>;

export type ResourceCatalogQuery = UseQueryResult<ResourceCatalogView, Error>;

export interface ResourceSectionProps {
  editor: DraftEditor;
  canWrite: boolean;
  rowDraftState: RowDraftStateLookup;
  runtime: RuntimeQuery;
}
