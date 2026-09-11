from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Dict, Iterator, Mapping, Optional
from urllib.error import HTTPError
from urllib.request import Request, urlopen

DEFAULT_API_VERSION = "2026-09-09"


class LiteAIGError(Exception):
    pass


@dataclass(frozen=True)
class A2AStreamEvent:
    delta: str


class Client:
    def __init__(self, base_url: str, api_key: str, *, api_version: str = DEFAULT_API_VERSION, timeout: float = 30.0) -> None:
        self.base_url = str(base_url or "").strip().rstrip("/")
        self.api_key = str(api_key or "").strip()
        self.api_version = str(api_version or DEFAULT_API_VERSION).strip() or DEFAULT_API_VERSION
        self.timeout = timeout
        if not self.base_url or not self.api_key:
            raise LiteAIGError("liteaig: base_url and api_key are required")

    def list_models(self) -> Dict[str, Any]:
        return self._request("GET", "/v1/models")

    def create_chat_completion(self, request: Mapping[str, Any]) -> Dict[str, Any]:
        return self._request("POST", "/v1/chat/completions", dict(request))

    def create_chat_completion_stream(self, request: Mapping[str, Any]) -> Iterator[Dict[str, Any]]:
        body = dict(request)
        body["stream"] = True
        with self._open("POST", "/v1/chat/completions", body) as response:
            yield from _iter_sse(response, _parse_chat_stream_frame)

    def create_response(self, request: Mapping[str, Any]) -> Dict[str, Any]:
        return self._request("POST", "/v1/responses", dict(request))

    def create_embedding(self, request: Mapping[str, Any]) -> Dict[str, Any]:
        return self._request("POST", "/v1/embeddings", dict(request))

    def call_mcp(self, request: Mapping[str, Any]) -> Dict[str, Any]:
        body = {"jsonrpc": "2.0", **dict(request)}
        return self._request("POST", "/mcp", body)

    def call_mcp_tool(self, request_id: Any, name: str, arguments: Optional[Any] = None) -> Dict[str, Any]:
        return self.call_mcp({"id": request_id, "method": "tools/call", "params": {"name": name, "arguments": arguments}})

    def call_a2a(self, request: Mapping[str, Any]) -> Dict[str, Any]:
        body = {"jsonrpc": "2.0", **dict(request)}
        return self._request("POST", "/a2a", body, json_rpc_error_as_response=True)

    def send_a2a_message(self, request_id: Any, message: Mapping[str, Any]) -> Dict[str, Any]:
        return self.call_a2a({"id": request_id, "method": "SendMessage", "params": {"message": dict(message)}})

    def send_a2a_message_stream(self, request_id: Any, message: Mapping[str, Any]) -> Iterator[A2AStreamEvent]:
        body = {"jsonrpc": "2.0", "id": request_id, "method": "SendStreamingMessage", "params": {"message": dict(message)}}
        with self._open("POST", "/a2a", body) as response:
            yield from _iter_sse(response, _parse_a2a_stream_frame)

    def _request(self, method: str, path: str, body: Optional[Mapping[str, Any]] = None, *, json_rpc_error_as_response: bool = False) -> Dict[str, Any]:
        try:
            with self._open(method, path, body) as response:
                payload = response.read().decode("utf-8")
        except HTTPError as err:
            payload = err.read().decode("utf-8")
            if json_rpc_error_as_response:
                try:
                    decoded = json.loads(payload)
                    if isinstance(decoded, dict) and decoded.get("error"):
                        return decoded
                except json.JSONDecodeError:
                    pass
            raise LiteAIGError(f"liteaig: {err.code} {err.reason}: {payload.strip()}") from err
        decoded = json.loads(payload)
        if not isinstance(decoded, dict):
            raise LiteAIGError("liteaig: expected JSON object response")
        return decoded

    def _open(self, method: str, path: str, body: Optional[Mapping[str, Any]] = None):
        data = None
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "LiteAIG-API-Version": self.api_version,
        }
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        request = Request(f"{self.base_url}{path}", data=data, headers=headers, method=method)
        return urlopen(request, timeout=self.timeout)


def _iter_sse(response: Any, parser: Any) -> Iterator[Any]:
    for raw_line in response:
        line = raw_line.decode("utf-8").strip()
        if not line.startswith("data:"):
            continue
        data = line[5:].strip()
        if not data:
            continue
        if data == "[DONE]":
            return
        event = parser(json.loads(data))
        if event is None:
            return
        if event is not False:
            yield event


def _parse_chat_stream_frame(frame: Dict[str, Any]) -> Dict[str, Any]:
    return frame


def _parse_a2a_stream_frame(frame: Dict[str, Any]) -> Optional[A2AStreamEvent | bool]:
    error = frame.get("error")
    if error:
        message = error.get("message") or "A2A stream error"
        raise LiteAIGError(f"a2a stream error: {message}")
    if frame.get("type") == "completed":
        return None
    if frame.get("type") != "message":
        return False
    parts = ((frame.get("message") or {}).get("parts") or [])
    text = "\n".join(str(part.get("text") or "") for part in parts if part.get("kind") in (None, "", "text"))
    if not text:
        return False
    return A2AStreamEvent(delta=text)
