import { useEffect, useRef, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  message,
  Modal,
  Space,
  Steps,
  Typography,
} from "antd";
import { useQuery } from "@tanstack/react-query";
import { useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { APIError, api, setCSRFToken } from "../api/client";

type OIDCConfig = { enabled: boolean };
type SessionResult = { csrfToken: string };
type ResetPasswordValues = {
  username: string;
  newPassword: string;
  bootstrapToken?: string;
};
type ForgotRequestValues = { username: string };
type ConfirmResetValues = { code: string; newPassword: string };
type PasswordResetConfig = { enabled: boolean };

const CODE_RESEND_COOLDOWN_S = 60;

export default function Login() {
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const handled = useRef(false);
  const [resetForm] = Form.useForm<ResetPasswordValues>();
  const [busy, setBusy] = useState(false);
  const [errorCode, setErrorCode] = useState("");
  const [resetOpen, setResetOpen] = useState(false);
  const [resetStep, setResetStep] = useState<1 | 2>(1);
  const [sendCooldown, setSendCooldown] = useState(0);
  const [resetUsername, setResetUsername] = useState("");
  const oidc = useQuery({
    queryKey: ["oidc-config"],
    queryFn: () => api<OIDCConfig>("/api/admin/oidc/config"),
    retry: false,
  });
  const resetConfig = useQuery({
    queryKey: ["password-reset-config"],
    queryFn: () =>
      api<PasswordResetConfig>("/api/admin/password/forgot/config"),
    retry: false,
  });
  const emailResetEnabled = resetConfig.data?.enabled === true;

  useEffect(() => {
    if (sendCooldown <= 0) return;
    const timer = window.setTimeout(
      () => setSendCooldown((seconds) => seconds - 1),
      1000,
    );
    return () => window.clearTimeout(timer);
  }, [sendCooldown]);

  const openResetModal = () => {
    setResetStep(1);
    setResetOpen(true);
  };

  const closeResetModal = () => {
    setResetOpen(false);
    setResetStep(1);
  };

  const establishSession = (result: SessionResult) => {
    setCSRFToken(result.csrfToken);
    void navigate("/dashboard", { replace: true });
  };

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const code = params.get("code");
    const state = params.get("state");
    if (!code || !state || handled.current) return;
    handled.current = true;
    setBusy(true);
    void api<SessionResult>("/api/admin/oidc/session", {
      method: "POST",
      body: JSON.stringify({ code, state }),
    })
      .then(establishSession)
      .catch((error: unknown) =>
        setErrorCode(error instanceof APIError ? error.code : "INTERNAL_ERROR"),
      )
      .finally(() => setBusy(false));
  }, [location.search]);

  const localLogin = async (values: { username: string; password: string }) => {
    setBusy(true);
    setErrorCode("");
    try {
      establishSession(
        await api<SessionResult>("/api/admin/session", {
          method: "POST",
          body: JSON.stringify(values),
        }),
      );
    } catch (error) {
      setErrorCode(error instanceof APIError ? error.code : "INTERNAL_ERROR");
    } finally {
      setBusy(false);
    }
  };

  const resetPassword = async (values: ResetPasswordValues) => {
    setBusy(true);
    try {
      const headers: Record<string, string> = {};
      if (values.bootstrapToken) {
        headers["X-Bootstrap-Token"] = values.bootstrapToken;
      }
      await api("/api/admin/password/reset", {
        method: "POST",
        headers,
        body: JSON.stringify({
          username: values.username,
          newPassword: values.newPassword,
        }),
      });
      resetForm.resetFields();
      setResetOpen(false);
      void message.success(t("auth.passwordResetDone"));
    } catch (error) {
      void message.error(
        error instanceof APIError
          ? t(`errors.${error.code}`)
          : t("errors.INTERNAL_ERROR"),
      );
    } finally {
      setBusy(false);
    }
  };

  const sendResetCode = async (values: ForgotRequestValues) => {
    setBusy(true);
    try {
      await api("/api/admin/password/forgot", {
        method: "POST",
        body: JSON.stringify(values),
      });
      setResetUsername(values.username);
      setResetStep(2);
      setSendCooldown(CODE_RESEND_COOLDOWN_S);
      void message.success(t("auth.resetCodeSent"));
    } catch (error) {
      void message.error(
        error instanceof APIError
          ? t(`errors.${error.code}`)
          : t("errors.INTERNAL_ERROR"),
      );
    } finally {
      setBusy(false);
    }
  };

  const confirmReset = async (values: ConfirmResetValues) => {
    setBusy(true);
    try {
      await api("/api/admin/password/reset/confirm", {
        method: "POST",
        body: JSON.stringify({ ...values, username: resetUsername }),
      });
      closeResetModal();
      resetForm.resetFields();
      void message.success(t("auth.passwordResetDone"));
    } catch (error) {
      void message.error(
        error instanceof APIError
          ? t(`errors.${error.code}`)
          : t("errors.INTERNAL_ERROR"),
      );
    } finally {
      setBusy(false);
    }
  };

  const startOIDC = async () => {
    setBusy(true);
    setErrorCode("");
    try {
      const result = await api<{ authorizationUrl: string }>(
        "/api/admin/oidc/start",
        { method: "POST" },
      );
      window.location.assign(result.authorizationUrl);
    } catch (error) {
      setErrorCode(error instanceof APIError ? error.code : "INTERNAL_ERROR");
      setBusy(false);
    }
  };
  const isChinese = i18n.language === "zh-CN";
  const toggleLanguage = () =>
    void i18n.changeLanguage(isChinese ? "en-US" : "zh-CN");

  return (
    <main className="login-page">
      <Card className="login-card">
        <Space direction="vertical" size="large" className="wide">
          <div className="login-heading">
            <Typography.Title level={2}>{t("auth.title")}</Typography.Title>
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
          </div>
          <Typography.Text type="secondary">
            {t("auth.subtitle")}
          </Typography.Text>
          {new URLSearchParams(location.search).get("expired") === "1" && (
            <Alert
              type="warning"
              showIcon
              message={t("account.sessionExpired")}
            />
          )}
          {new URLSearchParams(location.search).get("changed") === "1" && (
            <Alert
              type="success"
              showIcon
              message={t("account.passwordChangedReauth")}
            />
          )}
          {new URLSearchParams(location.search).get("reauth") === "1" && (
            <Alert type="info" showIcon message={t("account.reauthRequired")} />
          )}
          {errorCode && (
            <Alert type="error" showIcon message={t(`errors.${errorCode}`)} />
          )}
          <Form
            name="login"
            layout="vertical"
            onFinish={(values) => void localLogin(values)}
          >
            <Form.Item
              label={t("auth.username")}
              name="username"
              rules={[{ required: true }]}
            >
              <Input autoComplete="username" />
            </Form.Item>
            <Form.Item
              label={t("auth.password")}
              name="password"
              rules={[{ required: true }]}
            >
              <Input.Password autoComplete="current-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={busy} block>
              {t("auth.signIn")}
            </Button>
            <Button type="link" block onClick={openResetModal}>
              {t("auth.forgotPassword")}
            </Button>
          </Form>
          {oidc.data?.enabled && (
            <Button onClick={() => void startOIDC()} loading={busy} block>
              {t("auth.oidcSignIn")}
            </Button>
          )}
        </Space>
      </Card>
      {emailResetEnabled ? (
        <Modal
          title={t("auth.resetPassword")}
          open={resetOpen}
          onCancel={closeResetModal}
          footer={null}
          maskClosable={false}
          destroyOnHidden
        >
          <Steps
            size="small"
            current={resetStep - 1}
            items={[
              { title: t("auth.resetStepAccount") },
              { title: t("auth.resetStepCode") },
            ]}
            style={{ marginBottom: 24 }}
          />
          {resetStep === 1 ? (
            <>
              <Typography.Paragraph type="secondary">
                {t("auth.emailResetHelp")}
              </Typography.Paragraph>
              <Form<ForgotRequestValues>
                name="reset-request"
                layout="vertical"
                onFinish={(values) => void sendResetCode(values)}
              >
                <Form.Item
                  name="username"
                  label={t("auth.username")}
                  rules={[{ required: true }]}
                >
                  <Input autoComplete="username" />
                </Form.Item>
                <Button
                  type="primary"
                  htmlType="submit"
                  block
                  loading={busy}
                  disabled={sendCooldown > 0}
                >
                  {sendCooldown > 0
                    ? t("auth.resendIn", { seconds: sendCooldown })
                    : t("auth.sendResetCode")}
                </Button>
              </Form>
            </>
          ) : (
            <>
              <Typography.Paragraph type="secondary">
                {t("auth.resetCodeSent")}
              </Typography.Paragraph>
              <Form<ConfirmResetValues>
                name="reset-confirm"
                layout="vertical"
                onFinish={(values) => void confirmReset(values)}
              >
                <Form.Item
                  name="code"
                  label={t("auth.resetCode")}
                  rules={[{ required: true, len: 6 }]}
                >
                  <Input
                    autoComplete="one-time-code"
                    maxLength={6}
                    inputMode="numeric"
                  />
                </Form.Item>
                <Form.Item
                  name="newPassword"
                  label={t("auth.newPassword")}
                  rules={[{ required: true, min: 8 }]}
                >
                  <Input.Password autoComplete="new-password" />
                </Form.Item>
                <Button type="primary" htmlType="submit" block loading={busy}>
                  {t("auth.confirmReset")}
                </Button>
              </Form>
            </>
          )}
        </Modal>
      ) : (
        <Modal
          title={t("auth.resetPassword")}
          open={resetOpen}
          onCancel={closeResetModal}
          onOk={() => resetForm.submit()}
          confirmLoading={busy}
          okText={t("auth.resetPassword")}
          cancelText={t("common.close")}
        >
          <Alert
            type="info"
            showIcon
            message={t("auth.forgotPasswordHelp")}
            description={t("auth.forgotPasswordHelpDetail")}
          />
          <Form<ResetPasswordValues>
            name="emergency-reset"
            form={resetForm}
            layout="vertical"
            onFinish={(values) => void resetPassword(values)}
          >
            <Form.Item
              name="username"
              label={t("auth.username")}
              rules={[{ required: true }]}
            >
              <Input autoComplete="username" />
            </Form.Item>
            <Form.Item
              name="newPassword"
              label={t("auth.newPassword")}
              rules={[{ required: true, min: 8 }]}
            >
              <Input.Password autoComplete="new-password" />
            </Form.Item>
            <Form.Item
              name="bootstrapToken"
              label={t("auth.bootstrapToken")}
              help={t("auth.bootstrapTokenHelp")}
            >
              <Input.Password
                autoComplete="off"
                placeholder={t("auth.bootstrapTokenPlaceholder")}
              />
            </Form.Item>
          </Form>
        </Modal>
      )}
    </main>
  );
}
