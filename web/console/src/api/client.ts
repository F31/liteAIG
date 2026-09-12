export type APIErrorBody = {
  error: { code: string; params?: Record<string, unknown> };
};
export class APIError extends Error {
  constructor(
    public code: string,
    public params?: Record<string, unknown>,
  ) {
    super(code);
  }
}
// errorMessageKey returns a stable i18n errors.* key for a failed request, or
// null when the failure has no specific code worth surfacing.
export function errorMessageKey(error: unknown): string | null {
  if (
    error instanceof APIError &&
    error.code &&
    error.code !== "INTERNAL_ERROR"
  ) {
    return error.code;
  }
  return null;
}
export const UNAUTHORIZED_EVENT = "lia:unauthorized";
let csrfToken = "";
let unauthorizedHandled = false;
export const setCSRFToken = (value: string) => {
  csrfToken = value;
  unauthorizedHandled = false;
};
export const clearCSRFToken = () => {
  csrfToken = "";
};
export const getCSRFToken = () => csrfToken;
// Endpoints where a 401 is an expected outcome (wrong password, one-time
// bootstrap, a failed reset-code confirm) rather than a stale-session signal.
const AUTH_EXEMPT = (path: string) =>
  path.startsWith("/api/admin/setup") ||
  path === "/api/admin/session" ||
  path === "/api/admin/oidc/session" ||
  path === "/api/admin/password/reset/confirm" ||
  path === "/api/admin/password/forgot" ||
  // Wrong current password (WRONG_PASSWORD) and a missing session on the
  // loopback-only emergency reset (UNAUTHORIZED) are both expected outcomes;
  // treating them as a stale session would log the operator out mid-action.
  path === "/api/admin/password/change" ||
  path === "/api/admin/password/reset";
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type"))
    headers.set("Content-Type", "application/json");
  if (
    csrfToken &&
    !["GET", "HEAD"].includes((init.method ?? "GET").toUpperCase())
  )
    headers.set("X-CSRF-Token", csrfToken);
  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "include",
    cache: "no-store",
  });
  const body = await response
    .json()
    .catch(() => ({ error: { code: "INTERNAL_ERROR" } }));
  if (!response.ok) {
    const error = body as APIErrorBody;
    if (
      response.status === 401 &&
      error.error?.code !== "REAUTH_REQUIRED" &&
      !AUTH_EXEMPT(path) &&
      !unauthorizedHandled
    ) {
      unauthorizedHandled = true;
      clearCSRFToken();
      window.dispatchEvent(new CustomEvent(UNAUTHORIZED_EVENT));
    }
    throw new APIError(
      error.error?.code ?? "INTERNAL_ERROR",
      error.error?.params,
    );
  }
  return body as T;
}
export type PlaygroundResult = {
  requestId: string;
  output: string;
  selectedDeployment: string;
  inputTokens: number;
  outputTokens: number;
  cost?: number;
  latencyMS: number;
};
export type RequestRecord = {
  requestId: string;
  tenantId: string;
  projectId: string;
  keyId: string;
  logicalModel: string;
  deploymentId: string;
  outcome: string;
  inputTokens: number;
  outputTokens: number;
  routeEvidence: unknown[];
  attempts: unknown[];
  guardrailStatus: string;
  providerCost?: number;
  providerCurrency?: string;
  source: string;
  latencyMS: number;
  snapshotVersion: number;
  securityEpoch: number;
};
export type FinOpsView = {
  requests: number;
  inputTokens: number;
  outputTokens: number;
  spend: number;
  retryCost: number;
  fallbackCost: number;
  cacheHits: number;
  semanticHits: number;
  cacheHitRate: number;
  semanticHitRate: number;
  savedTokens: number;
  pricedSavedTokens: number;
  unpricedSavedTokens: number;
  cacheSavings: number;
  estimateVersion: string;
  byProject: {
    projectId: string;
    requests: number;
    inputTokens: number;
    outputTokens: number;
    spend: number;
    cacheHits: number;
    semanticHits: number;
    cacheHitRate: number;
    savedTokens: number;
    pricedSavedTokens: number;
    unpricedSavedTokens: number;
    cacheSavings: number;
  }[];
  byModel: {
    logicalModel: string;
    requests: number;
    inputTokens: number;
    outputTokens: number;
    spend: number;
    retryCount: number;
    fallbackCount: number;
    cacheHits: number;
    semanticHits: number;
    cacheHitRate: number;
    savedTokens: number;
    pricedSavedTokens: number;
    unpricedSavedTokens: number;
    cacheSavings: number;
  }[];
  recommendations: {
    kind: string;
    title: string;
    detail: string;
    impact?: number;
  }[];
};
export type RecommendationView = {
  id: string;
  kind: string;
  title: string;
  explanation: string;
  change: Record<string, unknown>;
  metric: string;
  samples: number;
  from: string;
  to: string;
  createdAt: string;
  acceptable: boolean;
};
export type RuntimeResources = {
  providers: unknown[];
  credentials: unknown[];
  deployments: unknown[];
  logicalModels: unknown[];
  routes: unknown[];
  keys: unknown[];
  mcpServers: { id: string; url: string; status: string }[];
  tools: {
    id: string;
    name: string;
    status: string;
    dataClassification?: string;
  }[];
  agents: { id: string; name: string; status: string }[];
  budgets: {
    id: string;
    projectId?: string;
    keyId?: string;
    windowHours: number;
    tokenLimit: number;
    mode: string;
    consistency: "regional" | "global_soft" | "global_hard";
  }[];
  cache: {
    enabled: boolean;
    ttlSeconds: number;
    namespaceVersion: number;
    semantic?: { enabled: boolean; model?: string; threshold?: number };
  };
  guardrail: {
    mode: string;
    ruleCount: number;
    judge: { enabled: boolean; model?: string; action?: string };
    groundedness: { enabled: boolean; minOverlap?: number };
  };
};
export type ResourceCatalogView = {
  providers: {
    id: string;
    label: string;
    type: string;
    endpoint: string;
  }[];
  models: {
    id: string;
    providerId: string;
    model: string;
    capabilities: string[];
    contextWindow?: number;
  }[];
  regions: { id: string; label: string }[];
};
export type HealthView = {
  providers: { id: string; type: string; healthy: boolean; reason?: string }[];
  circuits: { deploymentId: string; credentialId: string; state: string }[];
  ready: boolean;
  drift: string;
};
export type AlertView = {
  id: string;
  ruleId: string;
  severity: string;
  status: string;
  message: string;
  firedAt: string;
  evidence: Record<string, string>;
};
export type RuleView = {
  id?: string;
  name: string;
  ruleType: string;
  metric: string;
  operator: string;
  threshold: number;
  severity: string;
  enabled: boolean;
};
export type NotificationTargetView = {
  url: string;
  minSeverity: "low" | "medium" | "high" | "critical";
};
export type NotificationSettingsView = {
  webhookUrl: string;
  targets?: NotificationTargetView[];
  dedupSeconds?: number;
  enabled: boolean;
  requiresRestart: boolean;
  updatedAt?: string;
};
export type RebaseResult = {
  rebased: boolean;
  conflict: boolean;
  revision: number;
  messageKey?: string;
};
export type SecurityEventView = {
  id: string;
  ruleId: string;
  action: string;
  occurredAt: string;
};
export type ToolCallView = {
  id: string;
  toolName: string;
  sessionId: string;
  taskId: string;
  agentId: string;
  occurredAt: string;
};
export type DelegationGrantView = {
  id: string;
  delegatorId: string;
  delegateeId: string;
  permissions: string[];
  createdBy: string;
  createdAt: string;
};
export type SimulateResult = {
  selected: string;
  fallback: string[];
  evidence: {
    deploymentId: string;
    eligible: boolean;
    score: number;
    breakdown: Record<string, number>;
  }[];
};
export type FederationView = {
  relationships: {
    id: string;
    externalAgentID: string;
    status: string;
    assuranceLevel: string;
    hasVerifiedAnchor: boolean;
    dataBoundary: string;
  }[];
  externalAgents: {
    id: string;
    name: string;
    externalSubject: string;
    trustBoundary: string;
    status: string;
  }[];
  procurement: { relationshipId: string; limit: number; spent: number }[];
};
export type ApprovalView = {
  id: string;
  requester: string;
  action: string;
  target: string;
  status: string;
  approvers: number;
  dualApproval: boolean;
};
export type AgentGraphView = {
  rootTask: string;
  totalCost: number;
  hops: {
    order: number;
    agentId: string;
    model: string;
    cost: number;
    outcome: string;
  }[];
};
export type SetupStatus = {
  initialized: boolean;
  /** ready is true once a first config version is published; a false value
   *  means a previous setup attempt was interrupted and is retryable. */
  ready?: boolean;
  tenantName?: string;
  adminUsername?: string;
};

export type AuditRecord = {
  id: string;
  action: string;
  resourceType: string;
  resourceId: string;
  result: string;
  actor?: string;
  occurredAt?: string;
};
