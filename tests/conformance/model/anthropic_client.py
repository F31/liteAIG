#!/usr/bin/env python3
"""Official Anthropic SDK interop driver for LiteAIG."""

import argparse
import json
import sys


ap = argparse.ArgumentParser()
ap.add_argument("--base-url", required=True)
ap.add_argument("--token", required=True)
ap.add_argument("--model", default="default-chat")
args = ap.parse_args()


def fail(message):
    print(json.dumps({"sent": False, "error": message}))
    sys.exit(1)


def main():
    try:
        from anthropic import Anthropic

        client = Anthropic(api_key=args.token, base_url=args.base_url)
        message = client.messages.create(
            model=args.model,
            max_tokens=16,
            messages=[{"role": "user", "content": "hello official anthropic sdk"}],
        )
        text = "".join(
            block.text for block in message.content if getattr(block, "type", "") == "text"
        )
        print(json.dumps({"sent": True, "id": message.id, "message": text}))
    except Exception as exc:  # noqa: BLE001 -- conformance reports SDK failures
        fail("anthropic sdk failed: %r" % (exc,))


if __name__ == "__main__":
    main()
