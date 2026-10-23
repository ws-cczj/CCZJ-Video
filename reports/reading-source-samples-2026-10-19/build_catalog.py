"""Build research-only v3 source samples; no third-party code is executed."""
import json
from pathlib import Path

OUT = Path(__file__).parent
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
LEGADO = "https://github.com/XIU2/Yuedu/blob/426b24f59aa763ee0f2d9cc094612d2ef42c8293/shuyuan"
MIHON = "https://github.com/keiyoushi/extensions-source/blob/314a0d1b2b6b194f6a4f9aee252fec9ce55ae5ca/src/zh/"

def q(query, attr="text", **kw):
    return dict(query=query, attr=attr, **kw)

def p(path, **kw):
    return dict(path=path, **kw)

def rx(selector, pattern, replacement=""):
    return dict(selector, regex=dict(pattern=pattern, replacement=replacement))

def op(fmt="html", **kw):
    return dict(format=fmt, **kw)

def entry(key, name, base, kind, project, origin, doc, summary, sample):
    return dict(id=key, name=name, apiUrl=base, kind=kind, project=project, origin=origin,
                document=dict(version=3, kind=kind, encoding="utf-8", headers={"User-Agent":UA, "Referer":base+"/"}, **doc),
                checkedAt="2026-10-19", status="verified", summary=summary, sample=sample)

catalog=[]
catalog.append(entry("legado-kuwo", "酷我小说", "http://appi.kuwo.cn", "novel", "Legado", LEGADO,
    dict(check_keyword="三国", operations=dict(
        search=op("json", path="/novels/api/book/search", params={"ps":"10"}, param_names={"keyword":"keyword","page":"pi"}, list=p("data"), fields={
            "id":p("book_id"), "title":p("title"), "author":p("author_name"), "cover":p("cover_url"), "summary":p("intro"),
            "url":rx(p("book_id"),"^(.+)$","/novels/api/book/${1}"), "genres":p("category_name")}),
        detail=op("json", root=p("data"), fields={"id":p("book_id"),"title":p("title"),"author":p("author_name"),"cover":p("cover_url"),"summary":p("intro"),
            "toc_url":rx(p("book_id"),"^(.+)$","/novels/api/book/${1}/chapters?paging=0")}),
        toc=op("json", list=p("data"), fields={"id":p("chapter_id"),"title":p("chapter_title"),"chapter_url":{"concat":[rx(p("book_id"),"^(.+)$","/novels/api/book/${1}/chapters/"),p("chapter_id")]}}),
        content=op("json", text=p("data.content")))),
    "搜索、详情、目录、首章实测通过；只测免费章节。当前源 HTTP 可用，HTTPS 返回业务错误。",
    dict(keyword="三国", title="三国之仲谋天下", detailUrl="http://appi.kuwo.cn/novels/api/book/22543868000604102", chapterUrl="http://appi.kuwo.cn/novels/api/book/22543868000604102/chapters/96825218742211052", contentCharacters=3326)))
catalog.append(entry("legado-yueyou", "阅友小说", "http://m.suixkan.com", "novel", "Legado", LEGADO,
    dict(check_keyword="三国", operations=dict(
        search=op(path="/s/1.html", params={"page":"{page}"}, param_names={"keyword":"keyword"}, list=q(".v-list-item"), fields={
            "title":q(".v-title"),"author":q(".v-author"),"cover":q("img","src"),"summary":q(".v-intro"),
            "url":rx({"attr":"onclick"},"^.*newWebView\\('([^']+)'.*$","${1}")}),
        detail=op(fields={"title":q(".face-info-title"),"author":rx(q(".face-info .v-words span", index={"indices":[0]}),"^作者：",""),"cover":q(".face-cover img","src"),"summary":q("#intro"),"toc_url":q(".sumchapter a","href")}),
        toc=op(list=q(".catalog_ls li a"), fields={"title":{"attr":"text"},"chapter_url":{"attr":"href"}}),
        content=op(text={"concat":[rx(q(".con"),"[（(]本章未完[^）)]*[）)]|[（(]本章完[）)]","")],"separator":"\n\n"}))),
    "搜索、详情、779 章目录、首章全部 5 段实测通过；站点提示部分阅读需客户端，未测试付费章节。",
    dict(keyword="三国", title="三国争锋", detailUrl="http://m.suixkan.com/b/235207.html", chapterUrl="http://m.suixkan.com/r/235207/235208.html", chapterCount=779, contentCharacters=3399)))
catalog.append(entry("mihon-guazi", "瓜子漫画", "https://www.guazimanhua.com", "comic", "Mihon", MIHON+"guazimanhua/src/eu/kanade/tachiyomi/extension/zh/guazimanhua/Guazimanhua.kt",
    dict(reverse=True,check_keyword="斗罗",operations=dict(
        search=op(path="/category.php",params={"sort":"hits"},param_names={"keyword":"keyword","page":"page"},list=q("article.card"),fields={"title":q("h3 a"),"url":q("a.cover-wrap","href"),"cover":q("img.cover","src"),"author":rx(q("div.meta","ownText")," · .*$","")}),
        detail=op(fields={"title":q("div.mobile-comic-title"),"cover":q("img.mobile-comic-cover","src"),"summary":q("p.mobile-comic-desc"),"genres":q("p.mobile-comic-tags")}),
        toc=op(list=q("section.mobile-comic-all-chapters div.mobile-chapter-grid a"),fields={"title":{"attr":"text"},"chapter_url":{"attr":"href"}}),
        pages=op(list=q("section.reader-images img"),fields={"image_url":{"attr":"src"}}))),
    "搜索、详情、605 话目录、第一话 19 页、首图 JPEG 实测通过；必须桌面 User-Agent。",
    dict(keyword="斗罗",title="斗罗大陆",detailUrl="https://www.guazimanhua.com/comic.php?id=14917",chapterUrl="https://www.guazimanhua.com/chapter.php?id=1668692",chapterCount=605,pageCount=19,imageBytes=224815,imageMime="image/jpeg")))
catalog.append(entry("mihon-dongman", "咚漫漫画", "https://www.dongmanmanhua.cn", "comic", "Mihon", MIHON+"dongmanmanhua/src/eu/kanade/tachiyomi/extension/zh/dongmanmanhua/DongmanManhua.kt",
    dict(reverse=True,check_keyword="神之塔",operations=dict(
        search=op(path="/search",param_names={"keyword":"keyword","page":"page"},list=q("#content > div.card_wrap.search ul:not(#filterLayer) li a"),fields={"title":q("p.subj"),"url":{"attr":"href"},"cover":q("img","src")}),
        detail=op(fields={"title":q("h1.subj, h3.subj"),"author":{"fallback":[q(".detail_header .info .author","ownText"),q(".detail_header .info .author_area","ownText")]},"summary":q("#_asideDetail p.summary"),"cover":q("meta[property='og:image']","content")}),
        toc=op(list=q("ul#_listUl li"),fields={"title":q("span.subj span"),"chapter_url":q("a","href")},next=q("div.paginate a[onclick] + a","href")),
        pages=op(list=q("div#_imageList > img"),fields={"image_url":{"attr":"data-url"}}))),
    "搜索、详情、目录首屏、免费第3话 41 页、首图 JPEG 实测通过；完整目录翻页及付费章未实测。",
    dict(keyword="神之塔",title="神之塔",detailUrl="https://www.dongmanmanhua.cn/episodeList?titleNo=241",chapterUrl="https://www.dongmanmanhua.cn/BOY/tower-of-god/%E7%AC%AC3%E8%AF%9D-%E6%B5%B7%E9%A1%BF%E7%9A%84%E6%A5%BC%E5%B1%82-/viewer?title_no=241&episode_no=4",pageCount=41,imageBytes=57931,imageMime="image/jpeg")))
catalog.extend([
    dict(id="fqapp-fanqie",name="番茄小说 / 漫画",apiUrl="https://fanqienovel.com",kind="novel",project="fqapp",origin="https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/content.rs",status="unsupported",summary="项目采用设备会话、请求签名和正文协议处理，不能通过静态 JSON/CSS 文档直接导入；未做完整真实链路测试。"),
    dict(id="legado-69shuba",name="69书吧",apiUrl="https://www.69shuba.com",kind="novel",project="Legado",origin=LEGADO,status="blocked",summary="本机实测搜索 HTTP 403，返回挑战页；不应标为可用。"),
    dict(id="mihon-bh3",name="崩坏3官方漫画",apiUrl="https://comic.bh3.com",kind="comic",project="Mihon",origin=MIHON+"bh3/src/eu/kanade/tachiyomi/extension/zh/bh3/BH3.kt",status="partial",summary="官方列表和目录实测 HTTP 200；扩展明确无搜索，需支持列表浏览后才能导入。"),
    dict(id="mihon-terra",name="泰拉记事社",apiUrl="https://comic.hypergryph.com",kind="comic",project="Mihon",origin=MIHON+"terrahistoricus/src/eu/kanade/tachiyomi/extension/zh/terrahistoricus/TerraHistoricus.kt",status="partial",summary="官方漫画列表 JSON 实测 HTTP 200；搜索在客户端过滤，图片页接口需序号构造，待专用适配。"),
])
(OUT/"catalog.json").write_text(json.dumps(catalog,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
print("catalog written:",len(catalog))
