#!/usr/bin/env python3
"""Official OpenAI SDK interop driver for LiteAIG.

The Go harness boots a live Lite gateway and passes --base-url <gateway>/v1.
This script uses the official SDK only, changing base_url and api_key, then
prints a compact JSON report for the harness to assert.
"""

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
        from openai import OpenAI

        client = OpenAI(api_key=args.token, base_url=args.base_url)
        models = [item.id for item in client.models.list().data]
        completion = client.chat.completions.create(
            model=args.model,
            messages=[{"role": "user", "content": "hello official openai sdk"}],
        )
        text = completion.choices[0].message.content or ""
        stream_text = ""
        for chunk in client.chat.completions.create(
            model=args.model,
            messages=[{"role": "user", "content": "hello official openai stream"}],
            stream=True,
        ):
            if chunk.choices and chunk.choices[0].delta.content:
                stream_text += chunk.choices[0].delta.content
        print(
            json.dumps(
                {
                    "sent": True,
                    "models": models,
                    "message": text,
                    "stream": stream_text,
                }
            )
        )
    except Exception as exc:  # noqa: BLE001 -- conformance reports SDK failures
        fail("openai sdk failed: %r" % (exc,))


if __name__ == "__main__":
    main()
