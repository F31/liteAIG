export const DEFAULT_API_VERSION = '2026-09-09';

export class LiteAIGClient {
  constructor({ baseURL, apiKey, apiVersion = DEFAULT_API_VERSION, fetch: fetchImpl = globalThis.fetch } = {}) {
    this.baseURL = String(baseURL ?? '').trim().replace(/\/+$/, '');
    this.apiKey = String(apiKey ?? '').trim();
    this.apiVersion = String(apiVersion || DEFAULT_API_VERSION).trim() || DEFAULT_API_VERSION;
    this.fetch = fetchImpl;
    if (!this.baseURL || !this.apiKey) throw new Error('liteaig: baseURL and apiKey are required');
    if (typeof this.fetch !== 'function') throw new Error('liteaig: fetch is required');
  }

  listModels(options) {
    return this.request('GET', '/v1/models', undefined, options);
  }

  createChatCompletion(request, options) {
    return this.request('POST', '/v1/chat/completions', request, options);
  }

  async createChatCompletionStream(request, options) {
    const response = await this.raw('POST', '/v1/chat/completions', { ...request, stream: true }, options);
    await this.ensureOK(response);
    return parseSSE(response, parseChatStreamFrame);
  }

  createResponse(request, options) {
    return this.request('POST', '/v1/responses', request, options);
  }

  createEmbedding(request, options) {
    return this.request('POST', '/v1/embeddings', request, options);
  }

  callMCP(request, options) {
    return this.request('POST', '/mcp', { jsonrpc: '2.0', ...request }, options);
  }

  callMCPTool(id, name, args, options) {
    return this.callMCP({ id, method: 'tools/call', params: { name, arguments: args } }, options);
  }

  async callA2A(request, options) {
    const response = await this.raw('POST', '/a2a', { jsonrpc: '2.0', ...request }, options);
    const payload = await response.json();
    if (!response.ok && !payload?.error) throw new Error(`liteaig: ${response.status} ${response.statusText}`);
    return payload;
  }

  sendA2AMessage(id, message, options) {
    return this.callA2A({ id, method: 'SendMessage', params: { message } }, options);
  }

  async sendA2AMessageStream(id, message, options) {
    const response = await this.raw('POST', '/a2a', { jsonrpc: '2.0', id, method: 'SendStreamingMessage', params: { message } }, options);
    await this.ensureOK(response);
    return parseSSE(response, parseA2AStreamFrame);
  }

  async request(method, path, body, options) {
    const response = await this.raw(method, path, body, options);
    await this.ensureOK(response);
    return response.json();
  }

  raw(method, path, body, options = {}) {
    const headers = new Headers(options.headers);
    headers.set('authorization', `Bearer ${this.apiKey}`);
    headers.set('LiteAIG-API-Version', this.apiVersion);
    const init = { ...options, method, headers };
    if (body !== undefined) {
      headers.set('content-type', 'application/json');
      init.body = JSON.stringify(body);
    }
    return this.fetch(`${this.baseURL}${path}`, init);
  }

  async ensureOK(response) {
    if (response.ok) return;
    const text = await response.text();
    throw new Error(`liteaig: ${response.status} ${response.statusText}: ${text.trim()}`);
  }
}

export default LiteAIGClient;

async function* parseSSE(response, parseFrame) {
  const reader = response.body?.getReader?.();
  if (!reader) throw new Error('liteaig: response body is not readable');
  const decoder = new TextDecoder();
  let buffer = '';
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let index;
      while ((index = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, index).trim();
        buffer = buffer.slice(index + 1);
        if (!line.startsWith('data:')) continue;
        const data = line.slice(5).trim();
        if (!data) continue;
        if (data === '[DONE]') return;
        const event = parseFrame(JSON.parse(data));
        if (event === null) return;
        if (event !== undefined) yield event;
      }
    }
  } finally {
    reader.releaseLock();
  }
}

function parseChatStreamFrame(frame) {
  return frame;
}

function parseA2AStreamFrame(frame) {
  if (frame.error) throw new Error(`a2a stream error: ${frame.error.message || 'A2A stream error'}`);
  if (frame.type === 'completed') return null;
  if (frame.type !== 'message' || !frame.message?.parts) return undefined;
  const delta = frame.message.parts
    .filter((part) => !part.kind || part.kind === 'text')
    .map((part) => part.text ?? '')
    .join('\n');
  return delta ? { delta } : undefined;
}
