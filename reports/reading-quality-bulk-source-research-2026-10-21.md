# 阅读源批量候选采集与调度边界

采集时间：2026-10-21 15:27 UTC（北京时间 23:27）。本报告记录真实下载的配置元数据，以及它们能支持何种检测。没有下载 APK/JAR，没有执行书源 JavaScript，也没有把“首页能访问”计为章节质量通过。

## 本次实际取得的输入

可复跑脚本：[collect-reading-source-catalog.py](../scripts/collect-reading-source-catalog.py)。原始下载和调度清单：[catalog-inputs/manifest.json](source-quality/catalog-inputs/manifest.json)。主程序读取 `catalogs`，`attempts` 保留所有上游请求、HTTP 状态、最终 URL、时间、字节数和 SHA256。

| 输入集合 | 下载结果与结构 | 可调度内容 | 不能据此推断的内容 |
| --- | --- | --- | --- |
| [aoaostar 全量](https://raw.githubusercontent.com/aoaostar/legado/master/sources/b778fe6b.json) | 20,987,130 bytes，3907 个顶层 Legado 对象 | 小说3715条、漫画98条，其余94条类型不属于本次小说/漫画 | 规则条目不等于独立站，名称中的“优质”或标签不构成质量证据 |
| [XIU2 自用书源](https://raw.githubusercontent.com/XIU2/Yuedu/master/shuyuan) | 163,824 bytes，22个 Legado 对象 | 22条小说配置，作为另一维护集合交叉核对 | 与全量集合重合的配置不重复算网站 |
| [Keiyoushi 索引](https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.json) | 1,500,553 bytes，现代 `extensionList.extensions` 包含1444个扩展 | 展开为2432条漫画 source 元数据 | 索引不包含 Kotlin 抽取实现；尚未适配的条目不能进行章节质量实测 |
| [aoaostar release](https://raw.githubusercontent.com/aoaostar/legado/release/sources/b778fe6b.json) | 与 master 的 SHA256 完全相同 | 留下载证据，`duplicateOf=aoaostar-all`，不重复进入 `catalogs` | 分支或镜像不增加站点数量 |
| [Yūzōnō](https://raw.githubusercontent.com/yuzono/manga-repo/repo/index.json) | HTTP 200，但只含 `Please migrate to Keiyoushi` | 标记 `migration_notice`，排除调度 | 返回 JSON 不代表仍存在内容源库 |
| [Suwayomi](https://raw.githubusercontent.com/suwayomi/tachiyomi-extension/repo/index.min.json) | HTTP 200，只含一个空 `baseUrl` 服务器连接器 | 标记 `user_configured_server`，排除调度 | 用户自行配置的服务器不算新发现的漫画站 |

规范化后保存了6244个完全去重的规则/源身份变体：小说3714、漫画2530；它们涉及3125个规范化主机。这里的“主机”仅表示小写 hostname、删除开头 `www.`，没有擅自把不同子域合并，也没有把主机数声称为独立内容提供者数。小说变体数比某一集合的原始条目数更少，是原集合自身及集合间存在完全重复配置。

原始 JSON 的 SHA256（其余请求属性见 manifest）：

| 文件 | SHA256 |
| --- | --- |
| `aoaostar-all.json` | `a50c80e81e45a6ad77f6cdb94ab420e74de29691f8924692415a3e9f0e022747` |
| `xiu2.json` | `00644eda44f0fcabcfc015a024480376638d5e7a381a2010aeedfca5aa797845` |
| `keiyoushi.json` | `dd561ad8bec64007c3a1dc385ecde8635a0797283cbd3d67ec0d46f20856a9ca` |
| `yuzono.json` | `7377b396364c7769be8b5ec94625445c3dcc502d10a09cdac5ff6e6909b54248` |
| `suwayomi.json` | `936397db3619f66027185f8908d6289c80cb4926e9d342dfd5d907d21c546496` |

本次六个请求都成功取得 JSON，没有失败请求；脚本会把 HTTP、超时、大小超限、JSON 格式错误写入 `attempts`，失败输入不会进入调度清单。重复内容文件仍保留原始字节以支持核对，但不重复计入候选。

## 格式与实际适配障碍

### 小说与 Legado 漫画：配置不是单一 CSS 格式

[XIU2 项目说明](https://github.com/XIU2/Yuedu)明确描述书源需要提取搜索、详情、目录和正文，而不是一个网站根地址。[aoaostar 仓库](https://github.com/aoaostar/legado)维护的集合包含多种来源和规则能力。本次下载直接出现下列障碍；标记可重叠，不能把各列相加当成条目总数。

| 集合／类型 | JavaScript 标记 | POST／URL选项 | JSONPath | 有状态取存 | 缺搜索地址 | 缺操作对象 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| aoaostar 小说 | 1532 | 1282 | 760 | 400 | 691 | 504 |
| aoaostar 漫画 | 88 | 19 | 53 | 16 | 4 | 1 |
| XIU2 小说 | 17 | 14 | 5 | 0 | 0 | 0 |

登录流程分别出现在355条小说、10条漫画、4条XIU2规则中。XPath 标记出现在40条全量小说和4条XIU2规则中。部分 `bookSourceUrl` 本身是标签、非法地址或动态表达式：全量小说70条、漫画2条不具有可直接探测的合法HTTP(S)主机。

仅做保守显式标记筛选后，**666/3715（17.9%）小说、6/98（6.1%）漫画、3/22（13.6%）XIU2配置**没有命中上述障碍。这个比例仅用于决定先花精力评估哪些规则，**不是自动转换成功率、可用率或高质量率**：

- 标记扫描未验证 CSS 语法、正则兼容、字段完整性、路径模板和漫画图片选择器；真正的 Go `convertLegado`／`provider.ParseDocument` 还会拒绝一部分。
- 相反，脚本若仅用于与搜索无关的发现页，也可能被保守筛选标记；需要按实际操作依赖重新分析，不能因为配置含 `jsLib` 就断言整站无法适配。
- API JSON 规则不应硬转为 HTML/CSS。静态 JSON API 可低成本新增声明式适配；动态签名、字体解码、登录和验证码需要单独适配与权限处理。
- 漫画的正文有时是图片URL数组，有时需要解包、懒加载属性或携带 Referer；小说正文则需要段落、净化和分页合并，二者的内容质量检测应分开。

### 漫画扩展：现代元数据字段发生了变化

[Keiyoushi 当前索引](https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.json)的结构为：

```json
{
  "extensionList": {
    "extensions": [{
      "packageName": "...",
      "versionCode": "...",
      "contentWarning": "CONTENT_WARNING_NSFW",
      "sources": [{"id": "...", "name": "...", "language": "zh", "homeUrl": "https://..."}]
    }]
  }
}
```

不可只读取旧字段 `pkg`、`baseUrl`、`lang`、`nsfw`。本次2432条展开项中，633条明确标为 NSFW，828条明确标为 SAFE，其余971条警告字段未知或未给出已识别值。**未知不等于安全。** 标记 SAFE 也只表示维护者声明，不替代对检测样本的检查。

`language` 的 `zh` 有78条，另有 `zh-Hans`、`zh-Hant`、`zh-CN`、`zh-TW`、`zh-HK` 等。语言版本和同站变体应保留以便按用户语言匹配，但不能当成不同独立网站增加覆盖率。后续为中文作品找源时，可先使用语言和题材标签缩小候选，再检测同作品。

现代扩展提供的是 APK/JAR 资源地址及站点身份，**不是可直接转换成 CCZJ 规则的抽取代码**。此阶段对尚无适配器的2432条项，能够做格式识别、主机连通性、域名迁移和候选映射；不能宣称已经验证目录、图片质量或无广告。下载或执行扩展不属于本次采集过程。

## 数千条调度时应保留的证据边界

建议按照成本逐层推进，同一个总容量为5的调度池中分配小说和漫画任务，不为两种模式各开5个池而使总并发变成10。

1. **线性静态归一化**：源 JSON 按 `kind + 规范化URL + 规则指纹` 用哈希去重，累计所有来源证据；同 SHA256 的集合不重复解析。处理成本为输入总字节数与条目数的线性量级。
2. **按主机合并连通性探测**：同一主机只做一次初筛，保存请求 URL、重定向、状态和时间；应用已有 SSRF 与 TLS 判据。配置变体仍独立保留，不能靠相同主机合并不同正文规则。
3. **按语言／题材选择少量锚点**：作品基准可很大，但每个源不应与所有作品两两做模糊比较。预先建立规范书名、别名、作者和作品类型的索引，搜索结果只与相应候选桶比较；无命中时轮换少量不同题材和冷／热作品。
4. **已适配且能搜索的源才进入内容匹配**：核对同名作者、小说与改编漫画、重制版与单行本、卷／话／番外，确认目录分页，再抽首中末。漫画还需校验图片真实格式、尺寸、失效和广告页；小说需正文段落、广告行、乱码及重复片段。
5. **增量更新证据**：缓存以源规则指纹、基准版本和检查时间为键。对未变更规则重用未过期结果；对429使用Retry-After，对超时保留不确定结果，不把瞬态失败永久沉底。

“主机返回200”至多证明此次请求可达：广告停放页、登录页、Cloudflare挑战页同样可能返回200；403可能是反爬、权限或地区策略；根路径404也不一定代表实际API端点不可用。它们应进入 `reachability` 维度，不能转成阅读完整性或内容质量分数。只有执行真实搜索→同作品→目录→首中末内容链路，才有依据给出相应质量分。

## 复跑与产物

```powershell
python scripts/collect-reading-source-catalog.py
python scripts/collect-reading-source-catalog.py --only keiyoushi --output reports/source-quality/catalog-inputs-refresh
```

- `manifest.json`：适合主程序加载的输入列表；每项有 `kind/path/url/sha256/entries`。
- `candidate-metadata.json`：去完全重复后的源身份、主机、规则指纹、语言、成人声明与静态障碍；不包含可执行规则正文。
- 六份上游JSON：原始字节，用 SHA256 校验，不执行其中代码。
- 下载串行，单个响应上限32MiB，网络I/O超时30秒，符合最大5通道限制；源站批量探测和内容匹配由Go调度器负责。

验证：脚本已真实运行两次；原始文件 SHA256、清单条目数、排除的镜像和占位项均经回读核对。未以这些元数据采集结论替代后续内容链路实测。
