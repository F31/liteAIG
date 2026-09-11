# LiteAIG Python SDK

`packages/liteaig-python` is the official dependency-free Python client for LiteAIG's public Gateway API. It uses only the Python standard library and targets Python 3.10+.

## Quick Start

```python
from liteaig import Client

client = Client("http://127.0.0.1:8082", "sk-lia-v1_...")

chat = client.create_chat_completion({
    "model": "default-chat",
    "messages": [{"role": "user", "content": "hello"}],
})

print(chat["choices"][0]["message"]["content"])
```

## Supported Surfaces

| Method | Gateway route |
|---|---|
| `list_models` | `GET /v1/models` |
| `create_chat_completion` | `POST /v1/chat/completions` |
| `create_chat_completion_stream` | `POST /v1/chat/completions` with `stream:true` |
| `create_response` | `POST /v1/responses` |
| `create_embedding` | `POST /v1/embeddings` |
| `call_mcp` | `POST /mcp` JSON-RPC |
| `call_mcp_tool` | `POST /mcp` `tools/call` helper |
| `call_a2a` | `POST /a2a` JSON-RPC |
| `send_a2a_message` | `POST /a2a` official `SendMessage` helper |
| `send_a2a_message_stream` | `POST /a2a` official `SendStreamingMessage` helper |

The client sends `Authorization: Bearer <key>` and pins `LiteAIG-API-Version: 2026-09-09` by default. Pass `api_version` only when testing an explicit compatibility window.

## Streaming

```python
for event in client.create_chat_completion_stream({
    "model": "default-chat",
    "messages": [{"role": "user", "content": "hello"}],
}):
    print(event["choices"][0]["delta"].get("content", ""), end="")
```

## MCP

```python
result = client.call_mcp_tool("1", "invoice.read", {"id": "42"})
if result.get("error"):
    error = result["error"]
    raise RuntimeError(f"mcp error {error['code']}: {error['message']}")

print(result["result"]["content"])
```

## A2A

```python
reply = client.send_a2a_message("1", {
    "messageId": "m1",
    "role": "ROLE_USER",
    "parts": [{"text": "hello"}],
})

if reply.get("error"):
    error = reply["error"]
    raise RuntimeError(f"a2a error {error['code']}: {error['message']}")

print(reply["result"]["message"]["parts"][0]["text"])
```

Streaming A2A returns `A2AStreamEvent` deltas until the server sends a completed frame or `[DONE]`.

```python
stream = client.send_a2a_message_stream("1", {
    "messageId": "m1",
    "role": "ROLE_USER",
    "parts": [{"text": "hello"}],
})

for event in stream:
    print(event.delta, end="")
```

## Local Verification

```bash
python -m unittest discover -s packages/liteaig-python/tests
```
