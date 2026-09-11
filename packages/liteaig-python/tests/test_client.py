import json
import os
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from liteaig import DEFAULT_API_VERSION, Client, LiteAIGError

os.environ["NO_PROXY"] = "127.0.0.1,localhost"
os.environ["no_proxy"] = "127.0.0.1,localhost"


class Handler(BaseHTTPRequestHandler):
    seen = []
    bodies = []

    def do_GET(self):
        self._assert_headers()
        Handler.seen.append(f"GET {self.path}")
        if self.path == "/v1/models":
            self._json({"object": "list", "data": [{"id": "default-chat"}]})
        else:
            self.send_error(404)

    def do_POST(self):
        self._assert_headers()
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode("utf-8")
        body = json.loads(raw or "{}")
        Handler.seen.append(f"POST {self.path}")
        Handler.bodies.append(body)
        if self.path == "/v1/chat/completions":
            if body.get("stream"):
                self._sse([
                    {"choices": [{"delta": {"content": "ok"}}]},
                    "[DONE]",
                ])
            else:
                self._json({"id": "chat", "model": "default-chat", "choices": [{"message": {"content": "ok"}}]})
        elif self.path == "/v1/responses":
            self._json({"id": "resp", "output": [{"content": [{"type": "output_text", "text": "hello"}]}]})
        elif self.path == "/v1/embeddings":
            self._json({"data": [{"embedding": [0.1]}]})
        elif self.path == "/mcp":
            self._json({"jsonrpc": "2.0", "id": body.get("id"), "result": {"content": "ok"}})
        elif self.path == "/a2a" and body.get("method") == "SendMessage":
            self._json({"jsonrpc": "2.0", "id": body.get("id"), "result": {"message": {"parts": [{"text": "agent-ok"}]}}})
        elif self.path == "/a2a" and body.get("method") == "SendStreamingMessage":
            self._sse([
                {"type": "message", "message": {"parts": [{"kind": "text", "text": "hel"}]}},
                {"type": "message", "message": {"parts": [{"kind": "text", "text": "lo"}]}},
                {"type": "completed"},
            ])
        else:
            self.send_error(404)

    def log_message(self, _format, *args):
        return

    def _assert_headers(self):
        assert self.headers.get("Authorization") == "Bearer key"
        assert self.headers.get("LiteAIG-API-Version") == DEFAULT_API_VERSION

    def _json(self, payload, status=200):
        data = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _sse(self, events):
        chunks = []
        for event in events:
            data = event if isinstance(event, str) else json.dumps(event)
            chunks.append(f"data: {data}\n\n")
        payload = "".join(chunks).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


class ErrorHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        payload = b'data: {"jsonrpc":"2.0","id":"1","error":{"code":-32000,"message":"remote boom"}}\n\n'
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, _format, *args):
        return


class ClientTest(unittest.TestCase):
    def setUp(self):
        Handler.seen = []
        Handler.bodies = []
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.client = Client(f"http://127.0.0.1:{self.server.server_port}", "key")

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)

    def test_calls_gateway_surfaces(self):
        self.assertEqual(self.client.list_models()["data"][0]["id"], "default-chat")
        self.assertEqual(self.client.create_chat_completion({"model": "default-chat", "messages": [{"role": "user", "content": "hi"}]})["choices"][0]["message"]["content"], "ok")
        self.assertEqual(self.client.create_response({"model": "default-chat", "input": "hi"})["id"], "resp")
        self.assertEqual(self.client.create_embedding({"model": "default-chat", "input": ["hi"]})["data"][0]["embedding"][0], 0.1)
        self.assertEqual(self.client.call_mcp_tool("m1", "invoice.read", {"id": "42"})["result"]["content"], "ok")
        self.assertEqual(self.client.send_a2a_message("a1", {"messageId": "msg1", "role": "ROLE_USER", "parts": [{"text": "hi"}]})["result"]["message"]["parts"][0]["text"], "agent-ok")
        self.assertEqual(Handler.seen, ["GET /v1/models", "POST /v1/chat/completions", "POST /v1/responses", "POST /v1/embeddings", "POST /mcp", "POST /a2a"])

    def test_streams_chat_and_a2a(self):
        chat = list(self.client.create_chat_completion_stream({"model": "default-chat", "messages": [{"role": "user", "content": "hi"}]}))
        self.assertEqual(chat[0]["choices"][0]["delta"]["content"], "ok")
        a2a = list(self.client.send_a2a_message_stream("a1", {"messageId": "msg1", "role": "ROLE_USER", "parts": [{"text": "hi"}]}))
        self.assertEqual("".join(event.delta for event in a2a), "hello")

    def test_requires_base_url_and_key(self):
        with self.assertRaises(LiteAIGError):
            Client("", "key")


class A2AStreamErrorTest(unittest.TestCase):
    def test_surfaces_json_rpc_error(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), ErrorHandler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = Client(f"http://127.0.0.1:{server.server_port}", "key")
            with self.assertRaisesRegex(LiteAIGError, "remote boom"):
                list(client.send_a2a_message_stream("a1", {"messageId": "msg1", "role": "ROLE_USER", "parts": [{"text": "hi"}]}))
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)


if __name__ == "__main__":
    unittest.main()
