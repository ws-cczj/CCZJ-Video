# API 请求、响应与字段流转规范

本章补足开发时需要的线级协议、请求体、响应体和排错信息。适用源码提交与前文一致；不要将“客户端能接受的形状”当成上游服务承诺的全部响应规范。

## 1. 先确定正在调用哪一层

| 标识 | 接口层 | 传输与基址 | 本章证据强度 |
|---|---|---|---|
| FQ-LOCAL | fqapp Flutter → Rust 业务接口 | 默认 FFI；HTTP 模式使用运行时 localhost 服务 | 从路由、封装器和 Dart 消费端还原 |
| FQ-UPSTREAM | fqapp Rust → 第三方服务器 | 多个 HTTPS host，可能需 provider 会话/签名 | 只确认源码构造；未在线抓取响应 |
| LG-CONTROL | Legado 应用本地控制接口 | 开启 Web 服务后，例如 `http://127.0.0.1:1234` | 控制器、实体、服务端序列化确认 |
| LG-SOURCE | Legado → 书源站点 | URL、headers、body、解析方式由书源决定 | 引擎语法确认；具体站点响应无统一规范 |
| MH-SOURCE | Mihon 宿主 → Source | Kotlin suspend 函数调用 | 参数/返回类型确认；不是 HTTP JSON 服务 |
| FIXTURE | 本研究包的合成演示 API | `http://127.0.0.1:8765` | 可运行、可抓取、可做契约检查 |

后文 JSON 标注“**源码推导示例**”表示字段来自代码、值为合成示例，绝不是伪装成真实抓包。标注“**演示实际响应**”的样例由本地服务自动采集。未确认的字段不列为必填。机器可读规范中保留 `additionalProperties`，允许上游增加字段。

### 通用编码规则

- URL/query：UTF-8、每个参数值单独编码；不要把整个 URL 编码，也不要先手工 `%xx` 再交给 query builder 二次编码。
- JSON：UTF-8；GET 示例均为**无请求体**，不能擅自将 query 改成 JSON body。
- 远端 ID：跨层保留字符串，特别是超出 JavaScript 安全整数范围的 ID；损失精度后再转字符串无法修复。
- `null`、字段缺失、空字符串、空数组分别处理；不能用 `value || fallback` 丢掉合法的 `0/false`。
- 时间值必须带已确认的单位。fqapp 音频 `video_duration` 是秒；Mihon `date_upload` 的类型是 Long，适配时仍需核实扩展与宿主的时间单位，不能仅凭字段类型推断。新项目建议统一毫秒。未明确单位的字段先保留原值。
- HTML、图片、音频文件不是 JSON envelope。先根据阶段和 Content-Type 决定解析方式。

## 2. FQ-LOCAL：传输、认证、响应外壳

### 2.1 FFI 请求结构

`RustBackendTransport.send` 将 URL 拆成下列调用参数。下面是**调用模型**，不是发给远端服务器的 JSON 请求体：[F36](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/backend_transport.dart#L233-L295)

```json
{
  "requestId": "fq-example-1",
  "method": "GET",
  "path": "/api/v1/search",
  "query": "query=%E5%AD%A6%E4%B9%A0&tab_type=3&offset=0&count=10",
  "body": [],
  "timeoutMs": 20000
}
```

`body` 为字节数组，空 GET 传空字节；`timeoutMs=0` 的含义由 bridge/runtime 决定，不要自行当作“立即超时”。返回 BridgeResponse 含 status、body 字节、contentType；transport 将 499 解释为取消，504 解释为超时。FFI 模式没有让 UI 直接注入上游请求头的参数。[F36](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/backend_transport.dart#L233-L295)

### 2.2 HTTP 模式

基址从运行时获取，不能假设任何机器都已监听 8080。可使用会话路径，或在无浏览器 cookie 的 HTTP transport 中加本次运行的 capability bearer：[F37](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/backend_transport.dart#L320-L359) [F10](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/server.rs#L82-L172)

```http
GET /api/v1/search?query=demo&tab_type=3&offset=0&count=10 HTTP/1.1
Host: 127.0.0.1:实际端口
Authorization: Bearer 本次运行的capability
Accept: application/json
```

`Accept` 是调用建议；源码 HTTP fallback 明确附加的是 Authorization。能力 token 由运行时提供，不能从文档生成。也可通过 `/_session/{capability}/...` 路径访问；资源相对路径拼接时保留该前缀。Host/Origin 检查仍可能先返回 403；401 表示缺少会话。localhost capability 与上游登录 token 不是同一凭据。[F37](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/backend_transport.dart#L320-L359) [F10](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/server.rs#L82-L172)

### 2.3 三种返回外壳必须分别识别

**A. `/api/*` Web bridge 成功外壳，由本地代码生成：**

```json
{"code":200,"message":"success","data":{"example":"业务数据"}}
```

**B. `/api/v1/*` 类型化路由成功：** dispatcher 直接返回 endpoint Value，不强制添加一层 data，也不统一改 code。可能是 `{"code":0,"data":...}`，可能直接是 `{"images":[...]}` 或 `{"video_url":"..."}`。必须按具体操作解析。[F38](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/json.rs#L98-L123) [F39](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/webui.rs#L25-L45)

**C. 错误：**

```json
{"success":false,"error":"缺少video_id参数"}
```

该示例对应 Rust 参数错误外壳，HTTP 400；内部错误为 500。兼容桥接的参数错误也可能是 `{"code":400,"message":"需要 source=番茄 和 query"}`。上游业务失败可能仍 HTTP 200，body 里的非 0/200 code 才表示失败；媒体解析器还会检查已知 data 嵌套中的失败。[F38](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/json.rs#L98-L123) [F39](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/webui.rs#L25-L45) [F40](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/api_client.dart#L1429-L1478) [F41](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/chapter_media.dart#L280-L329) [F53](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/dispatch.rs#L200-L245) [F54](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/error.rs#L1-L57)

推荐解码顺序：HTTP 状态 → JSON 顶层类型 → 顶层 code/success → 操作对应的外壳路径 → 已知内层错误 → 业务字段类型/存在性 → 内容是否可消费。不要递归寻找“第一个 URL”后就判成功。

源码比较的是数值 `0/200`；字符串 `"0"/"200"` 不与它们相等。兼容层是否包装和 Dart 是否接受也有不同检查，应保留原始类型作诊断，不能一律强制转换后隐藏协议变化。

## 3. FQ-LOCAL：逐接口请求与响应

### 3.1 搜索

| 字段 | 位置/类型 | 约束、默认与意义 |
|---|---|---|
| `query` | query string | 必需，非空；搜索词 |
| `tab_type` | query integer（线路上传文本） | UI 常用 1 综合、3 书籍、11 短剧、8 漫画、2 听书；不是自定义 MediaKind 枚举 |
| `offset` | query integer | 起始偏移；通常首屏 0 |
| `count` | query integer | ApiClient 标签搜索传 10；endpoint 未传时默认字符串 `0`，不要声称服务器默认总为 10 |
| `passback` | query string | endpoint 支持；缺省使用 offset；非公共客户端必需字段 |
| `search_type` | query string | `fanqie` 时选择另一上游；普通搜索默认 reading 分支 |

请求：`GET /api/v1/search?query=demo&tab_type=3&offset=0&count=10`，**无 body**。兼容请求为 `GET /api/search?source=番茄&query=demo&page=1`，无 body。兼容 page 一基换算 `*9`，ApiClient 标签搜索默认页换算 `*10`，不要混用。[F02](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/api_client.dart#L233-L343) [F03](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/webui.rs#L47-L169) [F04](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/search.rs#L34-L106)

**源码推导示例：类型化路径的一种上游兼容形状。**

```json
{
  "code":0,
  "data":{
    "search_tabs":[{
      "title":"书籍",
      "tab_type":3,
      "has_more":true,
      "next_offset":10,
      "data":[{"book_data":{"book_id":"900000000000000001","book_name":"示例书","author":"示例作者","thumb_url":"https://example.test/cover.jpg"}}]
    }]
  }
}
```

| 响应路径 | 消费类型 | 规则 |
|---|---|---|
| `data.search_tabs`，或直接 `search_tabs` | array | `payload.data ?? payload` 后查找；不是 `items` |
| `search_tabs[i].tab_type` | integer/可转换整数 | 过滤所请求标签；缺失时可按 title 匹配 |
| `search_tabs[i].data` | array | 搜索卡片，可能含 book_data/video_data 子层 |
| `has_more` | boolean/缺失 | Dart 只在实际 bool 时保留；`"true"` 不等价 |
| `next_offset` | 可转整数/缺失 | 优先保留服务返回值；未知不能推断“还有下一页” |
| `book_data.book_id` | 应保留 string | 下一步详情/目录所用 bookId |
| `series_id_str` | string/缺失 | 剧集的精确 ID，优先于可能已失真的数值 series_id |
| `video_id/vid` | string/缺失 | 单集 ID，不可直接代替 seriesId |

`data` 是卡片列表内的字段，又是响应外壳字段，应按层级取值。媒体模型是客户端兼容投影，不表示每张卡片都携带全部元信息。[F42](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/media_item.dart#L889-L964) [F43](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/media_item.dart#L138-L275)

### 3.2 书籍详情

请求：`GET /api/v1/books/{bookId}/detail`，或兼容 `GET /api/detail?source=番茄&book_id={bookId}&tab=小说`。bookId path/query 非空字符串；无 body。兼容 tab 听书会换 endpoint；类型化普通 books 路由不要假设自动知道当前 UI 标签。[F02](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/api_client.dart#L233-L343) [F03](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/webui.rs#L47-L169)

**源码推导的最小可消费响应投影：**

```json
{"code":0,"data":{"book_id":"900000000000000001","book_name":"示例书","author":"示例作者","abstract":"简介","thumb_url":"https://example.test/cover.jpg","word_number":12000,"serial_count":2,"tags":"学习,示例","creation_status":0}}
```

| 原字段 | 客户端字段/类型 | 缺失处理与排错 |
|---|---|---|
| `book_id` | bookId:string | 保留查询时身份；不可用标题补 ID |
| `book_name` | title:string | 缺失会变空，需诊断解析/版本变化 |
| `author_info` | 作者对象 | 优先结构化作者；否则回退 author/author_id |
| `abstract` | abstract:string | 不是 description 字段 |
| `detail_page_thumb_url`，后备 `thumb_url` | cover:string | 有优先级，允许为空 |
| `word_number` | wordNumber:int | 数字或可转数值字符串；不是正文长度 |
| `serial_count`，后备 `content_chapter_number` | serialCount:int | 不是现有目录实际长度的保证 |
| `creation_status` | creationStatus:int | 未知为 -1；勿与 Mihon status 共用枚举 |

`BookDetail.resolveDetailData` 接受 payload.data、payload 本身及多一层 data 包裹；这是一条具体兼容规则，不是任意递归解壳。[F44](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/book_detail.dart#L185-L243)

### 3.3 目录

请求：`GET /api/v1/books/{bookId}/directory`；兼容 `GET /api/directory?source=番茄&book_id={bookId}&tab=小说`。无 body。短剧分支可能触发 pseries fallback，返回结构与小说不同。[F05](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/webui.rs#L171-L232)

**兼容 bridge 由扁平列表转换后的源码推导示例：**

```json
{
  "code":200,"message":"success",
  "data":{
    "item_data_list":[{"item_id":"900000000000000101","title":"第一章","version":"v1"}],
    "data":{"chapterListWithVolume":[[{"itemId":"900000000000000101","title":"第一章","volume_name":"正文"}]]}
  }
}
```

注意这里确实可能是 `data.data.chapterListWithVolume`；bridge 在提取上游 data 后插入自己的 data 键，再加 web_ok 外壳。不要只看到重复 data 就擅自删层。类型化请求可能直接保留 `data.item_data_list`。`parseDirectory` 接受几种已知目录形状。[F45](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/webui.rs#L232-L294) [F46](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/media_item.dart#L629-L880)

| 字段 | 类型/意义 | 下游用途 |
|---|---|---|
| `chapterListWithVolume` | 卷数组；卷可以是章节数组或可识别卷对象 | 按卷渲染，不是平铺字符串 |
| `itemId/item_id/chapter_id/id` | 章节身份兼容键 | 转 Chapter.itemId，传到 content；ID 为空的条目会被过滤 |
| `title/chapter_title/name/...` | string 或指定标题对象 | 显示章节名；缺失有生成式后备标题 |
| `volume_name/volumeName` | string | 卷归属 |
| `item_data_list[i].version` | string/缺失 | 与同 ID 章节合并；不可在扁平化时丢弃 |
| `episodes` | 单集列表/缺失 | 剧集目录分支；注意禁播与预告标记 |

开发中应把“HTTP 失败”“成功但目录空”“字段形状不支持”“所有章节缺 ID 被过滤”分别记录。目录响应 schema 是可接受形状集合，不是保证所有字段总存在。[F46](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/media_item.dart#L629-L880)

### 3.4 小说正文

请求：`GET /api/v1/chapters/{chapterId}/novel`；无 body。path 是目录得到的章节 itemId，不能放 bookId。内部路由赋 `item_ids` 与 `api_type=novel`。[F09](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/router.rs#L23-L115) [F28](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/content.rs#L178-L201)

**源码推导的最小正文响应：**

```json
{"code":0,"data":{"content":"<p>第一段示例正文。</p><p>第二段。</p>","content_decrypted":true}}
```

| 字段 | 要求 | 不满足时 |
|---|---|---|
| `data` | object | 正文主路径不可消费 |
| `data.content_decrypted` | 必须是 boolean `true` | 字符串 `"true"`、缺失、false 都不能当成功 |
| `data.content` | string | 可能是可解析正文 HTML；不能假设纯文本 |
| 正文解析结果 | 非空 | 空正文报错 |

失败可走兼容文本路径 `GET /api/content?source=番茄&item_id={chapterId}&tab=小说`，仍无 body；该路径可能从 content/text/article_content/body 等已知键抽取字符串。**它不是与主路径完全等价的图文内容结构**，应用会将其记为不完整内容以便以后重试。[F07](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/api_client.dart#L1091-L1171) [F40](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/api_client.dart#L1429-L1478)

### 3.5 漫画内容与图片

请求：`GET /api/v1/manga/{chapterId}`；或兼容 `GET /api/content?source=番茄&item_id={chapterId}&tab=漫画`；无 body。typed route 将 path id 写入 item_ids。不要在 `/manga/{id}` 中传整部漫画的 bookId。[F19](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/router.rs#L316-L440) [F06](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/webui.rs#L371-L420)

**两种由本地 handler 产生、客户端能够消费的源码推导响应：**

```json
{"images":["/src/img_example_1.png","/src/img_example_2.png"]}
```

```json
{"content":"<img src=\"/src/img_example_1.png\"><img src=\"/src/img_example_2.png\">"}
```

兼容桥接时这些对象放进 `{code:200,message:"success",data:...}`。`images` 优先是字符串 URL 数组，**不能把本包 fixture 的 `pages:[{image:{url}}]` 原样喂给 fqapp 的 parseComicImages**。`show_html=1` 可切 HTML 形状；默认图片表。图片字节另行 GET，不带 JSON body。[F47](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/manga.rs#L3016-L3042) [F41](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/chapter_media.dart#L280-L329)

图片相对路径必须解析到运行时资源服务。实际图片 HTTP 响应应按 PNG/JPEG/WebP 等二进制处理；收到 JSON/HTML 错误页时不要继续交图片解码器并掩盖原始错误。客户端 ComicImage 能携带 headers，不代表每一种上游响应都提供这些字段。[F41](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/chapter_media.dart#L280-L329)

### 3.6 听书

请求：`GET /api/v1/audio/books/{bookId}/chapters/{chapterId}?tone_id=0`；两个 path ID 均必需，tone_id 为字符串/线路文本默认 0；无 body。Rust 上游转换为 book_id、item_ids、tone_id、req_type 等 query。[F22](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/services/api_client.dart#L377-L435) [F31](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/audio.rs#L86-L119)

**源码推导的普通未加密音频最小响应：**

```json
{"code":0,"data":[{"item_id":"900000000000000101","code":0,"main_url":"https://example.test/chapter.wav"}]}
```

另一个受支持形状的 `video_model` 是**包含 JSON 文档的字符串**，不是 JSON 对象：

```json
{"code":0,"data":[{"item_id":"900000000000000101","code":0,"video_model":"{\"media_type\":\"audio\",\"status\":10,\"video_duration\":1.25,\"video_list\":[{\"main_url\":\"https://example.test/chapter.wav\"}]}"}]}
```

| 字段 | 类型/约束 | 排错 |
|---|---|---|
| `data` | stream 数组 | 不是固定的 data.url |
| `item_id` | 可选身份匹配字段 | 存在且与请求不同则跳过 |
| stream `code/success` | 行内业务状态 | 行内失败优先于看似可用的 URL |
| `main_url/backup_url` | string/可选 | 校验可用 URL，不把空串当资源 |
| `video_model` | JSON string/可选 | 两次解析；错误类型不能当普通 URL 成功 |
| model `media_type` | 缺失或 `audio` | 明确其他类型跳过 |
| model `status` | 缺失或 10 | 其他状态视为业务失败 |
| model `video_duration` | 正有限 number，单位秒 | 显示/存储时转换一次为毫秒；`indate` 不是时长 |

存在受保护媒体元数据时，需要合法、匹配资源的授权处理；本文普通媒体示例不包含密钥或受保护内容的替代授权流程。字幕桥接 `/api/content?...tab=听书` 不能替代此接口。[F48](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/chapter_media.dart#L55-L169)

### 3.7 剧集详情、视频播放信息

详情：`GET /api/v1/series/{seriesId}`，无 body。消费端查 `data.video_data`，不是普通书籍的 `data.book_name`：[F49](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/lib/models/series_detail.dart#L130-L158)

```json
{"code":0,"data":{"video_data":{"series_id_str":"900000000000000201","series_title":"示例剧","series_intro":"简介","series_cover":"https://example.test/poster.jpg","episode_cnt":2,"series_play_cnt":10,"category_schema":"[{\"name\":\"学习\"}]"}}}
```

`category_schema` 也是嵌入 JSON 字符串。series_id_str 优先。单集选择从目录获得 videoId，而非拿 seriesId 直接播放。

播放信息：`GET /api/v1/videos/{videoId}?mode=stream`，无 body。`mode=stream` 在本地层是返回播放源；**它不是让业务接口返回 MP4 字节流**。Rust 才会另发 POST 视频模型请求。[F50](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/video.rs#L66-L132)

**本地 handler 确认产生的普通媒体源码推导示例：**

```json
{"video_url":"https://example.test/sample.mp4","key_hex":"","video_id":"900000000000000301","variants":[{"name":"高清 720P","width":1280,"height":720,"url":"https://example.test/sample.mp4","key_hex":""}]}
```

`variants[].name` 的实际标签由本地函数生成，上面的文字仅为显示示例；消费端不应按标签字符串反推解析类型。width/height 为像素，排序/清晰度还考虑短边。空 variants 可表示单流，不能等同无视频。key_hex 空串表示普通流示例；不要为缺失密钥伪造值。[F50](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/video.rs#L66-L132) [F51](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/video.rs#L452-L483)

未传 stream 时 handler 可先下载并返回本地 `/src/...` 资源，响应还可能带 video_info；解析不到资源时甚至可能透传上游原始 Value。调用方必须检查最终 video_url/可消费候选，不能仅按 HTTP 200 判播放成功。[F50](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/video.rs#L66-L132)

## 4. FQ-UPSTREAM：真实请求体与响应投影的边界

### 4.1 GET 类上游请求

搜索、详情、目录、正文、playinfo 等本章列出的读取请求主体都在 URL query；**没有 JSON 请求体**。FQ-LOCAL 的 `bookId/chapterId/tone_id` 等会在 Rust 转成上游 book_id/item_ids/tone_id。上游请求执行器在没有显式 method 时按“有 body → POST；无 body → GET”推导方法。[F52](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/base.rs#L29-L95)

header 集合由具体 endpoint 与 upstream client 共同形成。可确认 User-Agent、部分 Accept/Content-Type 等；设备会话与签名相关字段不应由公共 UI 自行拼接。主文的业务参数不是完整匿名请求规范，也不是可保证实时有效的上游开放文档。开发时应优先调用已经封装的 provider，而不是从主文复制 URL 后期待 curl 独立成功。

### 4.2 视频模型 POST 请求体

下列 body 字段直接来自 `fetch_phoenix_video_model`，videoId 值为合成。目标为主文中的 `/novel/player/multi_video_model/v1/`。Content-Type 按 JSON 请求头生成函数设置。[F32](https://github.com/ch6vip/fqapp/blob/9ada894abac9cc94fded76958c1c71dbf479a754/rust/src/endpoints/video.rs#L137-L195)

```json
{
  "biz_param":{
    "detail_page_version":0,"device_level":3,"disable_digg_stat":false,
    "need_all_video_definition":true,"need_mp4_align":false,
    "use_os_player":false,"use_server_dns":false,"video_platform":1024
  },
  "mixed_video_id_map":{"1004":["900000000000000301"]}
}
```

短剧模型 `/novel/player/video_model/v1/` 的 body 不同：

```json
{
  "video_id":"900000000000000301","content_type":1004,
  "biz_param":{
    "detail_page_version":0,"device_level":3,"disable_digg_stat":false,
    "disable_video_relate_book":false,"from_video_id":"",
    "need_all_video_definition":true,"need_mp4_align":false,"source":4,
    "use_os_player":false,"use_server_dns":false,"video_platform":3
  }
}
```

两者响应会经过模型候选、格式、编码支持等判断，再变成本地 video_url/variants。本文能确认的响应是“客户端解析会读取什么”，不能把某个上游响应分支写成服务端完整 JSON Schema。`raw`、已解析的 source 和本地播放源是三种不同数据。未抓取在线上游响应，因此没有伪造其必填字段/状态枚举/未知业务码。

### 4.3 字段不匹配时的定位方法

记录四个阶段的脱敏样本：FQ-LOCAL 请求 → 实际选中的 endpoint/分支 → 上游响应的状态与结构摘要 → 规范化结果。若 query 正确却目录为空，先检查取的是 item_data_list 还是 bridge 嵌套 data；若有音频 URL 却失败，检查 stream 的业务 code、item_id 匹配和 video_model 类型；若视频请求超时，确认是否漏传 mode=stream 导致进入下载分支。

## 5. LG-CONTROL：应用控制 API 的请求体与响应体

### 5.1 基址、序列化与错误

先开启 Legado 的 Web 服务。下文使用 `${LEGADO_BASE}=http://127.0.0.1:1234` 作为示例；运行在手机时 PC 的 localhost 不是手机 localhost，需要正确端口转发。服务接口外壳不是 fqapp 的 code：[L14](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/web/HttpServer.kt#L25-L112) [L22](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/api/ReturnData.kt#L1-L29)

```json
{"isSuccess":true,"errorMsg":"","data":""}
```

失败对象的已确认字段是 `isSuccess:false`、`errorMsg:string`；data 初值 null，序列化时可能被省略，消费端必须兼容缺失或 null：

```json
{"isSuccess":false,"errorMsg":"未找到源，请检查书源地址"}
```

业务失败通常仍由 HttpServer 的成功 HTTP 响应分支输出，不能只判断 status==200。大列表分支明确 application/json 且可分块传输；普通分支直接调用未显式指定 MIME 的响应工厂，不能在本报告中保证所有业务 JSON 的 Content-Type 都正确为 application/json。未知 API 路径还可能回落静态资源。客户端应按预期操作尝试 JSON 解码、验证 isSuccess，并拒绝把静态 HTML 当业务数据。[L23](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/web/HttpServer.kt#L25-L134)

### 5.2 导入单个书源：`POST /saveBookSource`

请求头建议 `Content-Type: application/json; charset=utf-8`、`Accept: application/json`。body 是 **BookSource 对象本身**，不是 `{data:...}`、不是 `[source]`，也不是表单 `postData=`。服务端读出的 `files["postData"]` 是 HTTP 框架解析变量，不是要求客户端创建名为 postData 的 JSON 字段。[L23](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/web/HttpServer.kt#L25-L134) [L24](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/api/controller/BookSourceController.kt#L1-L80)

```http
POST /saveBookSource HTTP/1.1
Host: 127.0.0.1:1234
Content-Type: application/json; charset=utf-8

{"bookSourceUrl":"http://127.0.0.1:8765","bookSourceName":"本地学习源","bookSourceType":0,"enabled":true,"enabledExplore":false,"searchUrl":"/v1/search?kind=novel&query={{key}}&page={{page}}&pageSize=10","ruleSearch":{"bookList":"$.data.items[*]","name":"$.title","bookUrl":"$.detailUrl"},"ruleBookInfo":{"name":"$.data.title","tocUrl":"$.data.chaptersUrl"},"ruleToc":{"chapterList":"$.data[*]","chapterName":"$.title","chapterUrl":"$.contentUrl"},"ruleContent":{"content":"$.data.text"}}
```

| body 字段 | 类型 | 必需性与意义 |
|---|---|---|
| `bookSourceUrl` | string | 服务端明确检查非空；source identity |
| `bookSourceName` | string | 服务端明确检查非空 |
| `bookSourceType` | integer | 默认 0；0 文本、1 音频、2 图片、3 文件 |
| `enabled/enabledExplore` | boolean | 开关；缺省值见实体，不用字符串 true/false |
| `header` | string/null | **包含 JSON 对象文本的字符串**；不是直接的 header object |
| `searchUrl` | string/null | 包含 URL 模板/可选 options；不等于书源标识 |
| `ruleSearch/ruleBookInfo/ruleToc/ruleContent` | object/null | 对应规则实体；完整规则见本包样例 |

例如 BookSource 顶层请求头写作 `"header":"{\"User-Agent\":\"LearningClient/1\"}"`，而 AnalyzeUrl options 内 `headers` 可以是对象，见下一节。两者不要混淆。源码会宽松反序列化，不代表字段任意类型都合法。[L06](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/data/entities/BookSource.kt#L31-L98) [L24](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/api/controller/BookSourceController.kt#L1-L80)

成功：`{"isSuccess":true,"errorMsg":"","data":""}`。成功 data 是空字符串，不能按 `if (!data)` 判失败。失败可能是“数据不能为空”“转换源失败”“源名称和URL不能为空”。导入会写应用数据库；本文只展示协议，未执行导入。[L24](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/api/controller/BookSourceController.kt#L1-L80)

### 5.3 批量导入与读取书源

| 请求 | body | 成功 data | 易错点 |
|---|---|---|---|
| `POST /saveBookSources` | BookSource 数组 `[source1,source2]` | 实际通过过滤并保存的源数组 | 不是单对象；部分无效源可能被跳过，需比较返回数量 |
| `GET /getBookSource?url={encodedSourceUrl}` | 无 | 一个 BookSource 对象 | url 是 source identity，不是书籍 URL |
| `GET /getBookSources` | 无 | BookSource 数组 | 空源列表在本快照返回业务失败“设备源列表为空” |

成功读取书源响应投影：

```json
{"isSuccess":true,"errorMsg":"","data":{"bookSourceUrl":"http://127.0.0.1:8765","bookSourceName":"本地学习源","bookSourceType":0,"enabled":true,"searchUrl":"/v1/search?kind=novel&query={{key}}&page={{page}}&pageSize=10"}}
```

以上投影省略其他实体字段；不能将读取结果缩减后又写回并覆盖原始复杂配置。[L24](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/api/controller/BookSourceController.kt#L1-L80)

### 5.4 书架、目录与正文

`GET /getBookshelf` 无 body，data 为应用本地 Book 列表。后续目录接口需要在应用中已存在的书籍与书源，不是一个通用“任意 URL 抓取”服务。

`GET /getChapterList?url={encodedBookUrl}` 无 body。url 为书籍 Book.bookUrl；已有章节用数据库，空则 refreshToc。**响应 data 本身就是 array**：[L15](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/api/controller/BookController.kt#L159-L225) [L25](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/data/entities/BookChapter.kt#L42-L58)

```json
{"isSuccess":true,"errorMsg":"","data":[{"url":"http://127.0.0.1:8765/v1/chapters/n1-1/content","title":"第 1 章","isVolume":false,"baseUrl":"http://127.0.0.1:8765/v1/items/n1/chapters","bookUrl":"http://127.0.0.1:8765/v1/items/n1","index":0,"isVip":false,"isPay":false}]}
```

| Chapter 字段 | 类型与语义 | 调用方处理 |
|---|---|---|
| `url` | string，远端章节地址 | 稳定身份的一部分 |
| `bookUrl` | string | 与请求书籍匹配；不要换成源地址 |
| `index` | int，章节序号 | 直接传 getBookContent.index；第一条可能是 0 |
| `title` | string | 展示 |
| `baseUrl` | string | 解析相对 URL 的上下文 |
| `isVolume` | boolean | 卷标题，不一定是可取正文的普通章 |
| `isVip` | boolean | 会员章节标记，不证明当前有权访问 |
| `isPay` | boolean | 实体注释为“是否已购买”，不是“需要付费”的反义推断 |
| `resourceUrl` | nullable string | 音频真实 URL，可能缺失 |
| `start/end` | nullable Long | 本地文件/章节位置，不拿来当网络分页页码 |

`GET /getBookContent?url={encodedBookUrl}&index=0`，无 body。这里 url **仍是 bookUrl**，不是 chapter.url。缓存/远端读取并处理后，成功 data 是**正文字符串**：

```json
{"isSuccess":true,"errorMsg":"","data":"第一段正文。\n\n第二段正文。"}
```

不能解析 `data.content`。参数 url 为空、index 缺失、书或章不存在、书源不存在和抓取异常都有错误消息；章节记录暂不存在时最多等待一段有限时间。不要误将此等待当成 HTTP 客户端永不超时。非数值 index 在此实现可能抛转换异常，调用方先校验为整数。[L15](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/api/controller/BookController.kt#L159-L225)

### 5.5 本地保存进度的 POST body

`POST /saveBookProgress`，JSON body 对应 BookProgress：[L26](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/data/entities/BookProgress.kt#L1-L21) [L27](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/api/controller/BookController.kt#L253-L277)

```json
{"name":"示例书","author":"示例作者","durChapterIndex":0,"durChapterPos":12,"durChapterTime":1700000000000,"durChapterTitle":"第 1 章"}
```

name/author 用于此版本数据库查书；durChapterIndex、durChapterPos 为整数字段；durChapterTime 为 Long，本示例使用毫秒时间戳；durChapterTitle 可 null。调用方应提供全部关键字段，不能依赖 Gson 对缺失字段的宽松默认。成功仍是 data 空字符串。这个 API 会更新本地进度，配置了 WebDAV 时还可能触发同步；它不是 FQ-LOCAL 的历史保存协议，也不是建议新系统照抄的全局身份方案。

### 5.6 搜索实际为 WebSocket

连接 `ws://设备地址:配置端口/searchBook`，文档示例端口 1235。建立连接后发一条**文本 JSON 消息**：

```json
{"key":"学习"}
```

该 handler 读取 Map<string,string> 的 key，搜索范围来自应用配置。它不是 `GET /searchBook?query=...`，也没有统一 HTTP POST 搜索 body。每次命中可能回一帧 **SearchBook 数组本身**：[L28](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/web/socket/BookSearchWebSocket.kt#L28-L97)

```json
[{"bookUrl":"https://example.test/book/demo","origin":"https://example.test","originName":"示例源","name":"示例书","author":"作者","tocUrl":"https://example.test/book/demo/chapters","type":0}]
```

SearchBook 的 coverUrl/intro/kind 等可缺失；不是 fqapp 的 book_data。搜索完成由正常关闭 frame 和 reason `Search finish` 表示，不是 JSON `{"done":true}`。无效消息和空 key 可返回纯文本错误后关闭；因此每一条消息都无条件 JSON.parse 也会报错。服务有 ping，客户端应使用具备标准 pong 行为的 WebSocket 实现。[L28](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/web/socket/BookSearchWebSocket.kt#L28-L97) [L29](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/data/entities/SearchBook.kt#L30-L63)

## 6. LG-SOURCE：URL options、请求体与解析规则

### 6.1 GET、form POST、JSON POST 的区别

下面是**引擎语法推导示例**，指向假想自有服务 `https://example.test`；本包 mock 只有 GET，未实现这些 POST，不应拿这些地址当现成站点接口。

GET 规则：

```text
/search?q={{key}}&page={{page}}
```

最终 wire 请求：`GET /search?q=demo&page=1`，无 body。

form 规则：

```text
/search,{"method":"POST","body":"q={{key}}&page={{page}}"}
```

最终 wire：POST、表单编码（body 类似 `q=demo&page=1`）。不要把该 body 当 JSON 发。JSON 规则：

```text
/api/search,{"method":"POST","headers":{"Content-Type":"application/json"},"body":{"query":"{{key}}","page":"{{page}}"}}
```

最终 body 模板会序列化为 JSON 字符串，如 `{"query":"demo","page":"1"}`。**这个例子的 page 仍是字符串**；源 API 若要求数值 1，规则须实际生成数值或由对应脚本构造，不能假定替换后自动变 number。源码 `getBody` 对对象序列化，对字符串直接使用。[L04](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/model/analyzeRule/AnalyzeUrl.kt#L140-L270) [L30](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/model/analyzeRule/AnalyzeUrl.kt#L682-L801)

options `headers` 可为对象或可解码为对象的字符串；BookSource 顶层 `header` 则是 string。只有 POST 被此 AnalyzeUrl 方法分支识别为 POST，不能宣称任意 method 例如 PATCH 都已兼容。charset、retry、webView 等改变执行行为，见原 Rule 引擎章节。[L04](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/model/analyzeRule/AnalyzeUrl.kt#L140-L270) [L30](https://github.com/CCSSNE/legado/blob/da17bb2bed44f30b12a524c2457e32a20b16fa41/app/src/main/java/io/legado/app/model/analyzeRule/AnalyzeUrl.kt#L682-L801)

### 6.2 响应没有统一 envelope，要看书源规则

例如自有服务返回：

```json
{"result":{"books":[{"id":"demo","title":"示例书","detail":"/book/demo"}]}}
```

对应规则可以用 `bookList=$.result.books[*]`、`name=$.title`、`bookUrl=$.detail`。若实际返回 HTML，则应改 CSS/XPath 等解析；不能对所有书源套 `data.items`。这是“站点原始响应 → 规则提取 → SearchBook/Book/Chapter/正文”的链，不是“站点直接实现 Legado ReturnData”。

正文响应若为登录页、机器人验证页、JSON 业务失败，HTTP 200 也不能判成功。新规则适配层应先执行内容类型和站点业务检查，失败时保留脱敏的 status、最终 URL、前几层 key 与规则路径，避免最后仅显示“正文为空”。

## 7. MH-SOURCE：方法契约与站点 HTTP 响应不要混为一谈

### 7.1 宿主的真实输入/输出

| 方法 | 入参 | 返回 | 关键含义 |
|---|---|---|---|
| `getSearchManga(page,query,filters)` | Int、String、FilterList | MangasPage | mangas 数组与 hasNextPage；filter 结构由 Source 定义 |
| `getMangaUpdate(manga,chapters,fetchDetails,fetchChapters)` | SManga、旧 SChapter 列表、两个 Boolean | SMangaUpdate | manga 和 chapters 合并更新；未请求的部分可保留旧值 |
| `getPageList(chapter)` | SChapter | List<Page> | 按返回顺序读取；宿主可重建 index |
| HttpSource `getImageUrl(page)` | Page | String | 补全最终图片 URL |
| HttpSource `getImage(page,existingSize)` | Page、已有长度 | HTTP Response | 二进制图片，不是 JSON |

这些不是发送到站点的 JSON body。Source id、lang、supportsLatest 与过滤器是宿主能力；站点请求如何构造由扩展方法决定。[M01](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/Source.kt#L14-L106) [M02](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/CatalogueSource.kt#L21-L47) [M04](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/online/HttpSource.kt#L162-L198) [M05](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/online/HttpSource.kt#L243-L357) [M06](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/online/HttpSource.kt#L365-L420)

### 7.2 返回对象字段（JSON 仅作跨语言表示）

**MangasPage 序列化表示样例，不代表某个 Mihon HTTP URL 会返回它：**

```json
{"mangas":[{"url":"/manga/demo","title":"示例漫画","thumbnail_url":"https://example.test/cover.jpg","artist":null,"author":"作者","status":1,"description":null,"genre":"学习, 示例","initialized":false,"memo":{}}],"hasNextPage":true}
```

SManga 字段：url/title 为 string；thumbnail_url/artist/author/description/genre 可 null；status 为整数枚举 0 UNKNOWN、1 ONGOING、2 COMPLETED、3 LICENSED、4 PUBLISHING_FINISHED、5 CANCELLED、6 ON_HIATUS；initialized 为 Boolean；memo 为内部 JSON 对象。另有 update_strategy 枚举，跨语言映射时需显式处理，不要当成服务端普通业务字段。[M23](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/model/SManga.kt#L8-L74) [M25](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/model/MangasPage.kt#L3-L19)

SChapter：

```json
{"url":"/chapter/demo-1","name":"第 1 章","chapter_number":1.0,"scanlator":null,"date_upload":0,"memo":{}}
```

url 是身份关联依据，chapter_number 是 Float，不可当唯一主键；date_upload 是 Long，源如何给时间由扩展决定；scanlator 可 null。SMangaUpdate 的模型是 `{"manga":SManga,"chapters":[SChapter]}`，并非带 code/data 的统一网络响应。[M24](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/model/SChapter.kt#L8-L46) [M26](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/model/SMangaUpdate.kt#L1-L4)

Page 的跨语言表示：

```json
[{"index":0,"url":"https://example.test/view/page1","imageUrl":null},{"index":1,"url":"","imageUrl":"https://example.test/images/page2.jpg"}]
```

第一项要再解析 page.url 才拿到图片；第二项已有最终资源 URL。status/progress 是加载状态，不应从站点 JSON 盲目透传覆盖。[M20](https://github.com/mihonapp/mihon/blob/deeb9b2584b6e246135cbd31da49a39e6b6e1e52/source-api/src/main/kotlin/eu/kanade/tachiyomi/source/model/Page.kt#L11-L57)

### 7.3 一个新扩展如何适配本包演示协议

这是**新实现建议**，不是声称 Mihon 已内置该扩展：

| 扩展步骤 | 本包 HTTP 请求 | 从响应取什么 | 转宿主类型 |
|---|---|---|---|
| 搜索 | `GET /v1/search?kind=manga&query={q}&page={p}&pageSize=10` | `data.items[]`，data.hasNextPage | items.detailUrl → SManga.url；title → title |
| 详情 | `GET {manga.url}` | data.title/author/description/coverUrl | SManga；设置 initialized |
| 目录 | `GET {data.chaptersUrl}` | data[]，contentUrl/title/order | SChapter.url/name；稳定顺序 |
| 页表 | `GET {chapter.url}` | data.pages[] | image.url 解析为绝对 imageUrl；index 按列表顺序 |
| 图片 | `GET {resolved imageUrl}` | 二进制 PNG | HttpSource getImage/图片读取 |

每一步 fixture GET 都无 body。实际站点 extension 也可能用 POST/GraphQL、HTML 或不同分页；需要分析那个扩展才能给出真实 wire 请求体，宿主 core 没有足够信息生成所有站点的响应 schema。

## 8. FIXTURE：可直接实现的 HTTP API 规范

完整机器可读规范在 `api-contracts/fixture.openapi.json`，实际采样在 `evidence/api-captures.json`。示例均绑定本地合成服务；没有第三方登录条件。

### 8.1 通用请求与错误

`GET` / `HEAD`，无请求体；UTF-8 JSON；基址 localhost:8765。JSON 成功 `{data:T}`，错误 `{error:{code,message}}`，**没有 code=0 或 isSuccess**。所有相对 detailUrl/chaptersUrl/contentUrl/image.url/resource.url 都以服务基址解析。

| 条件 | HTTP | body |
|---|---:|---|
| 未知 kind、page<1、pageSize 不在 1–100、数值参数解析失败 | 400 | `{"error":{"code":"BadRequest","message":"参数格式错误"}}` |
| 条目/章节/资源不存在 | 404 | error.code 为 NotFound，message 随阶段不同 |
| 无效/不可满足的资源 Range | 416 | **空 body**，`Content-Range: bytes */{length}`；不是 JSON error |
| 视频文件尚未提供 | 200（内容描述接口） | data.available=false、variants=[]、reason 解释 |

mock 未提供任意 path 的 POST 接口，BaseHTTPRequestHandler 对未支持方法会产生框架错误，不能当成规范化 JSON。下面只承诺列出的 GET/HEAD 行为。

### 8.2 搜索

`GET /v1/search?kind=novel&query=&page=1&pageSize=10`

| query | 类型 | 缺省 | 约束 |
|---|---|---|---|
| kind | string | 空，表示全部 | novel/manga/video/audio；非空非法值 400 |
| query | string | 空 | 标题 casefold 子串；URL 编码 |
| page | integer | 1 | 一基，>=1 |
| pageSize | integer | 10 | 1–100；服务器严格检查 |

**演示响应形状（值由本地实际服务提供）：**

```json
{"data":{"items":[{"id":"n1","kind":"novel","title":"本地小说示例","author":"学习样本","chapterCount":2,"providerId":"fixture","detailUrl":"/v1/items/n1","chaptersUrl":"/v1/items/n1/chapters","coverUrl":"/assets/panel/n1-1/0.png","description":"仅包含本地合成数据，无第三方内容。"}],"page":1,"pageSize":10,"total":1,"hasNextPage":false}}
```

items 中上述字段均由此 mock 确定输出。正常无结果仍 HTTP 200，items=[]、total=0、hasNextPage=false。下一页的参数由调用方传 page+1；此分页语义不能移植到 fqapp offset 或 Mihon 外部源后不作转换。

### 8.3 详情与目录

`GET /v1/items/n1`：`data` 是与搜索卡相同的完整演示 Item。读 `data.chaptersUrl` 发下一请求。

`GET /v1/items/n1/chapters`：

```json
{"data":[{"id":"n1-1","itemId":"n1","order":0,"title":"第 1 章","contentUrl":"/v1/chapters/n1-1/content"},{"id":"n1-2","itemId":"n1","order":1,"title":"第 2 章","contentUrl":"/v1/chapters/n1-2/content"}]}
```

id/itemId/title/contentUrl 是 string，order 是零基非负 integer。目录不分页；没有 next 字段。使用 contentUrl 而非自行推测路径，能够避免后续服务改路由时 UI 随处改字符串。

### 8.4 四种内容响应

`GET /v1/chapters/n1-1/content`：

```json
{"data":{"kind":"text","chapterId":"n1-1","text":"第 1 章：这是本地生成的正文。\n\n……","blocks":[{"id":"n1-1:p0","text":"第 1 章：这是本地生成的正文。"}]}}
```

此处正文示例为节选，实际 blocks 有三项，完整值见自动采样；text 与 blocks 都是正文表示，不把 `kind=text` 转回 Item.kind=novel 字符串覆盖原条目。

`GET /v1/chapters/m1-1/content`：

```json
{"data":{"kind":"images","chapterId":"m1-1","pages":[{"index":0,"image":{"url":"/assets/panel/m1-1/0.png","mime":"image/png"}},{"index":1,"image":{"url":"/assets/panel/m1-1/1.png","mime":"image/png"}},{"index":2,"image":{"url":"/assets/panel/m1-1/2.png","mime":"image/png"}}]}}
```

`GET /v1/chapters/a1-1/content`：

```json
{"data":{"kind":"audio","chapterId":"a1-1","voiceId":"synthetic-tone","resource":{"url":"/assets/tone.wav","mime":"audio/wav"},"durationMs":1000}}
```

`GET /v1/chapters/v1-1/content`，未提供 MP4 时：

```json
{"data":{"kind":"video","chapterId":"v1-1","available":false,"reason":"请提供自己的 examples/assets/sample.mp4。","variants":[]}}
```

有自有 sample.mp4 时，available=true、reason=null、variants=[{id:"local",label:"本地文件",format:"mp4",resource:{url:"/assets/sample.mp4",mime:"video/mp4"}}]。客户端按 available 分支处理；文件存在仅证明可请求，未验证其解码能力。kind=text/images/audio/video 是内容判别联合，不能通过是否存在 `url` 猜类型。

### 8.5 二进制资源、HEAD 与 Range

```http
GET /assets/tone.wav HTTP/1.1
Host: 127.0.0.1:8765
Range: bytes=0-9
```

```http
HTTP/1.0 206 Partial Content
Content-Type: audio/wav
Accept-Ranges: bytes
Content-Range: bytes 0-9/16044
Content-Length: 10

此处是10字节二进制，不是JSON
```

16044 是当前合成 WAV 的实际长度，不是所有音频长度。无 Range 时返回 200 与完整资源；支持 start-end、start-、-suffix，后者需正长度；多范围不支持，返回 416。HEAD 沿用对应 GET 状态/头但无 body；JSON HEAD 也无 body，不可 JSON.parse 空串。PNG/WAV/MP4 body 按 bytes/blob/stream 读取，不做 UTF-8 解码。

## 9. 串联调用：字段从上一响应传到下一请求

### 9.1 可运行的 PowerShell 链

先启动本包 mock，再运行；GET 均不加 body：

```powershell
$base = 'http://127.0.0.1:8765'
$q = [Uri]::EscapeDataString('示例')
$search = Invoke-RestMethod "$base/v1/search?kind=novel&query=$q&page=1&pageSize=10"
if ($search.data.items.Count -eq 0) { throw '没有搜索结果' }
$item = $search.data.items[0]
$detail = Invoke-RestMethod ($base + $item.detailUrl)
$toc = Invoke-RestMethod ($base + $detail.data.chaptersUrl)
$chapter = $toc.data[0]
$content = Invoke-RestMethod ($base + $chapter.contentUrl)
if ($content.data.kind -ne 'text') { throw '内容类型与阅读器不匹配' }
$content.data.blocks | ForEach-Object { $_.text }
```

这是 fixture 的相对 URL 约定。真实 provider 中返回绝对 URL、不同域名、带会话前缀的 URL 时，使用 URL resolver 和 provider 网络上下文，不能一律 `$base + url`。

### 9.2 三套协议的最短字段链

```text
fqapp 小说：
search_tabs[].data[].book_data.book_id
 → books/{bookId}/detail、books/{bookId}/directory
 → 目录 itemId/item_id
 → chapters/{chapterId}/novel
 → data.content_decrypted + data.content

Legado 本地控制：
WebSocket 搜索批次中的 bookUrl/origin
 → 先由应用正常保存/加入书架，保留书源
 → getChapterList?url=bookUrl
 → data[].index
 → getBookContent?url=同一个bookUrl&index=返回的index
 → data（正文 string）

Mihon：
Source.getSearchManga → MangasPage.mangas[].url
 → getMangaUpdate → chapters[].url
 → getPageList → Page.imageUrl 或 Page.url 解析
 → 实际图片字节
```

不要省略 Legado 的书籍持久化前提；单有搜索结果并不保证本地控制正文接口已具备 Book 记录。不要给 Mihon 宿主方法硬编一套未经实现的 REST URL。

## 10. 开发排错表

| 现象 | 首查证据 | 处理方向 |
|---|---|---|
| fqapp 连接拒绝 | 是否启用了 HTTP 服务，默认是否仅走 FFI | 使用正确 transport，不凭文档固定端口 |
| fqapp HTTP 401/403 | capability、Host、Origin、运行时 token 是否更新 | 读取当前会话配置；不是靠改上游参数解决 |
| HTTP 200 但解析失败 | body 顶层类型、code/success/isSuccess | 先处理业务错误和协议层差异 |
| 搜索明明有内容却列表空 | search_tabs/data/book_data 与 items 的层次 | 按对应 parser 取值；别混入 fixture schema |
| 翻页重复/漏页 | page、offset、count、next_offset、passback | 独立维护来源分页，不混合 9/10 页大小 |
| 数字 ID 末位变化 | 原始 JSON 是 string 还是 number | 保留 string；已失真须重新获取精确字段 |
| 详情成功但目录全空 | bookId/chapterId 混用；itemId 缺失；data 层数 | 输出键摘要与过滤条数，定位 shape |
| 正文有 content 却显示不可用 | content_decrypted 类型与值；content 是否 string | 检查受支持 provider 输出；不要强制 true |
| 漫画拿到 URL 却无法显示 | 相对 URL、capability、headers、实际 MIME | 查看资源请求，识别登录 HTML/JSON 错误 |
| 音频返回数组却被当 map | data 的类型，item_id/stream code | 按流表解析，不读 data.url |
| 音频时长差 1000 倍 | video_duration 秒与 positionMs 毫秒 | 在单一转换点换算 |
| 视频解析接口长时间等待 | mode 是否 stream | 非 stream 可进入下载分支；区分解析与资源 |
| Legado POST 返回转换源失败 | 对象/数组、Content-Type、JSON 编码 | 单源对象、多源数组；不要发外壳或 form |
| Legado 保存成功却被报错 | isSuccess=true，data="" | 按 isSuccess，不按 data truthiness |
| Legado 正文接口未找到 | Book/Chapter 是否已入库，url 是否书籍 URL | 先正常建立书架/目录，传返回 index |
| WebSocket JSON.parse 报错 | 文本错误帧或 close frame | 区分数组结果、文本错误与关闭事件 |
| 下载文件变大且无法播放 | Range 状态、Content-Range、ETag/版本 | 200 返回时不能盲目 append；规范校验后续传 |
| HEAD 报 JSON 空响应 | 请求方法是否 HEAD | 仅查状态/头，跳过 body 解码 |

排错记录建议格式：requestId、接口层、provider/version、阶段、method、脱敏 path/query、HTTP status、response Content-Type、body 字节数、顶层 key、业务码、失败字段 JSONPath、耗时、是否 fallback。响应样本只保留解决问题必需部分，过滤 token/Cookie/Authorization/capability；外部 AI 不需要真实账号凭据。

## 11. 机器可读交付物与验证范围

- `api-contracts/fixture.openapi.json`：本地演示全部业务 GET/HEAD 与资源 Range 的规范。
- `api-contracts/fqapp-read.openapi.json`：fqapp 核心读取路由；响应是消费端投影/已知分支，不是未经验证的完整上游服务承诺。
- `api-contracts/legado-control.openapi.json`：书源导入/读取、目录、正文、进度的 HTTP 契约；WebSocket 独立见本章。
- `api-contracts/source-examples.json`：本章源码推导样例与出处标识；合成值，未冒充在线抓包。
- `api-contracts/requests.postman_collection.json`：三个环境分文件夹的请求模板；FQ/Legado 需用户自己的正在运行实例。本次未执行本地配置写接口。
- `evidence/api-captures.json`：本次临时端口 mock 的真实请求/响应样本，包括二进制长度与摘要。
- `evidence/api-contract-verification.json`：契约检查结果；不等于外部上游 API 在线验证。

已有三客户端的真实网络行为仍未实机验证。后续如果问题发生在具体外部书源/扩展，需提供脱敏后的实际请求、状态、响应样本和对应规则/扩展版本，才能从“宿主消费端投影”继续细化为那个站点的确定协议。
