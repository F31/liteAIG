export const DEFAULT_API_VERSION: '2026-09-09';

export interface LiteAIGClientOptions {
  baseURL: string;
  apiKey: string;
  apiVersion?: string;
  fetch?: typeof fetch;
}

export interface RequestOptions extends RequestInit {}

export interface Message {
  role: string;
  content: unknown;
}

export interface ChatCompletionRequest {
  model: string;
  messages: Message[];
  max_tokens?: number;
  temperature?: number;
  stream?: boolean;
}

export interface A2APart {
  kind?: string;
  text: string;
}

export interface A2AMessage {
  messageId: string;
  role: string;
  parts: A2APart[];
  metadata?: Record<string, string>;
}

export interface JSONRPCRequest {
  jsonrpc?: string;
  id?: unknown;
  method: string;
  params?: unknown;
}

export interface JSONRPCError {
  code: number;
  message: string;
}

export interface JSONRPCResponse<T = unknown> {
  jsonrpc: string;
  id?: unknown;
  result?: T;
  error?: JSONRPCError;
}

export interface A2AStreamEvent {
  delta: string;
}

export class LiteAIGClient {
  constructor(options: LiteAIGClientOptions);
  listModels<T = unknown>(options?: RequestOptions): Promise<T>;
  createChatCompletion<T = unknown>(request: ChatCompletionRequest, options?: RequestOptions): Promise<T>;
  createChatCompletionStream<T = unknown>(request: ChatCompletionRequest, options?: RequestOptions): Promise<AsyncIterable<T>>;
  createResponse<T = unknown>(request: unknown, options?: RequestOptions): Promise<T>;
  createEmbedding<T = unknown>(request: unknown, options?: RequestOptions): Promise<T>;
  callMCP<T = unknown>(request: JSONRPCRequest, options?: RequestOptions): Promise<JSONRPCResponse<T>>;
  callMCPTool<T = unknown>(id: unknown, name: string, args?: unknown, options?: RequestOptions): Promise<JSONRPCResponse<T>>;
  callA2A<T = unknown>(request: JSONRPCRequest, options?: RequestOptions): Promise<JSONRPCResponse<T>>;
  sendA2AMessage<T = unknown>(id: unknown, message: A2AMessage, options?: RequestOptions): Promise<JSONRPCResponse<T>>;
  sendA2AMessageStream(id: unknown, message: A2AMessage, options?: RequestOptions): Promise<AsyncIterable<A2AStreamEvent>>;
}

export default LiteAIGClient;
