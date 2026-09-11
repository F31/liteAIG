# LiteAIG A2A Streaming Profile

This document defines the streaming and async-delivery extensions of the
LiteAIG `/a2a` surface and its outbound A2A connector. We control both ends of
the wire (our outbound consumer in `internal/connectors/agent/a2a` and our
inbound server in `internal/gateway/server`), so instead of mirroring every A2A
event spelling we define one simple canonical profile. The framing helpers live
in `internal/access/protocol/a2a` (`EncodeStreamChunk`,
`EncodeStreamCompleted`, `ParseStreamFrame`) and are used byte-identically by
both sides.

Lite also serves the official A2A 1.0 SDK streaming method
`SendStreamingMessage`. That path uses the SDK's JSON-RPC-over-SSE shape rather
than the LiteAIG profile below, and is covered by `docs/A2A_CONFORMANCE.md`.

## Requesting the streaming profile

An inbound `POST /a2a` `message/send` is served as SSE when the caller asks for
it, exactly like the model endpoints:

- an `Accept: text/event-stream` request header, or
- a `?stream=1` query parameter.

A streaming request must not also be combined with `pushNotificationConfig`;
async delivery only applies to completed non-streaming results and is ignored on
a streaming path.

Official SDK v1.x callers use JSON-RPC method `SendStreamingMessage` instead of
`message/send` + `Accept`. Lite answers that method with SSE frames whose `data:`
payload is a JSON-RPC response and whose `result` is an A2A `StreamResponse`
protobuf-JSON object:

```text
data: {"jsonrpc":"2.0","id":"...","result":{"message":{"messageId":"...","role":"ROLE_AGENT","parts":[{"text":"<delta>"}]}}}
```

The official SDK's streaming parser is asserted in
`internal/app/a2a_sdk_conformance_test.go`.

## Canonical wire events

Every SSE data line carries exactly one JSON object discriminated by `type`:

```text
data: {"type":"message","message":{"parts":[{"kind":"text","text":"<delta>"}]}}

data: {"type":"completed"}
```

- `type: message` accumulates one text delta of the agent reply. The
  `message.parts` array uses the A2A `{kind:"text",text:...}` shape so a generic
  A2A client can read it; consumers concatenate text parts in order.
- `type: completed` terminates a successful stream, after which the server
  closes the connection. No `[DONE]` marker is required; a compliant peer MAY
  still send one, and the outbound consumer treats `[DONE]` as terminal.
- Comment/keep-alive and `event:` lines are ignored by the SSE readers, which
  deliver only `data:` lines to `ParseStreamFrame`.
- A peer that fails mid-stream emits a JSON-RPC `error` object
  (`{"jsonrpc":"2.0","id":...,"error":{...}}`); `ParseStreamFrame` surfaces it as
  an error and the consumer fails the stream (non-retryable).
- Unknown frame types are ignored so future event kinds never break a consumer.

## Inbound server behavior

When a caller requests the profile, the `/a2a` handler responds
`Content-Type: text/event-stream` and renders the relay result as one
`type: message` event followed by `type: completed`.

Today the Lite relay genuinely forwards incremental inbound deltas: the governed,
durable, fully-accounted outbound relay (`invokeA2A`) drives the connector's
real SSE consumption, and each remote text delta is emitted as its own
`type: message` event before the terminal `type: completed` event. A peer that
answers with plain JSON (no streaming profile) is honored as a single whole
message event, exactly like a blocking relay. A server whose Core does not
implement the optional `ToolStream(ctx, AdmitInput, contracts.StreamWriter)`
surface answers a streaming request with `503 STREAMING_UNSUPPORTED` (mirroring
the model endpoints) instead of an empty stream. `liteGatewayCore` implements
`ToolStream` by running the same admission, durable task/idempotency, and
accounting path as the blocking `Tool` relay while forwarding deltas live.

## Outbound connector behavior

`internal/connectors/agent/a2a` `Connector.Stream` implements **true incremental
SSE consumption**:

- When `Request.Stream` is true it issues the conformant `message/send` POST with
  `Accept: text/event-stream` and parses the response body as SSE, forwarding one
  `contracts.StreamChunk` per `type: message` delta and a final chunk on
  `type: completed` (or `[DONE]`). A stream that ends without a terminal frame is
  an error (`io.ErrUnexpectedEOF`), so a truncated peer never looks successful.
- A streaming request that is answered with `application/json` (a peer without
  the profile) falls back to the blocking result as a single final chunk.
- A non-streaming request keeps the historical behavior: one final chunk with
  the whole reply.
- Credential handling (`SecretRef`/`AuthScheme`/`Headers`) is identical to
  `Invoke` — the two share one request builder (`send`).
- Streaming consumption is bounded by the same `Config.Timeout` as blocking
  calls. `Capabilities` therefore advertises `a2a.streaming`, and only because
  the SSE profile above is genuinely consumed.

## Push to a caller callback (minimal)

A caller may attach an A2A-style async-delivery hint to a **non-streaming**
`message/send`:

```json
{
  "jsonrpc": "2.0",
  "id": "…",
  "method": "message/send",
  "params": {
    "message": {"messageId": "…", "role": "user", "parts": [{"kind": "text", "text": "…"}]},
    "pushNotificationConfig": {"url": "https://callback.example/hook", "token": "…"}
  }
}
```

When present and the relay succeeds, the completed task result is enqueued for
delivery to `url`:

- the URL must pass `egress.ValidateTarget(egress.LitePolicy())`; a rejected
  target is logged and never posted, and never affects the caller's reply;
- when queued durably by Lite, the callback URL is sealed with the existing
  AES-GCM master cipher before it is stored and opened only in the worker before
  egress validation and the callback POST;
- the token, when present, is attached as `Authorization: Bearer <token>`;
- when the callback is queued durably by Lite, that bearer token is sealed with
  the existing AES-GCM master cipher before it is stored and opened only in the
  worker immediately before the callback POST;
- durable callback payloads are also sealed with the same AES-GCM master cipher,
  so completed agent replies are not stored as plaintext in the push outbox;
- the body is the same result JSON the blocking reply returns (the
  `result.message.parts` text) plus `messageId` and a `task` object carrying
  `{"status":"completed"}` and the durable task id when known;
- when `A2APushOutbox` is configured, delivery is durable, signed, and retried
  within a bounded budget (default 3 attempts); failures are logged only after
  generic reason normalization, and URLs/tokens are never written to logs;
- the Lite composition runs the durable worker under the shared
  `coordination_leases` scope `platform:a2a_push_outbox` so only one process
  drains due callback rows at a time;
- due rows are atomically claimed with a `pending -> sending` SQL transition;
  a stale `sending` claim is recoverable after the worker-claim timeout, so the
  outbox still avoids duplicate sends if two workers briefly overlap;
- callback POSTs include `X-LiteAIG-A2A-Push-Timestamp` and
  `X-LiteAIG-A2A-Push-ID` (durable deliveries) plus
  `X-LiteAIG-A2A-Push-Signature: sha256=<hex-hmac>` when
  `A2APushSigningSecret` is configured;
- callback receivers can verify the signature with
  `a2a.VerifyPushPayloadSignatureWithID(secret, deliveryID, timestamp, signature, body, time.Now(), maxSkew)`;
  receivers should store accepted delivery ids and reject duplicates inside the
  timestamp skew window for replay protection;
- without an outbox the server keeps a one-shot compatibility path: delivery
  runs in a goroutine with a bounded 5s timeout so the HTTP response returns
  immediately and never waits on the callback;
- the outbound POST uses the guarded egress client (`CallbackClient` in the
  server config, otherwise a lazily built `egress.LitePolicy` client).

## Residual limitations

- No notification-config CRUD, task subscription, or resubscribe endpoints.
- Inbound and outbound streaming both consume/produce the same incremental
  profile: the inbound relay forwards each remote delta as its own event and a
  peer that streams deltas is never buffered into a single whole-reply event.
- Streaming a request disables `pushNotificationConfig` (async delivery is only
  meaningful for completed non-streaming results).
