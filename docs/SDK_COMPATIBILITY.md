# SDK Compatibility Matrix

LiteAIG maintains small official clients for the stable public Gateway contract. All clients pin `LiteAIG-API-Version: 2026-09-09` by default and expose the same core surfaces.

## Official LiteAIG Clients

| Language | Package path | Runtime target | Dependency policy | Verification |
|---|---|---|---|---|
| Go | `pkg/liteaig` | Go 1.25+ module consumers | Standard library only | `go test ./pkg/liteaig` |
| JavaScript / TypeScript | `packages/liteaig-js` | Node 18+ ESM with native `fetch` | No runtime dependencies | `node --test packages/liteaig-js/test.mjs`; `npm pack --dry-run --json packages/liteaig-js` |
| Python | `packages/liteaig-python` | Python 3.10+ | Standard library only | `python -m unittest discover -s packages/liteaig-python/tests -t packages/liteaig-python`; local `pip install --no-deps` smoke |

## Surface Coverage

| Gateway surface | Go | JS/TS | Python |
|---|---|---|---|
| `GET /v1/models` | `ListModels` | `listModels` | `list_models` |
| `POST /v1/chat/completions` | `CreateChatCompletion` | `createChatCompletion` | `create_chat_completion` |
| Chat SSE streaming | `CreateChatCompletionStream` | `createChatCompletionStream` | `create_chat_completion_stream` |
| `POST /v1/responses` | `CreateResponse` | `createResponse` | `create_response` |
| `POST /v1/embeddings` | `CreateEmbedding` | `createEmbedding` | `create_embedding` |
| `POST /mcp` JSON-RPC | `CallMCP` | `callMCP` | `call_mcp` |
| MCP `tools/call` helper | `CallMCPTool` | `callMCPTool` | `call_mcp_tool` |
| `POST /a2a` JSON-RPC | `CallA2A` | `callA2A` | `call_a2a` |
| A2A `SendMessage` helper | `SendA2AMessage` | `sendA2AMessage` | `send_a2a_message` |
| A2A `SendStreamingMessage` helper | `SendA2AMessageStream` | `sendA2AMessageStream` | `send_a2a_message_stream` |

## Compatibility Gate

Run the full SDK gate locally with:

```bash
bash scripts/check-sdks.sh
```

CI runs the same gate in the `sdk-packages` job. The gate intentionally avoids publishing credentials and only verifies package shape, importability, core request headers, JSON-RPC helpers, and SSE parsing behavior.

## Official Provider SDK Interop

This matrix covers LiteAIG-owned clients. OpenAI/Anthropic official SDK compatibility is tracked separately in `docs/MODEL_SDK_CONFORMANCE.md` and the `sdk-conformance` workflow.
