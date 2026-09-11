# LiteAIG Go SDK

`pkg/liteaig` is the official Go client for LiteAIG's public Gateway API. It is intentionally small and dependency-free.

## Quick Start

```go
client, err := liteaig.New("http://127.0.0.1:8082", "sk-lia-v1_...")
if err != nil {
    return err
}

chat, err := client.CreateChatCompletion(ctx, liteaig.ChatCompletionRequest{
    Model: "default-chat",
    Messages: []liteaig.Message{{Role: "user", Content: "hello"}},
})
if err != nil {
    return err
}
fmt.Println(chat.Choices[0].Message.Content)
```

## Supported Surfaces

| Method | Gateway route |
|---|---|
| `ListModels` | `GET /v1/models` |
| `CreateChatCompletion` | `POST /v1/chat/completions` |
| `CreateChatCompletionStream` | `POST /v1/chat/completions` with `stream:true` |
| `CreateResponse` | `POST /v1/responses` |
| `CreateEmbedding` | `POST /v1/embeddings` |
| `CallMCP` | `POST /mcp` JSON-RPC |
| `CallMCPTool` | `POST /mcp` `tools/call` helper |
| `CallA2A` | `POST /a2a` JSON-RPC |
| `SendA2AMessage` | `POST /a2a` official `SendMessage` helper |
| `SendA2AMessageStream` | `POST /a2a` official `SendStreamingMessage` helper |

The client sends `Authorization: Bearer <key>` and pins `LiteAIG-API-Version: 2026-09-09` by default. Use `WithAPIVersion` only when testing an explicit compatibility window.

## Scope

This SDK slice covers the stable Gateway request/response paths, chat streaming, MCP JSON-RPC calls, blocking A2A `SendMessage`, and streaming A2A `SendStreamingMessage`. Language-specific packages for TypeScript/Python can be added as separate slices without changing the underlying HTTP contract.

## Streaming

```go
stream, err := client.CreateChatCompletionStream(ctx, liteaig.ChatCompletionRequest{
    Model: "default-chat",
    Messages: []liteaig.Message{{Role: "user", Content: "hello"}},
})
if err != nil {
    return err
}
defer stream.Close()

for {
    event, err := stream.Recv()
    if errors.Is(err, io.EOF) {
        break
    }
    if err != nil {
        return err
    }
    fmt.Print(event.Choices[0].Delta.Content)
}
```

## MCP

```go
result, err := client.CallMCPTool(ctx, "1", "invoice.read", map[string]string{"id": "42"})
if err != nil {
    return err
}
if result.Error != nil {
    return fmt.Errorf("mcp error %d: %s", result.Error.Code, result.Error.Message)
}

var payload struct {
    Content string `json:"content"`
}
if err := json.Unmarshal(result.Result, &payload); err != nil {
    return err
}
fmt.Println(payload.Content)
```

## A2A

```go
reply, err := client.SendA2AMessage(ctx, "1", liteaig.A2AMessage{
    MessageID: "m1",
    Role:      "ROLE_USER",
    Parts:     []liteaig.A2APart{{Text: "hello"}},
})
if err != nil {
    return err
}
if reply.Error != nil {
    return fmt.Errorf("a2a error %d: %s", reply.Error.Code, reply.Error.Message)
}

var payload struct {
    Message struct {
        Role  string `json:"role"`
        Parts []struct {
            Text string `json:"text"`
        } `json:"parts"`
    } `json:"message"`
}
if err := json.Unmarshal(reply.Result, &payload); err != nil {
    return err
}
fmt.Println(payload.Message.Parts[0].Text)
```

Streaming A2A uses the same SSE pattern as chat streaming and returns text deltas until the server sends a completed frame or `[DONE]`.

```go
stream, err := client.SendA2AMessageStream(ctx, "1", liteaig.A2AMessage{
    MessageID: "m1",
    Role:      "ROLE_USER",
    Parts:     []liteaig.A2APart{{Text: "hello"}},
})
if err != nil {
    return err
}
defer stream.Close()

for {
    event, err := stream.Recv()
    if errors.Is(err, io.EOF) {
        break
    }
    if err != nil {
        return err
    }
    fmt.Print(event.Delta)
}
```
