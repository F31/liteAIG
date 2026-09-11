import { Card, Statistic, Typography } from "antd";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";

type DashboardView = {
  requests: number;
  inputTokens: number;
  outputTokens: number;
  avgLatencyMS: number;
  configVersion: number;
  spend?: number;
};

export default function Dashboard() {
  const { t } = useTranslation();
  const dashboard = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api<DashboardView>("/api/admin/dashboard"),
  });
  return (
    <section>
      <div className="page-title">
        <Typography.Title>{t("dashboard.title")}</Typography.Title>
        <Typography.Text type="secondary">
          {t("dashboard.subtitle")}
        </Typography.Text>
      </div>
      <div className="metric-grid">
        <Card loading={dashboard.isLoading}>
          <Statistic
            title={t("dashboard.requests")}
            value={dashboard.data?.requests ?? 0}
          />
        </Card>
        <Card loading={dashboard.isLoading}>
          <Statistic
            title={t("dashboard.tokens")}
            value={
              (dashboard.data?.inputTokens ?? 0) +
              (dashboard.data?.outputTokens ?? 0)
            }
          />
        </Card>
        <Card loading={dashboard.isLoading}>
          <Statistic
            title={t("dashboard.latency")}
            value={dashboard.data?.avgLatencyMS ?? 0}
            suffix="ms"
          />
        </Card>
        <Card loading={dashboard.isLoading}>
          <Statistic
            title={t("dashboard.configuration")}
            value={dashboard.data?.configVersion ?? 0}
          />
        </Card>
        <Card loading={dashboard.isLoading}>
          <Statistic
            title={t("dashboard.spend")}
            value={dashboard.data?.spend ?? 0}
            precision={4}
          />
        </Card>
      </div>
    </section>
  );
}
