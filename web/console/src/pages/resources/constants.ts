import { activeStatus, nonEmptyArray } from "./helpers";

// Translated route strategy labels / descriptions, parameterized by the
// i18next function so both the parent page and the route section can share
// the same label tables.
export const strategyLabels = (
  t: (key: string) => string,
): Record<string, string> => ({
  priority: t("resources.strategyPriority"),
  weighted: t("resources.strategyWeighted"),
  round_robin: t("resources.strategyRoundRobin"),
  soft: t("resources.strategySoft"),
});

export const strategyDescriptions = (
  t: (key: string) => string,
): Record<string, string> => ({
  priority: t("resources.strategyPriorityDesc"),
  weighted: t("resources.strategyWeightedDesc"),
  round_robin: t("resources.strategyRoundRobinDesc"),
  soft: t("resources.strategySoftDesc"),
});

export const providerTypeOptions = [
  { value: "openai", label: "OpenAI" },
  { value: "anthropic", label: "Anthropic" },
  { value: "openai-compatible", label: "OpenAI Compatible" },
];

export const providerEndpointDefaults: Record<string, string> = {
  openai: "https://api.openai.com",
  anthropic: "https://api.anthropic.com",
  "openai-compatible": "",
};

export type DraftRowState = "new" | "modified" | "deleted" | "unchanged";

// Maps a UI resource key to its draft-config collection name.
export const DRAFT_CONFIG_KEYS: Record<string, string> = {
  providers: "providers",
  deployments: "deployments",
  routes: "route_policies",
  logicalModels: "logical_models",
  mcpServers: "mcp_servers",
};

// Per-resource field pickers used to canonicalize draft vs baseline rows.
export const DRAFT_FIELD_PICKERS: Record<
  string,
  (item: Record<string, unknown>) => Record<string, unknown>
> = {
  providers: (p) => ({
    id: p.id,
    type: p.type,
    endpoint: p.endpoint,
    status: activeStatus(p.status),
  }),
  deployments: (d) => ({
    id: d.id,
    provider_id: d.provider_id ?? d.providerId,
    credential_id: d.credential_id ?? d.credentialId,
    upstream_model: d.upstream_model ?? d.model,
    data_region: d.data_region ?? d.region,
    capabilities: nonEmptyArray(d.capabilities),
    context_window: d.context_window ?? d.contextWindow,
    priority: d.priority,
    status: activeStatus(d.status),
  }),
  routes: (r) => ({
    id: r.id,
    strategy: r.strategy,
    deployment_ids: nonEmptyArray(r.deployment_ids ?? r.deploymentIds),
  }),
  logicalModels: (m) => ({
    id: m.id,
    alias: m.alias,
    route_policy_id: m.route_policy_id ?? m.routePolicyId ?? m.routePolicyID,
  }),
  mcpServers: (s) => ({ id: s.id, url: s.url, status: s.status }),
};
