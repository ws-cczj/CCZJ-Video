"""Copy a read-only desktop catalog snapshot into the acceptance app and inspect real UI.

The desktop DB is never written. Acceptance data is backed up before replacing
its video catalog, and the real catalog remains available for manual review.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sqlite3
import time
from urllib.parse import urlsplit
import io
from PIL import Image

ROOT=Path(__file__).resolve().parent.parent
spec=importlib.util.spec_from_file_location('upgrade',ROOT/'scripts/test-upgrade-android.py')
upgrade=importlib.util.module_from_spec(spec);spec.loader.exec_module(upgrade)

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--snapshot',type=Path,default=ROOT/'dist/audit/real-mobile/desktop-snapshot.db')
    parser.add_argument('--device',default='emulator-5554')
    args=parser.parse_args()
    out=ROOT/'dist/audit/real-mobile';out.mkdir(parents=True,exist_ok=True)
    d=upgrade.Device('adb',args.device)
    d.connect()
    cached=d.js('JSON.stringify(Object.fromEntries(Object.entries(localStorage).filter(([key])=>key==="cczj_ep_prog_v1"||key.startsWith("vp_t_"))))')
    (out/'acceptance-before-real-progress.json').write_text(cached,encoding='utf-8')
    d.close()
    d.native('shell','am','force-stop',upgrade.PACKAGE)
    target=out/'acceptance-real.db'
    target.write_bytes(d.native('exec-out','run-as',upgrade.PACKAGE,'cat',upgrade.DB,binary=True))
    # WAL commits can outlive a stopped WebView process.
    import subprocess
    try:(out/'acceptance-real.db-wal').write_bytes(d.native('exec-out','run-as',upgrade.PACKAGE,'cat',upgrade.DB+'-wal',binary=True))
    except subprocess.CalledProcessError:pass
    con=sqlite3.connect(target)
    if not (out/'acceptance-before-real.db').exists():
        backup=sqlite3.connect(out/'acceptance-before-real.db');con.backup(backup);backup.close()
    original=sqlite3.connect(args.snapshot.resolve().as_uri()+'?mode=ro',uri=True)
    original.row_factory=sqlite3.Row
    # Exclude the old synthetic loopback/test sources from prior desktop audits.
    sources=[dict(row) for row in original.execute("SELECT * FROM sources WHERE kind='video'") if urlsplit(row['api_url']).hostname not in ('localhost','127.0.0.1','example.test','example.invalid') and not (urlsplit(row['api_url']).hostname or '').endswith(('.test','.invalid'))]
    keys=[row['source_key'] for row in sources]
    assert keys,'Desktop snapshot contains no real video source'
    marks=','.join('?' for _ in keys)
    videos=[dict(row) for row in original.execute('SELECT * FROM source_videos WHERE source_key IN ('+marks+')',keys)]
    identities={row['global_id'] for row in videos}
    assert videos,'Desktop snapshot contains no collected video'
    con.execute('PRAGMA foreign_keys=OFF')
    for table in ['favorites','watch_history','source_types','source_videos','global_video','global_types','catalog_retirements']:
        con.execute('DELETE FROM '+table)
    con.execute("DELETE FROM sources WHERE kind='video'")
    def insert(table,rows):
        columns=[row[1] for row in con.execute('PRAGMA table_info('+table+')')]
        for row in rows:
            chosen=[name for name in columns if name in row]
            con.execute('INSERT OR REPLACE INTO '+table+' ('+','.join('"'+name+'"' for name in chosen)+') VALUES ('+','.join('?' for _ in chosen)+')',[row[name] for name in chosen])
    for row in sources:row['schedule_config']='{}'
    insert('sources',sources)
    insert('global_types',[dict(row) for row in original.execute('SELECT * FROM global_types')])
    insert('global_video',[dict(row) for row in original.execute('SELECT * FROM global_video') if row['id'] in identities])
    insert('source_types',[dict(row) for row in original.execute('SELECT * FROM source_types WHERE source_key IN ('+marks+')',keys)])
    insert('source_videos',videos)
    favorites=[dict(row) for row in original.execute('SELECT * FROM favorites') if row['source_key'] in keys and row['global_id'] in identities]
    insert('favorites',favorites)
    insert('watch_history',[dict(row) for row in original.execute('SELECT * FROM watch_history') if row['source_key'] in keys and row['global_id'] in identities])
    selected=max(keys,key=lambda key:sum(row['source_key']==key for row in videos))
    for key,value in [('default_source_key',selected),('application_mode','video')]:
        con.execute('INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value',(key,value))
    con.execute('PRAGMA user_version=20')
    con.commit();con.execute('PRAGMA wal_checkpoint(TRUNCATE)');con.close();original.close()
    d.native('push',str(target),'/data/local/tmp/cczj-real-catalog.db')
    d.native('shell','run-as',upgrade.PACKAGE,'cp','/data/local/tmp/cczj-real-catalog.db',upgrade.DB)
    d.native('shell','run-as',upgrade.PACKAGE,'rm','-f',upgrade.DB+'-wal',upgrade.DB+'-shm')
    d.native('shell','am','start','-W','-n',upgrade.PACKAGE+'/com.wails.app.MainActivity');d.connect()
    # The replacement catalog may reuse old global IDs. Restore progress from
    # its SQLite history, never from unrelated clips left in WebView storage.
    d.js('for(const key of Object.keys(localStorage))if(key==="cczj_ep_prog_v1"||key.startsWith("vp_t_"))localStorage.removeItem(key)')
    d.restart()

    def wait(expression,seconds=25):
        end=time.monotonic()+seconds
        while time.monotonic()<end:
            if d.js(expression):return
            time.sleep(.15)
        raise AssertionError(expression)
    def visit(route,selector):
        d.js('location.hash='+json.dumps(route));wait('!!document.querySelector('+json.dumps(selector)+')');time.sleep(.8)
    def shot(name):(out/(name+'.png')).write_bytes(d.native('exec-out','screencap','-p',binary=True))
    def swipe(direction):
        w=d.js('innerWidth');h=d.js('innerHeight');x=w*(.78 if direction=='left' else .22);y=h*.7
        d.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[dict(x=x,y=y)]))
        for i in range(1,9):
            d.call('Input.dispatchTouchEvent',dict(type='touchMove',touchPoints=[dict(x=x+(-1 if direction=='left' else 1)*w*.55*i/8,y=y)]));time.sleep(.03)
        d.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
    try:
        visit('/','.home')
        wait('document.querySelectorAll(".home .video-card").length>0')
        time.sleep(3);shot('real-home')
        home=d.js('({cards:document.querySelectorAll(".home .video-card").length,images:[...document.querySelectorAll(".home img")].filter(e=>e.naturalWidth>0).length})')
        launch=d.native('shell','run-as',upgrade.PACKAGE,'cat','shared_prefs/cczj-presentation.xml')
        d.js('wails.setSystemBarColor("#123456")');time.sleep(.3)
        pixel=Image.open(io.BytesIO(d.native('exec-out','screencap','-p',binary=True))).convert('RGB').getpixel((2,2))
        assert pixel==(18,52,86),pixel
        assert d.native('shell','run-as',upgrade.PACKAGE,'cat','shared_prefs/cczj-presentation.xml')==launch
        visit('/settings','.cczj-mobile-settings-index');visit('/','.home')
        shot('real-home')
        visit('/short-drama','.short-drama')
        wait('!!document.querySelector(".short-drama .empty-state")')
        shot('real-short-drama-empty')
        swipe('left');wait('location.hash==="#/search"')
        visit('/short-drama','.short-drama');swipe('right');wait('location.hash==="#/"')
        visit('/search','.search-page')
        keyword=next(row['vod_name'] for row in videos if len(row['vod_name'])>=2)[:2]
        d.js('document.querySelector(".search-page input").focus()')
        d.call('Input.insertText',dict(text=keyword))
        d.js('if(document.querySelector(".source-search-toggle")?.getAttribute("aria-pressed")==="true")document.querySelector(".source-search-toggle").click();document.querySelector(".cczj-mobile-search-form").requestSubmit()')
        wait('document.querySelectorAll(".search-page .video-card").length>0')
        time.sleep(3)
        shot('real-search')
        visit('/favorites','.favorites-page')
        if favorites:wait('document.querySelectorAll(".favorites-page .video-card").length>0')
        shot('real-favorites')
        result={'desktop_read_only':True,'copied_sources':len(sources),'copied_catalog_rows':len(videos),'copied_identities':len(identities),'favorites':len(favorites),'home':home,'system_bar_bridge_rgb':pixel,'system_bar_launch_cache_unchanged':True,'short_drama':'no collected short dramas in desktop snapshot; verified empty-state swipe both directions','search_matches':True}
        (out/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8');print(json.dumps(result))
    finally:d.close()

if __name__=='__main__':main()
