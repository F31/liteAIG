import { Card, List, Statistic, Table, Tag, Typography } from "antd";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type FinOpsView, type RecommendationView } from "../api/client";

const money = (value?: number) => (value ?? 0).toFixed(4);
const percent = (value?: number) =>
  `${(((value ?? 0) as number) * 100).toFixed(1)}%`;

export default function FinOps() {
  const { t } = useTranslation();
  const finops = useQuery({
    queryKey: ["finops"],
    queryFn: () => api<FinOpsView>("/api/admin/finops"),
  });
  const recommendations = useQuery({
    queryKey: ["recommendations"],
    queryFn: () => api<RecommendationView[]>("/api/admin/recommendations"),
  });
  return (
    <section>
      <div className="page-title">
        <Typography.Title>{t("finops.title")}</Typography.Title>
        <Typography.Text type="secondary">
          {t("finops.subtitle")}
        </Typography.Text>
      </div>
      <div className="metric-grid">
        <Card loading={finops.isLoading}>
          <Statistic
            title={t("finops.spend")}
            value={finops.data?.spend ?? 0}
            precision={4}
          />
        </Card>
        <Card loading={finops.isLoading}>
          <Statistic
            title={t("finops.requests")}
            value={finops.data?.requests ?? 0}
          />
        </Card>
        <Card loading={finops.isLoading}>
          <Statistic
            title={t("finops.tokens")}
            value={
              (finops.data?.inputTokens ?? 0) + (finops.data?.outputTokens ?? 0)
            }
          />
        </Card>
        <Card loading={finops.isLoading}>
          <Statistic
            title={t("finops.cacheHits")}
            value={
              (finops.data?.cacheHits ?? 0) + (finops.data?.semanticHits ?? 0)
            }
            suffix={percent(finops.data?.cacheHitRate)}
          />
        </Card>
        <Card loading={finops.isLoading}>
          <Statistic
            title={t("finops.semanticCacheHits")}
            value={finops.data?.semanticHits ?? 0}
            suffix={percent(finops.data?.semanticHitRate)}
          />
        </Card>
        <Card loading={finops.isLoading}>
          <Statistic
            title={t("finops.extraCost")}
            value={
              (finops.data?.retryCost ?? 0) + (finops.data?.fallbackCost ?? 0)
            }
            precision={4}
          />
        </Card>
      </div>
      <Card title={t("finops.recommendations")} loading={finops.isLoading}>
        <List
          dataSource={finops.data?.recommendations ?? []}
          locale={{ emptyText: t("common.empty") }}
          renderItem={(item) => (
            <List.Item>
              <List.Item.Meta
                title={
                  <span>
                    <Tag>{item.kind}</Tag>
                    {item.title}
                    {item.impact ? ` ${money(item.impact)}` : ""}
                  </span>
                }
                description={item.detail}
              />
            </List.Item>
          )}
        />
      </Card>
      <Card
        title={t("finops.configRecommendations")}
        loading={recommendations.isLoading}
      >
        <List
          dataSource={recommendations.data ?? []}
          locale={{ emptyText: t("common.empty") }}
          renderItem={(item) => (
            <List.Item>
              <List.Item.Meta
                title={
                  <span>
                    <Tag color={item.acceptable ? "green" : "default"}>
                      {item.kind}
                    </Tag>
                    {item.title}
                  </span>
                }
                description={`${item.explanation} · ${item.metric} · ${item.samples} ${t("finops.samples")}`}
              />
            </List.Item>
          )}
        />
      </Card>
      <Card title={t("finops.byProject")} loading={finops.isLoading}>
        <Table
          rowKey="projectId"
          dataSource={finops.data?.byProject ?? []}
          pagination={false}
          columns={[
            { title: t("finops.project"), dataIndex: "projectId" },
            { title: t("finops.requests"), dataIndex: "requests" },
            {
              title: t("finops.tokens"),
              render: (_, row) => row.inputTokens + row.outputTokens,
            },
            { title: t("finops.spend"), render: (_, row) => money(row.spend) },
          ]}
        />
      </Card>
      <Card title={t("finops.byModel")} loading={finops.isLoading}>
        <Table
          rowKey="logicalModel"
          dataSource={finops.data?.byModel ?? []}
          pagination={false}
          columns={[
            { title: t("finops.model"), dataIndex: "logicalModel" },
            { title: t("finops.requests"), dataIndex: "requests" },
            { title: t("finops.spend"), render: (_, row) => money(row.spend) },
            { title: t("finops.retries"), dataIndex: "retryCount" },
            { title: t("finops.fallbacks"), dataIndex: "fallbackCount" },
          ]}
        />
      </Card>
    </section>
  );
}
