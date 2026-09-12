import { lazy, Suspense, useEffect, useState } from "react";
import {
  Button,
  Dropdown,
  Form,
  Input,
  Layout,
  Menu,
  message,
  Modal,
  Spin,
  Tag,
  Typography,
} from "antd";
import {
  Link,
  Navigate,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  APIError,
  api,
  clearCSRFToken,
  setCSRFToken,
  SetupStatus,
  UNAUTHORIZED_EVENT,
} from "./api/client";
import { isAdminRole, useAuth } from "./state/auth";
const Dashboard = lazy(() => import("./pages/Dashboard"));
const FinOps = lazy(() => import("./pages/FinOps"));
const Setup = lazy(() => import("./pages/Setup"));
const Resources = lazy(() => import("./pages/Resources"));
const Playground = lazy(() => import("./pages/Playground"));
const Requests = lazy(() => import("./pages/Requests"));
const Config = lazy(() => import("./pages/Config"));
const Health = lazy(() => import("./pages/Health"));
const Alerts = lazy(() => import("./pages/Alerts"));
const Governance = lazy(() => import("./pages/Governance"));
const Login = lazy(() => import("./pages/Login"));
const Users = lazy(() => import("./pages/Users"));
const ADMIN_ONLY_ITEMS = ["setup", "config", "users"];

type MeResponse = {
  tenantId?: string;
  username?: string;
  role?: string;
  scopes?: string[];
  authMethod?: string;
  csrfToken?: string;
  email?: string;
};

function ChangePasswordModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const resetAuth = useAuth((state) => state.resetAuth);
  const [form] = Form.useForm<{ oldPassword: string; newPassword: string }>();
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    const values = await form.validateFields();
    setBusy(true);
    try {
      await api("/api/admin/password/change", {
        method: "POST",
        body: JSON.stringify({
          oldPassword: values.oldPassword,
          newPassword: values.newPassword,
        }),
      });
      // The backend voids every session of the account after a password
      // change, including this one; re-authenticate with the new password.
      clearCSRFToken();
      resetAuth();
      void navigate("/login?changed=1", { replace: true });
    } catch (error) {
      const code = error instanceof APIError && error.code ? error.code : "";
      void message.error(
        code
          ? t(`errors.${code}`, {
              ns: "common",
              defaultValue: t("common.error"),
            })
          : t("common.error"),
      );
      form.resetFields();
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      title={t("account.changePassword")}
      open={open}
      onCancel={onClose}
      onOk={() => void submit()}
      confirmLoading={busy}
      okText={t("common.submit")}
      cancelText={t("common.close")}
      destroyOnHidden
    >
      <Form form={form} layout="vertical">
        <Form.Item
          name="oldPassword"
          label={t("account.oldPassword")}
          rules={[{ required: true }]}
        >
          <Input.Password autoComplete="current-password" />
        </Form.Item>
        <Form.Item
          name="newPassword"
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
  );
}

function SetEmailModal({
  open,
  currentEmail,
  onClose,
}: {
  open: boolean;
  currentEmail?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const resetAuth = useAuth((state) => state.resetAuth);
  const [form] = Form.useForm<{ email: string }>();
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    const values = await form.validateFields();
    setBusy(true);
    try {
      await api("/api/admin/users/me/email", {
        method: "POST",
        body: JSON.stringify(values),
      });
      // Changing the address is treated like a credential change: the account
      // must re-authenticate.
      clearCSRFToken();
      resetAuth();
      void navigate("/login?reauth=1", { replace: true });
    } catch (error) {
      const code = error instanceof APIError && error.code ? error.code : "";
      void message.error(
        code
          ? t(`errors.${code}`, {
              ns: "common",
              defaultValue: t("common.error"),
            })
          : t("common.error"),
      );
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      title={t("account.setEmail")}
      open={open}
      onCancel={onClose}
      onOk={() => void submit()}
      confirmLoading={busy}
      okText={t("common.submit")}
      cancelText={t("common.close")}
      destroyOnHidden
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={{ email: currentEmail }}
        preserve={false}
      >
        <Form.Item
          name="email"
          label={t("account.email")}
          rules={[{ type: "email", message: t("account.emailInvalid") }]}
        >
          <Input autoComplete="email" />
        </Form.Item>
        <Typography.Paragraph type="secondary">
          {t("account.emailHelp")}
        </Typography.Paragraph>
      </Form>
    </Modal>
  );
}

export function App() {
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const setAuth = useAuth((state) => state.setAuth);
  const resetAuth = useAuth((state) => state.resetAuth);
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [emailOpen, setEmailOpen] = useState(false);
  const me = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      const data = await api<MeResponse>("/api/admin/me");
      setAuth({
        username: data.username ?? "",
        role: data.role ?? "",
        scopes: data.scopes ?? [],
        authMethod: data.authMethod ?? "local",
      });
      return data;
    },
    enabled: !["/login", "/setup"].includes(location.pathname),
    retry: false,
  });
  const queryClient = useQueryClient();
  const setupStatus = useQuery({
    queryKey: ["setup-status"],
    queryFn: () => api<SetupStatus>("/api/admin/setup/status"),
    enabled: isAdminRole(me.data?.role ?? ""),
    retry: false,
  });
  useEffect(() => {
    if (location.pathname !== "/setup") {
      void queryClient.invalidateQueries({ queryKey: ["setup-status"] });
    }
  }, [location.pathname, queryClient]);
  useEffect(() => {
    if (me.data?.csrfToken) setCSRFToken(me.data.csrfToken);
  }, [me.data]);
  useEffect(() => {
    const onUnauthorized = () => {
      void navigate("/login?expired=1", { replace: true });
      void queryClient.invalidateQueries({ queryKey: ["me"] });
    };
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
  }, [navigate, queryClient]);
  const role = me.data?.role ?? "";
  const isAdmin = isAdminRole(role);
  const isChinese = i18n.language === "zh-CN";
  const toggleLanguage = () =>
    void i18n.changeLanguage(isChinese ? "en-US" : "zh-CN");
  const logout = async () => {
    try {
      await api("/api/admin/session", { method: "DELETE" });
    } finally {
      clearCSRFToken();
      resetAuth();
      void navigate("/login", { replace: true });
    }
  };
  if (location.pathname === "/login") {
    return (
      <Suspense fallback={<Spin />}>
        <Login />
      </Suspense>
    );
  }
  if (location.pathname !== "/setup") {
    if (me.isLoading) {
      return <Spin />;
    }
    if (me.isError) {
      return <Navigate to="/login" replace />;
    }
  }
  const allItems = [
    "dashboard",
    "finops",
    "setup",
    "resources",
    "playground",
    "requests",
    "config",
    "health",
    "alerts",
    "governance",
    "users",
  ];
  const items = allItems
    .filter((key) => !ADMIN_ONLY_ITEMS.includes(key) || isAdmin)
    // Hide the setup entry once an install is usable (a config version has
    // been published). A half-initialized install keeps it visible so the
    // wizard can be retried.
    .filter((key) => key !== "setup" || !setupStatus.data?.ready)
    .map((key) => ({
      key: `/${key}`,
      label: <Link to={`/${key}`}>{t(`nav.${key}`)}</Link>,
    }));
  // Local accounts expose an "email" field on /me; IdP-only sessions omit
  // it, and for them there is no password-reset mailbox to configure.
  const isLocalAccount = me.data?.email !== undefined;
  const userMenu = {
    items: [
      {
        key: "password",
        label: t("account.changePassword"),
        onClick: () => setPasswordOpen(true),
      },
      ...(isLocalAccount
        ? [
            {
              key: "email" as const,
              label: t("account.setEmail"),
              onClick: () => setEmailOpen(true),
            },
          ]
        : []),
      { type: "divider" as const },
      {
        key: "logout",
        label: t("account.logout"),
        onClick: () => void logout(),
      },
    ],
  };
  return (
    <Layout className="app-shell">
      <Layout.Sider breakpoint="lg" collapsedWidth="0" className="side">
        <Typography.Title level={3} className="brand">
          {t("app.name")}
        </Typography.Title>
        <Menu
          aria-label={t("app.navigation")}
          theme="dark"
          selectedKeys={[location.pathname]}
          items={items}
        />
      </Layout.Sider>
      <Layout>
        <Layout.Header className="topbar">
          <Button
            aria-label={t("common.language")}
            aria-pressed={isChinese}
            onClick={toggleLanguage}
          >
            <span style={{ fontWeight: isChinese ? 700 : 400 }}>
              {t("common.chineseShort")}
            </span>
            <span>/</span>
            <span style={{ fontWeight: isChinese ? 400 : 700 }}>
              {t("common.englishShort")}
            </span>
          </Button>
          <Button className="mobile-more">{t("nav.more")}</Button>
          {location.pathname !== "/setup" && (
            <Dropdown menu={userMenu} trigger={["click"]}>
              <Button className="user-menu" aria-label={t("account.menu")}>
                <span>{me.data?.username || t("account.unknownUser")}</span>
                <Tag color={isAdmin ? "gold" : "default"}>
                  {t(`roles.${role || "viewer"}`)}
                </Tag>
              </Button>
            </Dropdown>
          )}
        </Layout.Header>
        <Layout.Content className="content">
          <Suspense
            fallback={
              <div role="status" aria-live="polite">
                <Spin />
              </div>
            }
          >
            <Routes>
              <Route path="/dashboard" element={<Dashboard />} />
              <Route path="/finops" element={<FinOps />} />
              <Route path="/setup" element={<Setup />} />
              <Route path="/resources" element={<Resources />} />
              <Route path="/playground" element={<Playground />} />
              <Route path="/requests/:id?" element={<Requests />} />
              <Route path="/config" element={<Config />} />
              <Route path="/health" element={<Health />} />
              <Route path="/alerts" element={<Alerts />} />
              <Route path="/governance" element={<Governance />} />
              <Route path="/users" element={<Users />} />
              <Route path="*" element={<Navigate to="/dashboard" replace />} />
            </Routes>
          </Suspense>
        </Layout.Content>
        <nav className="mobile-nav" aria-label={t("app.navigation")}>
          {["dashboard", "finops", "requests", "alerts"].map((key) => (
            <Link key={key} to={`/${key}`}>
              {t(`nav.${key}`)}
            </Link>
          ))}
        </nav>
      </Layout>
      <ChangePasswordModal
        open={passwordOpen}
        onClose={() => setPasswordOpen(false)}
      />
      <SetEmailModal
        open={emailOpen}
        currentEmail={me.data?.email}
        onClose={() => setEmailOpen(false)}
      />
    </Layout>
  );
}
