import { useEffect, useState } from "react";
import {
  Alert,
  AutoComplete,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  Spin,
  Space,
  Steps,
  Select,
  Typography,
} from "antd";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Controller, useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { APIError, api, type SetupStatus } from "../api/client";
const schema = z.object({
  username: z.string().min(1, "required"),
  adminPassword: z.string().min(8, "adminPasswordMin"),
  tenantName: z.string().min(1, "required"),
  providerName: z.string().min(1, "required"),
  providerType: z.string().min(1, "required"),
  providerEndpoint: z.string().url("url"),
  providerSecret: z.string().min(1, "required"),
  selectedModel: z.string().min(1, "required"),
});
type Values = z.infer<typeof schema>;
type WizardResult = {
  virtualKey: string;
  sdkExample: string;
  requestId: string;
  firstCallWarning?: string;
};

type ProviderPreset = {
  key: string;
  label: string;
  type: string;
  name: string;
  endpoint: string;
  models: string[];
};

const providerPresets: ProviderPreset[] = [
  {
    key: "openai",
    label: "OpenAI",
    type: "openai",
    name: "OpenAI",
    endpoint: "https://api.openai.com",
    models: ["gpt-4o-mini", "gpt-4o", "gpt-4.1"],
  },
  {
    key: "anthropic",
    label: "Anthropic",
    type: "anthropic",
    name: "Anthropic",
    endpoint: "https://api.anthropic.com",
    models: ["claude-3-5-sonnet", "claude-3-5-haiku"],
  },
  {
    key: "deepseek",
    label: "DeepSeek",
    type: "openai-compatible",
    name: "DeepSeek",
    endpoint: "https://api.deepseek.com",
    models: [
      "deepseek-v4-flash",
      "deepseek-v4-pro",
      "deepseek-v4-flash-vision-exp",
      "deepseek-chat",
      "deepseek-reasoner",
    ],
  },
  {
    key: "moonshot",
    label: "Moonshot",
    type: "openai-compatible",
    name: "Moonshot",
    endpoint: "https://api.moonshot.cn",
    models: ["moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"],
  },
  {
    key: "siliconflow",
    label: "SiliconFlow",
    type: "openai-compatible",
    name: "SiliconFlow",
    endpoint: "https://api.siliconflow.cn",
    models: ["Qwen/Qwen2.5-7B-Instruct", "deepseek-ai/DeepSeek-V3"],
  },
  {
    key: "ollama",
    label: "Ollama",
    type: "openai-compatible",
    name: "Ollama",
    endpoint: "http://127.0.0.1:11434",
    models: ["llama3.1", "qwen2.5", "mistral"],
  },
  {
    key: "custom",
    label: "OpenAI Compatible",
    type: "openai-compatible",
    name: "Custom Provider",
    endpoint: "",
    models: ["gpt-4o-mini", "deepseek-chat", "qwen-plus"],
  },
];

const defaultProvider = providerPresets[0];

function generatePassword() {
  const alphabet =
    "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*";
  const bytes = new Uint8Array(20);
  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i += 1) {
      bytes[i] = Math.floor(Math.random() * 256);
    }
  }
  return Array.from(bytes, (byte) => alphabet[byte % alphabet.length]).join("");
}

export default function Setup() {
  const { t } = useTranslation();
  const [step, setStep] = useState(0);
  const [result, setResult] = useState<WizardResult>();
  const [providerKey, setProviderKey] = useState(defaultProvider.key);
  const [advanced, setAdvanced] = useState(false);
  const [errorCode, setErrorCode] = useState("");
  // null = still probing whether the one-time bootstrap already ran.
  const [status, setStatus] = useState<SetupStatus | null>(null);
  useEffect(() => {
    let cancelled = false;
    api<SetupStatus>("/api/admin/setup/status")
      .then((value) => {
        if (!cancelled) setStatus(value);
      })
      .catch(() => {
        // Probe unavailable (older backend): fall back to the wizard.
        if (!cancelled) setStatus({ initialized: false });
      });
    return () => {
      cancelled = true;
    };
  }, []);
  const {
    control,
    handleSubmit,
    reset,
    setValue,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      username: "admin",
      adminPassword: "",
      tenantName: "Default Workspace",
      providerName: defaultProvider.name,
      providerType: defaultProvider.type,
      providerEndpoint: defaultProvider.endpoint,
      providerSecret: "",
      selectedModel: defaultProvider.models[0],
    },
  });
  useEffect(() => () => setResult(undefined), []);
  const selectedProvider =
    providerPresets.find((preset) => preset.key === providerKey) ??
    defaultProvider;
  const selectProvider = (key: string) => {
    const preset =
      providerPresets.find((candidate) => candidate.key === key) ??
      defaultProvider;
    setProviderKey(preset.key);
    setValue("providerType", preset.type, { shouldValidate: true });
    setValue("providerName", preset.name, { shouldValidate: true });
    setValue("providerEndpoint", preset.endpoint, { shouldValidate: true });
    setValue("selectedModel", preset.models[0] ?? "", {
      shouldValidate: true,
    });
    setAdvanced(preset.key === "custom");
  };
  const submit = handleSubmit(async (values) => {
    setErrorCode("");
    try {
      const response = await api<WizardResult>("/api/admin/setup", {
        method: "POST",
        body: JSON.stringify(values),
      });
      reset();
      setResult(response);
      setStep(4);
      setStatus({
        initialized: true,
        tenantName: values.tenantName,
        adminUsername: values.username,
      });
    } catch (error) {
      setErrorCode(error instanceof APIError ? error.code : "INTERNAL_ERROR");
    }
  });
  const labels = ["provider", "model", "tenant", "key", "call"].map((key) => ({
    title: t(`setup.${key}`),
  }));
  const errorText = (name: keyof Values) => {
    const key = errors[name]?.message;
    return key ? t(`validation.${key}`, { ns: "setup" }) : undefined;
  };
  const field = (name: keyof Values, label: string, secret = false) => (
    <Form.Item
      label={label}
      validateStatus={errors[name] ? "error" : ""}
      help={errorText(name)}
    >
      <Controller
        name={name}
        control={control}
        render={({ field: controlled }) =>
          secret ? (
            <Input.Password
              {...controlled}
              autoComplete="new-password"
              aria-label={label}
              placeholder={label}
            />
          ) : (
            <Input {...controlled} aria-label={label} placeholder={label} />
          )
        }
      />
    </Form.Item>
  );
  if (status === null) {
    return (
      <section>
        <Typography.Title className="page-title">
          {t("setup.title")}
        </Typography.Title>
        <Card>
          <Spin />
        </Card>
      </section>
    );
  }
  // A half-initialized state (admin exists but no config was ever published)
  // is retryable: resubmitting clears the interrupted attempt and restarts.
  const partialSetup = status.initialized && status.ready === false;
  if (status.initialized && status.ready !== false && !result) {
    return (
      <section>
        <Typography.Title className="page-title">
          {t("setup.title")}
        </Typography.Title>
        <Card>
          <Alert
            type="success"
            showIcon
            message={t("setup.initializedTitle")}
            description={t("setup.initializedHelp")}
          />
          <Descriptions
            column={{ xs: 1, sm: 2 }}
            className="setup-summary"
            items={[
              {
                key: "tenant",
                label: t("setup.tenantName"),
                children: status.tenantName ?? "-",
              },
              {
                key: "admin",
                label: t("fields.username", { ns: "setup" }),
                children: status.adminUsername ?? "-",
              },
            ]}
          />
          <Space>
            <Link to="/resources">
              <Button type="primary">{t("setup.goResources")}</Button>
            </Link>
            <Link to="/config">
              <Button>{t("setup.goConfig")}</Button>
            </Link>
          </Space>
        </Card>
      </section>
    );
  }
  return (
    <section>
      <Typography.Title className="page-title">
        {t("setup.title")}
      </Typography.Title>
      <Card>
        {errorCode ? (
          <Alert type="error" showIcon message={t(`errors.${errorCode}`)} />
        ) : null}
        {partialSetup && !result ? (
          <Alert
            type="warning"
            showIcon
            message={t("setup.partialTitle")}
            description={t("setup.partialHelp")}
          />
        ) : null}
        <Steps current={step} items={labels} />
        <form onSubmit={submit} className="form-grid">
          {field("username", t("fields.username", { ns: "setup" }))}
          <Form.Item
            label={t("fields.adminPassword", { ns: "setup" })}
            validateStatus={errors.adminPassword ? "error" : ""}
            help={errorText("adminPassword")}
          >
            <Space.Compact block>
              <Controller
                name="adminPassword"
                control={control}
                render={({ field: controlled }) => (
                  <Input.Password
                    {...controlled}
                    autoComplete="new-password"
                    aria-label={t("fields.adminPassword", { ns: "setup" })}
                    placeholder={t("fields.adminPassword", { ns: "setup" })}
                  />
                )}
              />
              <Button
                type="default"
                onClick={() =>
                  setValue("adminPassword", generatePassword(), {
                    shouldValidate: true,
                  })
                }
              >
                {t("setup.generatePassword")}
              </Button>
            </Space.Compact>
          </Form.Item>
          {field("tenantName", t("setup.tenantName"))}
          <Form.Item label={t("setup.providerPreset")}>
            <Select
              value={providerKey}
              options={providerPresets.map((preset) => ({
                value: preset.key,
                label: preset.label,
              }))}
              onChange={selectProvider}
            />
          </Form.Item>
          <div className="wide">
            {field("providerSecret", t("setup.providerSecret"), true)}
          </div>
          <Form.Item
            label={t("fields.selectedModel", { ns: "setup" })}
            validateStatus={errors.selectedModel ? "error" : ""}
            help={errorText("selectedModel")}
          >
            <Controller
              name="selectedModel"
              control={control}
              render={({ field: controlled }) => (
                <AutoComplete
                  {...controlled}
                  options={selectedProvider.models.map((model) => ({
                    value: model,
                  }))}
                  filterOption={(input, option) =>
                    String(option?.value ?? "")
                      .toLowerCase()
                      .includes(input.toLowerCase())
                  }
                  placeholder={t("fields.selectedModel", { ns: "setup" })}
                />
              )}
            />
          </Form.Item>
          <div className="wide">
            <Button type="link" onClick={() => setAdvanced((value) => !value)}>
              {advanced ? t("setup.hideAdvanced") : t("setup.showAdvanced")}
            </Button>
          </div>
          {advanced && (
            <>
              {field("providerName", t("setup.providerName"))}
              {field(
                "providerEndpoint",
                t("fields.providerEndpoint", { ns: "setup" }),
              )}
            </>
          )}
          <Space className="wide">
            <Button onClick={() => setStep((value) => Math.max(0, value - 1))}>
              {t("common.previous")}
            </Button>
            <Button onClick={() => setStep((value) => Math.min(4, value + 1))}>
              {t("common.next")}
            </Button>
            <Button htmlType="submit" type="primary">
              {t("setup.create")}
            </Button>
          </Space>
        </form>
        {result && (
          <Alert
            className="result-panel secret-panel"
            type="warning"
            showIcon
            message={t("setup.secretTitle")}
            description={
              <>
                {result.firstCallWarning ? (
                  <Alert
                    type="error"
                    showIcon
                    message={t("setup.firstCallWarning")}
                    description={result.firstCallWarning}
                  />
                ) : null}
                <Typography.Paragraph
                  copyable={{ text: result.virtualKey }}
                  code
                >
                  {result.virtualKey}
                </Typography.Paragraph>
                <Typography.Text>{t("setup.secretHelp")}</Typography.Text>
                <Typography.Title level={5}>
                  {t("result.sdk", { ns: "setup" })}
                </Typography.Title>
                <Typography.Paragraph
                  copyable={{ text: result.sdkExample }}
                  code
                >
                  {result.sdkExample}
                </Typography.Paragraph>
                <Link to={`/requests/${result.requestId}`}>
                  {t("result.request", { ns: "setup" })}
                </Link>
                <Button onClick={() => setResult(undefined)}>
                  {t("common.close")}
                </Button>
              </>
            }
          />
        )}
      </Card>
    </section>
  );
}
