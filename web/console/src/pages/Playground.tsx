import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  List,
  Select,
  Space,
  Switch,
  Typography,
} from "antd";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  APIError,
  api,
  PlaygroundResult,
  RuntimeResources,
} from "../api/client";
import { useSSEStream } from "../hooks/useSSEStream";

type RuntimeLogicalModel = {
  id: string;
  alias: string;
  routePolicyId?: string;
};

type RuntimeRoute = {
  id: string;
  strategy?: string;
};

type RuntimeDeployment = {
  id: string;
  providerId?: string;
  model?: string;
  status?: string;
};

export default function Playground() {
  const { t } = useTranslation();
  const [model, setModel] = useState("default-chat");
  const [input, setInput] = useState("");
  const [stream, setStream] = useState(false);
  const [runId, setRunId] = useState(0);
  const [streamText, setStreamText] = useState("");
  const [streamMeta, setStreamMeta] = useState<PlaygroundResult | null>(null);
  const [streaming, setStreaming] = useState(false);
  const [streamError, setStreamError] = useState("");
  const runtime = useQuery({
    queryKey: ["runtime-resources"],
    queryFn: () => api<RuntimeResources>("/api/admin/runtime"),
  });
  const modelOptions = useMemo(
    () =>
      (runtime.data?.logicalModels ?? []).map((entry) => {
        const item = entry as RuntimeLogicalModel;
        return { value: item.alias, label: item.alias };
      }),
    [runtime.data],
  );
  const selectedLogicalModel = useMemo(
    () =>
      (runtime.data?.logicalModels ?? []).find(
        (entry) => (entry as RuntimeLogicalModel).alias === model,
      ) as RuntimeLogicalModel | undefined,
    [model, runtime.data],
  );
  const selectedRoute = useMemo(
    () =>
      (runtime.data?.routes ?? []).find(
        (entry) =>
          (entry as RuntimeRoute).id === selectedLogicalModel?.routePolicyId,
      ) as RuntimeRoute | undefined,
    [runtime.data, selectedLogicalModel],
  );
  const enabledDeployments = useMemo(
    () =>
      ((runtime.data?.deployments ?? []) as RuntimeDeployment[]).filter(
        (deployment) => deployment.status === "enabled",
      ),
    [runtime.data],
  );
  const estimatedTokens = useMemo(() => Math.ceil(input.length / 4), [input]);
  const preflightIssues = useMemo(() => {
    const issues: string[] = [];
    if (!modelOptions.length) issues.push(t("playground.noModel"));
    if (modelOptions.length && !selectedLogicalModel)
      issues.push(t("playground.modelMissing"));
    if (selectedLogicalModel && !selectedRoute)
      issues.push(t("playground.routeMissing"));
    if (!enabledDeployments.length) issues.push(t("playground.noDeployment"));
    if (!input.trim()) issues.push(t("playground.inputRequired"));
    return issues;
  }, [
    enabledDeployments.length,
    input,
    modelOptions.length,
    selectedLogicalModel,
    selectedRoute,
    t,
  ]);
  useEffect(() => {
    if (
      modelOptions.length > 0 &&
      !modelOptions.some((option) => option.value === model)
    ) {
      setModel(modelOptions[0].value);
    }
  }, [modelOptions, model]);
  const mutation = useMutation({
    mutationFn: () =>
      api<PlaygroundResult>("/api/admin/playground", {
        method: "POST",
        body: JSON.stringify({ model, input, stream: false }),
      }),
  });
  const mutationError = mutation.error;
  const errorRequestId =
    mutationError instanceof APIError &&
    typeof mutationError.params?.requestId === "string"
      ? mutationError.params.requestId
      : "";
  const errorMessage = useMemo(() => {
    if (!mutationError) return "";
    if (mutationError instanceof APIError) {
      const message = mutationError.params?.message;
      const translated = t(`errors.${mutationError.code}`, {
        defaultValue: t("errors.INTERNAL_ERROR"),
      });
      const upstreamStatus = mutationError.params?.upstreamStatus;
      return [
        translated,
        typeof upstreamStatus === "number"
          ? `upstream status ${upstreamStatus}`
          : undefined,
        typeof message === "string" ? message : undefined,
      ]
        .filter(Boolean)
        .join(" · ");
    }
    return t("errors.INTERNAL_ERROR");
  }, [mutationError, t]);
  const onStreamEvent = useCallback(
    (event: { id: string; event: string; data: string }) => {
      if (event.event === "token") {
        try {
          const token = JSON.parse(event.data) as { delta?: string };
          if (token.delta) setStreamText((prev) => prev + token.delta);
        } catch {
          // ignore malformed token
        }
      } else if (event.event === "done") {
        try {
          setStreamMeta(JSON.parse(event.data) as PlaygroundResult);
        } catch {
          // ignore malformed done payload
        }
      } else if (event.event === "error") {
        try {
          const payload = JSON.parse(event.data) as { error?: string };
          setStreamError(payload.error ?? t("playground.streaming"));
        } catch {
          setStreamError(event.data);
        }
      }
    },
    [t],
  );
  useSSEStream(
    stream && runId > 0 ? "/api/admin/playground" : undefined,
    `playground-${runId}`,
    onStreamEvent,
    {
      method: "POST",
      body: JSON.stringify({ model, input, stream: true }),
      finite: true,
      onDone: () => setStreaming(false),
      onError: (message) => {
        setStreaming(false);
        setStreamError(message);
      },
    },
  );
  const run = () => {
    if (stream) {
      setStreamText("");
      setStreamMeta(null);
      setStreamError("");
      setStreaming(true);
      setRunId((n) => n + 1);
    } else {
      mutation.mutate();
    }
  };
  const result = stream ? streamMeta : (mutation.data ?? null);
  const output = stream ? streamText : (mutation.data?.output ?? "");
  return (
    <section>
      <Typography.Title className="page-title">
        {t("playground.title")}
      </Typography.Title>
      <Card>
        <div className="form-grid">
          <Form.Item label={t("playground.model")}>
            <Select
              value={model}
              aria-label={t("playground.model")}
              style={{ width: "100%" }}
              loading={runtime.isLoading}
              options={modelOptions}
              placeholder={
                modelOptions.length === 0 ? t("playground.noModel") : undefined
              }
              onChange={(value) => setModel(value)}
            />
          </Form.Item>
          <Form.Item className="wide" label={t("playground.input")}>
            <Input.TextArea
              rows={8}
              value={input}
              aria-label={t("playground.input")}
              onChange={(event) => setInput(event.target.value)}
            />
          </Form.Item>
          <Form.Item label={t("playground.stream")}>
            <Switch
              checked={stream}
              aria-label={t("playground.stream")}
              onChange={setStream}
            />
          </Form.Item>
          <Button
            type="primary"
            loading={stream ? streaming : mutation.isPending}
            disabled={preflightIssues.length > 0}
            onClick={run}
          >
            {stream && streaming
              ? t("playground.streaming")
              : t("playground.run")}
          </Button>
        </div>
        <Card size="small" style={{ marginBottom: 16 }}>
          <List
            size="small"
            dataSource={[
              t("playground.preflightModel", { model: model || "-" }),
              t("playground.preflightRoute", {
                route: selectedRoute?.id ?? "-",
                strategy: selectedRoute?.strategy ?? "-",
              }),
              t("playground.preflightDeployments", {
                count: enabledDeployments.length,
              }),
              t("playground.preflightTokens", { count: estimatedTokens }),
              t("playground.preflightGuardrail", {
                mode: runtime.data?.guardrail?.mode || "off",
              }),
              t("playground.preflightCache", {
                status: runtime.data?.cache?.enabled ? "on" : "off",
              }),
            ]}
            renderItem={(item) => <List.Item>{item}</List.Item>}
          />
        </Card>
        {preflightIssues.length > 0 && (
          <Alert
            type="warning"
            showIcon
            message={t("playground.preflightBlocked")}
            description={preflightIssues.join(" · ")}
            style={{ marginBottom: 16 }}
          />
        )}
        {!stream && errorMessage && (
          <Alert
            type="error"
            showIcon
            message={errorMessage}
            description={
              errorRequestId ? (
                <Link to={`/requests/${errorRequestId}`}>
                  {t("playground.openRequest")}
                </Link>
              ) : undefined
            }
            style={{ marginBottom: 16 }}
          />
        )}
        {streamError && (
          <Typography.Paragraph type="danger">
            {streamError}
          </Typography.Paragraph>
        )}
        {(output || result) && (
          <Card className="result-panel">
            <Typography.Title level={4}>
              {t("playground.output")}
            </Typography.Title>
            <Typography.Paragraph>
              {output || (streaming ? "" : result?.output)}
              {streaming && (
                <span style={{ animation: "none", opacity: 0.7 }}>▍</span>
              )}
            </Typography.Paragraph>
            {result && (
              <Space wrap>
                <Typography.Text>
                  {t("playground.selected")}: {result.selectedDeployment}
                </Typography.Text>
                <Typography.Text>
                  {t("playground.usage")}:{" "}
                  {result.inputTokens + result.outputTokens}
                </Typography.Text>
                <Typography.Text>
                  {t("playground.latency")}: {result.latencyMS}
                </Typography.Text>
                <Link
                  className="request-link"
                  to={`/requests/${result.requestId}`}
                >
                  {t("playground.openRequest")}
                </Link>
              </Space>
            )}
          </Card>
        )}
      </Card>
    </section>
  );
}
