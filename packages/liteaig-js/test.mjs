import { createServer } from 'node:http';
import assert from 'node:assert/strict';
import test from 'node:test';

import { DEFAULT_API_VERSION, LiteAIGClient } from './index.js';

test('calls gateway surfaces and sends pinned headers', async () => {
  const seen = [];
  const server = createServer((request, response) => {
    assert.equal(request.headers.authorization, 'Bearer key');
    assert.equal(request.headers['liteaig-api-version'], DEFAULT_API_VERSION);
    seen.push(`${request.method} ${request.url}`);
    let body = '';
    request.on('data', (chunk) => (body += chunk));
    request.on('end', () => {
      response.setHeader('Content-Type', 'application/json');
      if (request.url === '/v1/models') response.end(JSON.stringify({ object: 'list', data: [{ id: 'default-chat' }] }));
      else if (request.url === '/v1/chat/completions') response.end(JSON.stringify({ id: 'chat', model: 'default-chat', choices: [{ message: { content: 'ok' } }] }));
      else if (request.url === '/v1/responses') response.end(JSON.stringify({ id: 'resp', output: [{ content: [{ type: 'output_text', text: 'hello' }] }] }));
      else if (request.url === '/v1/embeddings') response.end(JSON.stringify({ data: [{ embedding: [0.1] }] }));
      else if (request.url === '/mcp') response.end(JSON.stringify({ jsonrpc: '2.0', id: 'm1', result: { content: 'ok' } }));
      else if (request.url === '/a2a') response.end(JSON.stringify({ jsonrpc: '2.0', id: 'a1', result: { message: { parts: [{ text: 'agent-ok' }] } } }));
      else {
        response.statusCode = 404;
        response.end(JSON.stringify({ error: { code: 'NOT_FOUND' } }));
      }
    });
  });
  await listen(server);
  try {
    const client = new LiteAIGClient({ baseURL: origin(server), apiKey: 'key' });
    assert.equal((await client.listModels()).data[0].id, 'default-chat');
    assert.equal((await client.createChatCompletion({ model: 'default-chat', messages: [{ role: 'user', content: 'hi' }] })).choices[0].message.content, 'ok');
    assert.equal((await client.createResponse({ model: 'default-chat', input: 'hi' })).id, 'resp');
    assert.equal((await client.createEmbedding({ model: 'default-chat', input: ['hi'] })).data[0].embedding[0], 0.1);
    assert.equal((await client.callMCPTool('m1', 'invoice.read', { id: '42' })).result.content, 'ok');
    assert.equal((await client.sendA2AMessage('a1', { messageId: 'msg1', role: 'ROLE_USER', parts: [{ text: 'hi' }] })).result.message.parts[0].text, 'agent-ok');
    assert.deepEqual(seen, ['GET /v1/models', 'POST /v1/chat/completions', 'POST /v1/responses', 'POST /v1/embeddings', 'POST /mcp', 'POST /a2a']);
  } finally {
    await close(server);
  }
});

test('streams chat and A2A SSE frames', async () => {
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk) => (body += chunk));
    request.on('end', () => {
      response.setHeader('Content-Type', 'text/event-stream');
      const input = JSON.parse(body || '{}');
      if (request.url === '/v1/chat/completions') {
        assert.equal(input.stream, true);
        response.end('data: {"choices":[{"delta":{"content":"ok"}}]}\n\ndata: [DONE]\n\n');
      } else if (request.url === '/a2a') {
        assert.equal(input.method, 'SendStreamingMessage');
        response.end('event: message\ndata: {"type":"message","message":{"parts":[{"kind":"text","text":"hel"}]}}\n\ndata: {"type":"message","message":{"parts":[{"kind":"text","text":"lo"}]}}\n\ndata: {"type":"completed"}\n\n');
      } else {
        response.statusCode = 404;
        response.end();
      }
    });
  });
  await listen(server);
  try {
    const client = new LiteAIGClient({ baseURL: origin(server), apiKey: 'key' });
    const chatStream = await client.createChatCompletionStream({ model: 'default-chat', messages: [{ role: 'user', content: 'hi' }] });
    let chatText = '';
    for await (const event of chatStream) chatText += event.choices[0].delta.content;
    assert.equal(chatText, 'ok');

    const a2aStream = await client.sendA2AMessageStream('a1', { messageId: 'msg1', role: 'ROLE_USER', parts: [{ text: 'hi' }] });
    let a2aText = '';
    for await (const event of a2aStream) a2aText += event.delta;
    assert.equal(a2aText, 'hello');
  } finally {
    await close(server);
  }
});

test('surfaces A2A stream JSON-RPC errors', async () => {
  const server = createServer((request, response) => {
    response.setHeader('Content-Type', 'text/event-stream');
    response.end('data: {"jsonrpc":"2.0","id":"1","error":{"code":-32000,"message":"remote boom"}}\n\n');
  });
  await listen(server);
  try {
    const client = new LiteAIGClient({ baseURL: origin(server), apiKey: 'key' });
    const stream = await client.sendA2AMessageStream('a1', { messageId: 'msg1', role: 'ROLE_USER', parts: [{ text: 'hi' }] });
    await assert.rejects(async () => {
      for await (const event of stream) assert.fail(`unexpected event ${JSON.stringify(event)}`);
    }, /remote boom/);
  } finally {
    await close(server);
  }
});

function listen(server) {
  return new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
}

function close(server) {
  return new Promise((resolve) => server.close(resolve));
}

function origin(server) {
  const address = server.address();
  return `http://127.0.0.1:${address.port}`;
}
