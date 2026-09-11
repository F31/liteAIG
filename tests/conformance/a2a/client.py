#!/usr/bin/env python3
"""Official A2A SDK interop driver.

Sends one A2A message to a live Lite gateway /a2a endpoint using the OFFICIAL
A2A SDK client, then prints a small JSON report the Go harness asserts on:

    {"sent": true, "result": <payload parsed back through the SDK's types>}
    {"sent": true, "mode": "stream", "result": [<SDK-parsed stream events>]}
    {"sent": false, "error": "..."}

The primary path targets the current official Python SDK (`a2a-sdk` >= 1.0,
A2A 1.0 wire): it builds an AgentCard pointing at the gateway, drives
ClientFactory/Client.send_message, and serializes the parsed response with
google.protobuf json_format. With --stream it enables the SDK streaming path,
which sends SendStreamingMessage and parses JSON-RPC-over-SSE responses through
the SDK's StreamResponse protobuf. A fallback path supports the legacy 0.3 SDK
API (A2AClient) for blocking sends only. A wiring or SDK-import failure exits
non-zero so the conformance job fails loudly instead of silently skipping.
"""

import argparse
import asyncio
import json
import os
import sys

ap = argparse.ArgumentParser()
ap.add_argument("--url", required=True)
ap.add_argument("--text", default="hello interop")
ap.add_argument("--token", default="")
ap.add_argument("--stream", action="store_true")
args = ap.parse_args()

sdk_src = os.environ.get("LITEAIG_A2A_SDK_PYTHON", "")
if sdk_src:
    sys.path.insert(0, sdk_src)


def report_ok(result):
    mode = "stream" if args.stream else "blocking"
    print(json.dumps({"sent": True, "mode": mode, "result": result}))


def report_fail(message):
    print(json.dumps({"sent": False, "error": message}))
    sys.exit(1)


def http_client():
    import httpx

    kwargs = {}
    if args.token:
        kwargs["headers"] = {"Authorization": "Bearer " + args.token}
    return httpx.AsyncClient(**kwargs)


def send_v1(url, text, stream=False):
    """Latest official SDK path (A2A 1.0 wire, protobuf types)."""

    from a2a.client.client_factory import ClientFactory, ClientConfig
    from a2a.types import a2a_pb2

    card = a2a_pb2.AgentCard(
        name="liteaig",
        version="1",
        description="LiteAIG governed A2A gateway",
        capabilities=a2a_pb2.AgentCapabilities(
            streaming=stream, push_notifications=False
        ),
        default_input_modes=["text"],
        default_output_modes=["text"],
        skills=[a2a_pb2.AgentSkill(id="gateway", name="Gateway")],
        supported_interfaces=[
            a2a_pb2.AgentInterface(url=args.url, protocol_binding="JSONRPC")
        ],
    )

    async def run():
        async with http_client() as http:
            client = ClientFactory(
                ClientConfig(
                    streaming=stream,
                    polling=False,
                    supported_protocol_bindings=["JSONRPC"],
                    httpx_client=http,
                )
            ).create(card)
            message_id = "interop-stream-1" if stream else "interop-1"
            message = a2a_pb2.Message(
                message_id=message_id,
                role=a2a_pb2.Role.ROLE_USER,
                parts=[a2a_pb2.Part(text=text)],
            )
            request = a2a_pb2.SendMessageRequest(message=message)
            response = None
            events = []
            async for event in client.send_message(request):
                if stream:
                    events.append(serialize_stream_response(event))
                    continue
                response = event
                break
            if stream:
                if not events:
                    raise RuntimeError("SDK returned no stream events")
                return events
            if response is None:
                raise RuntimeError("SDK returned no response")
            return serialize_stream_response(response)

    return asyncio.run(run())


def serialize_stream_response(response):
    from google.protobuf import json_format

    for field in ("message", "task", "status_update", "artifact_update"):
        if response.HasField(field):
            return {
                field: json_format.MessageToDict(
                    getattr(response, field), preserving_proto_field_name=True
                )
            }
    raise RuntimeError("SDK response has no StreamResponse payload")


def send_v03(url, text):
    """Legacy 0.3 SDK fallback path (A2AClient)."""

    import httpx

    from a2a.client import A2AClient
    from a2a.types import Message, Role, TextPart

    role = getattr(Role, "user", None) or getattr(Role, "USER", None)

    async def run():
        async with http_client() as http:
            client = A2AClient(httpx_client=http, url=url)
            message = Message(
                messageId="interop-1", role=role, parts=[TextPart(text=text)]
            )
            try:
                from a2a.types import MessageSendParams, SendMessageRequest

                request = SendMessageRequest(
                    id="1", params=MessageSendParams(message=message)
                )
            except Exception:  # noqa: BLE001 -- older 0.3 shapes
                request = message
            send = getattr(client, "send", None) or getattr(
                client, "send_message", None
            )
            if send is None:
                raise AttributeError(
                    "SDK client exposes neither send nor send_message"
                )
            response = await send(request)
            return json.loads(response.model_dump_json())

    return asyncio.run(run())


def main():
    try:
        result = send_v1(args.url, args.text, stream=args.stream)
    except Exception as v1_exc:  # noqa: BLE001
        if args.stream:
            report_fail("stream send failed: %r" % (v1_exc,))
            return
        try:
            result = send_v03(args.url, args.text)
        except Exception:  # noqa: BLE001 -- report the v1 error, it is primary
            report_fail("send failed: %r" % (v1_exc,))
            return
    report_ok(result)


if __name__ == "__main__":
    main()
