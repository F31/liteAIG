import { useEffect, useMemo, useRef, useState } from "react";
import { message } from "antd";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, APIError } from "../../api/client";
import { canonicalize } from "./helpers";
import {
  DRAFT_CONFIG_KEYS,
  DRAFT_FIELD_PICKERS,
  type DraftRowState,
} from "./constants";
import type {
  ConfigDiagnostic,
  ConfigVersion,
  DraftDetail,
  DraftSummary,
  TenantConfig,
} from "./types";

// useTenantConfigDraft owns the tenant config draft state machine shared by
// every resource editor on the Resources page: starting a draft, the
// serialized auto-save chain (with revision-conflict resync), publishing with
// validation diagnostics, and restoring an abandoned draft on page load.
export function useTenantConfigDraft(options: {
  canWrite: boolean;
  onPublished: () => void;
}) {
  const { canWrite, onPublished } = options;
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [providerDraft, setProviderDraft] = useState<DraftDetail>();
  const [draftBaseline, setDraftBaseline] = useState<TenantConfig>();
  const [ignoredDraftID, setIgnoredDraftID] = useState<string>();
  const [draftDirty, setDraftDirty] = useState(false);
  const [publishDiagnostics, setPublishDiagnostics] = useState<
    ConfigDiagnostic[]
  >([]);
  const drafts = useQuery({
    queryKey: ["config-drafts"],
    queryFn: () => api<DraftSummary[]>("/api/admin/config/drafts"),
    enabled: canWrite,
  });
  const versions = useQuery({
    queryKey: ["config-versions"],
    queryFn: () => api<ConfigVersion[]>("/api/admin/config/versions"),
    enabled: canWrite,
  });
  const latestVersion = useMemo(
    () =>
      Math.max(
        0,
        ...(versions.data ?? []).map(
          (version) => version.Version ?? version.version ?? 0,
        ),
      ),
    [versions.data],
  );
  const activeDraftSummary = drafts.data?.find(
    (draft) =>
      draft.status === "editing" &&
      draft.baseVersion === latestVersion &&
      draft.id !== ignoredDraftID,
  );
  const restoredDraft = useQuery({
    queryKey: ["config-draft", activeDraftSummary?.id],
    queryFn: () =>
      api<DraftDetail>(
        `/api/admin/config/drafts/${encodeURIComponent(activeDraftSummary!.id)}`,
      ),
    enabled:
      canWrite &&
      latestVersion > 0 &&
      Boolean(activeDraftSummary?.id) &&
      !providerDraft,
  });
  const draftSaveChain = useRef<Promise<void>>(Promise.resolve());
  const latestDraftRef = useRef<DraftDetail | undefined>(undefined);
  const [savePending, setSavePending] = useState(false);

  const loadDraft = (draft: DraftDetail) => {
    latestDraftRef.current = draft;
    setProviderDraft(draft);
  };

  const persistDraft = (nextDraft: DraftDetail) => {
    setPublishDiagnostics([]);
    latestDraftRef.current = nextDraft;
    setProviderDraft(nextDraft);
    setDraftDirty(true);
    const run = draftSaveChain.current.then(async () => {
      const pending = latestDraftRef.current;
      if (!pending) return;
      setSavePending(true);
      try {
        const saved = await api<DraftDetail>(
          `/api/admin/config/drafts/${encodeURIComponent(pending.ID)}`,
          {
            method: "PUT",
            body: JSON.stringify({
              revision: pending.Revision,
              config: pending.Config,
            }),
          },
        );
        if (latestDraftRef.current?.ID === saved.ID) {
          const merged: DraftDetail =
            latestDraftRef.current === pending
              ? saved
              : {
                  ...saved,
                  Config: latestDraftRef.current.Config,
                };
          latestDraftRef.current = merged;
          setProviderDraft(merged);
        }
      } catch (error) {
        if (error instanceof APIError && error.code === "REVISION_CONFLICT") {
          const resynced = await api<DraftDetail>(
            `/api/admin/config/drafts/${encodeURIComponent(pending.ID)}`,
          );
          const merged: DraftDetail = {
            ...resynced,
            Config: latestDraftRef.current?.Config ?? pending.Config,
          };
          latestDraftRef.current = merged;
          setProviderDraft(merged);
          const saved = await api<DraftDetail>(
            `/api/admin/config/drafts/${encodeURIComponent(merged.ID)}`,
            {
              method: "PUT",
              body: JSON.stringify({
                revision: merged.Revision,
                config: merged.Config,
              }),
            },
          );
          if (latestDraftRef.current?.ID === saved.ID) {
            setProviderDraft(saved);
            latestDraftRef.current = saved;
          }
        } else {
          void message.error(t("common.error"));
        }
      } finally {
        setSavePending(false);
      }
    });
    draftSaveChain.current = run;
  };
  // updateConfig applies a draft-config mutation from the current draft
  // state. Callers that may have to start a draft first must use the local
  // draft value plus persistDraft instead, since state updates are async.
  const updateConfig = (updater: (config: TenantConfig) => TenantConfig) => {
    if (!providerDraft) return;
    persistDraft({
      ...providerDraft,
      Config: updater(providerDraft.Config),
    });
  };

  const startProviderDraft = useMutation({
    mutationFn: () =>
      api<DraftDetail>("/api/admin/config/drafts", {
        method: "POST",
        body: JSON.stringify({}),
      }),
    onSuccess: (draft) => {
      loadDraft(draft);
      setDraftBaseline(draft.Config);
      setIgnoredDraftID(undefined);
      setDraftDirty(false);
      void queryClient.invalidateQueries({ queryKey: ["config-drafts"] });
      void message.success(t("resources.draftReady"));
    },
  });
  const publishProviderDraft = useMutation({
    mutationFn: async (draft: DraftDetail) => {
      await draftSaveChain.current;
      const storedDraft = await api<DraftDetail>(
        `/api/admin/config/drafts/${encodeURIComponent(draft.ID)}`,
      );
      loadDraft(storedDraft);
      return api(
        `/api/admin/config/drafts/${encodeURIComponent(storedDraft.ID)}/publish`,
        {
          method: "POST",
          body: JSON.stringify({ revision: storedDraft.Revision }),
        },
      );
    },
    onSuccess: () => {
      const publishedID = latestDraftRef.current?.ID;
      setIgnoredDraftID(publishedID);
      latestDraftRef.current = undefined;
      setProviderDraft(undefined);
      setDraftBaseline(undefined);
      setPublishDiagnostics([]);
      setDraftDirty(false);
      onPublished();
      void queryClient.invalidateQueries({ queryKey: ["config-drafts"] });
      void queryClient.invalidateQueries({ queryKey: ["config-versions"] });
      void message.success(t("resources.draftPublished"));
    },
    onError: (error) => {
      if (error instanceof APIError && error.code === "CONFIG_INVALID") {
        const diagnostics = error.params?.diagnostics;
        setPublishDiagnostics(
          Array.isArray(diagnostics) ? (diagnostics as ConfigDiagnostic[]) : [],
        );
      } else {
        setPublishDiagnostics([]);
      }
    },
  });

  useEffect(() => {
    if (!restoredDraft.data || providerDraft) return;
    const baseline = versions.data?.find(
      (version) =>
        (version.Version ?? version.version) === restoredDraft.data.BaseVersion,
    );
    loadDraft(restoredDraft.data);
    setDraftBaseline(
      baseline?.Config ?? baseline?.config ?? restoredDraft.data.Config,
    );
    setDraftDirty(true);
  }, [providerDraft, restoredDraft.data, versions.data]);

  return {
    providerDraft,
    draftBaseline,
    draftDirty,
    savePending,
    publishDiagnostics,
    startProviderDraft,
    publishProviderDraft,
    loadDraft,
    persistDraft,
    updateConfig,
  };
}

// draftRowStates compares the live draft against the baseline row by row so
// the tables can badge new / modified / deleted resources.
export const draftRowStates = (
  draft: DraftDetail | undefined,
  baseline: TenantConfig | undefined,
) => {
  const maps: Record<string, Map<string, DraftRowState>> = {};
  for (const resource of Object.keys(DRAFT_CONFIG_KEYS)) {
    const map = new Map<string, DraftRowState>();
    maps[resource] = map;
    if (!draft || !baseline) continue;
    const draftItems = ((draft.Config as Record<string, unknown>)[
      DRAFT_CONFIG_KEYS[resource]
    ] ?? []) as Record<string, unknown>[];
    const baselineItems = ((baseline as Record<string, unknown>)[
      DRAFT_CONFIG_KEYS[resource]
    ] ?? []) as Record<string, unknown>[];
    const picker = DRAFT_FIELD_PICKERS[resource];
    const baselineCanon = new Map<string, string>(
      baselineItems.map((item) => [
        String(item.id),
        canonicalize(picker, item),
      ]),
    );
    for (const item of draftItems) {
      const id = String(item.id);
      const base = baselineCanon.get(id);
      map.set(
        id,
        base === undefined
          ? "new"
          : base === canonicalize(picker, item)
            ? "unchanged"
            : "modified",
      );
    }
    const draftIds = new Set(draftItems.map((item) => String(item.id)));
    for (const item of baselineItems) {
      const id = String(item.id);
      if (!draftIds.has(id)) map.set(id, "deleted");
    }
  }
  return maps;
};
