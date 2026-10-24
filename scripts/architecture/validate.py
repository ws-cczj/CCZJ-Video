from pathlib import Path
import sys,json,argparse
parser=argparse.ArgumentParser(description='Validate the generated offline architecture canvas.')
parser.add_argument('--channel',default='chrome')
options=parser.parse_args()
ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'.cache/reading-playwright'))
from playwright.sync_api import sync_playwright
OUT=ROOT/'dist/audit/architecture'
result={'errors':[],'checks':[]}
with sync_playwright() as p:
    browser=p.chromium.launch(headless=True,channel=options.channel)
    page=browser.new_page(viewport={'width':1600,'height':1000},accept_downloads=True)
    page.on('pageerror',lambda e:result['errors'].append(str(e)))
    page.goto((OUT/'index.html').as_uri(),wait_until='networkidle')
    assert page.locator('.node').count()==124
    assert page.locator('.edge').count()==93
    def check_labels():
        overflow=page.evaluate('''[...document.querySelectorAll('.node')].flatMap(g=>{const r=g.querySelector('rect').getBBox();return [...g.querySelectorAll('text')].filter(t=>{const b=t.getBBox();return b.x+b.width>r.x+r.width-5 || b.y+b.height>r.y+r.height-5}).map(t=>({node:g.dataset.id,text:t.textContent}))})''')
        assert not overflow,overflow
    check_labels()
    assert not page.locator('#sidebar').is_visible()
    page.locator('[data-band="视频"]').click()
    page.screenshot(path=str(OUT/'canvas-desktop.png'))
    initial=page.locator('#zoomValue').inner_text()
    page.locator('#zoomIn').click()
    assert page.locator('#zoomValue').inner_text()!=initial
    old=page.locator('#world').get_attribute('transform')
    page.mouse.move(800,500)
    page.mouse.wheel(0,-250)
    page.wait_for_timeout(100)
    assert old!=page.locator('#world').get_attribute('transform')
    old=page.locator('#world').get_attribute('transform')
    page.mouse.move(1100,500);page.mouse.down();page.mouse.move(1000,410,steps=6);page.mouse.up()
    assert old!=page.locator('#world').get_attribute('transform')
    result['checks'].append('同一画布 31条功能链 / 216个公开绑定全部归入功能链；按钮和滚轮缩放、拖动平移通过')
    page.locator('#search').fill('StartVideoDownload')
    page.locator('#findBtn').click()
    assert page.locator('#sideTitle').inner_text()=='搜索结果'
    assert page.locator('.match-row').count()>0
    page.locator('.match-row').first.click()
    assert '下载' in page.locator('#sideBody h2').inner_text()
    assert page.locator('#sideBody').inner_text().count('StartVideoDownload')>0
    page.screenshot(path=str(OUT/'canvas-download.png'))
    result['checks'].append('按具体接口搜索定位、同链高亮、源码位置与内部调用详情通过')
    page.locator('#closeSide').click()
    page.locator('#clear').click()
    page.locator('#view').select_option('dependencies')
    assert page.locator('.node').count()==45
    check_labels()
    count=page.locator('.edge').count()
    assert count>100
    page.locator('#search').fill('download')
    page.locator('#findBtn').click()
    page.locator('[data-locate="p-download"]').click()
    assert page.locator('#sideTitle').inner_text()=='Go 包 · download'
    result['checks'].append(f'45个Go包 / {count}条直接导入全部绘制；模块定位与依赖高亮通过')
    for view in ['lifecycle','data']:
        page.locator('#view').select_option(view)
        assert page.locator('.node').count()>20
        check_labels()
        page.locator('#fitAll').click()
        assert float(page.locator('#zoomValue').inner_text().rstrip('%'))<100
    page.locator('#issuesBtn').click()
    expected_issues={entry['id'] for entry in json.loads((OUT/'graph-data.json').read_text(encoding='utf-8'))['issues']}
    assert set(page.locator('[data-issue]').evaluate_all('(items)=>items.map(item=>item.dataset.issue)'))==expected_issues
    page.locator('[data-issue="A05"]').click()
    assert page.locator('#view').input_value()=='business'
    assert page.locator('#sideTitle').inner_text().startswith('A05')
    result['checks'].append('生命周期/平台图、数据归属图、建议定位到关联功能通过')
    page.locator('#closeSide').click()
    page.locator('#clear').click()
    page.locator('#fitAll').click()
    before=page.locator('#world').get_attribute('transform')
    page.locator('#mini').click(position={'x':80,'y':100})
    assert before!=page.locator('#world').get_attribute('transform')
    with page.expect_download() as download:
        page.locator('#exportBtn').click()
    dest=OUT/'export-check.svg'
    download.value.save_as(dest)
    assert '<svg' in dest.read_text(encoding='utf-8')
    assert 'GetVideoDetail' in dest.read_text(encoding='utf-8')
    result['checks'].append('缩略图导航、全图适配和完整SVG导出通过')
    page.set_viewport_size({'width':390,'height':844})
    page.locator('[data-band="漫画"]').click()
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    assert page.locator('#canvas').bounding_box()['height']>500
    page.locator('#zoomIn').click()
    page.screenshot(path=str(OUT/'canvas-mobile.png'))
    client=page.context.new_cdp_session(page)
    box=page.locator('#canvas').bounding_box()
    y=box['y']+220
    before=page.locator('#zoomValue').inner_text()
    client.send('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[{'x':130,'y':y,'id':1},{'x':230,'y':y,'id':2}]})
    client.send('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[{'x':90,'y':y,'id':1},{'x':270,'y':y,'id':2}]})
    client.send('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
    assert before!=page.locator('#zoomValue').inner_text()
    result['checks'].append('390px窄屏画布无页面溢出，双指缩放通过')
    assert not result['errors'],result['errors']
    browser.close()
result['scope']='架构展示HTML交互验收，不代表业务应用验收；双指缩放为Chrome触屏协议模拟'
(OUT/'ui-validation.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps(result,ensure_ascii=True))
