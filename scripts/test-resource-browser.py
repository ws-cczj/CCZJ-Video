"""Run diagnostics, update and retained-memory checks with one isolated Vite server."""
import argparse
import os
from pathlib import Path
import subprocess
import sys
import time
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--channel', default=None)
    parser.add_argument('--port', type=int, default=9248)
    parser.add_argument('--diagnostics-only', action='store_true')
    args = parser.parse_args()
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    server = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags)
    url = f'http://127.0.0.1:{args.port}'
    try:
        for _ in range(100):
            if server.poll() is not None:
                raise RuntimeError('isolated browser test server exited')
            try:
                with urlopen(url+'/tests/browser/diagnostics.html', timeout=1):
                    break
            except OSError:
                time.sleep(.2)
        else:
            raise RuntimeError('browser test server did not become ready')
        scripts = ('test-diagnostics-browser.py',) if args.diagnostics_only else ('test-diagnostics-browser.py', 'test-update-browser.py', 'test-memory-browser.py')
        for script in scripts:
            command = [sys.executable, str(ROOT/'scripts'/script), '--url', url]
            if args.channel:
                command += ['--channel', args.channel]
            result = subprocess.run(command, check=False, creationflags=flags, capture_output=True, text=True, encoding='utf-8', errors='replace')
            print(result.stdout, end='')
            if result.stderr:
                print(result.stderr, end='', file=sys.stderr)
            result.check_returncode()
    finally:
        server.terminate()
        try:
            server.wait(timeout=10)
        except subprocess.TimeoutExpired:
            server.kill()
            server.wait()


if __name__ == '__main__':
    main()
