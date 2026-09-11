import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  api,
  AlertView,
  NotificationSettingsView,
  RuleView,
} from "../api/client";
import { isAdminRole, useAuth } from "../state/auth";

export default function Alerts() {
  const { t } = useTranslation();
  const role = useAuth((state) => state.role);
  const canWrite = isAdminRole(role);
  const alerts = useQuery({
    queryKey: ["alerts"],
    queryFn: () => api<AlertView[]>("/api/admin/alerts"),
  });
  const rules = useQuery({
    queryKey: ["alert-rules"],
    queryFn: () => api<RuleView[]>("/api/admin/alerts/rules"),
  });
  const notifications = useQuery({
    queryKey: ["alert-notifications"],
    queryFn: () =>
      api<NotificationSettingsView>("/api/admin/alerts/notifications"),
  });
  const action = useMutation({
    mutationFn: ({ id, action }: { id: string; action: string }) =>
      api(`/api/admin/alerts/${encodeURIComponent(id)}/action`, {
        method: "POST",
        body: JSON.stringify({ action }),
      }),
    onSuccess: () => alerts.refetch(),
  });
  const createRule = useMutation({
    mutationFn: (rule: RuleView) =>
      api<RuleView>("/api/admin/alerts/rules", {
        method: "POST",
        body: JSON.stringify(rule),
      }),
    onSuccess: () => rules.refetch(),
  });
  const importDefaultRules = useMutation({
    mutationFn: () =>
      api<RuleView[]>("/api/admin/alerts/rules/import-defaults", {
        method: "POST",
      }),
    onSuccess: () => rules.refetch(),
  });
  const updateNotifications = useMutation({
    mutationFn: (settings: NotificationSettingsView) =>
      api<NotificationSettingsView>("/api/admin/alerts/notifications", {
        method: "PUT",
        body: JSON.stringify(settings),
      }),
    onSuccess: () => notifications.refetch(),
  });
  const deleteNotifications = useMutation({
    mutationFn: () =>
      api("/api/admin/alerts/notifications", { method: "DELETE" }),
    onSuccess: () => notifications.refetch(),
  });

  const [form] = Form.useForm<RuleView>();
  const [notificationForm] = Form.useForm<NotificationSettingsView>();
  useEffect(() => {
    if (notifications.data) notificationForm.setFieldsValue(notifications.data);
  }, [notificationForm, notifications.data]);
  const severityColor: Record<string, string> = {
    low: "blue",
    medium: "orange",
    high: "red",
    critical: "magenta",
  };

  return (
    <section>
      <Typography.Title className="page-title">
        {t("alerts.title", { ns: "ops" })}
      </Typography.Title>
      <Card loading={alerts.isLoading}>
        <Table
          rowKey="id"
          dataSource={alerts.data ?? []}
          pagination={{ pageSize: 20 }}
          locale={{ emptyText: t("common.empty") }}
          columns={[
            { title: t("common.status"), dataIndex: "status", width: 120 },
            { title: t("alerts.metric", { ns: "ops" }), dataIndex: "message" },
            {
              title: t("alerts.severity", { ns: "ops" }),
              dataIndex: "severity",
              render: (value: string) => (
                <Tag color={severityColor[value] ?? "default"}>{value}</Tag>
              ),
            },
            {
              title: "",
              key: "action",
              render: (_, row) =>
                canWrite ? (
                  <Space>
                    {row.status === "firing" && (
                      <Button
                        size="small"
                        onClick={() =>
                          action.mutate({ id: row.id, action: "ack" })
                        }
                      >
                        {t("alerts.ack", { ns: "ops" })}
                      </Button>
                    )}
                    {row.status !== "resolved" && (
                      <Button
                        size="small"
                        onClick={() =>
                          action.mutate({ id: row.id, action: "resolve" })
                        }
                      >
                        {t("alerts.resolve", { ns: "ops" })}
                      </Button>
                    )}
                  </Space>
                ) : null,
            },
          ]}
        />
      </Card>
      {canWrite && (
        <Card
          className="result-panel"
          loading={notifications.isLoading}
          title={t("alerts.notifications", { ns: "ops" })}
        >
          <Form<NotificationSettingsView>
            form={notificationForm}
            layout="inline"
            initialValues={notifications.data}
            onFinish={(values) =>
              updateNotifications.mutate({ ...values, enabled: values.enabled })
            }
          >
            <Form.Item
              name="webhookUrl"
              label={t("alerts.webhookUrl", { ns: "ops" })}
              rules={[{ required: true }]}
            >
              <Input
                placeholder={t("alerts.webhookPlaceholder", { ns: "ops" })}
              />
            </Form.Item>
            <Form.Item
              name="enabled"
              label={t("alerts.enabled", { ns: "ops" })}
              initialValue={true}
            >
              <Select
                options={[
                  { value: true, label: t("alerts.enabled", { ns: "ops" }) },
                  { value: false, label: t("alerts.disabled", { ns: "ops" }) },
                ]}
              />
            </Form.Item>
            <Form.Item>
              <Space>
                <Button
                  type="primary"
                  htmlType="submit"
                  loading={updateNotifications.isPending}
                >
                  {t("common.submit")}
                </Button>
                <Button
                  danger
                  loading={deleteNotifications.isPending}
                  onClick={() => deleteNotifications.mutate()}
                >
                  {t("alerts.deleteNotifications", { ns: "ops" })}
                </Button>
              </Space>
            </Form.Item>
          </Form>
        </Card>
      )}
      {canWrite && (
        <Card
          className="result-panel"
          title={
            <Space>
              {t("alerts.newRule", { ns: "ops" })}
              <Button
                size="small"
                loading={importDefaultRules.isPending}
                onClick={() => importDefaultRules.mutate()}
              >
                {t("alerts.importDefaults", { ns: "ops" })}
              </Button>
            </Space>
          }
        >
          <Form<RuleView>
            form={form}
            layout="inline"
            onFinish={(values) =>
              createRule.mutate({ ...values, enabled: true })
            }
          >
            <Form.Item
              name="name"
              label={t("alerts.name", { ns: "ops" })}
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="ruleType"
              label={t("alerts.ruleType", { ns: "ops" })}
              initialValue="budget"
            >
              <Select
                options={[
                  { value: "budget", label: t("alerts.budget", { ns: "ops" }) },
                  { value: "rate", label: t("alerts.rate", { ns: "ops" }) },
                  {
                    value: "cost_anomaly",
                    label: t("alerts.costAnomaly", { ns: "ops" }),
                  },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="metric"
              label={t("alerts.metric", { ns: "ops" })}
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <Form.Item
              name="operator"
              label={t("alerts.operator", { ns: "ops" })}
              initialValue="gt"
            >
              <Select
                options={["gt", "gte", "lt", "lte"].map((value) => ({
                  value,
                  label: value,
                }))}
              />
            </Form.Item>
            <Form.Item
              name="threshold"
              label={t("alerts.threshold", { ns: "ops" })}
              rules={[{ required: true }]}
            >
              <InputNumber />
            </Form.Item>
            <Form.Item
              name="severity"
              label={t("alerts.severity", { ns: "ops" })}
              initialValue="high"
            >
              <Select
                options={[
                  {
                    value: "low",
                    label: t("alerts.severityLow", { ns: "ops" }),
                  },
                  {
                    value: "medium",
                    label: t("alerts.severityMedium", { ns: "ops" }),
                  },
                  {
                    value: "high",
                    label: t("alerts.severityHigh", { ns: "ops" }),
                  },
                  {
                    value: "critical",
                    label: t("alerts.severityCritical", { ns: "ops" }),
                  },
                ]}
              />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit">
                {t("alerts.create", { ns: "ops" })}
              </Button>
            </Form.Item>
          </Form>
        </Card>
      )}
    </section>
  );
}
