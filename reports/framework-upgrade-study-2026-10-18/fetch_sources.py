"""Fetch read-only, tagged research sources from official repositories."""
import concurrent.futures
import hashlib
import json
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parent
REPOS = {
    'go-gitea/gitea': ('v1.24.6', ['modules/queue/queue.go', 'modules/queue/base.go', 'modules/queue/queue_workerpool.go', 'modules/queue/queue_workerpool_test.go', 'modules/setting/queue.go']),
    'navidrome/navidrome': ('v0.61.2', ['scheduler/scheduler.go', 'plugins/README.md', 'scanner/scanner.go']),
    'syncthing/syncthing': ('v1.30.0', ['lib/svcutil/svcutil.go', 'lib/events/events.go', 'lib/connections/service.go']),
    'pocketbase/pocketbase': ('v0.28.4', ['core/db_connect.go', 'core/db.go', 'tools/hook/hook.go', 'core/base.go']),
}

def get(url):
    req = urllib.request.Request(url, headers={'User-Agent': 'CCZJ-architecture-research', 'Accept': 'application/vnd.github+json'})
    with urllib.request.urlopen(req, timeout=35) as response:
        return response.read()

def inspect(entry):
    repo, (sha, selected) = entry
    result = {'repository': repo, 'ref': sha, 'files': [], 'errors': []}
    for p in selected:
        url = 'https://raw.githubusercontent.com/' + repo + '/' + sha + '/' + p
        try:
            raw = get(url)
        except Exception as error:
            result['errors'].append({'path': p, 'error': str(error)})
            continue
        dest = ROOT / 'sources' / repo.replace('/', '__') / (p + '.txt')
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(raw)
        result['files'].append({'path': p, 'url': 'https://github.com/' + repo + '/blob/' + sha + '/' + p, 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)})
    return result

if __name__ == '__main__':
    results = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        pending = {pool.submit(inspect, item): item[0] for item in REPOS.items()}
        for future in concurrent.futures.as_completed(pending):
            try:
                value = future.result()
                results.append(value)
                print(value['repository'], value['ref'], len(value['files']), 'files', len(value['errors']), 'unavailable', flush=True)
            except Exception as error:
                results.append({'repository': pending[future], 'error': str(error)})
                print(pending[future], 'ERROR', str(error), flush=True)
    (ROOT / 'source-manifest.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
