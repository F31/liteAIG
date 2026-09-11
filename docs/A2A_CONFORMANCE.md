# Official A2A SDK Conformance

The A2A trusted release hinges on running the **official A2A SDK client against a
live Lite gateway**. No offline test can substitute for it, because the point is
outsourcing the wire contract: the official SDK speaks, we answer. This document
is the runbook.

## What it verifies

1. An official A2A SDK client sends a JSON-RPC `SendMessage` (A2A 1.0 blocking
   wire) to `POST /a2a` using the latest `a2a-sdk`.
2. Lite authenticates it (data-plane API key), relays it through the governed,
   durable, fully-accounted outbound path to a local A2A relay, and answers with
   the A2A 1.0 wire shape.
3. The same SDK client then enables streaming, sends `SendStreamingMessage`, and
   parses the JSON-RPC-over-SSE response into SDK `StreamResponse` events whose
   `message` carries the relayed text.
4. The **official SDK's own types** parse our blocking reply and streaming event
   — proving wire compatibility from the client's lens, not from our own
   connector's lens.

Lite serves both wire profiles on `/a2a`:

- `method: "message/send"` → 0.3-style parts (`[{"kind":"text","text":...}]`).
- `method: "SendMessage"` → A2A 1.0 protobuf-JSON
  (`{"message":{"messageId":...,"role":"ROLE_AGENT","parts":[{"text":...}]}}`),
  which the official SDK v1.x expects.
- `method: "SendStreamingMessage"` → A2A 1.0 JSON-RPC-over-SSE frames whose
  `result` is a `StreamResponse` protobuf-JSON object, which the official SDK
  v1.x streaming parser expects.

## The external runner

`.github/workflows/conformance.yaml` is the networked runner:

- `actions/setup-python@v6`;
- `python -m pip install a2a-sdk` (latest, brings `pydantic`/`httpx`);
- sets `LITEAIG_A2A_SDK_DIR=/tmp/a2a`;
- runs `go test ./internal/app -run TestA2AOfficialSDKInterop -v -count=1`;
- uploads `/tmp/interop-report.json` (written by the harness) as an artifact.

## Running it locally

```bash
python3 -m venv /tmp/a2a-venv
/tmp/a2a-venv/bin/python -m pip install a2a-sdk
cd <repo>
LITEAIG_A2A_SDK_DIR=/tmp/a2a \
PATH="/tmp/a2a-venv/bin:$PATH" \
  go test ./internal/app -run TestA2AOfficialSDKInterop -v -count=1
```

Verified locally against `a2a-sdk==1.1.2`: the driver reports a blocking result
with `mode: "blocking"` and a streaming result with `mode: "stream"`; both are
SDK-parsed objects whose message text is `echo-interop`.

## How the pieces fit

| Piece | Location | Role |
|---|---|---|
| Interop harness | `internal/app/a2a_sdk_conformance_test.go` | boots Lite + publisher config + relay, spawns the SDK driver, asserts the SDK-parsed result |
| SDK driver | `tests/conformance/a2a/client.py` | drives the official SDK (v1.x `ClientFactory` primary, 0.3 `A2AClient` fallback for blocking only), passes the data-plane token, dumps the SDK-parsed blocking result and streaming event list |
| Workflow | `.github/workflows/conformance.yaml` | networked runner that installs the latest `a2a-sdk` and sets the env |
| Gate | env `LITEAIG_A2A_SDK_DIR` | unset ⇒ `t.Skip`, so offline CI stays green |

## Failure modes

- SDK import fails (missing `a2a-sdk`/`pydantic`/`httpx`): the driver exits
  non-zero and the job fails loudly.
- SDK send errors: driver reports `{"sent":false,"error":...}`.
- Our blocking reply or streaming event no longer parses through the SDK, or
  loses the relayed text: the harness's assertions fail and the artifact shows
  what the SDK saw.
- A 401/403 means the data-plane token was not passed: pass `--token <key>`.
