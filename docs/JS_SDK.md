# LiteAIG JavaScript SDK

`packages/liteaig-js` is the official dependency-free JavaScript client for LiteAIG's public Gateway API. It ships ESM runtime code and TypeScript declarations, and targets Node 18+ native `fetch`.

## Quick Start

```js
import { LiteAIGClient } from '@liteaig/client';

const client = new LiteAIGClient({
  baseURL: 'http://127.0.0.1:8082',
  apiKey: 'sk-lia-v1_...',
});

const chat = await client.createChatCompletion({
  model: 'default-chat',
  messages: [{ role: 'user', content: 'hello' }],
});

console.log(chat.choices[0].message.content);
```

## Supported Surfaces

| Method | Gateway route |
|---|---|
| `listModels` | `GET /v1/models` |
| `createChatCompletion` | `POST /v1/chat/completions` |
| `createChatCompletionStream` | `POST /v1/chat/completions` with `stream:true` |
| `createResponse` | `POST /v1/responses` |
| `createEmbedding` | `POST /v1/embeddings` |
| `callMCP` | `POST /mcp` JSON-RPC |
| `callMCPTool` | `POST /mcp` `tools/call` helper |
| `callA2A` | `POST /a2a` JSON-RPC |
| `sendA2AMessage` | `POST /a2a` official `SendMessage` helper |
| `sendA2AMessageStream` | `POST /a2a` official `SendStreamingMessage` helper |

The client sends `Authorization: Bearer <key>` and pins `LiteAIG-API-Version: 2026-09-09` by default. Pass `apiVersion` only when testing an explicit compatibility window.

## Streaming

```js
const stream = await client.createChatCompletionStream({
  model: 'default-chat',
  messages: [{ role: 'user', content: 'hello' }],
});

for await (const event of stream) {
  process.stdout.write(event.choices?.[0]?.delta?.content ?? '');
}
```

## MCP

```js
const result = await client.callMCPTool('1', 'invoice.read', { id: '42' });
if (result.error) {
  throw new Error(`mcp error ${result.error.code}: ${result.error.message}`);
}

console.log(result.result.content);
```

## A2A

```js
const reply = await client.sendA2AMessage('1', {
  messageId: 'm1',
  role: 'ROLE_USER',
  parts: [{ text: 'hello' }],
});

if (reply.error) {
  throw new Error(`a2a error ${reply.error.code}: ${reply.error.message}`);
}

console.log(reply.result.message.parts[0].text);
```

Streaming A2A returns text deltas until the server sends a completed frame or `[DONE]`.

```js
const stream = await client.sendA2AMessageStream('1', {
  messageId: 'm1',
  role: 'ROLE_USER',
  parts: [{ text: 'hello' }],
});

for await (const event of stream) {
  process.stdout.write(event.delta);
}
```

## Local Verification

```bash
node --test packages/liteaig-js/test.mjs
```
