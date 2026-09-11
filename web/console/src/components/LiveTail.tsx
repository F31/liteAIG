import { useState } from "react";
import { Badge, Card, List, Typography } from "antd";
import { useTranslation } from "react-i18next";
import { useSSEStream } from "../hooks/useSSEStream";

type LiveSummary = {
  requestId: string;
  outcome: string;
  deploymentId: string;
  latencyMS: number;
};

export default function LiveTail({ tenantId }: { tenantId: string }) {
  const { t } = useTranslation();
  const [events, setEvents] = useState<LiveSummary[]>([]);
  const [connected, setConnected] = useState(false);

  useSSEStream(
    "/api/admin/live",
    tenantId,
    (event) => {
      if (event.event === "request.summary") {
        try {
          const summary = JSON.parse(event.data) as LiveSummary;
          setEvents((current) => [summary, ...current].slice(0, 50));
        } catch {
          // ignore malformed events
        }
      }
    },
    { onStateChange: setConnected },
  );

  return (
    <Card
      className="result-panel"
      title={t("live.title", { ns: "ops" })}
      extra={
        <Badge
          status={connected ? "success" : "warning"}
          text={
            connected
              ? t("live.connected", { ns: "ops" })
              : t("live.reconnecting", { ns: "ops" })
          }
        />
      }
    >
      <Typography.Paragraph type="secondary">
        {t("live.summaryOnly", { ns: "ops" })}
      </Typography.Paragraph>
      <List
        dataSource={events}
        locale={{ emptyText: t("common.empty") }}
        renderItem={(item) => (
          <List.Item>
            <List.Item.Meta
              title={item.requestId}
              description={`${item.outcome} · ${item.deploymentId} · ${item.latencyMS} ms`}
            />
          </List.Item>
        )}
      />
    </Card>
  );
}
