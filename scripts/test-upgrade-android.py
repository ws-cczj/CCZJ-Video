"""Native upgrade acceptance on a disposable, separately installed application.

Requires the current embedded frontend/native build, adb, websocket-client.
Refuses the user's preview package. Fixture HTTP binds loopback only; adb reverse
connects it to the emulator. SQL fault injection is confined to acceptance data.
"""
import argparse
import base64
import importlib.util
import json
from pathlib import Path
import subprocess
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs
from urllib.request import urlopen
import websocket

ROOT = Path(__file__).resolve().parent.parent
PACKAGE = 'com.cczj.video.acceptance'
DB = 'files/CCZJ Video/cczj_video.db'
SPEC = importlib.util.spec_from_file_location('diagnostics', ROOT/'scripts/collect-android-diagnostics.py')
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
VIEW_SPEC=importlib.util.spec_from_file_location('android_webview',ROOT/'scripts/android_webview.py')
VIEW_MODULE=importlib.util.module_from_spec(VIEW_SPEC)
VIEW_SPEC.loader.exec_module(VIEW_MODULE)


class Device:
    def __init__(self, adb, serial):
        self.adb = [adb, '-s', serial]
        self.socket = None
        self.port = None
        self.sequence = 0
        self.events = []

    def native(self, *args, binary=False):
        import shlex
        if args[0] in ('shell','exec-out') and len(args)>2:
            args=(args[0], ' '.join(shlex.quote(arg) for arg in args[1:]))
        result = subprocess.check_output(self.adb+list(args), timeout=45)
        return result if binary else result.decode('utf-8').strip()

    def connect(self):
        self.close()
        transport=VIEW_MODULE.AndroidWebView(application_id=PACKAGE)
        transport.adb=self.adb
        transport.__enter__()
        self.socket,self.port=transport.socket,transport.port
        transport.socket=transport.port=None
        self.socket.settimeout(55)
        try:
            self.call('Runtime.enable')
            self.ready()
        except BaseException:
            self.close()
            raise

    def ready(self):
        deadline=time.monotonic()+25
        while not self.js('!!document.querySelector("#app")?.__vue_app__?.config.globalProperties.$pinia?._s.get("applicationMode")?.loaded'):
            if time.monotonic()>deadline:raise RuntimeError('Native Vue application did not finish loading')
            time.sleep(.1)
        self.call('Log.enable')

    def close(self):
        if self.socket:
            self.socket.close()
            self.socket = None
        if self.port:
            self.native('forward', '--remove', 'tcp:'+self.port)
            self.port = None

    def call(self, method, params=None):
        self.sequence += 1
        identity = self.sequence
        self.socket.send(json.dumps(dict(id=identity, method=method, params=params or {})))
        while True:
            row = json.loads(self.socket.recv())
            if row.get('id') == identity:
                if 'error' in row:
                    raise RuntimeError(row['error'])
                return row.get('result', {})
            if row.get('method') in ('Runtime.exceptionThrown', 'Runtime.consoleAPICalled', 'Log.entryAdded'):
                self.events.append(row)

    def js(self, expression):
        result = self.call('Runtime.evaluate', dict(expression=expression, awaitPromise=True, returnByValue=True))
        if result.get('exceptionDetails'):
            raise RuntimeError(json.dumps(result['exceptionDetails'], ensure_ascii=False)[:2500])
        return result.get('result', {}).get('value')

    def rpc(self, name, *args):
        try:
            path, alias = MODULE.compiled_binding(name)
        except ValueError:
            # Compatibility bindings can be tree-shaken out of the current UI.
            # Exercise their actual native contract rather than requiring Vite
            # to export an otherwise unused JavaScript wrapper.
            import re
            source = (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
            method = re.search(r'export function '+re.escape(name)+r'\([^)]*\)\s*\{\s*return \$Call.ByID\((\d+)', source)
            if not method:raise
            identity = 'upgrade-compat-'+uuid.uuid4().hex
            payload = json.dumps(dict(object=0,method=0,args={'methodID':int(method[1]),'args':args,'call-id':identity}))
            return self.js('''new Promise((resolve,reject)=>{
              const previous=window._wailsAndroidCallback,id='''+json.dumps(identity)+''';
              const timer=setTimeout(()=>{window._wailsAndroidCallback=previous;reject(Error('Native compatibility call timed out'))},30000);
              window._wailsAndroidCallback=(key,response,error)=>{
                if(key!==id){previous?.(key,response,error);return}
                clearTimeout(timer);window._wailsAndroidCallback=previous;
                try{if(error)throw Error(error);const value=JSON.parse(response);if(!value.ok)throw Error(JSON.stringify(value.error));resolve(value.text?JSON.parse(value.text):value.data)}catch(e){reject(e)}
              };
              wails.invokeAsync(id,'''+json.dumps(payload)+''');
            })''')
        return self.js('(async()=>{const m=await import('+json.dumps(path)+');try{return await m['+json.dumps(alias)+'](...'+json.dumps(args)+')}catch(error){throw Error(JSON.stringify({code:error.code,message:error.message}))}})()')

    def restart(self):
        self.close()
        self.native('shell', 'am', 'force-stop', PACKAGE)
        self.native('shell', 'am', 'start', '-n', PACKAGE+'/com.wails.app.MainActivity')
        time.sleep(2)
        self.connect()

    def sql(self, statement):
        # One shell, quoted literals. Never targets the user's preview application.
        import shlex
        command = 'run-as '+PACKAGE+' sqlite3 -json '+shlex.quote(DB)+' '+shlex.quote(statement)
        raw = self.native('shell', command)
        return json.loads(raw) if raw else None

    def visit(self, route):
        self.ready()
        mode = 'novel' if route.startswith('/novel') else 'comic' if route.startswith('/comic') else 'video'
        current = self.store('applicationMode', 's.mode')
        if current != mode or self.rpc('GetSetting','application_mode') != mode:
            self.rpc('SetSetting', 'application_mode', mode)
            self.js('location.href='+json.dumps('https://wails.localhost/?application-mode='+mode+'#'+route))
            time.sleep(2)
            self.ready()
        self.js('location.hash='+json.dumps(route))
        time.sleep(1.5)
        assert self.store('applicationMode','s.mode')==mode
        assert self.rpc('GetRuntimeMetrics')['scope']['mode']==mode

    def store(self, name, expression):
        return self.js('(async()=>{const s=document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get('+json.dumps(name)+');if(!s)throw Error("Store not mounted");const value=await ('+expression+');return value===undefined?null:JSON.parse(JSON.stringify(value))})()')


class Fixture(BaseHTTPRequestHandler):
    requests = []
    active = 0
    maximum = 0
    delays = {}
    cancelled = []
    lock = threading.Lock()
    png = base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jGmQAAAAASUVORK5CYII=')

    def log_message(self, *_):
        pass

    def do_HEAD(self):
        self.do_GET(head=True)

    def do_GET(self, head=False):
        with self.lock:
            type(self).active += 1
            type(self).maximum = max(self.maximum, self.active)
            self.requests.append(dict(path=self.path, referer=self.headers.get('Referer'), audit=self.headers.get('X-Audit'), range=self.headers.get('Range')))
        try:
            path = urlparse(self.path).path
            if path in self.delays:
                import select, socket
                deadline=time.monotonic()+self.delays[path]
                while time.monotonic()<deadline:
                    readable,_,_=select.select([self.connection],[],[],.04)
                    if readable and self.connection.recv(1,socket.MSG_PEEK)==b'':
                        self.cancelled.append(path)
                        return
            mode = path.split('/')[1]
            if path.endswith('.png'):
                if self.headers.get('X-Audit') != 'native-acceptance' or not self.headers.get('Referer'):
                    self.send_error(403)
                    return
                body = self.png
                mime = 'image/png'
            elif path == '/download.bin':
                total = 4*1024*1024
                range_header = self.headers.get('Range', '')
                start, end = 0, total-1
                if range_header.startswith('bytes='):
                    left, right = range_header[6:].split('-')
                    start, end = int(left or 0), int(right) if right else end
                    end = min(end, total-1)
                self.send_response(206 if range_header else 200)
                self.send_header('Accept-Ranges', 'bytes')
                self.send_header('Content-Length', str(end-start+1))
                if range_header:
                    self.send_header('Content-Range', f'bytes {start}-{end}/{total}')
                self.end_headers()
                if not head:
                    for offset in range(start, end+1, 16384):
                        self.wfile.write(bytes((n % 251 for n in range(offset, min(offset+16384,end+1)))))
                        self.wfile.flush()
                        time.sleep(.025)
                return
            elif parse_qs(urlparse(self.path).query).get('wd'):
                body = '<ul class="results">'+''.join(f'<li><a href="/{mode}/book/{n:03}">同名作品{n:03}</a><span class="genre">'+('rare' if n == 130 else 'adventure')+'</span></li>' for n in range(1,136))+'</ul>'
                mime = 'text/html; charset=utf-8'
            elif path.endswith('/toc'):
                body = '<ul class="chapters">'+''.join(f'<li><a href="/{mode}/chapter/{n}">第{n}章</a></li>' for n in range(1,4))+'</ul>'
                mime = 'text/html; charset=utf-8'
            elif '/chapter/' in path:
                body = '<div id="content">'+''.join('<p>原生验收正文段落，验证网络、提取和缓存。</p>' for _ in range(30))+'</div>'+''.join(f'<img class="page" src="/{mode}/{n}.png">' for n in range(1,4))
                mime = 'text/html; charset=utf-8'
            elif '/book/' in path:
                body = f'<div class="detail"><h1>同名作品{path[-3:]}</h1><p class="intro">原生测试作品</p><a class="toc" href="{path}/toc">目录</a></div>'
                mime = 'text/html; charset=utf-8'
            else:
                self.send_error(404)
                return
            if isinstance(body, str):
                body = body.encode('utf-8')
            self.send_response(200)
            self.send_header('Content-Type', mime)
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            if not head:
                self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
            pass
        finally:
            with self.lock:
                type(self).active -= 1


def document(kind):
    ops = dict(
        search=dict(format='html', list={'query':'li'}, fields={'title':{'query':'a'}, 'url':{'query':'a','attr':'href'}, 'genres':{'query':'.genre'}}),
        detail=dict(format='html', root={'query':'.detail'}, fields={'title':{'query':'h1'},'summary':{'query':'.intro'},'toc_url':{'query':'.toc','attr':'href'}}),
        toc=dict(format='html', list={'query':'.chapters li'},fields={'title':{'query':'a'},'chapter_url':{'query':'a','attr':'href'}}))
    if kind == 'novel':
        ops['content'] = dict(format='html', text={'query':'#content'})
    else:
        ops['pages'] = dict(format='html', list={'query':'img.page'}, fields={'image_url':{'attr':'src'}})
    return json.dumps(dict(version=3,kind=kind,headers={'X-Audit':'native-acceptance'},operations=ops), ensure_ascii=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--adb', default='adb')
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--output', type=Path, default=ROOT/'reports/upgrade-acceptance-2026-10-19/android')
    parser.add_argument('--phase', choices=['reading','download','all'], default='all')
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    device = Device(args.adb,args.device)
    server = ThreadingHTTPServer(('127.0.0.1',0), Fixture)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    port = server.server_port
    device.native('reverse',f'tcp:{port}',f'tcp:{port}')
    checks = []

    def check(name, fn):
        started = time.time()
        try:
            value = fn()
            row = dict(name=name,status='passed',result=value)
        except Exception as error:
            row = dict(name=name,status='failed',error=str(error))
        row['seconds'] = round(time.time()-started,3)
        checks.append(row)
        (args.output/(args.phase+'-checks.json')).write_text(json.dumps(checks,ensure_ascii=False,indent=2),encoding='utf-8')
        print(json.dumps(row,ensure_ascii=True),flush=True)
        return row

    def require(condition, detail):
        if not condition:
            raise AssertionError(detail)

    try:
        device.connect()
        device.rpc('SetSetting','outbound_proxy_pool_v1','{"enabled":false}')
        device.restart()
        device.js('[...document.querySelectorAll("button")].find(b=>b.textContent.includes("同意并继续")&&!b.disabled)?.click()')
        check('embedded-host',lambda: require(device.js('Array.from(document.scripts).some(s=>s.src.includes("/assets/"))&&!Array.from(document.scripts).some(s=>s.src.includes("@vite"))'), 'Expected offline embedded frontend'))
        def markers():
            device.rpc('StartDiagnosticsRecording')
            for kind in ['player_media_load','playback_page_closed','player_issue','preview_started','preview_stopped']:
                device.rpc('AddDiagnosticsRecordingMarker',kind,json.dumps({'player_trace_id':71,'fixture':True}))
            result=device.rpc('StopDiagnosticsRecording')
            require(result['markers'] >= 5,result)
            return result
        check('native-new-player-markers',markers)
        device.rpc('SetAllowPrivateNetwork', True)
        fixture_path=args.output/'fixture-items.json'
        if args.phase in ('reading','all'):
            data={}
            def sources():
                # This package exists solely for acceptance. A rerun must not
                # query sources whose ephemeral fixture server has stopped.
                device.rpc('SetSetting','application_mode','video')
                device.visit('/')
                for kind in ['novel','comic']:
                    for old in device.rpc('LibrarySources',kind):
                        if old['name'].startswith('验收'):
                            device.rpc('LibraryDeleteSource',old['sourceKey'])
                # The fixture assertions count only the controlled sources.
                # This is the disposable acceptance DB, never the preview app.
                device.sql("UPDATE sources SET enabled=0 WHERE kind IN ('novel','comic')")
                device.sql('DELETE FROM reading_state; DELETE FROM reading_state_imports; DELETE FROM library_chapters; DELETE FROM library_items')
                device.js('Object.keys(localStorage).filter(k=>k.startsWith("cczj.novel.")||k.startsWith("cczj.comic.")).forEach(k=>localStorage.removeItem(k))')
                for mode,kind in [('nA','novel'),('nB','novel'),('cA','comic')]:
                    source=device.rpc('LibraryAddSource',kind,'验收'+mode,f'http://127.0.0.1:{port}/{mode}',document(kind))
                    data[mode]={'source':source}
                for kind in ['novel','comic']:
                    result=device.rpc('LibrarySearchPage',kind,'同名','')
                    require(all(not s.get('errorCode') for s in result['results']),result)
                for mode,kind in [('nA','novel'),('nB','novel'),('cA','comic')]:
                    source=data[mode]['source']
                    key=source['sourceKey'] if isinstance(source,dict) else source
                    page=device.rpc('LibraryCatalogPage',dict(kind=kind,genre='',keyword='',sort='title',cursor='',limit=60))
                    # Resolve by durable identity, not local numeric ID.
                    item=device.rpc('LibraryResolveRefs',[dict(kind=kind,sourceKey=key,remoteId=f'/{mode}/book/001')])[0]
                    require(item is not None,dict(source=source,page=page))
                    detail=device.rpc('LibraryItem',item['id'],True)
                    data[mode].update(item=item,detail=detail)
                fixture_path.write_text(json.dumps(data,ensure_ascii=False,indent=2),encoding='utf-8')
                return {'counts':{kind:device.rpc('LibraryCatalogPage',dict(kind=kind,genre='',keyword='',sort='title',cursor='',limit=60)) for kind in ['novel','comic']},'items':data}
            check('native-provider-search-detail-toc',sources)
            def chains():
                chapter=data['nA']['detail']['chapters'][0]
                text=device.rpc('LibraryChapterText',chapter['id'])
                require(bool(text['paragraphs']),text)
                pages=device.rpc('LibraryChapterPages',data['cA']['detail']['chapters'][0]['id'])
                require(len(pages['pages'])==3,pages)
                results=device.js('(async()=>Promise.all('+json.dumps(pages['pages'])+'.map(async p=>{const r=await fetch(p.url);return {status:r.status,type:r.headers.get("content-type"),bytes:(await r.arrayBuffer()).byteLength}})))()')
                require(all(r['status']==200 and r['type'].startswith('image/') for r in results),results)
                return dict(text=text,pages=pages,images=results)
            check('native-text-and-protected-comic-images',chains)
            def shared_cancel():
                chapters=data['nA']['detail']['chapters']
                path='/nA/chapter/3';Fixture.delays[path]=1.5
                module,alias=MODULE.compiled_binding('LibraryChapterText')
                before=sum(urlparse(r['path']).path==path for r in Fixture.requests)
                result=device.js('(async()=>{const m=await import('+json.dumps(module)+'),one=m['+json.dumps(alias)+']('+str(chapters[2]['id'])+'),two=m['+json.dumps(alias)+']('+str(chapters[2]['id'])+');const first=one.then(()=>"resolved",()=>"cancelled");setTimeout(()=>one.cancel(),200);return {first:await first,second:await two}})()')
                count=sum(urlparse(r['path']).path==path for r in Fixture.requests)-before
                require(result['first']=='cancelled' and len(result['second']['paragraphs'])>0 and count==1,dict(result=result,upstreamRequests=count))
                path2='/nA/chapter/2';Fixture.delays[path2]=5
                stopped=device.js('(async()=>{const m=await import('+json.dumps(module)+'),one=m['+json.dumps(alias)+']('+str(chapters[1]['id'])+'),two=m['+json.dumps(alias)+']('+str(chapters[1]['id'])+');const done=Promise.allSettled([one,two]);setTimeout(()=>{one.cancel();two.cancel()},250);return await done})()')
                time.sleep(.7)
                require(path2 in Fixture.cancelled,dict(results=stopped,cancelled=Fixture.cancelled,metrics=device.rpc('GetRuntimeMetrics')['network_budget']))
                return dict(sharedRequestCount=count,remainingWaiterSucceeded=True,allWaitersCancelledUpstream=True)
            check('native-shared-read-and-upstream-cancellation',shared_cancel)
            def pagination():
                counts={}
                for kind,expected in [('novel',270),('comic',135)]:
                    cursor='';ids=[];sizes=[]
                    while True:
                        page=device.rpc('LibraryCatalogPage',dict(kind=kind,genre='',keyword='',sort='title',cursor=cursor,limit=60))
                        ids += [item['id'] for item in page['items']];sizes.append(len(page['items']))
                        cursor=page['nextCursor']
                        if not cursor:break
                    require(len(ids)==expected and len(set(ids))==expected,dict(kind=kind,count=len(ids),sizes=sizes))
                    rare=device.rpc('LibraryCatalogPage',dict(kind=kind,genre='rare',keyword='',sort='title',cursor='',limit=60))
                    require(len(rare['items'])==(2 if kind=='novel' else 1),rare)
                    counts[kind]=dict(total=len(ids),pages=sizes,rare=len(rare['items']))
                return counts
            check('native-large-catalog-pagination',pagination)
            def legacy():
                values={}
                for mode in ['nA','nB']:
                    item=data[mode]['item'];key='\0'.join(('novel',item['sourceKey'],item['remoteId']))
                    values.setdefault('cczj.novel.shelf.v2',[]).append(dict(key=key,itemId=item['id'],addedAt=1,available=True,item=item))
                    chapter=data[mode]['detail']['chapters'][0]
                    values.setdefault('cczj.novel.progress.v2',{})[key]=dict(itemId=item['id'],chapterId=chapter['id'],chapterOrd=0,chapterRemoteId=chapter['remoteId'],ratio=.25 if mode=='nA' else .75,updatedAt=1)
                item=data['cA']['item'];key='\0'.join(('comic',item['sourceKey'],item['remoteId']))
                values['cczj.comic.follow.v2']=[dict(kind='comic',sourceKey=item['sourceKey'],remoteId=item['remoteId'],itemId=item['id'],title=item['title'],cover='',addedAt=1)]
                values['cczj.comic.progress.v2']={key:dict(itemId=item['id'],episodeOrd=0,page=2,offset=.5,updatedAt=1)}
                values['cczj.novel.reader.v2']={'size':22}
                values['cczj.comic.preferences.v2']={'brightness':.8,'flow':'pages'}
                device.js('Object.entries('+json.dumps(values)+').forEach(([k,v])=>localStorage.setItem(k,JSON.stringify({version:1,value:v})))')
                device.visit('/novel/shelf')
                novel=device.store('novelReading','({ready:s.ready,shelf:s.shelf,progress:s.progress,preferences:s.preferences})')
                require(novel['ready'] and len(novel['shelf'])==2,novel)
                require(novel['preferences']['size']==22 and sorted(p['ratio'] for p in novel['progress'].values())==[.25,.75],novel)
                device.visit('/comic/library')
                comic=device.store('comicReading','({ready:s.ready,followed:s.followed,progress:s.progress,preferences:s.preferences})')
                require(comic['ready'] and len(comic['followed'])==1,comic)
                return dict(novel=novel,comic=comic,retained=device.js('localStorage.getItem("cczj.novel.shelf.v2")!==null'),sql=device.sql('SELECT kind,bucket,count(*) AS count FROM reading_state GROUP BY kind,bucket'))
            check('native-v2-localstorage-sqlite-import',legacy)
            def restore():
                device.restart();device.visit('/novel/shelf')
                require(device.store('novelReading','s.shelf.length')==2,'restart lost shelf')
                # Clear only browser state. SQLite stays in application files.
                device.js('localStorage.clear();sessionStorage.clear()')
                device.restart();device.visit('/novel/shelf')
                novel=device.store('novelReading','({shelf:s.shelf,progress:s.progress,preferences:s.preferences})')
                require(len(novel['shelf'])==2,novel)
                device.visit('/comic/library')
                comic=device.store('comicReading','({followed:s.followed,progress:s.progress})')
                require(len(comic['followed'])==1,comic)
                return dict(novel=novel,comic=comic)
            check('native-force-stop-and-webview-clear-recovery',restore)
            def readers():
                device.visit('/novel/read/'+str(data['nA']['item']['id'])+'/first')
                time.sleep(.8)
                require(device.js('document.querySelectorAll(".novel-reading-paragraph").length>0'),'Novel reader did not render extracted text')
                device.visit('/comic/read/'+str(data['cA']['item']['id'])+'/0')
                time.sleep(1)
                images=device.js('[...document.querySelectorAll(".comic-reader img")].map(e=>({width:e.naturalWidth,height:e.naturalHeight,complete:e.complete}))')
                require(images and all(i['width']>0 for i in images),images)
                indicator=device.js('document.querySelector(".comic-reader-indicator")?.textContent')
                require('3 / 3' in (indicator or ''),dict(expectedSavedPage=3,indicator=indicator))
                device.visit('/novel/shelf')
                return dict(novelRendered=True,comicDecodedImages=images,restoredPage=indicator)
            check('native-actual-novel-and-comic-reader-rendering',readers)
            def write_retry():
                device.visit('/novel/shelf')
                device.sql("CREATE TRIGGER audit_fail_write BEFORE INSERT ON reading_state BEGIN SELECT RAISE(FAIL,'acceptance injected storage failure'); END")
                try:
                    result=device.store('novelReading','s.setPreferences({size:25})')
                    failed=device.store('novelReading','s.storageError')
                    require(result is False and failed,dict(result=result,visibleError=failed))
                finally:device.sql('DROP TRIGGER audit_fail_write')
                require(device.store('novelReading','s.retry()') is True,'retry failed')
                rows=device.rpc('ReadingStateLoad','novel','[]')
                preferences=json.loads(next(r['value'] for r in rows if r['bucket']=='preferences'))
                require(preferences['size']==25,preferences)
                return dict(preferences=preferences,errorAfterRetry=device.store('novelReading','s.storageError'))
            check('native-sqlite-write-failure-and-ui-store-retry',write_retry)
            def initialization_retry():
                before=device.sql('SELECT count(*) AS n FROM reading_state')[0]['n']
                device.sql('ALTER TABLE reading_state RENAME TO audit_reading_saved')
                try:
                    device.call('Page.reload');time.sleep(2)
                    status=device.store('novelReading','({ready:s.ready,error:s.storageError})')
                    require(not status['ready'] and status['error'],status)
                    require(device.store('novelReading','s.setPreferences({size:18})') is False,'Failed initialization allowed a write')
                    require(device.sql('SELECT count(*) AS n FROM audit_reading_saved')[0]['n']==before,'Initialization lost durable records')
                finally:device.sql('ALTER TABLE audit_reading_saved RENAME TO reading_state')
                require(device.store('novelReading','s.retry()') is True,'Initialization retry failed')
                require(device.store('novelReading','s.preferences.size')==25,'Failed edit overwrote durable preferences')
                return dict(recordsRetained=before,statusDuringFailure=status)
            check('native-load-failure-blocks-empty-overwrite',initialization_retry)
            def removed_legacy():
                item=data['nA']['item'];key='\0'.join(('novel',item['sourceKey'],item['remoteId']))
                old=dict(key=key,itemId=item['id'],addedAt=1,available=True,item=item)
                device.js('localStorage.setItem("cczj.novel.shelf.v2",'+json.dumps(json.dumps(dict(version=1,value=[old])))+')')
                require(device.store('novelReading','s.removeShelf('+json.dumps(key)+')') is True,'Removal failed')
                device.restart();device.visit('/novel/shelf')
                require(not device.store('novelReading','s.shelf.some(e=>e.key==='+json.dumps(key)+')'),'Legacy shelf resurrected')
                require(device.js('localStorage.getItem("cczj.novel.shelf.v2")!==null'),'Legacy value was destroyed')
                # Restore the fixture through the real domain store for backup.
                require(device.store('novelReading','s.toggleShelf('+json.dumps(item)+')') is True,'Fixture restore failed')
                return dict(legacyRetained=True,removedDidNotResurrect=True)
            check('native-removed-item-not-resurrected-by-v2',removed_legacy)
            def backup_identity():
                path=device.rpc('BackupExport','')
                raw=device.native('exec-out','run-as',PACKAGE,'cat',path,binary=True)
                # BackupExport uses the application's Brotli format.
                decoded=subprocess.check_output(['node','-e','const z=require("node:zlib");let b=[];process.stdin.on("data",c=>b.push(c));process.stdin.on("end",()=>process.stdout.write(z.brotliDecompressSync(Buffer.concat(b))))'],input=raw) if path.endswith('.br') else raw
                payload=json.loads(decoded)
                require(payload['version']==2 and len(payload['reading']['items'])==3,payload.keys())
                (args.output/'reading-backup-v2.json.br').write_bytes(raw)
                # Simulate a different destination database inside the disposable
                # app. Retain sources but deliberately collide numeric item IDs.
                original=data['nA']['item'];old_id=original['id']
                device.visit('/')
                device.sql('DELETE FROM reading_state; DELETE FROM reading_state_imports; DELETE FROM library_chapters; DELETE FROM library_items')
                device.sql("INSERT INTO library_items(id,kind,source_key,remote_id,title) VALUES ("+str(old_id)+",'novel','acceptance_decoy','/decoy','Different work with reused ID')")
                result=device.rpc('BackupImportFromBase64','reading-v2.json.br',base64.b64encode(raw).decode())
                rows=device.rpc('ReadingStateLoad','novel','[]')
                progress=[json.loads(r['value']) for r in rows if r['bucket']=='progress']
                require(len(progress)==2 and all(p['itemId']!=old_id for p in progress),progress)
                require(len(set(p['itemId'] for p in progress))==2,'Same-title sources merged')
                key=next(r['key'] for r in rows if r['bucket']=='progress')
                newer=dict(json.loads(next(r['value'] for r in rows if r['key']==key and r['bucket']=='progress')),ratio=.9,updatedAt=int(time.time()*1000))
                device.rpc('ReadingStateApply','novel',json.dumps([dict(bucket='progress',key=key,value=json.dumps(newer))]))
                device.rpc('BackupImportFromBase64','reading-v2-repeat.json.br',base64.b64encode(raw).decode())
                after=device.rpc('ReadingStateLoad','novel','[]')
                require(json.loads(next(r['value'] for r in after if r['key']==key and r['bucket']=='progress'))['ratio']==.9,'Repeated import regressed local progress')
                v1=dict(payload);v1['version']=1;v1.pop('reading',None)
                device.rpc('BackupImportFromBase64','legacy-v1.json',base64.b64encode(json.dumps(v1).encode()).decode())
                future=dict(kind=payload['kind'],version=99)
                try:device.rpc('BackupImportFromBase64','future.json',base64.b64encode(json.dumps(future).encode()).decode())
                except RuntimeError as error:require('UNSUPPORTED' in str(error),str(error))
                else:raise AssertionError('Unknown backup version accepted')
                return dict(export=path,importResult=result,remappedProgress=progress,repeatKeptNewer=True,v1Accepted=True,futureRejected=True)
            check('native-backup-v2-cross-catalog-identity-and-version-compatibility',backup_identity)
        def downloads():
            device.visit('/')
            task='acceptance-resume-'+str(int(time.time()))
            initial=device.rpc('StartVideoDownload',dict(task_id=task,url=f'http://127.0.0.1:{port}/download.bin',filename=task+'.bin',save_dir='',force=False))
            time.sleep(.8)
            device.rpc('PauseDownload',task)
            paused=device.rpc('GetDownloadProgress',task)
            require(paused['status']=='paused' and paused['downloaded']>0,paused)
            device.restart()
            restored=device.rpc('GetDownloadProgress',task)
            require(restored['status']=='paused' and restored['downloaded']>=paused['downloaded'],restored)
            device.rpc('ResumeDownload',task)
            for _ in range(35):
                final=device.rpc('GetDownloadProgress',task)
                if final['status'] in ['done','error']:break
                time.sleep(.5)
            require(final['status']=='done' and final['downloaded']==4*1024*1024,final)
            payload=device.native('exec-out','run-as',PACKAGE,'cat',final['save_path'],binary=True)
            require(payload==bytes(n%251 for n in range(4*1024*1024)),'Resumed content differs')
            return dict(initial=initial,paused=paused,restored=restored,final=final,verifiedBytes=len(payload))
        if args.phase in ('download','all'):
            check('native-download-pause-restart-resume-content',downloads)
            def pressure():
                task_ids=[]
                stamp=str(int(time.time()*1000))
                try:
                    for n in range(2):
                        task='budget-'+stamp+'-'+str(n);task_ids.append(task)
                        device.rpc('StartVideoDownload',dict(task_id=task,url=f'http://127.0.0.1:{port}/download.bin?budget={task}',filename=task+'.bin',save_dir='',force=False))
                    time.sleep(.1)
                    before=device.rpc('GetRuntimeMetrics')['network_budget']
                    require(before['active']<=6 and before['waiting']>0,before)
                    upstream=f'http://127.0.0.1:{port}/download.bin?playback={stamp}'
                    proxy='/__cczj/hls?u='+base64.urlsafe_b64encode(upstream.encode()).decode().rstrip('=')
                    device.js('window.__budgetAbort=new AbortController();window.__budgetRead=fetch('+json.dumps(proxy)+',{signal:window.__budgetAbort.signal}).then(r=>r.arrayBuffer()).catch(()=>null);void 0')
                    time.sleep(.12)
                    during=device.rpc('GetRuntimeMetrics')['network_budget']
                    require(during['active']<=8 and during['lanes'].get('playback',0)>=1,during)
                    return dict(downloadContention=before,playbackReservedCapacity=during)
                finally:
                    device.js('window.__budgetAbort?.abort()')
                    for task in task_ids:device.rpc('CancelDownload',task)
                    time.sleep(.6)
                    idle=device.rpc('GetRuntimeMetrics')['network_budget']
                    require(idle['active']==0 and idle['waiting']==0,idle)
            check('native-download-contention-playback-reserve-and-budget-release',pressure)
        check('runtime-after-acceptance',lambda:device.rpc('GetRuntimeMetrics'))
    finally:
        (args.output/(args.phase+'-events.json')).write_text(json.dumps(device.events,ensure_ascii=False,indent=2),encoding='utf-8')
        (args.output/(args.phase+'-http.json')).write_text(json.dumps(dict(requests=Fixture.requests,max_active=Fixture.maximum),ensure_ascii=False,indent=2),encoding='utf-8')
        device.close()
        device.native('reverse','--remove',f'tcp:{port}')
        server.shutdown()
    raise SystemExit(1 if any(row['status']=='failed' for row in checks) else 0)


if __name__ == '__main__':
    main()
