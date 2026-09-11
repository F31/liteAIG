import { Card, Descriptions, Empty, Table, Timeline, Typography } from "antd";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { api, RequestRecord } from "../api/client";
import LiveTail from "../components/LiveTail";
export default function Requests() {
  const { t, i18n } = useTranslation();
  const { id } = useParams();
  const detail = useQuery({
    queryKey: ["request", id],
    queryFn: () =>
      api<RequestRecord>(`/api/admin/requests/${encodeURIComponent(id!)}`),
    enabled: Boolean(id),
  });
  const history = useQuery({
    queryKey: ["request-history"],
    queryFn: () => api<RequestRecord[]>("/api/admin/requests"),
    enabled: !id,
  });
  const record = detail.data;
  const tenantId = record?.tenantId ?? "";
  const attempts = (record?.attempts ?? []).map((raw) => {
    const attempt = raw as Record<string, unknown>;
    return {
      deploymentId: String(
        attempt.deploymentId ??
          attempt.DeploymentID ??
          record?.deploymentId ??
          "-",
      ),
      outcome: String(
        attempt.outcome ?? attempt.Outcome ?? record?.outcome ?? "-",
      ),
      number: Number(attempt.number ?? attempt.Number ?? 1),
    };
  });
  const routeEvidence = (record?.routeEvidence ?? []).map((raw) => {
    const evidence = raw as Record<string, unknown>;
    const exclusions = evidence.exclusions ?? evidence.Exclusions ?? [];
    return {
      deploymentId: String(
        evidence.deploymentId ?? evidence.DeploymentID ?? "-",
      ),
      eligible: Boolean(evidence.eligible ?? evidence.Eligible),
      exclusions: Array.isArray(exclusions)
        ? exclusions.map((item) => String(item))
        : [],
    };
  });
  const cost =
    record?.providerCost === undefined
      ? ""
      : new Intl.NumberFormat(i18n.language, {
          style: "currency",
          currency: record.providerCurrency || "USD",
        }).format(record.providerCost);
  const detailItems = record
    ? [
        ...(
          [
            ["requestId", record.requestId],
            ["outcome", record.outcome],
            ["model", record.logicalModel],
            ["deployment", record.deploymentId],
            ["inputTokens", record.inputTokens],
            ["outputTokens", record.outputTokens],
          ] as const
        ).map(([key, value]) => ({
          key,
          label: t(`requests.${key}`),
          children: String(value ?? ""),
        })),
        {
          key: "key",
          label: t("details.key", { ns: "requests" }),
          children: record.keyId,
        },
        {
          key: "route",
          label: t("details.route", { ns: "requests" }),
          children: <pre>{JSON.stringify(record.routeEvidence, null, 2)}</pre>,
        },
        {
          key: "attempts",
          label: t("details.attempts", { ns: "requests" }),
          children: <pre>{JSON.stringify(record.attempts, null, 2)}</pre>,
        },
        {
          key: "guardrail",
          label: t("details.guardrail", { ns: "requests" }),
          children: record.guardrailStatus,
        },
        {
          key: "cost",
          label: t("details.cost", { ns: "requests" }),
          children: cost,
        },
        {
          key: "latency",
          label: t("details.latency", { ns: "requests" }),
          children: String(record.latencyMS),
        },
        {
          key: "snapshot",
          label: t("details.snapshot", { ns: "requests" }),
          children: String(record.snapshotVersion),
        },
        {
          key: "security",
          label: t("details.security", { ns: "requests" }),
          children: String(record.securityEpoch),
        },
        {
          key: "source",
          label: t("details.source", { ns: "requests" }),
          children: record.source,
        },
      ]
    : [];
  return (
    <section>
      <Typography.Title className="page-title">
        {t("requests.title")}
      </Typography.Title>
      <Card loading={detail.isLoading || history.isLoading}>
        {id ? (
          record ? (
            <>
              <Descriptions column={{ xs: 1, sm: 2 }} items={detailItems} />
              <Card
                className="result-panel"
                size="small"
                title={t("timeline.title", { ns: "ops" })}
              >
                <Timeline
                  items={[
                    {
                      children: `${t("timeline.stage", { ns: "ops" })}: admission → input guardrail → preflight`,
                    },
                    {
                      children: `${t("timeline.stage", { ns: "ops" })}: resolution · ${t("timeline.candidates", { ns: "ops", count: routeEvidence.length })}`,
                    },
                    ...routeEvidence.map((evidence) => ({
                      color: evidence.eligible ? "green" : "gray",
                      children: `${t("timeline.evidence", { ns: "ops" })}: ${evidence.deploymentId} · ${
                        evidence.eligible
                          ? t("timeline.eligible", { ns: "ops" })
                          : evidence.exclusions.join(", ") ||
                            t("timeline.excluded", { ns: "ops" })
                      }`,
                    })),
                    ...attempts.map((attempt) => ({
                      color: attempt.outcome === "success" ? "green" : "red",
                      children: `${t("timeline.attempt", { ns: "ops" })} #${attempt.number} · ${attempt.deploymentId} · ${attempt.outcome}`,
                    })),
                    {
                      color: record.outcome === "success" ? "green" : "red",
                      children: `${t("timeline.stage", { ns: "ops" })}: output guardrail → accounting · ${record.outcome}`,
                    },
                  ]}
                />
              </Card>
            </>
          ) : (
            <Empty description={t("requests.notFound")} />
          )
        ) : (
          <Table
            rowKey="requestId"
            dataSource={history.data ?? []}
            locale={{ emptyText: t("common.empty") }}
            columns={[
              { title: t("requests.requestId"), dataIndex: "requestId" },
              { title: t("requests.outcome"), dataIndex: "outcome" },
              { title: t("requests.model"), dataIndex: "logicalModel" },
              { title: t("requests.deployment"), dataIndex: "deploymentId" },
            ]}
          />
        )}
      </Card>
      {!id && <LiveTail tenantId={tenantId} />}
    </section>
  );
}
