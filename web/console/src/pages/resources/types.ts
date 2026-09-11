import type { ReactNode } from "react";

export type ResourceSelectOption = {
  value: string;
  title: string;
  searchText: string;
  label: ReactNode;
};

export type Project = {
  id: string;
  tenantId: string;
  name: string;
  status: string;
  residencyEnforcement: string;
  allowedDataRegions: string[];
};

export type Provider = {
  id: string;
  tenant_id?: string;
  owner_scope?: string;
  type: string;
  endpoint?: string;
  status: string;
};

export type Credential = {
  id: string;
  providerId: string;
  fingerprint: string;
  status: string;
};

export type ConfigCredential = {
  id: string;
  provider_id: string;
  status: string;
};

export type Deployment = {
  id: string;
  tenant_id?: string;
  providerId?: string;
  provider_id?: string;
  credential_id?: string;
  upstream_model?: string;
  model?: string;
  data_region?: string;
  region?: string;
  capabilities?: string[];
  context_window?: number;
  priority?: number;
  status: string;
};

export type RoutePolicy = {
  id: string;
  tenant_id?: string;
  project_id?: string;
  projectId?: string;
  strategy?: string;
  deployment_ids?: string[];
  deploymentIds?: string[];
  score_weights?: Record<string, number>;
  scoreWeights?: Record<string, number>;
  weights?: Record<string, number>;
  version?: number;
};

export type LogicalModel = {
  id: string;
  tenant_id?: string;
  alias: string;
  route_policy_id?: string;
  routePolicyId?: string;
};

export type APIKey = {
  id: string;
  name: string;
  projectId?: string;
  fingerprint: string;
  status: string;
  createdAt?: string;
  revealable?: boolean;
};

export type MCPServer = {
  id: string;
  tenant_id?: string;
  url: string;
  status: string;
};

export type Tool = {
  id: string;
  tenant_id?: string;
  server_id: string;
  name: string;
  data_classification?: string;
  endpoint?: string;
  status: string;
  schema?: unknown;
  capability_tags?: string[];
};

export type DiscoveredTool = {
  name: string;
  description?: string;
  inputSchema?: unknown;
};

export type TenantConfig = {
  tenant?: { id?: string };
  cache?: {
    enabled?: boolean;
    ttl_seconds?: number;
    namespace_version?: number;
    allowed_kinds?: string[];
    max_temperature?: number;
    semantic?: { enabled?: boolean; model?: string; threshold?: number };
  };
  providers?: Provider[];
  credentials?: ConfigCredential[];
  deployments?: Deployment[];
  route_policies?: RoutePolicy[];
  logical_models?: LogicalModel[];
  mcp_servers?: MCPServer[];
  tools?: Tool[];
  [key: string]: unknown;
};

export type DraftDetail = {
  ID: string;
  BaseVersion: number;
  Revision: number;
  Status: string;
  Config: TenantConfig;
};

export type DraftSummary = {
  id: string;
  status: string;
  baseVersion: number;
  revision: number;
  updatedAt: string;
};

export type ConfigVersion = {
  Version?: number;
  version?: number;
  Config?: TenantConfig;
  config?: TenantConfig;
};

export type ConfigDiagnostic = {
  severity?: string;
  code?: string;
  path?: string;
  message_key?: string;
  params?: Record<string, string>;
};

export type ProviderFormValues = {
  id: string;
  type: string;
  endpoint?: string;
  status: string;
};

export type RotateCredentialValues = {
  secret: string;
};

export type CredentialFormValues = {
  providerId: string;
  secret: string;
  status: string;
};

export type DeploymentFormValues = {
  id: string;
  provider_id: string;
  credential_id?: string;
  upstream_model: string;
  data_region: string;
  capabilities?: string[];
  context_window?: number;
  priority?: number;
  status: string;
};

export type LogicalModelFormValues = {
  id: string;
  alias: string;
  route_policy_id: string;
};

export type RoutePolicyFormValues = {
  id: string;
  strategy: string;
  projectId?: string;
  deploymentIds: string[];
  scoreLatency?: number;
  scoreCost?: number;
  scoreLoad?: number;
  scoreCache?: number;
};

export type APIKeyFormValues = {
  name: string;
  projectId: string;
  modelAllowlist?: string[];
  ipAllowlist?: string[];
};

export type APIKeyCreateResult = {
  Key: string;
};

export type MCPServerFormValues = {
  id: string;
  url: string;
  status: string;
};
