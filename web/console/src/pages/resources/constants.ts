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

// Canonical model capability vocabulary. Keep in sync with the backend
// vocabulary in internal/controlplane/config/capabilities.go.
export const CANONICAL_CAPABILITIES = [
  "chat",
  "vision",
  "reasoning",
  "tools",
  "embedding",
  "rerank",
  "audio",
  "speech",
] as const;

const CAPABILITY_ALIASES: Record<string, string> = {
  embeddings: "embedding",
  embed: "embedding",
  multimodal: "vision",
  vlm: "vision",
  "vision-language": "vision",
  image: "vision",
  function_calling: "tools",
  "function-calling": "tools",
  tool_calling: "tools",
  "tool-calling": "tools",
  stt: "audio",
  transcription: "audio",
  translation: "audio",
  asr: "audio",
  tts: "speech",
  "text-to-speech": "speech",
  "speech-synthesis": "speech",
};

// normalizeCapability maps legacy/synonym tokens onto the canonical vocabulary.
export const normalizeCapability = (value: string): string => {
  const token = value.trim().toLowerCase();
  return CAPABILITY_ALIASES[token] ?? token;
};

// canonicalCapabilities normalizes, de-duplicates and drops empty capability
// tokens (mirroring the backend canonicalizer) for draft diffing.
export const canonicalCapabilities = (
  values: unknown,
): string[] | undefined => {
  if (!Array.isArray(values)) return undefined;
  const out: string[] = [];
  for (const raw of values) {
    const value = normalizeCapability(String(raw));
    if (value && !out.includes(value)) out.push(value);
  }
  return out.length > 0 ? out : undefined;
};

export const capabilityOptions = (t: (key: string) => string) =>
  CANONICAL_CAPABILITIES.map((value) => ({
    value,
    label: t(`resources.capability_${value}`),
  }));

export const capabilityLabel = (
  t: (key: string) => string,
  value: string,
): string => {
  const canonical = normalizeCapability(value);
  const option = capabilityOptions(t).find((item) => item.value === canonical);
  return option?.label ?? value;
};

// capabilityColor gives each capability family a distinct, stable tag color so
// speech/vision/audio deployments are visually distinguishable at a glance.
export const capabilityColor = (value: string): string => {
  switch (normalizeCapability(value)) {
    case "chat":
      return "default";
    case "vision":
      return "purple";
    case "reasoning":
      return "gold";
    case "tools":
      return "geekblue";
    case "embedding":
      return "green";
    case "rerank":
      return "orange";
    case "audio":
      return "blue";
    case "speech":
      return "cyan";
    default:
      return "default";
  }
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
    capabilities: canonicalCapabilities(d.capabilities),
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
