# 阅读源真实数据研究与可选导入样本

核对时间：2026-10-19（中国标准时间）。本次研究使用 `research` 技能；未执行下载的第三方源码、扩展或 APK。目标是给可选源目录提供来源明确、可用程度可解释的候选，并验证项目现有 v3 提取器确实能读取真实响应。

## 可交付结果

`reading-source-samples-2026-10-19/catalog.json` 含 4 个可导入 v3 文档与 4 个需要处理的候选。每项保留独立身份、项目、原始规则链接、验证状态、限制说明；可导入项额外包含 `document` 与 `sample`。不能把这次采样结论解释为站点长期 SLA、所有作品均完整、付费内容可读，或桌面/Android 已验收。

| 候选 | 真实搜索 | 真实目录 | 真实内容 | 已知限制 |
|---|---:|---:|---|---|
| 酷我小说 | 10 本 | 1,651 章 | 第一章 3,326 字 | HTTP 可用；同路径 HTTPS 返回业务错误；仅测免费首章 |
| 阅友小说 | 5 本 | 779 章 | 第一章清洗后 3,399 字 | HTTP；正文 HTML 内有 5 个 `.con`，需全部拼接；部分内容提示客户端阅读 |
| 瓜子漫画 | 7 部 | 605 话 | 第一话 19 页；首图 HTTP 200、JPEG、224,815 字节 | 需桌面 UA；目录倒序；图片扩展名 `.webp` 但实际 JPEG，应以内容为准 |
| 咚漫漫画 | 2 部 | 首屏 9 话 | 免费第 3 话 41 页；首图 HTTP 200、JPEG、57,931 字节 | 目录分页；完整目录和付费话未测；图片需保留 Referer |

上表数目来自保存的真实响应与项目提取器回放，不是自建 mock。回放命令：

```powershell
go run reports/reading-source-samples-2026-10-19/verify_catalog.go
```

当前输出：

```text
legado-kuwo search=10 title=三国之仲谋天下 chapters=1651 contentCharacters=3326 author=拾一
legado-yueyou search=5 title=三国争锋 chapters=779 contentCharacters=3399 author=逆天吼
mihon-guazi search=7 title=斗罗大陆 chapters=605 pages=19
mihon-dongman search=2 title=神之塔 chapters=9 pages=41
```

此命令直接调用当前项目的 `provider.ParseDocument`、`ExtractItems`、`ExtractDetail`、`ExtractChapters`、`ExtractText` 和 `ExtractPages`。HTTP 采样使用 Python requests；本段回放验证提取器，未重跑产品完整服务网络链。另用 Pillow 完整解码两张首图成功：瓜子 JPEG 900×1667，咚漫 JPEG 690×1230，见 `image-decode-check.json`。

## 第一手来源与版本

- [XIU2/Yuedu](https://github.com/XIU2/Yuedu)：通过仓库原始 `shuyuan` 下载 22 项规则；`git ls-remote` 固定到 `426b24f59aa763ee0f2d9cc094612d2ef42c8293`。酷我、阅友规则依据见[固定原文件](https://github.com/XIU2/Yuedu/blob/426b24f59aa763ee0f2d9cc094612d2ef42c8293/shuyuan)。仓库自身明确说明不能保证第三方源可用、维护不积极；不能把 star 数量当站点质量。
- [aoaostar/legado](https://github.com/aoaostar/legado/blob/0dde287d9b0e8e650b02c99acc324855fc29b8b2/index.html)：核对聚合库的上游链接，抓取其公开全量 JSON 共 3,907 项用于筛选。该仓库是聚合者，不能据其旧 `respondTime` 宣称当前站点速度。全量文件没有留在项目中，仅保留 URL、大小、SHA-256 和数量，见 `source-provenance.json`。
- [keiyoushi/extensions-source](https://github.com/keiyoushi/extensions-source)：`git ls-remote` 固定到 `314a0d1b2b6b194f6a4f9aee252fec9ce55ae5ca`，读取中文扩展源码与 build 元数据。没有安装、执行扩展。
- [fqapp](https://github.com/ch6vip/fqapp)：网页核对当前原项目；本地研究 zip 记录的源码版本是 `9ada894abac9cc94fded76958c1c71dbf479a754`。读取 zip 内 `repository-provenance.json`、`source-examples.json`，并与已有 `01_fqapp_多媒体分发与请求链.md` 对照。原项目公开端点不等于无需设备会话和签名的稳定公共 API。

## 可直接表达的请求与规则

### 酷我小说（Legado）

基础地址 `http://appi.kuwo.cn`。搜索 `GET /novels/api/book/search?keyword={keyword}&pi={page}&ps=10`，结果 `data`；书籍字段 `title/author_name/cover_url/intro/book_id`。详情 `/novels/api/book/{book_id}`，根 `data`；目录 `/novels/api/book/{book_id}/chapters?paging=0`，数组 `data`；章节字段 `chapter_title/chapter_id/book_id`。正文 `/novels/api/book/{book_id}/chapters/{chapter_id}` 的 `data.content`。

规则的 URL 拼接使用 v3 选择器 `regex` 和 `concat`，无需执行原规则里仅做状态文本替换的 JS。实测样本作品《三国之仲谋天下》，书 ID `22543868000604102`，免费首章 ID `96825218742211052`。详情、目录与正文均收到 `code:200` 且结构正确。HTTPS 搜索响应 127 字节业务错误，不能仅因 HTTP 200 当作成功；本目录明确采用原规则的 HTTP 地址。

### 阅友小说（Legado）

基础地址 `http://m.suixkan.com`。搜索 `GET /s/1.html?keyword={keyword}&page={page}`；条目 `.v-list-item`，标题 `.v-title`，作者 `.v-author`，封面 `img@src`。条目 `onclick` 的 `newWebView('/b/235207.html', '', '')` 由明确的正则提取 URL，不执行脚本。

详情 `.face-info-title`、`.face-info .v-words span`、`.face-cover img@src`、`#intro`；目录入口 `.sumchapter a@href`。目录 `/c/235207.html`，`.catalog_ls li a`。首章 `/r/235207/235208.html` 一次响应内有 5 个 `.con`；v3 `concat` 的一个子选择器可以收集全部节点，再以换行连接。若仅用 `text.query=.con`，当前 `String` 会只取首段，造成约 80% 正文丢失。本样本明确覆盖此真实故障形态。删除“本章未完/本章完”提示，保留正文段落。

### 瓜子漫画（Mihon）

依据[固定扩展实现](https://github.com/keiyoushi/extensions-source/blob/314a0d1b2b6b194f6a4f9aee252fec9ce55ae5ca/src/zh/guazimanhua/src/eu/kanade/tachiyomi/extension/zh/guazimanhua/Guazimanhua.kt)。搜索 `GET https://www.guazimanhua.com/category.php?keyword={keyword}&sort=hits&page={page}`；条目 `article.card`，`h3 a`、`a.cover-wrap@href`、`img.cover@src`。详情 `div.mobile-comic-title`、`img.mobile-comic-cover@src`、`p.mobile-comic-desc`；目录 `section.mobile-comic-all-chapters div.mobile-chapter-grid a`，须反转。章节页 `section.reader-images img@src`。

样本《斗罗大陆》 `/comic.php?id=14917`，第一话 `/chapter.php?id=1668692`。首图返回 JPEG magic `ffd8ffe000104a4649460001`。扩展注释记录移动 UA 可能拿到下载 App 提示页；本样本使用桌面 UA 并确实取得 19 张图的地址。

### 咚漫漫画（Mihon）

依据[固定扩展实现](https://github.com/keiyoushi/extensions-source/blob/314a0d1b2b6b194f6a4f9aee252fec9ce55ae5ca/src/zh/dongmanmanhua/src/eu/kanade/tachiyomi/extension/zh/dongmanmanhua/DongmanManhua.kt)。搜索 `GET https://www.dongmanmanhua.cn/search?keyword={keyword}&page={page}`；结果 `#content > div.card_wrap.search ul:not(#filterLayer) li a`，标题 `p.subj`，链接当前 `href`，封面 `img@src`。详情 `h1.subj, h3.subj`、`#_asideDetail p.summary`；目录 `ul#_listUl li`，标题 `span.subj span`，链接 `a@href`，下一页 `div.paginate a[onclick] + a@href`。页图 `div#_imageList > img@data-url`。

搜索《神之塔》，作品 `/episodeList?titleNo=241` 重定向到 `/BOY/tower-of-god/list?title_no=241`。本次测试第 3 话 `episode_no=4` 的 41 页与首图。目录首屏有未连续的话号，不能据此声称所有话齐全；排序展示应单独标识“目录首屏实测，完整性待验证”。

## 需要解释性展示的候选

| 候选 | 验证/兼容状态 | 原因 |
|---|---|---|
| 69书吧、铅笔小说 | 请求失败 | 本机搜索 HTTP 403、挑战页面，不能作为可用源推荐 |
| 天天看小说 | 请求失败 | 本机搜索 HTTP 403 |
| 独步小说网 | 响应无效 | 搜索 HTTP 200，但为提示页，不是书籍列表 |
| 快书网 | 连接失败 | 本机严格 TLS 验证失败，未绕过证书校验 |
| 汉典古籍 | 规则过期 | 聚合库记录的小说分类 URL 本次 HTTP 404 |
| 读书网 | 旧搜索规则失效 | 旧 URL HTTP 200，但页面提示“请输入查询关键词”，未取得匹配列表 |
| 崩坏3官方漫画 | 待列表浏览适配 | `/book` HTTP 200、目录 `/book/1001/get_chapter` HTTP 200；[扩展明确没有搜索](https://github.com/keiyoushi/extensions-source/blob/314a0d1b2b6b194f6a4f9aee252fec9ce55ae5ca/src/zh/bh3/src/eu/kanade/tachiyomi/extension/zh/bh3/BH3.kt) |
| 泰拉记事社 | 待专用请求适配 | `/api/comic` 返回 HTTP 200 JSON；[原扩展](https://github.com/keiyoushi/extensions-source/blob/314a0d1b2b6b194f6a4f9aee252fec9ce55ae5ca/src/zh/terrahistoricus/src/eu/kanade/tachiyomi/extension/zh/terrahistoricus/TerraHistoricus.kt)在客户端过滤列表、按页号取图，图片 403 时重新请求签名地址 |
| fqapp 番茄小说/漫画 | 静态规则不可直接导入 | [上游 client](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/upstream/client.rs)涉及设备、签名和协议处理；[正文端点](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/content.rs)不等于明文公共 API；本次未声称完成全链路 |

## 排序的证据边界

建议按“应用当前可执行 + 最近全链路成功程度 + 用户当前本机测试结果 + 耗时”排序，失败/待适配置后，并展示具体失败阶段。不要给未经测试的源编造分数，不要把 HTTP 200、第三方旧 respondTime 或仓库 star 当成可读性证明。展示至少包括来源、可导入性、搜索/详情/目录/正文或图片各阶段、最近测试时间、耗时、作品/目录/页数样本以及限制。尚未测试应明确显示“未测试”，区别于失败。

样本目录不保留完整上游源库，也不携带其 Cookie/账号配置；两项原 Legado 规则保留了所需声明规则，删除登录与头配置，v3 文档使用新定义的 UA/Referer。图片仅验证响应签名与 MIME/大小，字节已移除；未缓存整本小说或整部漫画。保存响应文本是便于提取回放，不代表产品内容分发。
