import { createServer } from 'node:http';

import Anthropic from '@anthropic-ai/sdk';
import OpenAI from 'openai';

const baseURL = process.env.LITEAIG_BASE_URL;
const apiKey = process.env.LITEAIG_API_KEY ?? 'contract-key';

const dataPNG = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=';

async function runSmoke(origin, key) {
  const openai = new OpenAI({ apiKey: key, baseURL: `${origin}/v1` });
  const chat = await openai.chat.completions.create({ model: 'default-chat', messages: [{ role: 'user', content: 'hello' }] });
  if (!chat.choices?.[0]?.message?.content) throw new Error('OpenAI chat response missing content');

  // Multimodal chat: the official SDK emits a content array with an image_url part.
  const vision = await openai.chat.completions.create({
    model: 'default-chat',
    messages: [{ role: 'user', content: [{ type: 'text', text: 'what is this' }, { type: 'image_url', image_url: { url: dataPNG } }] }],
  });
  if (!vision.choices?.[0]?.message?.content) throw new Error('OpenAI multimodal chat response missing content');

  const stream = await openai.chat.completions.create({ model: 'default-chat', stream: true, messages: [{ role: 'user', content: 'hello stream' }] });
  let streamed = '';
  for await (const chunk of stream) streamed += chunk.choices?.[0]?.delta?.content ?? '';
  if (!streamed) throw new Error('OpenAI stream response missing content');

  const embedding = await openai.embeddings.create({ model: 'default-chat', input: 'hello' });
  if (!embedding.data?.[0]) throw new Error('OpenAI embedding response missing data item');

  await openai.models.list();

  // Stateless /v1/responses surface, text input.
  const responsesText = await openai.responses.create({ model: 'default-chat', input: 'hello responses' });
  if (!responsesText.output_text) throw new Error('OpenAI responses output_text missing');

  // Stateless /v1/responses surface, multimodal input item (input_text + input_image).
  const responsesVision = await openai.responses.create({
    model: 'default-chat',
    input: [{ role: 'user', content: [{ type: 'input_text', text: 'describe' }, { type: 'input_image', image_url: { url: dataPNG } }] }],
  });
  if (!responsesVision.output_text) throw new Error('OpenAI multimodal responses output_text missing');

  // Stateless /v1/responses surface, streaming. The wire mirrors the LiteAIG
  // responses SSE encoder (response.created … response.output_text.delta …
  // response.completed), which the SDK must accumulate back into text.
  const responsesStream = await openai.responses.create({ model: 'default-chat', input: 'hello responses', stream: true });
  let streamedText = '';
  for await (const event of responsesStream) {
    if (event.type === 'response.output_text.delta') streamedText += event.delta;
  }
  if (streamedText !== 'Hello world') throw new Error(`OpenAI responses stream text = ${JSON.stringify(streamedText)}`);

  const anthropic = new Anthropic({ apiKey: key, baseURL: origin });
  const message = await anthropic.messages.create({ model: 'default-chat', max_tokens: 16, messages: [{ role: 'user', content: 'hello' }] });
  if (!message.content?.length) throw new Error('Anthropic message response missing content');

  // Multimodal Anthropic: the official SDK emits an inline base64 image block.
  const anthropicVision = await anthropic.messages.create({
    model: 'default-chat',
    max_tokens: 16,
    messages: [{ role: 'user', content: [{ type: 'text', text: 'describe' }, { type: 'image', source: { type: 'base64', media_type: 'image/png', data: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=' } }] }],
  });
  if (!anthropicVision.content?.length) throw new Error('Anthropic multimodal message response missing content');

  if (process.env.LITEAIG_SMOKE_MCP === '1') {
    const mcp = await fetch(`${origin}/mcp`, {
      method: 'POST',
      headers: { authorization: `Bearer ${key}`, 'content-type': 'application/json' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 'mcp-1', method: 'tools/call', params: { name: 'invoice.read', arguments: { id: '42' } } }),
    });
    const body = await mcp.text();
    if (!mcp.ok || !body.includes('invoice-ok')) throw new Error(`MCP smoke failed: ${mcp.status} ${body}`);
  }

  if (process.env.LITEAIG_SMOKE_A2A === '1') {
    const a2a = await fetch(`${origin}/a2a`, {
      method: 'POST',
      headers: { authorization: `Bearer ${key}`, 'content-type': 'application/json' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 'a2a-1', method: 'message/send', params: { message: { messageId: 'm1', role: 'user', parts: [{ kind: 'text', text: 'hello agent' }] } } }),
    });
    const body = await a2a.text();
    if (!a2a.ok || !body.includes('agent-ok')) throw new Error(`A2A smoke failed: ${a2a.status} ${body}`);
  }
}

if (baseURL) {
  if (!process.env.LITEAIG_API_KEY) throw new Error('LITEAIG_API_KEY is required when LITEAIG_BASE_URL is set');
  await runSmoke(baseURL.replace(/\/$/, ''), apiKey);
} else {
  const paths = [];
  const posts = [];
  const server = createServer((request, response) => {
    let body = '';
    request.on('data', (chunk) => (body += chunk));
    request.on('end', () => {
      paths.push(request.url);
      if (request.method === 'POST') posts.push({ url: request.url, body });
      response.setHeader('Content-Type', 'application/json');
      if (request.url === '/v1/chat/completions') {
        const input = JSON.parse(body || '{}');
        if (input.stream) {
          response.setHeader('Content-Type', 'text/event-stream');
          response.end('data: {"id":"chat","object":"chat.completion.chunk","model":"default-chat","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n');
          return;
        }
        response.end(JSON.stringify({ id: 'chat', model: 'default-chat', choices: [{ index: 0, message: { role: 'assistant', content: 'ok' }, finish_reason: 'stop' }], usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 } }));
        return;
      }
      if (request.url === '/v1/responses') {
        const input = JSON.parse(body || '{}');
        if (input.stream) {
          // Responses SSE event sequence, matching the LiteAIG server encoder
          // (data-only JSON lines, no [DONE]; the stream ends on connection close).
          const skeleton = { id: 'resp_1', object: 'response', created_at: 1700000000, status: 'in_progress', model: 'default-chat', output: [] };
          const item = { id: 'msg_liteaig', type: 'message', status: 'in_progress', role: 'assistant', content: [] };
          const events = [
            { type: 'response.created', response: skeleton },
            { type: 'response.in_progress', response: skeleton },
            { type: 'response.output_item.added', output_index: 0, item },
            { type: 'response.content_part.added', item_id: 'msg_liteaig', output_index: 0, content_index: 0, part: { type: 'output_text', text: '', annotations: [] } },
            { type: 'response.output_text.delta', item_id: 'msg_liteaig', output_index: 0, content_index: 0, delta: 'Hel' },
            { type: 'response.output_text.delta', item_id: 'msg_liteaig', output_index: 0, content_index: 0, delta: 'lo ' },
            { type: 'response.output_text.delta', item_id: 'msg_liteaig', output_index: 0, content_index: 0, delta: 'world' },
            { type: 'response.output_text.done', item_id: 'msg_liteaig', output_index: 0, content_index: 0, text: 'Hello world', logprobs: [] },
            { type: 'response.content_part.done', item_id: 'msg_liteaig', output_index: 0, content_index: 0, part: { type: 'output_text', text: 'Hello world', annotations: [] } },
            { type: 'response.output_item.done', output_index: 0, item: { id: 'msg_liteaig', type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text: 'Hello world', annotations: [] }] } },
            { type: 'response.completed', response: { id: 'resp_1', object: 'response', created_at: 1700000000, status: 'completed', model: 'default-chat', output: [{ id: 'msg_liteaig', type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text: 'Hello world', annotations: [] }] }], usage: { input_tokens: 1, output_tokens: 3, total_tokens: 4 } } },
          ];
          response.setHeader('Content-Type', 'text/event-stream');
          response.end(events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(''));
          return;
        }
        response.end(JSON.stringify({ id: 'resp_1', object: 'response', created_at: 1700000000, status: 'completed', model: 'default-chat', output: [{ id: 'msg_1', type: 'message', role: 'assistant', status: 'completed', content: [{ type: 'output_text', text: 'ok', annotations: [] }] }], usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 } }));
        return;
      }
      if (request.url === '/v1/embeddings') return response.end(JSON.stringify({ object: 'list', model: 'default-chat', data: [{ object: 'embedding', index: 0, embedding: [0.1] }], usage: { prompt_tokens: 1, total_tokens: 1 } }));
      if (request.url === '/v1/models') return response.end(JSON.stringify({ object: 'list', data: [{ id: 'default-chat', object: 'model', created: 1, owned_by: 'liteaig' }] }));
      if (request.url === '/v1/messages') return response.end(JSON.stringify({ id: 'message', type: 'message', role: 'assistant', model: 'default-chat', content: [{ type: 'text', text: 'ok' }], stop_reason: 'end_turn', usage: { input_tokens: 1, output_tokens: 1 } }));
      response.statusCode = 404;
      response.end(JSON.stringify({ error: { code: 'NOT_FOUND' } }));
    });
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  const origin = `http://127.0.0.1:${address.port}`;
  try {
    await runSmoke(origin, apiKey);
    for (const path of ['/v1/chat/completions', '/v1/responses', '/v1/embeddings', '/v1/models', '/v1/messages']) {
      if (!paths.includes(path)) throw new Error(`SDK did not call ${path}: ${paths.join(',')}`);
    }
    const bodiesFor = (url) => posts.filter((p) => p.url === url).map((p) => p.body);
    // The SDK must emit multimodal bodies in the OpenAI-compatible and
    // Anthropic wire shapes so a real gateway can decode them.
    const responsesVisionBody = bodiesFor('/v1/responses').find((raw) => raw.includes('input_image'));
    if (!responsesVisionBody) throw new Error(`responses multimodal body missing: ${bodiesFor('/v1/responses').join('\n')}`);
    for (const marker of ['image_url', 'input_image', 'input_text']) {
      if (!responsesVisionBody.includes(marker)) throw new Error(`responses body missing ${marker}: ${responsesVisionBody}`);
    }
    const chatVision = bodiesFor('/v1/chat/completions').find((raw) => raw.includes('"type":"image_url"'));
    if (!chatVision) throw new Error(`chat multimodal body missing image_url: ${bodiesFor('/v1/chat/completions').join('\n')}`);
    const chatContent = JSON.parse(chatVision).messages?.[0]?.content;
    if (!Array.isArray(chatContent) || !chatContent.some((part) => part.type === 'image_url')) throw new Error(`chat multimodal content missing image_url: ${chatVision}`);
    const anthropicBody = bodiesFor('/v1/messages').find((raw) => raw.includes('"type":"image"'));
    if (!anthropicBody) throw new Error(`anthropic multimodal body missing image block: ${bodiesFor('/v1/messages').join('\n')}`);
    const anthropicContent = JSON.parse(anthropicBody).messages?.[0]?.content;
    if (!Array.isArray(anthropicContent) || !anthropicContent.some((part) => part.type === 'image' && part.source?.type === 'base64' && part.source.media_type === 'image/png')) throw new Error(`anthropic multimodal content missing image block: ${anthropicBody}`);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}
