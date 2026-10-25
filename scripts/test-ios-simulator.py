"""Check native startup and SQLite persistence in a disposable iOS simulator.

This is a storage/startup smoke test, not playback or real-device acceptance.
Only the simulator created by this process is modified or removed.
"""
import argparse
import json
from pathlib import Path
import plistlib
import shutil
import sqlite3
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]


def sim(*args, check=True):
    result = subprocess.run(["xcrun", "simctl", *map(str, args)], check=False,
                            capture_output=True, text=True, timeout=180)
    if result.stderr:
        print(result.stderr, file=sys.stderr)
    if check and result.returncode:
        if result.stdout:
            print(result.stdout, file=sys.stderr)
        result.check_returncode()
    return result.stdout.strip()


def ready(data, previous):
    deadline = time.monotonic() + 75
    while time.monotonic() < deadline:
        logs = "\n".join(p.read_text(errors="replace") for p in data.glob("Library/Application Support/CCZJ Video/applog/*.log"))
        if logs.count("application ready") > previous:
            database = data / "Library/Application Support/CCZJ Video/cczj_video.db"
            with sqlite3.connect(f"{database.as_uri()}?mode=ro", uri=True) as connection:
                assert connection.execute("PRAGMA quick_check").fetchone()[0] == "ok"
                tables = {r[0] for r in connection.execute("SELECT name FROM sqlite_master WHERE type='table'")}
                assert {"settings", "sources"} <= tables, tables
            return database, logs
        time.sleep(.5)
    raise RuntimeError("iOS service startup did not finish; inspect the saved logs")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, default=ROOT / "bin/ios/simulator/CCZJVideo.app")
    args = parser.parse_args()
    if sys.platform != "darwin":
        parser.error("This test needs macOS, full Xcode and an installed iOS simulator runtime")
    with (args.app / "Info.plist").open("rb") as source:
        bundle_id = plistlib.load(source)["CFBundleIdentifier"]
    runtimes = json.loads(sim("list", "runtimes", "-j"))["runtimes"]
    devices = json.loads(sim("list", "devices", "available", "-j"))["devices"]
    # Reuse only the *type* of an available simulator, so a newer Xcode's device
    # catalogue cannot accidentally select a phone unsupported by the runtime.
    runtime = next((r for r in reversed(runtimes) if r.get("isAvailable")
        and r["identifier"].startswith("com.apple.CoreSimulator.SimRuntime.iOS-")
        and any(d["name"].startswith("iPhone") for d in devices.get(r["identifier"], []))), None)
    if runtime is None:
        raise RuntimeError("No installed iOS simulator runtime")
    types = json.loads(sim("list", "devicetypes", "-j"))["devicetypes"]
    template = next(d for d in devices[runtime["identifier"]] if d["name"].startswith("iPhone"))
    device_type = next(t for t in types if t["identifier"] == template.get("deviceTypeIdentifier") or t["name"] == template["name"])
    identifier = sim("create", "CCZJ acceptance " + uuid.uuid4().hex[:8], device_type["identifier"], runtime["identifier"])
    audit = ROOT / "dist/audit/ios-simulator"
    audit.mkdir(parents=True, exist_ok=True)
    data = None
    result = {"environment": "iOS simulator", "scope": "service startup and local SQLite persistence", "passed": False, "modes": []}
    try:
        sim("boot", identifier)
        sim("bootstatus", identifier, "-b")
        sim("install", identifier, args.app.resolve())
        data = Path(sim("get_app_container", identifier, bundle_id, "data"))
        sim("launch", identifier, bundle_id)
        database, logs = ready(data, 0)
        for mode in ("video", "novel", "comic"):
            sim("terminate", identifier, bundle_id)
            with sqlite3.connect(database) as connection:
                connection.execute("INSERT OR REPLACE INTO settings(key,value) VALUES('application_mode',?)", (mode,))
                connection.execute("INSERT OR REPLACE INTO settings(key,value) VALUES('ios_acceptance_marker','retained')")
            previous = logs.count("application ready")
            sim("launch", identifier, bundle_id)
            database, logs = ready(data, previous)
            with sqlite3.connect(f"{database.as_uri()}?mode=ro", uri=True) as connection:
                assert connection.execute("SELECT value FROM settings WHERE key='ios_acceptance_marker'").fetchone() == ("retained",)
                assert connection.execute("SELECT value FROM settings WHERE key='application_mode'").fetchone() == (mode,)
            # Give the WebView a render opportunity. Images are evidence for
            # manual review; their existence is not counted as a UI assertion.
            time.sleep(3)
            sim("io", identifier, "screenshot", audit / f"{mode}.png")
            result["modes"].append(mode)
        result["passed"] = True
    finally:
        if data:
            for path in data.glob("Library/Application Support/CCZJ Video/applog/*.log"):
                shutil.copy2(path, audit / path.name)
        (audit / "result.json").write_text(json.dumps(result, indent=2), encoding="utf-8")
        sim("shutdown", identifier, check=False)
        sim("delete", identifier, check=False)


if __name__ == "__main__":
    main()
