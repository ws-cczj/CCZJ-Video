"""Native WebView2 upgrade/rename continuity check; requires Go and websocket-client.

Runs only isolated helper processes and profiles under dist/audit. No production
database or browser profile is opened. The first two launches use Wails' legacy
default; the remaining launches use the application's storage resolver.
"""
import argparse
import http.server
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import threading
import time
from urllib.request import urlopen

import websocket


class Page(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"<html><body>Storage continuity check</body></html>")

    def log_message(self, *args):
        pass


def check(root):
    exe = root / "cczjVideo-2.4.0.exe"
    renamed = root / "arbitrary-renamed.exe"
    subprocess.run(["go", "build", "-tags", "production", "-ldflags", "-H windowsgui",
                    "-o", str(exe), "./scripts/fixtures/native-playback-probe"], check=True)
    shutil.copy2(exe, renamed)
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Page)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{server.server_port}/"
    env = dict(os.environ)
    env["APPDATA"] = str(root / "profile")
    Path(env["APPDATA"]).mkdir()
    for key in ("WEBVIEW2_USER_DATA_FOLDER", "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"):
        env.pop(key, None)
    value = json.dumps({"version": 1, "value": {"123-2": {
        "position": 300, "duration": 900, "updatedAt": int(time.time() * 1000)}}})

    def run(binary, label, write=False, profile=""):
        with socket.socket() as free:
            free.bind(("127.0.0.1", 0))
            port = free.getsockname()[1]
        log = root / (label + ".jsonl")
        startup = subprocess.STARTUPINFO()
        startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        startup.wShowWindow = 0
        with (root / (label + ".stderr")).open("w") as stderr:
            process = subprocess.Popen([str(binary), url, profile, str(port), str(log)],
                                       env=env, startupinfo=startup, stdout=stderr, stderr=stderr)
            connection = None
            try:
                deadline = time.monotonic() + 30
                while True:
                    try:
                        with urlopen(f"http://127.0.0.1:{port}/json/list", timeout=1) as response:
                            targets = json.load(response)
                        target = next(t for t in targets if t.get("url") == url)
                        connection = websocket.create_connection(target["webSocketDebuggerUrl"],
                                                                 timeout=10, suppress_origin=True)
                        break
                    except (OSError, StopIteration):
                        if process.poll() is not None or time.monotonic() >= deadline:
                            raise
                        time.sleep(.1)
                expression = """(async () => {
                  const database = await new Promise((resolve, reject) => {
                    const request = indexedDB.open('cczj-storage-continuity', 1);
                    request.onupgradeneeded = () => request.result.createObjectStore('segments');
                    request.onsuccess = () => resolve(request.result);
                    request.onerror = () => reject(request.error);
                  });
                  try {
                    WRITE
                    const segment = await new Promise((resolve, reject) => {
                      const request = database.transaction('segments').objectStore('segments').get('segment');
                      request.onsuccess = () => resolve(request.result ?? null);
                      request.onerror = () => reject(request.error);
                    });
                    return {progress: localStorage.getItem('cczj_ep_prog_v1'),
                      dismissed: localStorage.getItem('cczj.legacy-data-dismissed'), segment};
                  } finally { database.close(); }
                })()"""
                writes = ""
                if write:
                    writes = """
                    localStorage.setItem('cczj_ep_prog_v1', VALUE);
                    localStorage.setItem('cczj.legacy-data-dismissed', 'true');
                    await new Promise((resolve, reject) => {
                      const tx = database.transaction('segments', 'readwrite');
                      tx.objectStore('segments').put('cached-segment', 'segment');
                      tx.oncomplete = resolve; tx.onerror = () => reject(tx.error);
                    });""".replace("VALUE", json.dumps(value))
                expression = expression.replace("WRITE", writes)
                expression = f"location.href === {json.dumps(url)} && document.readyState === 'complete' ? ({expression}) : undefined"
                # A target may be discoverable before its initial navigation commits.
                for attempt in range(50):
                    connection.send(json.dumps({"id": attempt + 1, "method": "Runtime.evaluate",
                                                "params": {"expression": expression, "awaitPromise": True,
                                                           "returnByValue": True}}))
                    while True:
                        response = json.loads(connection.recv())
                        if response.get("id") == attempt + 1:
                            break
                    result = response.get("result", {})
                    if "exceptionDetails" in result or "error" in response:
                        raise RuntimeError(response)
                    if result.get("result", {}).get("type") == "object":
                        return result["result"].get("value")
                    time.sleep(.1)
                raise RuntimeError("WebView did not provide a storage result")
            finally:
                if connection:
                    connection.close()
                Path(str(log) + ".stop").touch()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.terminate()
                    process.wait(timeout=10)

    try:
        result = {"legacy_original": run(exe, "legacy-original", write=True),
                  "legacy_renamed": run(renamed, "legacy-renamed")}
        result["fixed_renamed"] = run(renamed, "fixed-renamed", profile="@cczj")
        result["fixed_original"] = run(exe, "fixed-original", profile="@cczj")
        (root / "result.json").write_text(json.dumps(result, indent=2), encoding="utf-8")
        print(json.dumps(result, ensure_ascii=False))
        expected = {"progress": value, "dismissed": "true", "segment": "cached-segment"}
        assert result["legacy_original"] == expected, "seed did not persist"
        assert result["legacy_renamed"]["progress"] is None, "legacy reproduction no longer applies"
        assert result["fixed_renamed"] == expected, "renamed release failed to reuse existing storage"
        assert result["fixed_original"] == expected, "subsequent rename changed the profile"
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", default="dist/audit/windows-storage", help="Parent evidence directory")
    args = parser.parse_args()
    if os.name != "nt":
        parser.error("This check requires Windows and WebView2")
    parent = Path(args.output).resolve()
    parent.mkdir(parents=True, exist_ok=True)
    evidence = Path(tempfile.mkdtemp(prefix="run-", dir=parent))
    print(f"Evidence: {evidence}")
    check(evidence)
