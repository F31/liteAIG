import { useState } from "react";
import {
  Button,
  message,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { Navigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, errorMessageKey } from "../api/client";
import { isAdminRole, useAuth } from "../state/auth";

type LocalUser = {
  id: string;
  username: string;
  role: string;
  status: string;
  email?: string;
  createdAt: string;
  isSelf: boolean;
};

type CreateValues = {
  username: string;
  password: string;
  role: string;
  email?: string;
};
type ResetValues = { password: string };
type EmailValues = { email: string };

const ROLE_OPTIONS = ["tenant_admin", "tenant_operator", "viewer"];

export default function Users() {
  const { t } = useTranslation();
  const role = useAuth((state) => state.role);
  const [createOpen, setCreateOpen] = useState(false);
  const [resetTarget, setResetTarget] = useState<LocalUser>();
  const [emailTarget, setEmailTarget] = useState<LocalUser>();
  const [createForm] = Form.useForm<CreateValues>();
  const [resetForm] = Form.useForm<ResetValues>();
  const [emailForm] = Form.useForm<EmailValues>();
  const users = useQuery({
    queryKey: ["users"],
    queryFn: () => api<LocalUser[]>("/api/admin/users"),
  });
  const invalidate = () => void users.refetch();
  const mutationError = () => (error: unknown) => {
    const code = errorMessageKey(error);
    void message.error(
      code
        ? t(`errors.${code}`, { defaultValue: t("common.error") })
        : t("common.error"),
    );
  };
  const create = useMutation({
    mutationFn: (values: CreateValues) =>
      api<LocalUser>("/api/admin/users", {
        method: "POST",
        body: JSON.stringify(values),
      }),
    onSuccess: () => {
      void message.success(t("users.userCreated"));
      createForm.resetFields();
      setCreateOpen(false);
      invalidate();
    },
    onError: mutationError(),
  });
  const changeRole = useMutation({
    mutationFn: (input: { id: string; role: string }) =>
      api<LocalUser>(`/api/admin/users/${encodeURIComponent(input.id)}/role`, {
        method: "PATCH",
        body: JSON.stringify({ role: input.role }),
      }),
    onSuccess: () => {
      void message.success(t("users.roleChanged"));
      invalidate();
    },
    onError: mutationError(),
  });
  const changeStatus = useMutation({
    mutationFn: (input: { id: string; status: string }) =>
      api<LocalUser>(
        `/api/admin/users/${encodeURIComponent(input.id)}/status`,
        { method: "POST", body: JSON.stringify({ status: input.status }) },
      ),
    onSuccess: () => {
      void message.success(t("users.statusChanged"));
      invalidate();
    },
    onError: mutationError(),
  });
  const setEmail = useMutation({
    mutationFn: (input: { id: string; email: string }) =>
      api<LocalUser>(`/api/admin/users/${encodeURIComponent(input.id)}/email`, {
        method: "PATCH",
        body: JSON.stringify({ email: input.email }),
      }),
    onSuccess: () => {
      void message.success(t("users.emailUpdated"));
      emailForm.resetFields();
      setEmailTarget(undefined);
      invalidate();
    },
    onError: mutationError(),
  });
  const resetPassword = useMutation({
    mutationFn: (input: { id: string; password: string }) =>
      api(`/api/admin/users/${encodeURIComponent(input.id)}/reset-password`, {
        method: "POST",
        body: JSON.stringify({ password: input.password }),
      }),
    onSuccess: () => {
      void message.success(t("users.passwordReset"));
      resetForm.resetFields();
      setResetTarget(undefined);
      invalidate();
    },
    onError: mutationError(),
  });
  const remove = useMutation({
    mutationFn: (id: string) =>
      api(`/api/admin/users/${encodeURIComponent(id)}`, { method: "DELETE" }),
    onSuccess: () => {
      void message.success(t("users.userDeleted"));
      invalidate();
    },
    onError: mutationError(),
  });
  if (!isAdminRole(role)) {
    return <Navigate to="/dashboard" replace />;
  }
  const roleTag = (value: string) => (
    <Tag
      color={
        value === "tenant_admin"
          ? "gold"
          : value === "tenant_operator"
            ? "blue"
            : "default"
      }
    >
      {t(`roles.${value}`)}
    </Tag>
  );
  const columns = [
    { title: t("users.username"), dataIndex: "username", key: "username" },
    {
      title: t("users.email"),
      dataIndex: "email",
      key: "email",
      render: (value: string) => value || "-",
    },
    {
      title: t("users.role"),
      dataIndex: "role",
      key: "role",
      render: (value: string, row: LocalUser) =>
        row.isSelf ? (
          roleTag(value)
        ) : (
          <Select
            size="small"
            value={value}
            style={{ width: 150 }}
            options={ROLE_OPTIONS.map((item) => ({
              value: item,
              label: t(`roles.${item}`),
            }))}
            onChange={(next: string) =>
              changeRole.mutate({ id: row.id, role: next })
            }
          />
        ),
    },
    {
      title: t("common.status"),
      dataIndex: "status",
      key: "status",
      render: (value: string, row: LocalUser) => (
        <Tag color={value === "active" ? "green" : "default"}>
          {t(value === "active" ? "common.active" : "resources.disabled")}
        </Tag>
      ),
    },
    {
      title: t("users.createdAt"),
      dataIndex: "createdAt",
      key: "createdAt",
      render: (value: string) =>
        value ? new Date(value).toLocaleString() : "-",
    },
    {
      title: t("resources.actions"),
      key: "actions",
      render: (_: unknown, row: LocalUser) =>
        row.isSelf ? (
          <Space>
            <Button size="small" onClick={() => setEmailTarget(row)}>
              {t("users.setEmail")}
            </Button>
            <Tag>{t("users.self")}</Tag>
          </Space>
        ) : (
          <Space>
            <Button size="small" onClick={() => setEmailTarget(row)}>
              {t("users.setEmail")}
            </Button>
            <Button
              size="small"
              onClick={() =>
                changeStatus.mutate({
                  id: row.id,
                  status: row.status === "active" ? "disabled" : "active",
                })
              }
            >
              {t(
                row.status === "active" ? "resources.disable" : "users.enable",
              )}
            </Button>
            <Button size="small" onClick={() => setResetTarget(row)}>
              {t("users.resetPassword")}
            </Button>
            <Popconfirm
              title={t("users.deleteConfirm")}
              onConfirm={() => remove.mutate(row.id)}
              okText={t("resources.delete")}
              cancelText={t("common.close")}
            >
              <Button size="small" danger>
                {t("resources.delete")}
              </Button>
            </Popconfirm>
          </Space>
        ),
    },
  ];
  return (
    <section>
      <Typography.Title className="page-title">
        {t("users.title")}
      </Typography.Title>
      <Card
        title={t("users.title")}
        extra={
          <Button type="primary" onClick={() => setCreateOpen(true)}>
            {t("users.create")}
          </Button>
        }
      >
        <Table
          rowKey="id"
          columns={columns}
          dataSource={users.data ?? []}
          loading={users.isLoading}
          pagination={false}
          locale={{ emptyText: t("common.empty") }}
        />
      </Card>
      <Modal
        title={t("users.create")}
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => void createForm.submit()}
        confirmLoading={create.isPending}
        okText={t("common.submit")}
        cancelText={t("common.close")}
        destroyOnHidden
      >
        <Form
          form={createForm}
          layout="vertical"
          onFinish={(values) => create.mutate(values)}
          initialValues={{ role: "viewer" }}
        >
          <Form.Item
            name="username"
            label={t("users.username")}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="email"
            label={t("users.email")}
            rules={[{ type: "email", message: t("users.emailInvalid") }]}
          >
            <Input autoComplete="email" />
          </Form.Item>
          <Form.Item
            name="password"
            label={t("auth.password")}
            rules={[
              { required: true },
              { min: 8, message: t("account.passwordMin") },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="role"
            label={t("users.role")}
            rules={[{ required: true }]}
          >
            <Select
              options={ROLE_OPTIONS.map((item) => ({
                value: item,
                label: t(`roles.${item}`),
              }))}
            />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={t("users.resetPassword")}
        open={Boolean(resetTarget)}
        onCancel={() => setResetTarget(undefined)}
        onOk={() => void resetForm.submit()}
        confirmLoading={resetPassword.isPending}
        okText={t("common.submit")}
        cancelText={t("common.close")}
        destroyOnHidden
      >
        <Typography.Paragraph>
          {t("users.resetPasswordHelp", { user: resetTarget?.username ?? "" })}
        </Typography.Paragraph>
        <Form
          form={resetForm}
          layout="vertical"
          onFinish={(values) =>
            resetPassword.mutate({ id: resetTarget!.id, ...values })
          }
        >
          <Form.Item
            name="password"
            label={t("account.newPassword")}
            rules={[
              { required: true },
              { min: 8, message: t("account.passwordMin") },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={t("users.setEmail")}
        open={Boolean(emailTarget)}
        onCancel={() => setEmailTarget(undefined)}
        onOk={() => void emailForm.submit()}
        confirmLoading={setEmail.isPending}
        okText={t("common.submit")}
        cancelText={t("common.close")}
        destroyOnHidden
      >
        <Typography.Paragraph>
          {t("users.emailHelp", { user: emailTarget?.username ?? "" })}
        </Typography.Paragraph>
        <Form
          form={emailForm}
          layout="vertical"
          onFinish={(values) =>
            setEmail.mutate({ id: emailTarget!.id, ...values })
          }
          initialValues={{ email: emailTarget?.email }}
          preserve={false}
        >
          <Form.Item
            name="email"
            label={t("users.email")}
            rules={[{ type: "email", message: t("users.emailInvalid") }]}
          >
            <Input autoComplete="email" />
          </Form.Item>
        </Form>
      </Modal>
    </section>
  );
}
