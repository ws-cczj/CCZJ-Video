#!/usr/bin/env python3
"""Collect bounded public metadata, never chapters/images, for source matching.

Network requests are deliberately serial (one active request, below the five
channel ceiling). Successful JSON responses are cached by URL for resumable runs;
failure records and per-provider counts survive in a sibling manifest.
"""
import argparse
from collections import Counter
from datetime import date
import hashlib
import json
from pathlib import Path
import time
import urllib.error
import urllib.parse
import urllib.request


class SameHostRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, target):
        before, after = urllib.parse.urlsplit(request.full_url), urllib.parse.urlsplit(target)
        if after.scheme != 'https' or after.hostname != before.hostname:
            raise ValueError('cross-host redirect rejected before connection')
        return super().redirect_request(request, fp, code, message, headers, target)


class Collector:
    def __init__(self, args):
        self.args = args
        self.today = date.today().isoformat()
        self.works = {}
        self.requests = []
        self.bytes = 0
        self.network_requests = 0
        self.observed_at = self.today
        self.opener = urllib.request.build_opener(SameHostRedirect())
        self.cache = Path(args.cache)
        self.cache.mkdir(parents=True, exist_ok=True)

    def fetch(self, url):
        parsed = urllib.parse.urlsplit(url)
        if parsed.scheme != 'https' or parsed.hostname not in {
            'api.fanqiesdk.com', 'fanqienovel.com', 'api.mangadex.org', 'gutendex.com'
        }:
            raise ValueError('unapproved metadata endpoint')
        cache = self.cache / (hashlib.sha256(url.encode()).hexdigest() + '.json')
        record = {'url': url, 'checkedAt': self.today}
        try:
            if cache.exists() and not self.args.refresh:
                raw = cache.read_bytes()
                if len(raw) > self.args.max_response_bytes:
                    raise ValueError('cached response exceeds limit')
                record['cache'] = True
                record['checkedAt'] = date.fromtimestamp(cache.stat().st_mtime).isoformat()
            else:
                if self.network_requests >= self.args.max_requests:
                    raise ValueError('request budget exhausted')
                self.network_requests += 1
                request = urllib.request.Request(url, headers={
                    'User-Agent': 'CCZJSourceQuality/1.0 (public metadata benchmark)',
                    'Accept': 'application/json',
                })
                with self.opener.open(request, timeout=self.args.timeout) as response:
                    if urllib.parse.urlsplit(response.url).hostname != parsed.hostname:
                        raise ValueError('cross-host redirect rejected')
                    raw = response.read(self.args.max_response_bytes + 1)
                if len(raw) > self.args.max_response_bytes:
                    raise ValueError('response exceeds limit')
                self.bytes += len(raw)
                if self.bytes > self.args.max_total_bytes:
                    raise ValueError('total byte budget exhausted')
                value = json.loads(raw)
                cache.write_bytes(raw)
                time.sleep(self.args.delay)
            value = json.loads(raw)
            record.update(ok=True, bytes=len(raw))
            self.requests.append(record)
            self.observed_at = record['checkedAt']
            return value
        except (OSError, ValueError) as error:
            record.update(ok=False, error=str(error))
            self.requests.append(record)
            print(json.dumps(record, ensure_ascii=False), flush=True)
            return None

    def add(self, work):
        if not work['title'] or len(work['title'].encode()) > 600:
            return
        work['aliases'] = list(dict.fromkeys(
            x for x in work['aliases'] if x and x != work['title'] and len(x.encode()) <= 600
        ))
        work['authors'] = list(dict.fromkeys(x for x in work['authors'] if x))
        work['tags'] = list(dict.fromkeys(x for x in work['tags'] if x))
        old = self.works.get(work['id'])
        if old:
            old['tags'] = list(dict.fromkeys(old['tags'] + work['tags']))
            old['aliases'] = list(dict.fromkeys(old['aliases'] + work['aliases']))
            for ref in work['references']:
                if ref not in old['references']:
                    old['references'].append(ref)
        else:
            self.works[work['id']] = work

    def reference(self, url, role='metadata'):
        return {'url': url, 'checkedAt': self.observed_at, 'role': role}

    def fanqie(self):
        # Read category IDs from the site's own category directory, rather than
        # assuming older category codes still represent the same genre.
        channels = []
        for gender in (0, 1, 2):
            payload = self.fetch('https://fanqienovel.com/api/author/book/category_list/v0/?gender=' + str(gender))
            if payload and payload.get('code') == 0:
                main = [x for x in payload.get('data', []) if x.get('label') == '主分类']
                channels.extend((x['category_id'], gender) for x in main[:self.args.category_limit])
        if not channels:
            channels = [(511, 1), (273, 1), (506, 1), (516, 1), (259, 1)]
        plan = [(category, gender, self.args.fanqie_pages) for category, gender in channels]
        # These still-working browse routes add traditional fantasy/history/city
        # coverage not present in the first eight current main-category entries.
        supplemental = [(511, 1), (273, 1), (506, 1), (516, 1), (259, 1), (261, 1), (258, 1)]
        planned = set(channels)
        plan.extend((category, gender, self.args.supplemental_pages)
                    for category, gender in supplemental if (category, gender) not in planned)
        for category, gender, pages in plan:
            for page in range(pages):
                query = urllib.parse.urlencode(dict(aid=1967, offset=page * 100,
                    limit=100, category_id=category, gender=gender))
                url = 'https://api.fanqiesdk.com/api/novel/channel/homepage/new_category/book_list/v1/?' + query
                payload = self.fetch(url)
                if not payload or payload.get('code') != 0:
                    break
                rows = payload.get('data', {}).get('data', [])
                for row in rows:
                    if str(row.get('genre')) not in ('0', '6'):
                        continue
                    book_id = str(row.get('book_id', ''))
                    if not book_id.isdigit():
                        continue
                    tags = ['中文网络小说', row.get('category', '')]
                    try:
                        tags += [t['Name'] for t in json.loads(row.get('category_v2') or '[]') if t.get('Name')]
                    except (ValueError, TypeError):
                        pass
                    self.add(dict(id='fanqie-' + book_id, kind='novel',
                        title=row.get('book_name', ''),
                        aliases=[row.get('book_flight_alias_name', '')],
                        authors=[row.get('author', '')], tags=tags, language='zh', searchLanguages=['zh'],
                        searchTitles={'zh': row.get('book_name', '')}, layout='text',
                        edition='番茄中文网络版；目录与正文尚未核验',
                        selection='第一方分类分页样本，非全网热度名次',
                        references=[self.reference('https://fanqienovel.com/page/' + book_id), self.reference(url)]))
                print('fanqie', category, page, len(rows), len(self.works), flush=True)
                if not rows or not payload.get('data', {}).get('has_more'):
                    break

    def mangadex(self):
        # Separate origin-language strata prevent Japanese follow counts dominating.
        for language in ('zh', 'ko', 'ja', 'en'):
            for page in range(self.args.manga_pages):
                query = urllib.parse.urlencode([
                    ('limit', 100), ('offset', page * 100), ('contentRating[]', 'safe'),
                    ('includes[]', 'author'), ('includes[]', 'artist'),
                    ('originalLanguage[]', language), ('order[followedCount]', 'desc')])
                url = 'https://api.mangadex.org/manga?' + query
                payload = self.fetch(url)
                if not payload or payload.get('result') != 'ok':
                    break
                rows = payload.get('data', [])
                for row in rows:
                    attrs = row.get('attributes', {})
                    if attrs.get('contentRating') != 'safe':
                        continue
                    names = dict(attrs.get('title', {}))
                    alternate = attrs.get('altTitles', [])
                    for title_map in alternate:
                        for lang, title in title_map.items():
                            names.setdefault(lang, title)
                    title = next((names[x] for x in ('zh', 'zh-hk', 'en', language) if names.get(x)), next(iter(names.values()), ''))
                    tags = [t.get('attributes', {}).get('name', {}).get('en', '') for t in attrs.get('tags', [])]
                    # Long Strip is an explicit upstream format tag, not inferred from country.
                    layout = 'strip' if 'Long Strip' in tags else 'page'
                    authors = [r.get('attributes', {}).get('name', '') for r in row.get('relationships', []) if r.get('type') in ('author', 'artist')]
                    self.add(dict(id='mangadex-' + row['id'], kind='comic', title=title,
                        aliases=[x for d in [attrs.get('title', {})] + alternate for x in d.values()],
                        authors=authors, tags=['MangaDex safe目录', *tags],
                        language=attrs.get('originalLanguage') or 'und',
                        searchLanguages=sorted(names), searchTitles=names, layout=layout,
                        edition='MangaDex作品级目录；译本与章节尚未核验',
                        selection='按原作语言分层，层内按MangaDex关注数排序；非全网热度',
                        references=[self.reference('https://mangadex.org/title/' + row['id']), self.reference(url)]))
                print('mangadex', language, page, len(rows), len(self.works), flush=True)
                if len(rows) < 100:
                    break

    def gutenberg(self):
        # Supplemental classic fiction only; never presented as current Chinese web fiction.
        for topic in ('science fiction', 'mystery fiction', 'historical fiction', 'fantasy fiction'):
            url = 'https://gutendex.com/books/?' + urllib.parse.urlencode({'topic': topic})
            for _ in range(self.args.gutenberg_pages):
                payload = self.fetch(url)
                if not payload:
                    break
                for row in payload.get('results', []):
                    subjects = row.get('subjects', [])
                    if row.get('media_type') != 'Text' or not any('fiction' in s.lower() for s in subjects):
                        continue
                    bid = str(row['id'])
                    self.add(dict(id='gutenberg-' + bid, kind='novel', title=row.get('title', ''), aliases=[],
                        authors=[a['name'] for a in row.get('authors', [])],
                        tags=['经典虚构文学；长短篇形态待核验', *subjects, *row.get('bookshelves', [])],
                        language=(row.get('languages') or ['und'])[0], searchLanguages=(row.get('languages') or ['und'])[:1],
                        searchTitles={(row.get('languages') or ['und'])[0]: row.get('title', '')}, layout='text',
                        edition='Project Gutenberg电子版，非特定纸本版次',
                        selection='Gutendex题材分层按Gutenberg下载量排序；非当代热门榜',
                        references=[self.reference('https://www.gutenberg.org/ebooks/' + bid), self.reference('https://gutendex.com/books/' + bid + '/')]))
                url = payload.get('next')
                if not url:
                    break

    def save(self):
        output = Path(self.args.output)
        output.parent.mkdir(parents=True, exist_ok=True)
        # Failure is never allowed to replace a richer successful existing corpus.
        if output.exists():
            existing = json.loads(output.read_text(encoding='utf-8'))
            for work in existing.get('works', []):
                self.works.setdefault(work['id'], work)
        corpus = {'version': 'reading-quality.v1', 'updatedAt': self.today,
                  'works': sorted(self.works.values(), key=lambda w: w['id'])}
        temp = output.with_suffix('.tmp')
        temp.write_text(json.dumps(corpus, ensure_ascii=False, separators=(',', ':')) + '\n', encoding='utf-8')
        temp.replace(output)
        manifest = {'checkedAt': self.today, 'networkConcurrency': 1,
            'networkRequests': self.network_requests, 'downloadedBytes': self.bytes,
            'worksByKind': dict(Counter(w['kind'] for w in self.works.values())),
            'worksByLanguage': dict(Counter(w['language'] for w in self.works.values())),
            'worksByProvider': dict(Counter(w['id'].split('-')[0] for w in self.works.values())),
            'requests': self.requests}
        manifest_path = output.with_suffix('.manifest.json')
        if manifest_path.exists():
            previous = json.loads(manifest_path.read_text(encoding='utf-8'))
            records = previous.get('requests', []) + self.requests
            # Preserve earlier failures as well as successful network evidence.
            manifest['requests'] = list({json.dumps(r, sort_keys=True): r for r in records}.values())[-4000:]
        manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        print(json.dumps({k: v for k, v in manifest.items() if k != 'requests'}, ensure_ascii=False), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', default='reports/source-quality/bulk-corpus.v1.json')
    parser.add_argument('--cache', default='dist/source-quality/metadata-cache')
    parser.add_argument('--refresh', action='store_true')
    parser.add_argument('--providers', default='fanqie,mangadex,gutenberg')
    parser.add_argument('--fanqie-pages', type=int, default=2)
    parser.add_argument('--supplemental-pages', type=int, default=3)
    parser.add_argument('--category-limit', type=int, default=8)
    parser.add_argument('--manga-pages', type=int, default=4)
    parser.add_argument('--gutenberg-pages', type=int, default=1)
    parser.add_argument('--timeout', type=float, default=25)
    parser.add_argument('--delay', type=float, default=0.35)
    parser.add_argument('--max-requests', type=int, default=100)
    parser.add_argument('--max-response-bytes', type=int, default=6 << 20)
    parser.add_argument('--max-total-bytes', type=int, default=100 << 20)
    args = parser.parse_args()
    if not (0 <= args.fanqie_pages <= 10 and 0 <= args.supplemental_pages <= 10 and 1 <= args.category_limit <= 30 and 0 <= args.manga_pages <= 20 and
            0 <= args.gutenberg_pages <= 10 and 0 < args.timeout <= 60 and
            0 <= args.delay <= 10 and 0 < args.max_requests <= 1000 and
            0 < args.max_response_bytes <= 16 << 20 and
            0 < args.max_total_bytes <= 512 << 20):
        parser.error('bounded limits are outside supported ranges')
    collector = Collector(args)
    try:
        for provider in args.providers.split(','):
            if provider not in ('fanqie', 'mangadex', 'gutenberg'):
                parser.error('unknown provider')
            getattr(collector, provider)()
    finally:
        collector.save()


if __name__ == '__main__':
    main()
