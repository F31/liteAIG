import { createServer } from "node:http";

const KEY = "sk-test-provider";
const PORT = Number(process.env.PROVIDER_PORT ?? "8092");
const HOST = "127.0.0.1";

const server = createServer((req, res) => {
  if (req.url === "/healthz") {
    res.writeHead(200, { "content-type": "application/json" });
    res.end("{}");
    return;
  }
  if (req.headers.authorization !== `Bearer ${KEY}`) {
    res.writeHead(401, { "content-type": "application/json" });
    res.end(
      JSON.stringify({
        error: {
          message: "invalid api key",
          type: "invalid_request_error",
          code: "invalid_api_key",
        },
      }),
    );
    return;
  }
  if (req.method === "GET" && req.url === "/v1/models") {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(
      JSON.stringify({
        object: "list",
        data: [
          { id: "gpt-4o-mini", object: "model" },
          { id: "gpt-4o", object: "model" },
        ],
      }),
    );
    return;
  }
  if (req.method === "POST" && req.url === "/v1/chat/completions") {
    let body = "";
    req.on("data", (chunk) => (body += chunk));
    req.on("end", () => {
      const request = JSON.parse(body || "{}");
      const model = request.model ?? "gpt-4o-mini";
      if (request.stream) {
        res.writeHead(200, { "content-type": "text/event-stream" });
        res.write(
          `data: ${JSON.stringify({ id: "chatcmpl-1", object: "chat.completion.chunk", model, choices: [{ index: 0, delta: { role: "assistant", content: "stream-" } }] })}\n\n`,
        );
        res.write(
          `data: ${JSON.stringify({ id: "chatcmpl-1", object: "chat.completion.chunk", model, choices: [{ index: 0, delta: { content: "ok" }, finish_reason: "stop" }], usage: { prompt_tokens: 3, completion_tokens: 5, total_tokens: 8 } })}\n\n`,
        );
        res.write("data: [DONE]\n\n");
        res.end();
        return;
      }
      res.writeHead(200, { "content-type": "application/json" });
      res.end(
        JSON.stringify({
          id: "chatcmpl-1",
          object: "chat.completion",
          model,
          choices: [
            {
              index: 0,
              message: { role: "assistant", content: "provider-echo" },
              finish_reason: "stop",
            },
          ],
          usage: { prompt_tokens: 3, completion_tokens: 5, total_tokens: 8 },
        }),
      );
    });
    return;
  }
  if (req.method === "POST" && req.url === "/v1/embeddings") {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(
      JSON.stringify({
        object: "list",
        model: "gpt-4o-mini",
        data: [{ object: "embedding", index: 0, embedding: [0.1, 0.2] }],
        usage: { prompt_tokens: 2, total_tokens: 2 },
      }),
    );
    return;
  }
  res.writeHead(404, { "content-type": "application/json" });
  res.end(JSON.stringify({ error: { message: "not found" } }));
});
server.listen(PORT, HOST, () => {
  console.log(`test provider ready on ${HOST}:${PORT}`);
});
