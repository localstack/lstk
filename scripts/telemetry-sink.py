#!/usr/bin/env python3
"""Local stand-in for the analytics backend: prints every lstk telemetry event
it receives, with the nested `payload` decoded.

    make telemetry-sink                       # listens on 127.0.0.1:8089
    LSTK_ANALYTICS_ENDPOINT=http://127.0.0.1:8089 lstk aws s3 ls

Events arrive a moment after lstk exits: the flush runs in a detached
subprocess. PORT overrides the port. LOCALSTACK_DISABLE_EVENTS=1 in the
calling shell turns lstk's telemetry off entirely; clear it for the run.
"""
import http.server
import json
import os


class Sink(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length") or 0))
        try:
            body = json.loads(raw)
        except ValueError:
            print(f"ignoring non-JSON body ({len(raw)} bytes)", flush=True)
            self.send_response(400)
            self.end_headers()
            return
        for event in body.get("events", [body]):
            if isinstance(event.get("payload"), str):
                event["payload"] = json.loads(event["payload"])
            print(json.dumps(event, indent=2), flush=True)
        self.send_response(200)
        self.end_headers()

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8089"))
    print(f"telemetry sink listening on http://127.0.0.1:{port}", flush=True)
    print(f"  LSTK_ANALYTICS_ENDPOINT=http://127.0.0.1:{port} lstk <command>", flush=True)
    if os.environ.get("LOCALSTACK_DISABLE_EVENTS") == "1":
        print("  note: LOCALSTACK_DISABLE_EVENTS=1 is set in this shell; lstk sends nothing while it is", flush=True)
    http.server.HTTPServer(("127.0.0.1", port), Sink).serve_forever()
