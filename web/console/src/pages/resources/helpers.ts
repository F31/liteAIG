import type { LogicalModel } from "./types";

export function newID(prefix: string) {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}`;
  return `${prefix}-${random}`;
}

export function uniqueAlias(base: string, models: LogicalModel[]) {
  const taken = new Set(models.map((model) => model.alias));
  if (!taken.has(base)) return base;
  let suffix = 2;
  while (taken.has(`${base}-${suffix}`)) suffix += 1;
  return `${base}-${suffix}`;
}

export function shortID(id?: string) {
  if (!id) return "-";
  return id.length > 18 ? `${id.slice(0, 10)}...${id.slice(-6)}` : id;
}

export const activeStatus = (value: unknown) =>
  value === "enabled" ? "active" : value;

export const nonEmptyArray = (value: unknown) =>
  Array.isArray(value) && value.length > 0 ? value : undefined;

// Draft config (snake_case) vs runtime view (camelCase): compare the fields
// that matter, normalized per resource, to avoid key-shape false positives.
export const canonicalize = (
  picker: (item: Record<string, unknown>) => Record<string, unknown>,
  item: Record<string, unknown>,
) => {
  const picked = picker(item);
  return JSON.stringify(
    Object.fromEntries(
      Object.keys(picked)
        .sort()
        .map((key) => [key, picked[key]]),
    ),
  );
};
