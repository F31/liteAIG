import { Table, Typography } from "antd";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, AuditRecord } from "../../api/client";

// AuditSection renders the audit log table.
export function AuditSection() {
  const { t } = useTranslation();
  const audit = useQuery({
    queryKey: ["audit"],
    queryFn: () => api<AuditRecord[]>("/api/admin/audit"),
  });

  const auditColumns = [
    { title: t("resources.auditAction"), dataIndex: "action", key: "action" },
    {
      title: t("resources.auditResource"),
      key: "resource",
      render: (_: unknown, row: AuditRecord) => {
        const label = row.resourceId
          ? `${row.resourceType} · ${row.resourceId}`
          : row.resourceType;
        return <Typography.Text code>{label}</Typography.Text>;
      },
    },
    { title: t("resources.auditActor"), dataIndex: "actor", key: "actor" },
    { title: t("resources.auditResult"), dataIndex: "result", key: "result" },
    {
      title: t("resources.auditTime"),
      dataIndex: "occurredAt",
      key: "occurredAt",
      render: (value: string) =>
        value ? new Date(value).toLocaleString() : "-",
    },
  ];

  return (
    <Table
      rowKey="id"
      columns={auditColumns}
      dataSource={audit.data ?? []}
      pagination={{ pageSize: 20 }}
      loading={audit.isLoading}
      locale={{ emptyText: t("common.empty") }}
      scroll={{ x: true }}
    />
  );
}
