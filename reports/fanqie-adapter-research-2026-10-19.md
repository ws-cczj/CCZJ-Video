# 番茄内置源适配：公开网页链路与 fqapp 源码核对

核对日期：2026-10-19。使用 research 技能；本次没有执行下载的源码/JS/APK，没有注册设备、添加账号或绕过收费。结论：**当前可用最小实现是 Go 专用 `fanqie-web` adapter：SDK 公共搜索 JSON → 官网详情/目录 JSON → 官网免费 reader HTML → 对网页公开字体作字符还原。无需用户另外部署 fqapp 服务。**

## 实测链路

采样用桌面 User-Agent、普通 HTTP GET，无 Cookie、签名、账号头。并非官方稳定 API 承诺；应保留超时、限流、鉴权/挑战页和规则变化错误。

| 操作 | 地址与数据 |
|---|---|
| 搜索 | `https://api.fanqiesdk.com/api/novel/channel/homepage/search/search/v1/?q={keyword}&offset={offset}&aid=13&app_name=news_article&version_code=708&device_platform=android`；`code=0`，`data.ret_data[]`，`data.offset`、`data.has_more` |
| 详情 | `https://fanqienovel.com/page/{bookId}`；HTML 中 `window.__INITIAL_STATE__=` 后可按 JSON 解码，不执行脚本；字段 `page.bookName/author/bookId/thumbUri/abstract/wordNumber/creationStatus/lastChapterTitle/chapterTotal/categoryV2` |
| 目录 | `https://fanqienovel.com/api/reader/directory/detail?bookId={bookId}`；`data.chapterListWithVolume` 是二维数组；依次 flatten，字段 `itemId/title/volume_name/realChapterOrder/firstPassTime/needPay/isChapterLock/isPaidPublication/isPaidStory` |
| 正文 | `https://fanqienovel.com/reader/{itemId}`；同样提取 INITIAL_STATE，读取 `reader.chapterData.content` HTML；同时检查 `needPay/isChapterLock/isPaidPublication/isPaidStory`，以及 `bookId/itemId/title/nextItemId/preItemId` |

搜索字段：`title` 可能带 `<em>` 高亮，应去标签；`author/book_id/category/abstract/thumb_url/genre/creation_status`。实际 `genre=4` 是音频，`genre=0` 是原创小说、`genre=6` 是出版物；目前可按两种小说白名单过滤，不把音频混入小说。作品 ID 应以十进制字符串保存。

`/search/关键词` HTML 仅有等待搜索结果占位，不宜用作纯静态搜索。`/api/author/search/search_book/v0/` 本次返回 HTTP 200 空响应，也不能作为成功路径。上表 SDK 搜索则对两个查询均取得真实结果。

### 样本一：部族荣光

- 搜索 `q=部族荣光`，10 项列表包含书 `7069948840148732967`，genre `0`。
- 详情 `/page/7069948840148732967` HTTP 200，196,564 字节。
- 目录 JSON HTTP 200，100,914 字节，351 章。
- 首章 `/reader/7069952706521236001` HTTP 200，58,457 字节；标题“第1章 部族生死战”，`needPay=0`、`isChapterLock=false`。
- 字符还原后首章 HTML 5,077 字符，未知私用区字符为 0；首句为“大乾皇朝，疆域亿万里。”。

### 样本二：三国搜索的首个非音频结果

- 搜索 `q=三国`，10 项结果，首项 genre `4` 必须跳过；首个非音频为 `7509794271109598233`，genre `6`。
- 官网实际标题“三国大全：三国演义”。前两章是“版权信息”“内容简介”，不能要求首个目录项必有千字内容。
- 第三章 `/reader/7509794302738844697` 为“第一回 宴桃园豪杰三结义 斩黄巾英雄首立功”，`needPay=0`、`isChapterLock=false`，内容 HTML 5,621 字符，真实可读。细节见 `fanqie-adapter-samples-2026-10-19/sanguo-chapter-0.json`。

## 字体转换：独立可复现的 362 字符映射

正文使用私用区 Unicode，页面 `common.css` / `@font-face` 声明字体 ID `dc027189e0ba4cd`。公开字体 `https://lf6-awef.bytetos.com/obj/awesome-font/c/dc027189e0ba4cd.woff` 的 `cmap` 只有 PUA，post 3.0；glyph 名称 `gid58344` 等本身不能还原中文。

但字体 name 元数据明确标明这是 **Adobe SourceHanSansSC-Normal 2.002** 子集，并含 **SIL Open Font License 1.1** 信息。下载 [Adobe 官方同版本 OTF](https://github.com/adobe-fonts/source-han-sans/blob/2.002R/OTF/SimplifiedChinese/SourceHanSansSC-Normal.otf)，对字体轮廓路径逐条精确匹配，可以自主导出全部 362 个 PUA → 普通 Unicode，不需要复制第三方字体表实现，也不需要运行 OCR。

复现：

```powershell
python reports/fanqie-adapter-samples-2026-10-19/derive_font_map.py
```

脚本用 fontTools RecordingPen 分解并记录路径，再对操作序列算 SHA-256，与 Adobe 字形匹配。结果 **362/362 精确匹配**；同形字符多映射时优先 ASCII，再基础 CJK，再最低码点，避免把汉字变成部首兼容字符。输出：

- `font-dc027189e0ba4cd-derived.json`：可直接嵌入的自主推导映射。
- `font-derivation.json`：源 URL、SHA-256、匹配方法、歧义决策和字体许可证元数据。
- `source-han-OFL.txt`：Adobe 同版本许可证原文，见[官方许可证](https://github.com/adobe-fonts/source-han-sans/blob/2.002R/LICENSE.txt)。

初步曾用 [404-novel-project 字体表](https://github.com/404-novel-project/fanqie_font_tables/blob/b76d07b95268a9cbbf24866d9f9a4874bb9a9453/dc027189e0ba4cd.woff2.json)验证可行性；最终表**不由该表复制生成**。独立推导还发现该表将部分小写字母误映为大写，以及“内/內”差异，故最终使用独立导出结果。

应用运行时应识别字体 ID；只有已知字体表才转换；新字体 ID 或转换后仍有 PUA，报“字体规则变化，需要更新适配”，不要将乱码作为已成功正文写缓存。不要对任意站点全局套这个表。无需把 16 MB Adobe 字体带入应用，运行时只需小映射与来源说明。

## fqapp 代码实际包含什么

原始项目 [ch6vip/fqapp](https://github.com/ch6vip/fqapp)，本地 zip 固定到 `9ada894abac9cc94fded76958c1c71dbf479a754`，根 [LICENSE](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/LICENSE) 是 GPL-3.0。下列是存在性核对和工程复杂度判断，不是“项目无法适配”：

- [sign/mod.rs](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/sign/mod.rs)及 `abogus/argus/ladon/gorgon/simon/sm3/protobuf` 有签名实现和向量测试。当前默认请求注入 `x-ss-req-ticket/x-khronos/x-argus/x-ladon`，旧 Gorgon 默认关闭。
- [device/register.rs](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/device/register.rs)含设备注册；[upstream/client.rs](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/upstream/client.rs)替换设备 ID、签名和设备失效替换。
- [book.rs](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/book.rs)的官网目录分支给 `bookId` 加 `a_bogus/msToken`；本次普通请求未带它们仍成功。不要把源代码写了签名等同于所有请求必需签名。
- [dhcontent.rs](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/dhcontent.rs)支持无设备池的 Signed DH 分支：`https://novel.snssdk.com/api/novel/book/reader/content/v1?aid=13&...&item_id=...`，请求 `y`，响应 `y/c` 与 `data.content` 交给 [upstream/dh.rs](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/upstream/dh.rs)处理。若网页链变化，可评估移植这条独立路径，不必先搬完整设备池。
- [content.rs](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/content.rs)还有设备绑定 AES 正文流程及 fallback；[manga.rs](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/manga.rs)处理漫画页面/图片协议。漫画未在本研究中完成可用免费上游链路，不能因为小说已通过就宣称番茄漫画已通过。

本次最小 Go adapter 不需要复制上述签名、设备或加解密实现。它借鉴 fqapp 的请求入口，解析公开响应，使用独立字体映射即可。若日后选择移植 fqapp 代码，应记录其 GPL-3.0 来源；这是源码许可证标识，不替代项目具体分发决策。

## 实施约束

专用适配仍复用项目现有网络守卫、响应容量/超时、context 取消和来源身份；允许访问的实际端点保持固定公开域名，不把任意导入 JSON 变成可访问内网的适配器。INITIAL_STATE 解码只接受 JSON；输出段落前先检查章节支付/锁定状态，再作已知字体转换，再剥除脚本和 HTML。保留真实的缺失、授权、限流、挑战页、字体变化错误；不要把它们变空结果。

内置源可以自动提供，源目录显示“番茄小说·公开网页”与当前全链路测试时间/结果。不要将本次可读免费章节外推成登录、付费或 App 独占作品均可读。
