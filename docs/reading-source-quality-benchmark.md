# 小说与漫画源质量基准

这套程序用同一批作品和规则检查新旧来源，分别输出覆盖面、身份可信度、目录情况、当前可读范围和内容异常。它不会导入书籍、合并同名进度、改源开关或执行下载的源脚本。

入口有单源深检 `cmd/source-quality` 和大集合调度 `cmd/source-batch-quality`；检测与评分在 `app/library/quality`，现有生产 `provider` 负责实际搜索、详情、目录、正文和页图读取。质量程序不替代源适配器：尚未支持的 Legado 脚本、Mihon 扩展会明确标记适配缺口，不伪造正文质量分。

## 大集合调度

已从实际网络响应冻结 6,832 部作品：5,201 部中文网文、1,600 部漫画和 31 部经典虚构文学补充样本，合计 433 个细分标签。来源、失败请求、语言和版式局限见[样本调查](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/reading-quality-bulk-corpus-research-2026-10-21.md)。这批大样本用于身份发现；没有独立目录证据的作品不填入猜测的总章节数。原来的 51 部小基准继续用于带目录锚点的专项回归。

下载的 aoaostar、XIU2、Keiyoushi 集合经格式解析、规则身份去重并加入内置适配后，首轮实际调度 6,232 份小说／漫画配置。配置、主机、可搜索适配和完整阅读适配是不同维度，不能把六千份配置宣称为六千个可用网站。[集合调查](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/reading-quality-bulk-source-research-2026-10-21.md)保留原始文件 SHA256 和排除的重复镜像、迁移公告。

调度和匹配规则：

- 最多 5 个工作协程，任务队列、结果队列的容量都为 5。搜索、目录、正文、漫画图片共享同一 HTTP 闸门，响应体关闭才归还额度；小说和漫画合起来最多 5 路。
- 按 origin 轮转分发，同轮相同 origin 的首页请求合并并缓存。每份源规则仍保留自己的结果和身份；失败或超时保留未知状态。
- 标题、明确别名和上游提供的本地化标题预编译为索引，小说／漫画分桶，再按真实检索语言、题材轮转选样。当前批处理算法 `indexed-discovery.v2` 按源 ID 稳定分散检索书目，保留配置自带检查词；每个源只发有预算的少量搜索，并匹配每次返回的全部条目。同名异作者拒绝匹配，缺作者保留暂定，歧义不计为多部已确认作品。
- 索引构建与题材编排为输入规模的线性工作；查询期按返回标题查表，不做“源数 × 全部作品”的扫描。单个同名桶最多核对 16 部，超限保留歧义；目录锚点使用一遍建表，期望复杂度为 `O(目录项 + 锚点)`。哈希查表是期望复杂度，不承诺理论最坏情况恒定时间。
- 状态分阶段：网络可达、首页候选、搜索命中、实际深检。仅标题命中和能打开首页都不给正文质量分。静态搜索规则可独立检测；不能执行的脚本、APK 不被当作完整适配。
- `sources.jsonl` 逐源追加，`summary.json` 定期原子更新，支持 Ctrl+C 和同一输入／选项的断点续跑。恢复键包含基准哈希、源身份、批算法版本和参数；变更算法／输入应使用新输出目录。整体内存仍与样本、源配置和 origin 缓存规模相关，只有在途任务与通道缓冲受固定容量约束。

在仓库根目录刷新数据，顺序运行采集脚本与批处理；不要同时启动多份扫描器，以免多进程各自占用 5 路：

```powershell
python scripts/collect-reading-source-catalog.py
python scripts/collect-reading-benchmark.py --refresh --category-limit 30 --max-requests 150
go run ./cmd/source-batch-quality --workers 5 --queries 3 --matches 2 --max-deep 30 --out reports/source-quality/new-batch
```

采集器仅下载公开元数据，单个脚本串行；已有冻结大样本可直接复用。单源深检适合少量精查，数千份集合应使用批处理入口。`--manifest` 指定含 `catalogs` 的清单，条目提供 `kind/path/sha256`；CLI 校验文件摘要，读取本地配置。批处理不初始化应用数据库，不修改应用源设置。

首轮后只对之前 2xx 的地址做首页补充检测，或筛选全部已适配源深入回归：

```powershell
go run ./cmd/source-batch-quality --homepages-only --recheck-http reports/source-quality/new-batch/sources.jsonl --max-deep 0 --out reports/source-quality/new-homepages
go run ./cmd/source-batch-quality --searchable-only --queries 4 --out reports/source-quality/new-discovery
go run ./cmd/source-batch-quality --readable-only --corpus app/library/quality/benchmark.v1.json --queries 4 --max-deep 47 --out reports/source-quality/new-deep
```

`--homepages-only` 最多读取 256 KiB HTML、2,048 个链接，最多留下 64 部作品候选。`homepage_candidate` 仅为标题线索；`homepage_inspected` 才表示实际解析过 HTML，`homepage_no_html / homepage_unavailable` 分别保留未取得 HTML 和请求失败。早期已完成日志中的 `homepage_inspected` 曾表示首页分支结束，复核时以 `probe.homepage` 是否存在及失败字段为准，不能改写旧原始证据。

`--probe-timeout` 是短探测期限，默认 1.5 秒；超时不能判定永久失效。`--operation-timeout` 约束发现阶段每次搜索；目录／正文／页图由 provider 与 `--source-timeout` 的整源期限共同约束。`--delay` 是操作间隔，不是每个底层 HTTP 的精确间隔。`--resume` 须带上与首次运行一致的参数，且同一目录仅允许一个写入进程。网络计数含重定向、策略／缓存传输层调用，与唯一站点数不同；`runNetwork` 为当前进程，`network` 为有摘要可恢复的累计值，进程硬崩溃后的未快照尾部请求不保证可追溯。

本轮实跑结果与检测局限见[批量实测报告](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/reading-quality-bulk-run-2026-10-22.md)。

## 基准作品

`app/library/quality/benchmark.v1.json` 当前有 31 本小说、20 部漫画，按具体题材轮转选样。包含平台榜单/推荐、经典、类型代表和已知可读校准样本，并非所有作品都是当下热门。作品身份、选样原因、版本、作者与目录依据见[第一方调查](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/reading-quality-benchmark-research-2026-10-21.md)。

作品记录规范书名、显式别名、作者/创作团队、模式、语言、版本、题材与第一方链接。标题规范化只忽略排版和标点，不去掉续作数字、外传、重绘、改编等身份。繁简别名明确列出，不模糊猜测。同名异作者拒绝自动匹配，作者缺失则暂定匹配；多个同等候选保留歧义，不自动挑第一条。

作者署名规则 `reading-author-credit.v2` 由质量检查与章节换源共同使用：只移除白名单国籍／朝代括号以及明确的“著／原著／编著”角色后缀，忽略独立的译者署名。不会将任意括号内容、名称子串或未经核实的译名强行视为相同作者。经典检索报告单独记录 `identityAlgorithm`；语料 JSON 格式仍为 `reading-quality.v1`。比较新旧结果时同时核对身份规则标识及 `corpusHash`。

机器不会仅凭同名把小说、漫画混合；也不会把本源自己的目录数写成独立完整度证明。部分官方漫画没有个人作者字段，允许精确第一方作品 URL 加标题验证身份。

目录基准使用 `unit`（章/话/分节）、`scope`（完整/公开/快照）、核验日期、状态和分散的“编号＋标题＋卷”锚点。至少两个同版标题、编号、单位与顺序吻合，且返回目录都能识别编号，才比较总数。番外、通知、预告、不同分卷与不同公开范围无法对齐时，完整度保留未知。少于参考条目是待核查警告，不直接宣称盗漏章节。连载快照超过 14 天停止比较，完结版仍需在修订时更新。

规则版本为 `reading-quality.v1`，每份结果额外保存基准 JSON 解码后重新编码的 SHA256 `corpusHash`，用于区分同日不同样本集；它与原始 JSON 文件字节的 SHA256 可以不同。扩充作品时保留稳定 ID，修订规则时升级规则版本，保留原始结果，不能把不同版本当同轮检测。

## 检测与评分

| 项目 | 当前检测 | 基础权重 |
| --- | --- | ---: |
| 身份 | 同名、别名、作者、第一方作品 URL、歧义候选 | 15 |
| 元信息 | 标题、作者、封面地址、简介是否提取；作者字段异常 | 10 |
| 目录 | 分卷编号、重复入口、乱序、编号跳跃；满足同版条件时比较参考目录 | 30 |
| 阅读 | 每部返回目录的首、中、末位置实际正文/页图请求 | 35 |
| 正文清洁度 | 短行推广网址、下载/群引流、独立导航、乱码/混淆/截断、重复正文 | 10 |

同一缺陷码不因重复出现无限扣分；错误扣该项 45 分，待核查警告扣 20 分，信息提示不扣。未知项目不填满分，已观测项目按权重归一。覆盖量另记 `tagCoverage`，未收录不扣内容质量分。付费/登录限制与网络/解析错误分别记录；限制影响当前匿名可读范围，不标为坏正文。

分数再乘以证据系数：不足为 0.59；至少两部已确认身份、六次读取尝试且三次能读取为 0.84；至少三部确认身份、九次尝试、六次可读并有两部目录达到多锚点比较条件为 1.00。置信度名称为 `insufficient / limited / substantial`，都只描述本轮证据，不是长期稳定性。

目录和正文问题、无广告判断仍有人工核对空间：

- 短完结说明不因字少直接判截断；剧情里出现“下一章”“订阅”等词不单独当广告。
- 漫画每个抽样话取首、中、尾图，实际读取、解码并记录尺寸、格式、字节数和指纹；不同话重复图只提示待查，可能是公共扉页。
- 当前没有图片 OCR、二维码/推广视觉分类和人工逐页审阅，不能自动证明图片无广告、翻译准确或全部图序正确。
- 检查的是适配器实际返回的搜索、详情和阅读信息；尚未提取的首页榜单/更新字段不在本轮检查内。瓜子首页错配的人工调查不会因本轮读取分数较高就被推翻。
- 一轮抽样不能证明全站齐全、全年稳定。超时、资源上限和无匹配都在原始结果中保留，未完成的整源任务列 `failures`，不发布成功分数。

本版权重是可审计的初始规则，经过故障用例校验，不是已经训练出统计准确率的模型。之后应增加人工标注的好/坏样本来调整；调规则时保留能捕获原故障的回归用例。

## 使用

### 应用多源搜索回归

`scripts/fixtures/reading-classics.v1.json` 是另一个小型验收集：24 本小说、16 部漫画，覆盖经典与类型代表，保留本轮网上核对的书名、作者、版本边界和依据。它不替代 6,832 部发现语料或原有 51 部目录基准，也不是随机抽样的全网命中率统计。

使用应用实际内置目录和路由，顺序运行两种模式，避免多进程各占五路：

```powershell
go run ./cmd/source-quality --kind novel --search-routing --corpus scripts/fixtures/reading-classics.v1.json --queries 24 --matches 20 --timeout 90s --out dist/audit/reading-classics/novels.json
go run ./cmd/source-quality --kind comic --search-routing --corpus scripts/fixtures/reading-classics.v1.json --queries 16 --matches 16 --timeout 90s --out dist/audit/reading-classics/comics.json
```

该入口强制临时独立数据库，拒绝 `--persist` 和 `--data-dir`，复用生产搜索服务及已有身份、目录、正文检测。自动扩源上限与前端相同，最多三轮，32 秒后不启动下一轮，精确标题先返回；余下来源和游标记录在结果中。每部完成即用已有原子报告写入器落盘，取消不会清空已记录的作品；重新执行会重新检测，不把报告文件当作可恢复任务。

`--queries` 限制当前模式的作品数，`--matches` 限制抽查阅读的作品数。每部只抽查第一个身份已核对的候选，首／中／末位置按它实际返回的目录选取；不把前言、版权页、公告当作正常剧情通过。页表数量与真实图片解码分开，图片抽样仍用下方单源深检入口。某候选失败不能推断所有译本／所有来源都不可读；`absent` 仅表示本轮严格规则无命中，不表示全网没有作品。

浏览器验收复用 `scripts/test-reading-search-browser.py` 的 `--live-rpc`，可用 `--live-novel-title`、`--live-novel-author` 指定小说，并用 `--live-comic-title`、`--live-comic-author` 增加真实漫画搜索、详情及首图显示检查。后端由 `scripts/reading-browser-host --managed-sources --relative-images` 提供，前端仍是浏览器传输替身，不能据此宣称原生桌面或 Android 通过。

### 单源深检

在仓库根目录运行。默认使用临时独立数据库，检测报告逐源落盘，不接触设备上的源设置：

```powershell
go run ./cmd/source-quality --kind novel --sources '酷我小说,阅友小说,番茄小说' --queries 31 --matches 5 --out reports/source-quality/novels.json
go run ./cmd/source-quality --kind comic --sources '咚漫漫画,瓜子漫画' --queries 20 --matches 5 --out reports/source-quality/comics.json
```

新来源可给出本地配置集合或集合 URL，沿用同一套规则；传入的分数不会被信任：

```powershell
go run ./cmd/source-quality --kind novel --input 'D:/sources/new-sources.json' --all --corpus app/library/quality/benchmark.v1.json --queries 200 --matches 5 --out reports/source-quality/new-sources.json
```

默认最多查询 24 部、读取五部命中作品；`--queries`、`--matches` 控制深度。题材以第一标签轮转。`--works` 可按逗号指定基准作品 ID 做专项回归，此时与全样本报告的 `corpusHash` 不同。默认操作间隔 350ms，每源最长五分钟，源内请求串行，收到明确限流后停止继续查询；不能把操作间隔当成 HTTP 层的全局限流器（目录分页由 provider 管理）。取消通过 Ctrl+C 传到真实网络请求。

需要将本机检测摘要用于应用质量排序时，显式提供应用数据库目录并加 `--persist`；它只保存质量摘要，详细证据仍在报告文件：

```powershell
go run ./cmd/source-quality --kind novel --sources '酷我小说,阅友小说' --data-dir 'D:/CCZJData' --persist --queries 31 --out reports/source-quality/local.json
```

`D:/CCZJData` 必须是实际使用的数据目录；不要把示例路径当成自动识别结果。未匹配出同版样本的 `inconclusive` 不覆盖已有质量记录。应用重新加载源列表后读取摘要。新版排序将本轮有足够证据的基准检查放在旧首章快速检查前面，24 小时后按过期处理；单次快速检查仍是原有功能，不能当作本基准运行。成功的快速检查保留尚未过期的深度摘要，也不刷新深度检查日期；快速检查失败可以使其失效，快速检查次数与章节样本数不会混算。CLI 是当前深度检查入口，本次没有新增界面按钮或后台定时扫描。

## 验证

```powershell
go test ./app/library/quality ./app/library ./cmd/source-quality
cd frontend
npm run test -- --run tests/source-catalog.test.ts
npm run build
```

回归用例覆盖同名异书、小说与改编/续作区分、缺作者、分卷重置、反序目录、多锚点和过期快照、推广误报、重复正文、权限限制、取消、损坏 JSON 与存储/排序兼容。真实源结果保存在 `reports/source-quality/`；当前检查在桌面开发机网络执行，不代表 Android 或华为设备验证。
