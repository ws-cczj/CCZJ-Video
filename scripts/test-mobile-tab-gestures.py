"""Native tab swipes in all modes and safe hold-to-remove list interactions."""
import json,time,subprocess,argparse
from pathlib import Path
from android_webview import AndroidWebView
args=argparse.ArgumentParser();args.add_argument('--output',default='mobile-tab-gestures');args=args.parse_args()
OUT=Path(__file__).resolve().parent.parent/'dist/audit'/args.output;OUT.mkdir(exist_ok=True,parents=True)
PINIA="document.querySelector('#app').__vue_app__.config.globalProperties.$pinia"
ROUTER="document.querySelector('#app').__vue_app__.config.globalProperties.$router"
with AndroidWebView() as p:
 def wait(expr,seconds=30):
  end=time.monotonic()+seconds
  while time.monotonic()<end:
   try:
    if p.evaluate(expr):return
   except RuntimeError:pass
   time.sleep(.1)
  raise AssertionError(expr)
 def visit(path):
  p.evaluate(ROUTER+'.push('+json.dumps(path)+')');wait('location.hash==='+json.dumps('#'+path)+" && document.querySelectorAll('.cczj-mobile-route-scene').length===1");time.sleep(.5)
 def touch(kind,x=0,y=0):p.call('Input.dispatchTouchEvent',{'type':kind,'touchPoints':[] if kind in ('touchEnd','touchCancel') else [{'x':x,'y':y}]})
 def point(selector,x=.5,y=.5):return p.evaluate('(()=>{const e=document.querySelector("[data-route-active=true] "+'+json.dumps(selector)+')||document.querySelector('+json.dumps(selector)+');e.scrollIntoView({block:"center"});const r=e.getBoundingClientRect();return{x:r.x+r.width*'+str(x)+',y:r.y+r.height*'+str(y)+'}})()')
 def swipe(selector,reverse=False,cancel=False,vertical=False,short=False):
  wait("document.querySelectorAll('.cczj-mobile-route-scene').length===1")
  a=point(selector,.25 if reverse else .75,.5);b=point(selector,.75 if reverse else .25,.5)
  if vertical:b={'x':a['x'],'y':a['y']-80}
  if short:b={'x':a['x']-20,'y':a['y']}
  touch('touchStart',**a)
  for i in range(1,9):touch('touchMove',a['x']+(b['x']-a['x'])*i/8,a['y']+(b['y']-a['y'])*i/8);time.sleep(.03)
  touch('touchCancel' if cancel else 'touchEnd');time.sleep(.65)
 def tap(selector):at=point(selector);touch('touchStart',**at);touch('touchEnd');time.sleep(.25)
 def hold(selector):at=point(selector);touch('touchStart',**at);time.sleep(.65);touch('touchEnd');time.sleep(.3)
 def back():subprocess.run(p.adb+['shell','input','keyevent','KEYCODE_BACK'],capture_output=True,check=True);time.sleep(.5)
 def switch(target):
  current=p.evaluate(PINIA+"._s.get('applicationMode').mode")
  if current==target:return
  visit('/settings?group=basic' if current=='video' else '/'+current+'/settings')
  p.evaluate("document.querySelectorAll('.mode-switch-options button')["+str(['video','novel','comic'].index(target))+"].click()")
  wait("!!document.querySelector('.app-shell[data-application-mode="+target+"]') && !document.querySelector('.splash-wrap')")
  time.sleep(.6)
 original_mode=p.evaluate(PINIA+"._s.get('applicationMode').mode");original_route=p.evaluate('location.hash')
 results={}
 try:
  for mode,tabs,nav in [('video',['/','/search','/favorites','/history','/settings'],'.cczj-mobile-navigation'),('novel',['/novel','/novel/categories','/novel/shelf','/novel/me'],'.novel-bottom-nav'),('comic',['/comic','/comic/categories','/comic/library','/comic/me'],'.comic-bottom-nav')]:
   switch(mode);visit(tabs[0]);swipe(nav,reverse=True);assert p.evaluate('location.hash')=='#'+tabs[0],'Tab endpoints wrapped'
   for tab in tabs[1:]:swipe(nav);assert p.evaluate('location.hash')=='#'+tab,'Left swipe did not select '+tab+' (actual '+p.evaluate('location.hash')+')'
   for tab in reversed(tabs[:-1]):swipe(nav,reverse=True);assert p.evaluate('location.hash')=='#'+tab,'Right swipe did not select '+tab+' (actual '+p.evaluate('location.hash')+')'
   swipe(nav,cancel=True);assert p.evaluate('location.hash')=='#'+tabs[0],'Cancelled swipe navigated'
   swipe(nav,short=True);assert p.evaluate('location.hash')=='#'+tabs[0],'Short swipe navigated'
   results[mode+'Tabs']=True
   visit('/settings?group=basic' if mode=='video' else '/'+mode+'/settings')
   icons=p.evaluate("[...document.querySelectorAll('.mode-switch-options button')].map(e=>e.querySelectorAll('svg').length)")
   assert icons==[1,1,1],'Mode options still have an extra selected check'
   p.screenshot(OUT/(mode+'-modes.png'))
   if mode in ('novel','comic'):
    path='/novel/shelf' if mode=='novel' else '/comic/library';selector='.novel-shelf-item' if mode=='novel' else '.comic-library .comic-card'
    store='novelReading' if mode=='novel' else 'comicReading';field='shelf' if mode=='novel' else 'followed'
    visit(path)
    # Populate only in-memory display state if the user's list is empty; never persist or confirm removal.
    snapshot=p.evaluate("JSON.parse(JSON.stringify("+PINIA+"._s.get('"+store+"')."+field+"))")
    if not snapshot:p.evaluate(PINIA+"._s.get('"+store+"')."+field+"="+json.dumps(['long-wind'] if mode=='novel' else ['moon-mail']))
    try:
     assert p.evaluate("![...document.querySelectorAll('.novel-text-button,.comic-text-button')].some(e=>e.textContent.trim()==='管理')"),'Mobile manage entry still visible'
     hold(selector);assert p.evaluate("!!document.querySelector('.cczj-mobile-item-remove') && !document.querySelector('[role=dialog]')"),'Hold did not reveal X'
     tap('.cczj-mobile-item-remove');wait("!!document.querySelector('[role=dialog]')");p.screenshot(OUT/(mode+'-remove-confirm.png'))
     back();assert p.evaluate("!document.querySelector('[role=dialog]') && !!document.querySelector('.cczj-mobile-item-remove')"),'Back did not cancel confirmation first'
     back();assert p.evaluate('location.hash')=='#'+path and p.evaluate("!document.querySelector('.cczj-mobile-item-remove')"),'Back did not exit editing'
     assert p.evaluate("JSON.parse(JSON.stringify("+PINIA+"._s.get('"+store+"')."+field+"))")==(snapshot or (['long-wind'] if mode=='novel' else ['moon-mail']))
    finally:p.evaluate(PINIA+"._s.get('"+store+"')."+field+'='+json.dumps(snapshot))
    results[mode+'HoldConfirmationAndBack']=True
  switch('video');visit('/favorites');swipe('.main-content',reverse=True);assert p.evaluate('location.hash')=='#/search','Main page swipe did not change tab'
  swipe('.main-content',vertical=True);assert p.evaluate('location.hash')=='#/search','Vertical scroll changed tab'
  swipe('.cczj-mobile-search-field input');assert p.evaluate('location.hash')=='#/search','Input swipe changed tab'
  visit('/settings?group=basic');swipe('.main-content');assert p.evaluate('location.hash')=='#/settings?group=basic','Secondary settings page changed bottom tab'
  visit('/history');wait("!!document.querySelector('.history-page')")
  if p.evaluate("!!document.querySelector('.history-card')"):
   hold('.history-card');assert p.evaluate("!!document.querySelector('.cczj-mobile-item-remove')")
   tap('.cczj-mobile-item-remove');wait("!!document.querySelector('[role=dialog]')");back();back()
   assert p.evaluate("location.hash==='#/history' && !document.querySelector('.cczj-mobile-item-remove')")
   results['watchHistoryConfirmation']=True
  visit('/')
  if p.evaluate("!!document.querySelector('.carousel')"):
   swipe('.carousel');assert p.evaluate('location.hash')=='#/','Carousel drag changed the bottom tab'
   results['carouselOwnsSwipe']=True
  if p.evaluate("!!document.querySelector('.cczj-mobile-continue-group .rec-card-wrap')"):
   assert p.evaluate("!document.querySelector('.cczj-mobile-continue-group .rec-remove-btn')"),'Continue removal remains always visible'
   hold('.cczj-mobile-continue-group .rec-card-wrap');tap('.cczj-mobile-continue-group .rec-remove-btn');wait("!!document.querySelector('[role=dialog]')");back();back()
   assert p.evaluate("location.hash==='#/' && !document.querySelector('.cczj-mobile-continue-group .rec-remove-btn')")
   results['continueWatchingHoldConfirmation']=True
  visit('/favorites')
  # A local, non-persisted card exercises the confirmation without touching backend favorites.
  snapshot=p.evaluate("JSON.parse(JSON.stringify(document.querySelector('.favorites-page').__vueParentComponent.setupState.favorites))")
  p.evaluate("document.querySelector('.favorites-page').__vueParentComponent.setupState.favorites=[{source_key:'cczj-gesture-fixture',vod_id:'never-delete',folderId:'default',video:{vod_name:'Gesture fixture'}}]")
  try:
   wait("!!document.querySelector('.fav-card')");hold('.fav-card');tap('.cczj-mobile-item-remove');wait("!!document.querySelector('[role=dialog]')");back();back()
   assert p.evaluate("location.hash==='#/favorites' && !document.querySelector('.cczj-mobile-item-remove')")
  finally:p.evaluate("document.querySelector('.favorites-page').__vueParentComponent.setupState.favorites="+json.dumps(snapshot))
  results.update({'contentSwipe':True,'verticalAndInputExcluded':True,'secondaryPagesExcluded':True,'favoritesConfirmation':True})
  (OUT/'results.json').write_text(json.dumps(results,indent=2),encoding='utf-8');print('PASS: '+json.dumps(results))
 finally:
  if p.evaluate("!!document.querySelector('[role=dialog]')"):back()
  switch(original_mode);visit(original_route.lstrip('#'))
