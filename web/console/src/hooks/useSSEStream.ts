import { useEffect, useRef } from "react";
import { scopedController } from "../api/abort";
import { getCSRFToken } from "../api/client";
type EventHandler = (event: {
  id: string;
  event: string;
  data: string;
}) => void;
export type SSEOptions = {
  method?: string;
  body?: string;
  finite?: boolean;
  onDone?: () => void;
  onError?: (message: string) => void;
  onStateChange?: (connected: boolean) => void;
};
export function useSSEStream(
  url: string | undefined,
  scopeKey: string,
  onEvent: EventHandler,
  options?: SSEOptions,
) {
  const optionsRef = useRef(options);
  optionsRef.current = options;
  // onEvent is read through a ref so inline callbacks in consumers do not
  // retrigger the effect (which would abort and reconnect the stream on
  // every event).
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;
  useEffect(() => {
    if (!url) return;
    const opts = optionsRef.current;
    let stopped = false;
    let lastEventId = "";
    let retry = 500;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
    let activeController: AbortController | undefined;
    let doneFired = false;
    const fireDone = () => {
      if (doneFired) return;
      doneFired = true;
      opts?.onDone?.();
    };
    const connect = async () => {
      const controller = scopedController();
      activeController = controller;
      try {
        const headers = new Headers({ Accept: "text/event-stream" });
        if (lastEventId) headers.set("Last-Event-ID", lastEventId);
        if (opts?.body) {
          headers.set("Content-Type", "application/json");
          const token = getCSRFToken();
          if (token) headers.set("X-CSRF-Token", token);
        }
        const response = await fetch(url, {
          method: (opts?.method ?? "GET").toUpperCase(),
          headers,
          body: opts?.body,
          credentials: "include",
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok || !response.body) throw new Error("SSE_UNAVAILABLE");
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        retry = 500;
        opts?.onStateChange?.(true);
        while (!stopped) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          let boundary = buffer.indexOf("\n\n");
          while (boundary >= 0) {
            const block = buffer.slice(0, boundary);
            buffer = buffer.slice(boundary + 2);
            let id = "",
              event = "message",
              data = "";
            for (const line of block.split("\n")) {
              if (line.startsWith("id:")) id = line.slice(3).trim();
              else if (line.startsWith("event:")) event = line.slice(6).trim();
              else if (line.startsWith("data:")) data += line.slice(5).trim();
            }
            if (id) lastEventId = id;
            if (data) onEventRef.current({ id, event, data });
            if (opts?.finite && event === "done") fireDone();
            boundary = buffer.indexOf("\n\n");
          }
        }
        if (stopped) return;
        if (opts?.finite) {
          fireDone();
          return;
        }
        throw new Error("SSE_DISCONNECTED");
      } catch (error) {
        if (
          !stopped &&
          !(error instanceof DOMException && error.name === "AbortError")
        ) {
          if (opts?.finite) {
            opts.onError?.(
              error instanceof Error ? error.message : String(error),
            );
            return;
          }
          opts?.onStateChange?.(false);
          reconnectTimer = setTimeout(() => void connect(), retry);
          retry = Math.min(retry * 2, 10_000);
        }
      }
    };
    void connect();
    return () => {
      stopped = true;
      activeController?.abort();
      if (reconnectTimer) clearTimeout(reconnectTimer);
    };
    // options and onEvent are read through refs so one-shot bodies and
    // inline callbacks need not retrigger (and reset) the stream.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, scopeKey]);
}
