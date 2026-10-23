# 小说书城：像进图书馆一样浏览

2026-10-21。本轮只开发小说，漫画保持现状。小说默认进入真实书城，不要求收藏、阅读历史、搜索词或手动导入书籍。

## 界面与参考

书城使用温暖纸色和棕色文字，宋体标题、主题导览、真实作品阅读桌与分排书架。手机每排三本，桌面每排六本。书城、个人书架、设置分开；精准搜索在顶部，按书名或作者查找。真实出版列表中的作品不包装为人工推荐或虚构评分。

参考 [Daunt Books](https://dauntbooks.co.uk/) 的分区书店、[Libreria](https://libreria.io/) 的主题漫游，以及 [Bookstore Studio](https://bookstorestudio.framer.website/) 的排印与留白。论坛参考 [r/SideProject 的图书应用讨论](https://www.reddit.com/r/SideProject/comments/1wu9rsk/selfhosted_library_app/)，其中对视觉风格、模板感和阅读记录用途存在不同意见；本项目自行决定中文长书名排版和浏览路径。

代码阅读参考：[BookLore 封面卡片模板](https://github.com/booklore-app/booklore/blob/develop/booklore-ui/src/app/features/book/components/book-browser/book-card/book-card.component.html) 与 [对应样式](https://github.com/booklore-app/booklore/blob/develop/booklore-ui/src/app/features/book/components/book-browser/book-card/book-card.component.scss)。查看了固定封面比例、懒加载、加载完成状态和轻量悬停反馈；没有复制组件源码或引入该项目依赖。

此前用户要求移除的前端设计 skills 已移出项目技能发现目录，本轮未使用它们。

## 数据路径

- `app/provider/browse.go` 提供阅读榜、出版作品、历史古代、东方玄幻、悬疑推理、都市异能、奇幻仙侠七个真实浏览入口。请求不携带搜索词，客户端不能提交任意远端 URL。
- 分类通过番茄公开分类接口，榜单使用公开榜单接口，出版作品使用官网出版列表。榜单过滤漫画与音频。上游数字 ID 以字符串保留。分类 `has_more` 的数字 0/1 与榜单布尔值分别正确解码；游标按上游原始行数前进，出版页按页尺寸前进。
- 书目按模式、来源身份与作品身份写入本地库；浏览不自动加入个人书架。第一次配置内置编译适配器，不导入外部脚本，不启用已停用来源。
- 每个频道只缓存首屏，使用既有 SQLite settings 管理，共七份。缓存绑定源配置版本，重新校验自然身份与本地 ID，并重新生成图片代理地址。30 分钟内复用；过期时请求上游，失败后可展示明确标记的旧书目。
- 单独保留每个板块的失败与重试；切分类、退出页面或销毁会取消请求并阻止迟到结果回写。详情返回保留已加载分页；书城与书架切换保留所选分类。
- 封面沿用带来源请求头与作品 Referer 的 Go 图片代理。修正番茄 0=完结、1=连载的映射；字数显示单位。

## 验证证据

浏览器真实源测试：[结果](novel-bookcity-2026-10-21/result.json)、[日志](novel-bookcity-2026-10-21/browser.log)。脚本 `scripts/test-novel-bookcity.py` 从全新临时 SQLite 开始，用生产 Go 服务响应，只替换 Wails 传输和同源图片路由。验证：零历史直接浏览三排真实书目、历史分类 20→40 本、自然身份无重复、打开《废物皇子：为何要逼我做皇帝？》、收藏、真实正文超过 500 字且无未映射私用字符、返回保留分页、重新加载后 SQLite 书目与收藏仍存在、390px 不溢出。它不是 Windows Wails 原生验收。

截图：[桌面书城](novel-bookcity-2026-10-21/bookcity-desktop.png)、[手机书城](novel-bookcity-2026-10-21/bookcity-mobile.png)、[真实作品详情](novel-bookcity-2026-10-21/book-detail-desktop.png)、[正文](novel-bookcity-2026-10-21/reader-desktop.png)。

Android 原生回归已通过，使用独立 `com.cczj.video.acceptance` 包与实际 Go/SQLite/网络服务。验证真实书城和封面、可信触摸选分类与 20→40 本分页、打开真实正文、两次系统历史返回保留 40 本列表，以及书城/个人书架切换保留分类。结果见 [android/result.json](novel-bookcity-2026-10-21/android/result.json)，截图见 [手机原生书城](novel-bookcity-2026-10-21/android/home.png)、[夜间分类](novel-bookcity-2026-10-21/android/night.png)。截图等待启动遮罩和转场结束后采集。测试不清除个人数据，不操作 preview 包；结束后恢复验收包的模式与路由。验收 APK 为 amd64 模拟器版本，并非 arm64 手机安装包。

静态和单元验证：`scripts/verify.ps1` 的完整输出见 [verify.log](novel-bookcity-2026-10-21/verify.log)，包含 59 个前端测试文件、282 个测试、类型检查、生产构建、架构约定与 Go 全量测试。另运行相关 Go 包的 `go vet`。Windows 上额外尝试 `go test -race ./app/provider ./app/library` 时运行时退出码为 `0xc0000139`，未获得 race 验证结果；不能据此宣称 race 通过。

既有个人书架测试迁移到明确的 `/novel/shelf` 路由，保留过滤、最近阅读、原始来源解析、源失效快照、空状态与阅读返回断言。`test-reading-redesign.mjs` 的桌面和手机 22 项交互回归通过；`test-reading-usability.py` 的四项源管理/阅读操作回归通过。这些是确定性浏览器替身验证，未用于宣称真实源或原生端通过。

## 当前边界

远端浏览先接通番茄一个完整适配器。其他小说源仍可用于搜索与阅读，未伪装成支持同样的浏览频道。上游接口可能变化，失败时展示原因和重试，不制造本地假推荐。Windows 原生窗口本轮尚未交互验收；浏览器桌面尺寸截图不等于 Windows 原生通过。漫画独立设计留到后续用户评审后推进。
