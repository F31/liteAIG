import { useState } from "react";
import { Button, Card, Form, Input, message, Select, Table } from "antd";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { ProjectsQuery } from "./sections";
import type { ResourceRows } from "./useResourceRows";

// ProjectSection renders the projects creation form and the projects table.
export function ProjectSection({
  canWrite,
  projects,
  rows,
}: {
  canWrite: boolean;
  projects: ProjectsQuery;
  rows: ResourceRows;
}) {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [regions, setRegions] = useState<string[]>([]);

  const createProject = useMutation({
    mutationFn: (values: { name: string; residencyEnforcement: string }) =>
      api("/api/admin/projects", {
        method: "POST",
        body: JSON.stringify({ ...values, allowedDataRegions: regions }),
      }),
    onSuccess: () => {
      void message.success(t("resources.projectCreated"));
      form.resetFields();
      setRegions([]);
      void projects.refetch();
    },
  });

  const projectColumns = [
    { title: t("resources.name"), dataIndex: "name", key: "name" },
    { title: t("common.status"), dataIndex: "status", key: "status" },
    {
      title: t("resources.residency"),
      dataIndex: "residencyEnforcement",
      key: "residencyEnforcement",
    },
    {
      title: t("resources.regions"),
      dataIndex: "allowedDataRegions",
      key: "allowedDataRegions",
      render: (value: string[] = []) => value.join(", ") || "-",
    },
  ];

  return (
    <Card>
      {canWrite && (
        <Form
          form={form}
          layout="inline"
          onFinish={(values) => createProject.mutate(values)}
        >
          <Form.Item
            name="name"
            label={t("resources.name")}
            rules={[{ required: true }]}
          >
            <Input aria-label={t("resources.name")} />
          </Form.Item>
          <Form.Item
            name="residencyEnforcement"
            label={t("resources.residency")}
            initialValue="advisory"
          >
            <Select
              aria-label={t("resources.residency")}
              style={{ width: 160 }}
              options={[
                { value: "advisory", label: "advisory" },
                { value: "strict", label: "strict" },
              ]}
            />
          </Form.Item>
          <Form.Item label={t("resources.regions")}>
            <Select
              mode="multiple"
              options={rows.regionOptions}
              aria-label={t("resources.regions")}
              style={{ minWidth: 220 }}
              value={regions}
              onChange={setRegions}
            />
          </Form.Item>
          <Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              loading={createProject.isPending}
            >
              {t("resources.createProject")}
            </Button>
          </Form.Item>
        </Form>
      )}
      <Table
        className="result-panel"
        rowKey="id"
        columns={projectColumns}
        dataSource={projects.data ?? []}
        pagination={false}
        loading={projects.isLoading}
        locale={{ emptyText: t("common.empty") }}
      />
    </Card>
  );
}
