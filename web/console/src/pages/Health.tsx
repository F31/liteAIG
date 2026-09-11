import { Card, Table, Tag, Typography } from "antd";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, HealthView } from "../api/client";

export default function Health() {
  const { t } = useTranslation();
  const health = useQuery({
    queryKey: ["health"],
    queryFn: () => api<HealthView>("/api/admin/health"),
  });

  const providerColumns = [
    {
      title: t("health.provider", { ns: "ops" }),
      dataIndex: "id",
      key: "id",
    },
    {
      title: t("health.type", { ns: "ops" }),
      dataIndex: "type",
      key: "type",
    },
    {
      title: t("health.state", { ns: "ops" }),
      key: "state",
      render: (_: unknown, row: HealthView["providers"][number]) => (
        <Tag color={row.healthy ? "green" : "red"}>
          {row.healthy
            ? t("health.healthy", { ns: "ops" })
            : t("health.unhealthy", { ns: "ops" })}
        </Tag>
      ),
    },
    {
      title: t("health.reason", { ns: "ops" }),
      key: "reason",
      render: (_: unknown, row: HealthView["providers"][number]) => {
        if (!row.reason) return "-";
        return (
          <Typography.Text type="secondary">
            {t(`health.reasons.${row.reason}`, {
              ns: "ops",
              defaultValue: row.reason,
            })}
          </Typography.Text>
        );
      },
    },
  ];

  const providers = health.data?.providers ?? [];

  return (
    <section>
      <Typography.Title className="page-title">
        {t("health.title", { ns: "ops" })}
      </Typography.Title>
      <Card loading={health.isLoading}>
        <Typography.Text>
          {health.data?.ready
            ? t("health.ready", { ns: "ops" })
            : t("health.notReady", { ns: "ops" })}
          {" · "}
          {t("health.drift", { ns: "ops" })}: {health.data?.drift ?? "none"}
        </Typography.Text>
        <Table
          className="result-panel"
          rowKey="id"
          dataSource={providers}
          pagination={false}
          locale={{ emptyText: t("common.empty") }}
          columns={providerColumns}
        />
      </Card>
    </section>
  );
}
