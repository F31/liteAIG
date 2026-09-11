# Official Model SDK Conformance

LiteAIG's OpenAI and Anthropic compatibility promise is: an application can keep
using the official SDK and change only `api_key` plus `base_url` to point at the

## What It Verifies

1. The official OpenAI Python SDK sends `models.list`,
   `chat.completions.create`, and streaming `chat.completions.create(...,
   stream=True)` through `base_url=<gateway>/v1`.
2. The official Anthropic Python SDK sends `messages.create` through
   `base_url=<gateway>`.
3. Lite authenticates the gateway API key, routes the calls through the normal
   seven-stage pipeline, invokes the local OpenAI-compatible provider test
   double, and returns SDK-parsable wire responses.
4. The SDK-parsed outputs contain the expected provider reply (`provider-echo`)
   and, for OpenAI streaming, the incremental text `stream-ok`.

## Running It Locally

```bash
python3 -m venv /tmp/model-sdk-venv
/tmp/model-sdk-venv/bin/python -m pip install openai anthropic

PATH="/tmp/model-sdk-venv/bin:$PATH" \
LITEAIG_MODEL_SDK_CONFORMANCE=1 \
LITEAIG_MODEL_SDK_REPORT=/tmp/model-sdk-interop-report.json \
  go test ./internal/app -run TestOfficialModelSDKInterop -v -count=1
```

Verified locally against `openai==3.9.0` and `anthropic==1.4.0`.

## Pieces

| Piece | Location | Role |
|---|---|---|
| Go harness | `internal/app/model_sdk_conformance_test.go` | boots Lite + local provider, spawns official SDK drivers, asserts SDK-parsed replies |
| OpenAI driver | `tests/conformance/model/openai_client.py` | official OpenAI SDK with only `api_key` and `base_url` changed |
| Anthropic driver | `tests/conformance/model/anthropic_client.py` | official Anthropic SDK with only `api_key` and `base_url` changed |
| Workflow | `.github/workflows/conformance.yaml` | networked runner that installs latest SDKs and uploads reports |

## Failure Modes

- SDK import or API shape changes: the driver exits non-zero and the workflow
  fails loudly.
- Gateway wire regression: the SDK raises a parse/API error or returns a payload
  missing the expected text.
- Authentication regression: official SDK calls fail with 401/403 and the report
  includes the SDK-visible error.
