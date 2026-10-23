"""Exercise production segment caching against real IndexedDB in a fresh browser."""
import argparse
from contextlib import contextmanager
import json
import os
from pathlib import Path
import subprocess
import time
from urllib.request import urlopen

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent


@contextmanager
def local_server(args):
    if args.url:
        yield args.url
        return
    url = f'http://127.0.0.1:{args.port}'
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    server = subprocess.Popen(
        ['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'],
        cwd=ROOT / 'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags,
    )
    try:
        for _ in range(100):
            if server.poll() is not None:
                raise RuntimeError('isolated Vite server exited before becoming ready')
            try:
                with urlopen(url + '/tests/browser/segment-disk.html', timeout=1):
                    break
            except OSError:
                time.sleep(.2)
        else:
            raise RuntimeError('isolated Vite server did not become ready')
        yield url
    finally:
        server.terminate()
        try:
            server.wait(timeout=10)
        except subprocess.TimeoutExpired:
            server.kill()
            server.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', default=None)
    parser.add_argument('--port', type=int, default=9276)
    parser.add_argument('--channel', default=None)
    args = parser.parse_args()
    with local_server(args) as url, sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True, channel=args.channel)
        try:
            page = browser.new_page()
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            network = []
            def fetch_segment(route):
                network.append(route.request.url)
                route.fulfill(status=200, content_type='video/mp2t', body=bytes([7, 8, 9]))
            page.route('https://segments.invalid/**', fetch_segment)
            page.goto(url + '/tests/browser/segment-disk.html')
            page.wait_for_load_state('networkidle')
            page.wait_for_function('!!window.segmentDiskTest')
            page.evaluate('window.segmentDiskTest.seed()')
            page.evaluate('''() => {
              window.__segmentReads = [];
              const get = IDBObjectStore.prototype.get;
              IDBObjectStore.prototype.get = function(key) {
                if (this.name === 'segments') window.__segmentReads.push(String(key));
                return get.call(this, key);
              };
              window.segmentDiskTest.open();
            }''')
            # A real IndexedDB read barrier: finish any restore initiated by open().
            page.evaluate('''async () => {
              const db = await new Promise((resolve, reject) => {
                const request = indexedDB.open('tscache', 2);
                request.onsuccess = () => resolve(request.result);
                request.onerror = () => reject(request.error);
              });
              await new Promise(resolve => {
                const tx = db.transaction(['segment_meta', 'segments'], 'readwrite');
                tx.oncomplete = resolve;
              });
              db.close();
            }''')
            # Allow any dependent transaction from the former bulk restore to finish.
            page.wait_for_timeout(300)
            opened = page.evaluate('window.segmentDiskTest.stats().bytes')
            reads = page.evaluate('window.__segmentReads.length')
            print(json.dumps({'opened_cache_mib': opened / 1024**2, 'binary_reads_on_open': reads}), flush=True)
            assert opened == 0 and reads == 0, 'opening an episode restored unrequested segment binaries'
            segment = page.evaluate('window.segmentDiskTest.load(17)')
            assert segment == {'bytes': 2 * 1024**2, 'first': 18, 'last': 18}, segment
            assert page.evaluate('window.segmentDiskTest.stats().bytes') == 2 * 1024**2
            assert page.evaluate('window.__segmentReads') == ['https://segments.invalid/17.ts']
            # The second request uses the memory cache; other disk data stays on disk.
            assert page.evaluate('window.segmentDiskTest.load(17)') == segment
            assert page.evaluate('window.__segmentReads.length') == 1
            assert not network, network
            page.evaluate('window.segmentDiskTest.prefetch(19)')
            page.wait_for_function('window.segmentDiskTest.stats().bytes === 4 * 1024**2')
            assert page.evaluate('window.__segmentReads') == ['https://segments.invalid/17.ts', 'https://segments.invalid/19.ts']
            assert not network, network
            # Expired metadata must fall back without cloning its binary first.
            page.evaluate('window.segmentDiskTest.expire(18)')
            page.evaluate('window.segmentDiskTest.clearMemory()')
            expired = page.evaluate('window.segmentDiskTest.load(18)')
            assert expired == {'bytes': 3, 'first': 7, 'last': 9}, expired
            assert network == ['https://segments.invalid/18.ts'], network
            assert page.evaluate('window.__segmentReads.length') == 2
            assert not errors, errors
            print('PASS: only the requested 2 MiB fragment was restored from an 80 MiB disk episode')
        finally:
            browser.close()


if __name__ == '__main__':
    main()
