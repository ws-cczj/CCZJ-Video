#!/usr/bin/env python3
"""Download reading-source metadata only; never install or evaluate source code.

The manifest is a list of catalog inputs for the Go quality runner. Entries are
configuration variants, not promises of independent sites or usable adapters.
"""
import argparse
import collections
import datetime as dt
import hashlib
import json
from pathlib import Path
import time
import urllib.error
import urllib.parse
import urllib.request

MAX_BYTES = 32 * 1024 * 1024
CATALOGS = [
    ("aoaostar-all", "legado", "https://raw.githubusercontent.com/aoaostar/legado/master/sources/b778fe6b.json"),
    ("aoaostar-release", "legado", "https://raw.githubusercontent.com/aoaostar/legado/release/sources/b778fe6b.json"),
    ("xiu2", "legado", "https://raw.githubusercontent.com/XIU2/Yuedu/master/shuyuan"),
    ("keiyoushi", "mihon", "https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.json"),
    ("yuzono", "mihon", "https://raw.githubusercontent.com/yuzono/manga-repo/repo/index.json"),
    ("suwayomi", "mihon", "https://raw.githubusercontent.com/suwayomi/tachiyomi-extension/repo/index.min.json"),
]


def utcnow():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def host_of(address):
    try:
        parsed = urllib.parse.urlsplit(str(address))
        if parsed.scheme not in ("http", "https") or not parsed.hostname:
            return ""
        return parsed.hostname.rstrip(".").lower().removeprefix("www.")
    except ValueError:
        return ""


def flatten(payload, declared_format):
    if isinstance(payload, dict) and isinstance(payload.get("extensionList"), dict):
        payload = payload["extensionList"].get("extensions", [])
    elif isinstance(payload, dict) and "sources" in payload and "pkg" not in payload:
        payload = payload["sources"]
    if not isinstance(payload, list):
        payload = [payload]
    out = []
    for item in payload:
        if not isinstance(item, dict):
            continue
        if "bookSourceUrl" in item:
            stype = item.get("bookSourceType", 0)
            if stype not in (0, 2):
                continue
            out.append({"kind": "comic" if stype == 2 else "novel", "name": item.get("bookSourceName", ""), "url": item.get("bookSourceUrl", ""), "format": "legado", "raw": item})
        elif "sources" in item and ("pkg" in item or "packageName" in item):
            for source in item.get("sources", []):
                if not isinstance(source, dict):
                    continue
                warning = item.get("contentWarning")
                adult = True if warning == "CONTENT_WARNING_NSFW" else False if warning == "CONTENT_WARNING_SAFE" else item.get("nsfw", item.get("isNsfw"))
                out.append({"kind": "comic", "name": source.get("name", ""), "url": source.get("baseUrl") or source.get("homeUrl", ""), "format": "mihon", "language": source.get("lang") or source.get("language", ""), "package": item.get("pkg") or item.get("packageName", ""), "adult": adult, "contentWarning": warning, "raw": source})
        elif item.get("apiUrl"):
            out.append({"kind": item.get("kind", "novel"), "name": item.get("name", ""), "url": item["apiUrl"], "format": item.get("format", declared_format), "raw": item})
    return payload, out


def rule_barriers(row):
    """Conservative static triage, NOT the application's converter verdict."""
    if row["format"] == "mihon":
        return ["kotlin_apk_adapter_required"]
    raw = row["raw"]
    text = json.dumps(raw, ensure_ascii=False)
    issues = []
    if any(marker in text for marker in ("<js>", "@js:", "java.", "<javascript>")):
        issues.append("javascript")
    if any(marker in text for marker in ("@xpath:", "//div", "//a[", "//ul")):
        issues.append("xpath")
    if any(marker in text for marker in ("@json:", "$.")):
        issues.append("jsonpath")
    if any(marker in text for marker in ("@put:", "@get:")):
        issues.append("stateful_rule")
    search = raw.get("searchUrl", "")
    if not isinstance(search, str) or not search:
        issues.append("missing_search")
    elif "," in search:
        issues.append("url_options_or_post")
    if any(not isinstance(raw.get(key), dict) for key in ("ruleSearch", "ruleBookInfo", "ruleToc", "ruleContent")):
        issues.append("missing_operation")
    if raw.get("loginCheckJs") or raw.get("loginUrl"):
        issues.append("login_flow")
    content = raw.get("ruleContent") if isinstance(raw.get("ruleContent"), dict) else {}
    if any(content.get(key) for key in ("webJs", "webView", "imageDecode", "fontDecode", "contentJs", "nextContentJs")):
        issues.append("dynamic_content_or_decode")
    if not host_of(row["url"]):
        issues.append("invalid_base_url")
    return sorted(set(issues))


def collect(output, selected):
    output.mkdir(parents=True, exist_ok=True)
    manifest = {"version": 1, "collectedAt": utcnow(), "limits": {"maxBytes": MAX_BYTES, "timeoutSeconds": 30, "concurrency": 1}, "catalogs": [], "attempts": [], "semantics": {"entries": "configuration variants after expansion; not independent usable sites", "hosts": "lowercase hostname without leading www; no public-suffix/ownership inference", "staticTriage": "conservative marker screening; actual Go conversion and network checks remain required"}}
    unique_rows = {}
    seen_documents = {}
    for name, fmt, url in CATALOGS:
        if selected and name not in selected:
            continue
        started = time.monotonic()
        attempt = {"name": name, "url": url, "fetchedAt": utcnow(), "format": fmt}
        try:
            request = urllib.request.Request(url, headers={"User-Agent": "CCZJ-SourceCatalogAudit/1.0", "Accept": "application/json", "Accept-Encoding": "identity"})
            with urllib.request.urlopen(request, timeout=30) as response:
                attempt.update({"httpStatus": response.status, "finalUrl": response.url, "contentType": response.headers.get("Content-Type", ""), "etag": response.headers.get("ETag", ""), "lastModified": response.headers.get("Last-Modified", "")})
                declared_length = response.headers.get("Content-Length")
                if declared_length and int(declared_length) > MAX_BYTES:
                    raise ValueError("declared body exceeds 32 MiB")
                raw = response.read(MAX_BYTES + 1)
                if len(raw) > MAX_BYTES:
                    raise ValueError("response exceeds 32 MiB")
            payload = json.loads(raw.decode("utf-8-sig"))
            top_rows, rows = flatten(payload, fmt)
            if not rows:
                raise ValueError("JSON has no recognized reading sources")
            digest = hashlib.sha256(raw).hexdigest()
            path = output / (name + ".json")
            path.write_bytes(raw)
            attempt.update({"status": "ok", "sha256": digest, "bytes": len(raw), "topLevelEntries": len(top_rows), "expandedEntries": len(rows), "path": path.as_posix()})
            # Keep mirror and deprecation evidence, but do not schedule it as
            # another upstream site. A user-configured server has no host yet.
            excluded = collections.Counter()
            site_rows = []
            for row in rows:
                if "migrate to" in row["name"].lower():
                    excluded["migration_notice"] += 1
                elif row.get("package") == "eu.kanade.tachiyomi.extension.all.tachidesk" and not row["url"]:
                    excluded["user_configured_server"] += 1
                else:
                    site_rows.append(row)
            attempt["excludedNonSiteEntries"] = dict(excluded)
            attempt["siteEntries"] = len(site_rows)
            if digest in seen_documents:
                attempt["duplicateOf"] = seen_documents[digest]
                site_rows = []
            else:
                seen_documents[digest] = name
            by_kind = collections.defaultdict(list)
            for row in site_rows:
                by_kind[row["kind"]].append(row)
                fingerprint = hashlib.sha256(json.dumps(row["raw"], ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
                unique_rows.setdefault((row["kind"], row["url"], fingerprint), row)
            for kind, kind_rows in sorted(by_kind.items()):
                barriers = collections.Counter()
                eligible = 0
                for row in kind_rows:
                    issues = rule_barriers(row)
                    barriers.update(issues)
                    eligible += not issues
                manifest["catalogs"].append({"name": name, "kind": kind, "path": path.as_posix(), "url": url, "sha256": digest, "entries": len(kind_rows), "uniqueHosts": len({host_of(row["url"]) for row in kind_rows} - {""}), "format": fmt, "staticTriageEligible": eligible, "barriers": dict(barriers.most_common()), "languages": dict(collections.Counter(row.get("language", "unspecified") for row in kind_rows)), "adultMetadata": dict(collections.Counter(str(row.get("adult", "unspecified")) for row in kind_rows))})
        except (OSError, ValueError, TypeError) as error:
            attempt.update({"status": "failed", "error": str(error)[:500]})
            if isinstance(error, urllib.error.HTTPError):
                attempt["httpStatus"] = error.code
        attempt["elapsedMillis"] = round((time.monotonic() - started) * 1000)
        manifest["attempts"].append(attempt)
        print(json.dumps(attempt, ensure_ascii=False), flush=True)
    index = []
    for (kind, url, fingerprint), row in unique_rows.items():
        index.append({"kind": kind, "name": row["name"], "url": url, "host": host_of(url), "format": row["format"], "ruleFingerprint": fingerprint, "barriers": rule_barriers(row), "language": row.get("language", ""), "adult": row.get("adult")})
    write_json(output / "candidate-metadata.json", index)
    manifest["distinctVariants"] = len(index)
    manifest["distinctHosts"] = len({item["host"] for item in index} - {""})
    write_json(output / "manifest.json", manifest)
    return 0 if manifest["catalogs"] else 1


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path("reports/source-quality/catalog-inputs"))
    parser.add_argument("--only", action="append", choices=[row[0] for row in CATALOGS], help="Limit upstream collections, repeat as needed")
    args = parser.parse_args()
    raise SystemExit(collect(args.output, set(args.only or [])))
