import { useEffect, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Input,
  message,
  Popconfirm,
  Select,
  Space,
  Table,
  Typography,
} from "antd";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, errorMessageKey, RebaseResult } from "../api/client";

type Diff = {
  path: string;
  operation: string;
  before?: string;
  after?: string;
  sensitive: boolean;
};

type DraftSummary = {
  id: string;
  status: string;
  baseVersion: number;
  revision: number;
  updatedAt: string;
};

type DraftDetail = {
  ID: string;
  Revision: number;
  Status: string;
  Config: Record<string, unknown>;
};

type VersionRow = {
  id: string;
  version: number;
  publishedBy: string;
  publishedAt: string;
};

export default function Config() {
  const { t } = useTranslation();
  const [draftId, setDraftId] = useState<string>();
  const [editor, setEditor] = useState<string>("");
  const [revision, setRevision] = useState(1);
  const [rebase, setRebase] = useState<RebaseResult | undefined>();

  const drafts = useQuery({
    queryKey: ["config-drafts"],
    queryFn: () => api<DraftSummary[]>("/api/admin/config/drafts"),
  });
  const versions = useQuery({
    queryKey: ["config-versions"],
    queryFn: () => api<VersionRow[]>("/api/admin/config/versions"),
  });
  const detail = useQuery({
    queryKey: ["config-draft", draftId],
    queryFn: () =>
      api<DraftDetail>(
        `/api/admin/config/drafts/${encodeURIComponent(draftId!)}`,
      ),
    enabled: Boolean(draftId),
  });
  const diff = useQuery({
    queryKey: ["config-diff", draftId],
    queryFn: () =>
      api<Diff[]>(
        `/api/admin/config/drafts/${encodeURIComponent(draftId!)}/diff`,
      ),
    enabled: Boolean(draftId),
  });

  useEffect(() => {
    if (detail.data) {
      setEditor(JSON.stringify(detail.data.Config, null, 2));
      setRevision(detail.data.Revision);
    }
  }, [detail.data]);

  const save = useMutation({
    mutationFn: () => {
      let config: Record<string, unknown>;
      try {
        config = JSON.parse(editor) as Record<string, unknown>;
      } catch {
        throw new Error("config.invalidJson");
      }
      return api<DraftDetail>(
        `/api/admin/config/drafts/${encodeURIComponent(draftId!)}`,
        {
          method: "PUT",
          body: JSON.stringify({ revision, config }),
        },
      );
    },
    onSuccess: () => {
      void message.success(t("config.saved"));
      void diff.refetch();
      void drafts.refetch();
    },
    onError: (error: Error) => {
      const key =
        error.message === "config.invalidJson"
          ? "config.invalidJson"
          : "common.error";
      void message.error(t(key));
    },
  });

  const publish = useMutation({
    mutationFn: () =>
      api(`/api/admin/config/drafts/${encodeURIComponent(draftId!)}/publish`, {
        method: "POST",
        body: JSON.stringify({ revision }),
      }),
    onSuccess: () => {
      void message.success(t("config.published"));
      void versions.refetch();
      void drafts.refetch();
      void detail.refetch();
    },
  });

  const rebaseDraft = useMutation({
    mutationFn: () =>
      api<RebaseResult>("/api/admin/config/rebase", {
        method: "POST",
        body: JSON.stringify({ draftId }),
      }),
    onSuccess: (result) => setRebase(result),
  });

  const rollback = useMutation({
    mutationFn: (version: number) =>
      api("/api/admin/config/rollback", {
        method: "POST",
        body: JSON.stringify({ version }),
      }),
    onSuccess: (_data, version) => {
      void message.success(t("config.rolledBack", { version }));
      void versions.refetch();
      void drafts.refetch();
    },
    onError: (error: unknown) => {
      const code = errorMessageKey(error);
      void message.error(
        code
          ? t(`errors.${code}`, { defaultValue: t("common.error") })
          : t("common.error"),
      );
    },
  });
  const latestVersion = (versions.data ?? []).reduce(
    (max, row) => Math.max(max, row.version),
    0,
  );

  const diffColumns = [
    { title: t("config.path"), dataIndex: "path" },
    { title: t("config.operation"), dataIndex: "operation" },
    {
      title: t("config.before"),
      render: (_: unknown, row: Diff) =>
        row.sensitive ? t("config.secret") : row.before,
    },
    {
      title: t("config.after"),
      render: (_: unknown, row: Diff) =>
        row.sensitive ? t("config.secret") : row.after,
    },
  ];

  return (
    <section>
      <Typography.Title className="page-title">
        {t("config.title")}
      </Typography.Title>
      <Card loading={drafts.isLoading}>
        <Space wrap>
          <Select
            aria-label={t("config.draft")}
            style={{ minWidth: 320 }}
            placeholder={t("config.selectDraft")}
            value={draftId}
            onChange={setDraftId}
            options={(drafts.data ?? []).map((draft) => ({
              value: draft.id,
              label: `${draft.id.slice(0, 8)} · r${draft.revision} · ${draft.status}`,
            }))}
          />
          <Input
            type="number"
            value={revision}
            onChange={(event) => setRevision(Number(event.target.value))}
            aria-label={t("config.revision")}
            style={{ width: 120 }}
          />
          <Button
            loading={save.isPending}
            disabled={!draftId || !editor}
            onClick={() => save.mutate()}
          >
            {t("config.save")}
          </Button>
          <Button
            loading={rebaseDraft.isPending}
            disabled={!draftId}
            onClick={() => rebaseDraft.mutate()}
          >
            {t("rebase.rebase", { ns: "ops" })}
          </Button>
          <Button
            type="primary"
            loading={publish.isPending}
            disabled={!draftId}
            onClick={() => publish.mutate()}
          >
            {t("config.publish")}
          </Button>
        </Space>

        {rebase?.rebased && (
          <Alert
            className="result-panel"
            type="success"
            message={t("rebase.rebased", { ns: "ops" })}
          />
        )}
        {rebase?.conflict && (
          <Alert
            className="result-panel"
            type="error"
            message={t("rebase.conflict", { ns: "ops" })}
          />
        )}

        {draftId && (
          <>
            <Input.TextArea
              className="result-panel"
              rows={14}
              value={editor}
              onChange={(event) => setEditor(event.target.value)}
              aria-label={t("config.editor")}
              spellCheck={false}
            />
            <Table
              className="result-panel"
              rowKey="path"
              dataSource={diff.data ?? []}
              pagination={false}
              locale={{ emptyText: t("common.empty") }}
              columns={diffColumns}
            />
          </>
        )}
      </Card>

      <Card className="result-panel" title={t("config.versions")}>
        <Table
          rowKey="id"
          dataSource={versions.data ?? []}
          pagination={false}
          locale={{ emptyText: t("common.empty") }}
          columns={[
            { title: t("config.version"), dataIndex: "version" },
            { title: t("config.publishedBy"), dataIndex: "publishedBy" },
            { title: t("config.publishedAt"), dataIndex: "publishedAt" },
            {
              title: t("config.actions"),
              key: "actions",
              render: (_: unknown, row: VersionRow) => {
                const isLatest = row.version === latestVersion;
                return (
                  <Popconfirm
                    title={t("config.confirmRollback", {
                      version: row.version,
                    })}
                    okText={t("config.rollback")}
                    cancelText={t("common.close")}
                    okButtonProps={{ danger: true }}
                    onConfirm={() => rollback.mutate(row.version)}
                  >
                    <Button
                      size="small"
                      danger
                      disabled={isLatest || rollback.isPending}
                    >
                      {t("config.rollback")}
                    </Button>
                  </Popconfirm>
                );
              },
            },
          ]}
        />
      </Card>
    </section>
  );
}
