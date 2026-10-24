"""Rebuild the offline architecture canvas from current source and reviewed paths.

The inventory records structure; its existence does not establish runtime QA.
Use --validate to run the canvas interaction checks with Python Playwright.
"""
import argparse
from pathlib import Path
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--validate', action='store_true')
    parser.add_argument('--channel', default='chrome')
    options = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    scripts = root / 'scripts/architecture'
    for name in ('inventory.py', 'render.py'):
        subprocess.run([sys.executable, str(scripts / name)], cwd=root, check=True)
    if options.validate:
        subprocess.run([sys.executable, str(scripts / 'validate.py'), '--channel', options.channel], cwd=root, check=True)
    print(root / 'dist/audit/architecture/index.html')


if __name__ == '__main__':
    main()
