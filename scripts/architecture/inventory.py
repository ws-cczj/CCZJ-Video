from pathlib import Path
import re, json, hashlib, subprocess, collections, datetime, os

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / 'dist/audit/architecture'
OUT.mkdir(parents=True, exist_ok=True)
def read(p): return p.read_text(encoding='utf-8-sig', errors='replace')
def rel(p): return p.relative_to(ROOT).as_posix()

# Snapshot inventory is structural evidence; it does not claim runtime coverage.
files=[]
for folder in ['app','frontend/src','frontend/tests','frontend/build','build','scripts','cmd']:
    for directory, dirs, names in os.walk(ROOT/folder):
        dirs[:] = [d for d in dirs if d not in ['node_modules','.gradle','outputs','intermediates','gen','generated','build','dist','bin','__pycache__']]
        for name in names:
            p=Path(directory)/name
            if p.suffix in ['.go','.ts','.vue','.js','.mjs','.java','.ps1','.py','.yml','.gradle']: files.append(p)
files += list(ROOT.glob('*.go'))
packages={}
functions=[]
for p in files:
    s=read(p); path=rel(p)
    if path.startswith('app/') and p.suffix=='.go':
        name=p.parent.relative_to(ROOT/'app').as_posix()
        x=packages.setdefault(name,dict(name=name,files=[],tests=[],deps=set(),symbols=[]))
        if p.name.endswith('_test.go'): x['tests'].append(path); continue
        x['files'].append(path)
        imports='\n'.join(re.findall(r'(?ms)^import\s*\((.*?)^\)',s)+re.findall(r'(?m)^import\s+([^\n]+)',s))
        x['deps'].update(re.findall(r'"cczjVideo/app/([^"\n]+)"',imports))
        for n,line in enumerate(s.splitlines(),1):
            if re.match(r'^func (?:\([^)]*\) )?[A-Za-z_]',line):
                item=dict(file=path,line=n,signature=line.rstrip(' {'))
                functions.append(item); x['symbols'].append(item)
    elif path.startswith('frontend/src/') and p.suffix in ['.ts','.vue']:
        for n,line in enumerate(s.splitlines(),1):
            if re.match(r'^export (?:async )?function ',line): functions.append(dict(file=path,line=n,signature=line[:240]))
for x in packages.values(): x['deps']=sorted(x['deps'])
for x in packages.values(): x['users']=sorted(y for y,z in packages.items() if x['name'] in z['deps'])
routes=[]
router=read(ROOT/'frontend/src/router/index.ts')
for m in re.finditer(r"path\s*:\s*['\"]([^'\"]+)['\"]\s*,\s*component\s*:\s*\(\)\s*=>\s*import\(['\"]([^'\"]+)['\"]\)",router):
    p=(ROOT/'frontend/src/router'/m[2]).resolve()
    routes.append(dict(route=m[1],file=rel(p),mode='novel' if m[1].startswith('/novel') else 'comic' if m[1].startswith('/comic') else 'video'))
bindings=[]
for p in (ROOT/'app/service').glob('*.go'):
    if p.name.endswith('_test.go'): continue
    for n,line in enumerate(read(p).splitlines(),1):
        m=re.match(r'^func \(a \*App\) ([A-Z]\w*)\(',line)
        if m and m[1] not in ['ServiceStartup','ServiceShutdown']:
            bindings.append(dict(name=m[1],file=rel(p),line=n,signature=line.rstrip(' {')))
tables=[]
for p in (ROOT/'app').rglob('*.go'):
    if p.name.endswith('_test.go'): continue
    for m in re.finditer(r'CREATE TABLE (?:IF NOT EXISTS )?([a-z_]+)',read(p),re.I):
        tables.append(dict(name=m[1],file=rel(p),line=read(p)[:m.start()].count('\n')+1))
tables=list({(t['name'],t['file']):t for t in tables}.values())
meta=dict(date=datetime.date.today().isoformat(),commit=subprocess.check_output(['git','rev-parse','--short','HEAD'],cwd=ROOT,text=True).strip(),dirty=subprocess.check_output(['git','status','--short'],cwd=ROOT,text=True,encoding='utf-8'),files=len(files),packages=len(packages),routes=len(routes),bindings=len(bindings),tests=sum(len(x['tests']) for x in packages.values()),hash=hashlib.sha256(''.join(rel(p)+hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(files)).encode()).hexdigest())

descs={
'apperror':'公开错误码、错误包装与 Wails MarshalError；诊断原因与展示消息分开。',
'applog':'滚动日志、环形缓冲与结构化诊断字段。',
'backup':'只读一致快照、恢复单事务、独立归档副本与信任隔离。',
'cache':'跨模块缓存信息、清理与失效通知。',
'cast':'局域网设备发现、设备描述解析与 SOAP 播放控制。',
'collect':'视频源抓取、响应解码、策略解析、分页处理与采集引擎。',
'collection':'手动／定时采集、运行注册、进度事件与定时器代次所有权。',
'danmaku':'弹幕搜索、匹配、集数、绑定与时间窗口读取；包含独立持久化接口。',
'db':'SQLite 连接、顺序迁移，以及视频、来源、阅读、任务等多领域数据适配。',
'detail':'视频详情新鲜度、合并在途请求、缓存与远程回源。',
'diagnosticrecording':'应用所有的诊断录制、事件与导出文件。',
'douban':'榜单、详情、评论、冷却与模式所有的可取消补全批次。',
'download':'执行器、注册表、目录与断点存储契约，供领域实现和 SQLite 适配复用。',
'download/engine':'下载任务所有者：准入、HTTP/HLS、状态、文件、断点与进度快照；宿主显式注入依赖。',
'files':'文件路径、可取消复制、唯一发布、原子替换与平台文件能力。',
'source':'来源配置、原子导入导出、健康与搜索；变更协调已有采集所有者。',
'handoff':'重启/更新时新旧进程交接和 PID 等待。',
'library':'阅读获取编排：浏览、聚合搜索、目录、正文、漫画页、源质量和换源。',
'library/comic':'漫画话次/卷匹配及源路由策略。',
'library/novel':'小说章节匹配及源路由策略。',
'library/matching':'阅读匹配共用的规范化与比较工具。',
'library/quality':'阅读源检测、证据、调度及共享五路 HTTP 闸门。',
'lifecycle':'有所有者的工作组、关闭等待与请求上下文联动。',
'media':'视频列表、搜索、详情、线路、收藏与历史的领域入口；上下文与显式依赖装配。',
'model':'来源、视频、播放线路等共享数据结构。',
'netaddress':'共享网络地址分类与可访问地址判据。',
'netbudget':'应用出站并发、每主机额度、排队上限与播放预留。',
'netpolicy':'请求冷却/限流策略、响应反馈与 SQLite 持久化。',
'netstats':'按模式/类别统计请求、失败、流量及网络观测。',
'operation':'请求身份、统一操作观测、恢复与指标。',
'outbound':'公网代理池、健康探测、传输缓存和回退路由。',
'parallel':'固定批次的受控并行执行。',
'payload':'输入大小、Base64、压缩内容与解包限制。',
'plugin':'扩展包扫描、安装、信任、启用和资源访问；前端负责脚本执行。',
'provider':'阅读源规则文档、HTTP/会话、提取/解码与内置源适配。',
'proxy':'图片/HLS/Android 媒体桥、SSRF 拨号、缓存及线路测速。',
'readingstate':'阅读记录与变更格式、身份与内容校验；SQLite 适配位于 db。',
'resource':'应用资源和运行状态采样。',
'service':'Wails 唯一绑定入口、应用装配/生命周期、模式准入、跨域编排与事件适配。',
'settings':'设置读写与持久化入口。',
'storagepaths':'Windows WebView 资料目录选择与稳定身份。',
'update':'更新业务状态、忽略版本、安装安排与回滚状态。',
'updater':'更新请求、完整性检查、下载、替换脚本及进程退出钩子。',
'window':'桌面窗口尺寸、关闭偏好和全屏等控制。',
'workflight':'有所有者、有容量的合并在途工作与订阅者取消。'}

# These are manually reviewed responsibility paths, not an inferred call graph.
features=[]
def feature(group,title,steps,files,note):
    features.append(dict(group=group,title=title,steps=steps.split('|'),files=files.split('|'),note=note))
feature('系统','启动与宿主装配','main / handoff|buildApp|service.NewApp|ServiceStartup|runtime → Vue mount','main.go|app.go|app/service/app.go|frontend/src/main.ts','桌面单实例、静态资源嵌入；Android 通过 RegisterAndroidMain 进入同一 Go 入口。')
feature('系统','模式切换与恢复','ApplicationModeSwitch|applicationMode|settings façade|switchApplicationMode|停止旧任务 → 保存模式 → 启动新任务|文档导航 → 恢复轻量快照','frontend/src/modes/applicationMode.ts|frontend/src/modes/modeSessions.ts|app/service/application_mode.go','不是同时挂载三个完整应用。切换会暂停视频下载，下载需显式恢复。')
feature('视频','首页 / 最近更新 / 搜索 / 分类','Home / Search / Recent|useVideoList / stores|api/app → façade_video|media.Service.List / Search|db 目录分页 / collect 远程搜索','frontend/src/views/Home.vue|frontend/src/views/Search.vue|frontend/src/composables/useVideoList.ts|app/service/facade_video.go|app/media/catalog.go','列表和远程搜索归 media；请求上下文传到目录查询、源请求与搜索落库事务。')
feature('视频','视频详情 / 多线路 / 测速','Detail|api/video|facade_video → media|应用拥有的 detail.Service|workflight / 缓存 / collect / proxy','frontend/src/views/Detail.vue|frontend/src/api/video.ts|app/media/catalog.go|app/detail/service.go|app/media/playline.go|app/media/playline_quality.go','共享详情最后等待者取消即停止上游；后台刷新归应用所有者，关库前等待；失效代次阻止旧请求回填，返回独立快照。')
feature('视频','播放 / 选集 / 全屏 / 画中画','Player|preparation / playbackControl|playbackSession|player/platform|HTML media / hls.js / 原生设备|HLS / Android 媒体桥','frontend/src/views/Player.vue|frontend/src/player/playbackSession.ts|frontend/src/player/platform/index.ts|app/proxy/hls.go|app/proxy/media_bridge.go','媒体数据通道与 Wails RPC 通道分开；播放器有会话代次、取消与延迟工作清理。')
feature('视频','弹幕匹配与加载','DanmakuSources / 播放器|api/danmaku|facade_danmaku|danmaku.Service|Client / Store','frontend/src/views/DanmakuSources.vue|frontend/src/api/danmaku.ts|app/service/facade_danmaku.go|app/danmaku/service.go','匹配、集数绑定和窗口读取有独立接口，避免全部弹幕常驻页面。')
feature('视频','收藏 / 历史 / 续播 / 推荐','Favorites / History / Recommendations|API / stores|facade_favorite / history|media|db 视频身份 / 收藏 / 历史','frontend/src/views/Favorites.vue|frontend/src/views/History.vue|frontend/src/views/Recommendations.vue|app/media/service.go','SQLite 为收藏和观看历史入口；浏览器仍存在视频进度等本地键，应逐项核对权威来源。')
feature('视频','来源 / 导入导出 / 类型映射','Sources / VideoTypes|source store / API|source façades|source / db|sources / source_types / global_types','frontend/src/views/Sources.vue|frontend/src/views/VideoTypes.vue|app/source/service.go|app/source/manage.go','来源身份与名称区分；共用 sources 表，通过 kind 区分视频和阅读来源。')
feature('视频','采集 / 补采 / 定时调度','来源操作 / 调度设置|facade_collect / scheduler|collection|collection.Registry / collect.Engine|分页抓取 → SQLite → 进度事件','app/service/facade_collect.go|app/collection/service.go|app/collection/scheduler_service.go|app/collect/engine.go','视频模式启动调度；切模式/退出由 modeTasks 停止。')
feature('视频','豆瓣榜单 / 评论 / 元数据补全','首页 / 详情 / 诊断|facade_douban|douban|冷却 / 缓存 / HTTP|db 补全与榜单','app/service/facade_douban.go|app/douban/chart.go|app/douban/interactive.go|app/douban/updater.go','后台补全与用户触发操作具有不同节奏，不能把统一限流理解为相同业务策略。')
feature('视频','下载 / 暂停 / 恢复 / 文件输出','Downloads / download store|service 模式准入 / 事件适配|download/engine 所有者|HTTP / HLS / Registry / Executor|SQLite checkpoint + 文件','frontend/src/views/Downloads.vue|frontend/src/stores/download.ts|app/service/facade_download.go|app/download/engine/engine.go|app/download/engine/download_control.go|app/download/engine/download_hls.go|app/download/engine/download_journal.go|app/download/executor.go','请求负责准入，传输拥有独立身份及 parent_request_id；退出等待网络、文件和断点收尾，再关库。恢复任务保持暂停。')
feature('视频','回收站 / 身份人工合并','RecycleBin / 诊断 / 入库|facade_video / collect|media 入库 / 回收站 / 身份|MediaStore 事务 → CatalogChange|提交后缓存失效 / SQLite v20','frontend/src/views/RecycleBin.vue|app/media/import.go|app/media/recycle.go|app/media/union.go|app/db/catalog_changes.go|app/db/identity_merge.go|app/db/catalog_write_owner.go','目录删除保留用户记录，彻底删除用退役坐标继续隐藏，再入库解除；合并校验身份依据，保留进度坐标与时间，失败回滚。')
feature('视频','投屏发现与控制','播放器投屏入口|service/cast|cast.Service|SSDP / 设备描述 / SOAP|局域网渲染设备','app/service/cast.go|app/cast/service.go|frontend/src/player/platform/index.ts','Android 还涉及组播锁与系统投屏设置；浏览器替身不证明局域网实机可用。')
feature('小说','书城 / 分类 / 聚合搜索','NovelHome / Catalog / Search|novelLibrary / novelBrowse|library façades|library 浏览 / 搜索|provider → db','frontend/src/novel/views/NovelHome.vue|frontend/src/novel/views/NovelSearch.vue|frontend/src/novel/data/novelLibrary.ts|app/library/search.go','搜索按源渐进反馈 requestID / kind，分页游标按来源保留。')
feature('小说','详情 / 目录 / 换源 / 章节对齐','NovelBook / SourceSwitch|api/library / readingSources|library|novel 路由 + matching|来源证据 / 目录 / provider','frontend/src/novel/views/NovelBook.vue|frontend/src/novel/components/NovelSourceSwitch.vue|app/library/novel/policy.go|app/library/routing.go','小说章节与漫画话次保持独立语义；底层比较工具可以共享。')
feature('小说','阅读 / 分页 / 预取 / 进度','NovelReader|reader/session / pagination / progress|api/library|library → provider / textCache|reading persistence → SQLite','frontend/src/novel/views/NovelReader.vue|frontend/src/novel/reader/session.ts|frontend/src/novel/reader/pagination.ts|frontend/src/reading/chapterPrefetch.ts|app/library/service.go','正文磁盘缓存上限 256MiB / TTL 30 天；阅读布局和缓存生命周期分开。')
feature('小说','书架 / 阅读偏好 / 旧数据导入','NovelShelf / readingState|reading/persistence|ReadingStateLoad / Apply|readingstate 校验|db.ReadingStateStore','frontend/src/novel/readingState.ts|frontend/src/reading/persistence.ts|app/service/facade_reading_state.go|app/db/reading_state.go','旧浏览器数据首次原子导入并保留原值；按模式/来源/远程身份保存。')
feature('漫画','发现 / 分类 / 趋势 / 搜索','ComicHome / Catalog / Trending|comic repository / comicBrowse|comic / library façades|library|provider / 缓存 / db','frontend/src/comic/views/ComicHome.vue|frontend/src/comic/data/repository.ts|frontend/src/api/comicBrowse.ts|app/library/comic_browse.go','榜单、渠道浏览和多源搜索是不同获取链路。')
feature('漫画','详情 / 话次 / 换源匹配','ComicSeries / SourceSwitch|repository / readingSources|library|comic 路由 + matching|目录和来源证据','frontend/src/comic/views/ComicSeries.vue|frontend/src/comic/components/ComicSourceSwitch.vue|app/library/comic/policy.go','话次、卷与章节的匹配策略分开维护。')
feature('漫画','图片阅读 / 方向 / 页码 / 预取','ComicReader|reader/session / useComicPages|Library pages|provider 图片列表|readingImages /__cczj/img → WebView','frontend/src/comic/views/ComicReader.vue|frontend/src/comic/reader/useComicPages.ts|app/library/comic_pages.go|app/proxy/imgproxy.go','图片保留 Referer 与源头信息，图片字节不经 JSON RPC 搬运。')
feature('漫画','追漫 / 偏好 / 阅读进度','ComicLibrary / state|reading/persistence|ReadingStateLoad / Apply|readingstate 校验|SQLite reading_state','frontend/src/comic/views/ComicLibrary.vue|frontend/src/comic/state.ts|frontend/src/comic/reader/progress.ts|app/readingstate/records.go','漫画与小说共享存储规则，记录 kind 及来源身份独立。')
feature('阅读共享','源目录 / 导入 / 检测 / 维护','NovelSources / ComicSources|useBuiltinSources / api/readingSources|Novel / Comic SourceDirectory|library directory / quality / maintenance|共享 NetworkGate / provider / db','frontend/src/novel/views/NovelSources.vue|frontend/src/comic/views/ComicSources.vue|frontend/src/reading/useBuiltinSources.ts|frontend/src/api/readingSources.ts|app/library/source_directory.go|app/library/source_maintenance.go|app/library/quality/gate.go','当前界面使用内置目录与启停；Preview / Import 等公开绑定保留兼容能力。获取、搜索和质量检测共享五路出站闸门。')
feature('共享','设置 / 主题 / 语言 / 动效 / 公共控件','各模式 Settings|settingsQueue / stores|settings / window façades|settings.Service / window|SQLite / CSS variables / locales','frontend/src/api/settingsQueue.ts|frontend/src/components/ui/index.ts|frontend/src/styles/cczj-utilities.css|frontend/src/locales/zh-CN.ts|app/window/service.go','宿主通过 runtime 与 presentation 选择，屏幕宽度只负责布局。')
feature('共享','扩展包 / 信任 / 脚本 / 卸载','Plugin UI / dropInstall|facade_plugin|plugin.Service|磁盘 manifest / 权限 / enabled|plugins/runtime 执行与清理','app/service/facade_plugin.go|app/plugin/service.go|frontend/src/plugins/runtime.ts|frontend/src/plugins/dropInstall.ts','扩展脚本为同进程可信代码，权限检查不能描述为安全沙箱。')
feature('共享','备份 / 恢复 / 旧数据 / schema 提示','DataPreferences / LegacyDataPrompt|facade_backup / legacydata|backup / db migrations|压缩备份 / 合并 / 归档|平台文件选择 / 分享','frontend/src/views/settings/SettingsDataPreferences.vue|app/backup/service.go|app/db/migrations.go|app/service/legacydata.go','导出一致快照，恢复所有类别同一事务；取消回滚，副本独立清理，备份不转移信任。')
feature('共享','更新 / 校验 / 安装 / 回滚','UpdateModal / installer|service update façade|update.Service|updater|Windows 交接替换 / Android 安装器','frontend/src/components/UpdateModal.vue|frontend/src/platform/updateInstaller.ts|app/update/service.go|app/updater/integrity.go|app/updater/install.go','更新下载归应用所有者，共享网络预算、单次准入及取消；Windows 交接，Android 原生安装。')
feature('共享','日志 / 诊断 / 录制 / 导出','RuntimeDiagnostics / LogPanel|diagnostics API|service diagnostics|operation / netstats / resource / applog|diagnosticrecording → 文件','frontend/src/views/RuntimeDiagnostics.vue|app/service/diagnostics.go|app/operation/middleware.go|app/diagnosticrecording/recorder.go','包含请求、任务、网络和资源观测；诊断导出需检查公开字段与路径信息。')
feature('共享','请求 / 取消 / 错误 / 事件','视图 RequestScope / generation|requestPipelines / decode / events|生成 .js bindings|runRequest / lifecycle|领域 I/O / MarshalError','frontend/src/api/requestPipelines.ts|frontend/src/api/decode.ts|frontend/src/api/events.ts|app/service/requests.go|app/apperror/error.go','读合并、订阅者取消和迟到结果属于不同层；并非每个旧绑定都接入同一处理路径。')
feature('网络','HTTP / 限流 / 路由 / 缓存','collect / provider / proxy / download / douban|netbudget / netpolicy / netstats|outbound 公网代理及回退|netaddress / SafeDialContext|源站与 CDN','app/netbudget/budget.go|app/netpolicy/policy.go|app/outbound/pool.go|app/netaddress/address.go|app/proxy/service.go','这是责任关系图，具体 Transport 包装顺序按调用点不同；投屏有独立局域网约束。')
feature('平台','Android 原生能力与生命周期','MainActivity / WebView|WailsJSBridge / WailsBridge|JNI → Go runtime|播放器 / 文件 / 分享 / APK 安装|Activity 返回/销毁','build/android/app/src/main/java/com/wails/app/MainActivity.java|build/android/app/src/main/java/com/wails/app/WailsJSBridge.java|build/android/app/src/main/java/com/wails/app/WailsBridge.java|build/android/main_android.go','控制消息与业务请求共用执行器的风险需优先评估；Activity 生命周期与 Go 单例所有权需对齐。')
feature('工程','构建 / 绑定 / 测试 / 发布 / 质量工具','scripts/verify.ps1|Go tests / frontend tests / build|generate-bindings|Windows / Android build|release / source-quality CLI','scripts/verify.ps1|scripts/generate-bindings.ps1|build/android/build.ps1|cmd/source-quality/main.go|cmd/source-batch-quality/main.go','Wails Go/JS 当前均 beta.30；源码静态审查不能替代两端原生验收。')

# Repair references by searching real files; never invent successful source links.
missing=[]
for f in features:
    fixed=[]
    for name in f['files']:
        p=ROOT/name
        if not p.exists(): missing.append(name)
        else: fixed.append(name)
    f['files']=fixed

issues=[]
def issue(id,priority,title,fact,impact,proposal,evidence,status='本轮代码确认；未做故障复现'):
    issues.append(dict(id=id,priority=priority,title=title,fact=fact,impact=impact,proposal=proposal,evidence=evidence,status=status))
issue('A01','已调整','启动回滚与 Windows 原生错误提示已接通','启动失败先回滚已拥有资源，主入口记录诊断和关联号，通过不依赖 WebView 的原生提示展示公共错误。','启动错误不再 panic；窗口关闭后以非零退出码结束。非 Windows 桌面保留 stderr，Android 走 JNI 启动边界。','保留异常路径测试；其他桌面平台的原生提示未验证。',['main.go','startup_error_windows.go','app/service/lifecycle.go','scripts/test-windows-lifecycle.py'],'Go 失败注入与 Windows 真实启动失败提示通过；私有策略正文未显示')
issue('A02','已调整','Android 取消独立控制路径已接入','取消绕过业务队列，队列任务可移除；构建副本保留注册前取消记录。','真实 Java/JNI/Go/HTTP 饱和取消及排队取消已通过；注册瞬间竞态与全部队列满组合仍需覆盖。','复用现有桥接验收入口继续覆盖边界。',['build/android/app/src/main/java/com/wails/app/CczjRuntimeDispatcher.java','scripts/test-android-cancellation.py'],'受控调度器及 Android 原生 HTTP 取消通过')
issue('A03','已调整','Go 进程持有运行时，Activity 附着视图','WailsBridge 按进程初始化一次，Activity detach 清理本宿主资源并隔离旧 UI 回调。','连续三次 CLEAR_TOP 同进程重建通过；进程恢复与系统能力矩阵仍待覆盖。','继续按宿主能力验证，保持进程与视图所有权分离。',['build/android/app/src/main/java/com/wails/app/WailsBridge.java','scripts/test-android-lifecycle.py'],'Android 同进程重建原生通过')
issue('A04','已调整','窗口偏好先持久化再应用','写入失败返回 error，尺寸事务提交；WindowClosing 按偏好隐藏或退出，主动关停放行。','真实 Windows WM_CLOSE 与主动退出通过；标题栏、Alt+F4、托盘交互矩阵仍待覆盖。','保留失败注入及原生验收，继续验证其他窗口入口。',['app/window/service.go','app.go','scripts/test-windows-lifecycle.py'],'持久化失败回归及 Windows 原生退出通过')
issue('A05','已调整','下载实现已归入领域所有者','download/engine 管理 HTTP/HLS、状态、文件和断点；service 只保留模式准入、绑定和事件适配。','任务不再依赖 App 内部结构，协议测试直接作用于领域实现。','沿现有接口继续验证暂停恢复与平台文件能力，不机械增加 Repository。',['app/download/engine/engine.go','app/download/engine/ownership_test.go','app/service/facade_download.go'],'协议、SQLite、取消、独立执行身份及退出收尾回归通过')
issue('A06','已评估','按真实替换点注入，保留单进程兼容安装点','媒体存储、阅读网络预算、下载执行器与生命周期已显式装配；包级数据库与网络安装点仍服务单应用运行时。','不为单进程应用机械生成全套仓储；测试与工具可用 db.Open 隔离。WAL 写事务统一提前取得写入资格，只读事务保持并发。','若引入同进程多应用运行时，再迁移剩余全局依赖；目前保持兼容并验证资源关闭。',['app/db/sqlite.go','app/db/wal_test.go','app/db/mapping.go','app/netpolicy/policy.go','app/library/service.go'],'双连接写入竞争已复现并修复；未宣称完全移除包级状态')
issue('A07','已调整','视频查询与目录维护已归领域入口','查询、详情、线路、收藏历史、显式入库、回收站和人工合并统一经 media 与请求跟踪。写事务返回已提交影响范围，再统一失效缓存。','删除后记录不再重新出现，合并保持进度坐标和观看时间，批次不会使用已合并的身份。源管理归 source，运行状态与调度归 collection；旧 handler 包已移除。','源变更先停止并等待旧采集；配置、目录与分类导入原子提交，后台运行拥有独立身份。',['app/media/service.go','app/media/import.go','app/media/recycle.go','app/media/union.go','app/db/catalog_changes.go','app/db/identity_merge.go'],'Go / SQLite / 迁移故障验证通过；Windows / Android 目录生命周期绑定链路通过，原生合并界面未验收')
issue('A08','已调整','阅读共享获取有明确关闭与执行上下文','小说、漫画匹配及质量策略保留现有领域模块；正文、漫画页与趋势共享取消所有者，再分别等待收尾。','读取已有目录也接收请求 context；共享 HTTP 有独立执行 ID，并关联首个发起请求，取消一个等待者不拖走其他等待者。','保留 library 的获取协调职责，不因文件体量另建平行工具层。',['app/library/service.go','app/library/content.go','app/library/ownership_test.go','app/library/content_flight_test.go','app/operation/identity.go'],'阻塞关闭、取消查库、真实 HTTP 上下文关联与共享读取回归通过')
issue('A09','已调整','持久业务、缓存与会话恢复承诺已列清','SQLite 保存观看位置、阅读状态与任务；近期视频时长/百分比、详情与阅读正文是可丢缓存；模式快照限时恢复。','视频旧缓存现在运行时校验根对象、位置、时长和时间，坏条目不会阻断新增进度；写失败保留待写状态。','保留旧名称键兼容，不把它当跨来源身份；丢失时从 SQLite 位置兜底，百分比可不完整。',['frontend/src/utils/episodeProgress.ts','frontend/tests/episode-progress-write-failure.test.ts','frontend/src/modes/modeSessions.ts','docs/request-pipelines.md'],'坏数据与写失败回归通过；不宣称可丢缓存进入内容备份')
issue('A10','已调整','设置页路由与分组快照已同步','漫画设置使用实际 /comic/settings，旧 /comic/me 读入时规范化；小说与漫画分别保留设置分组滚动键。','跨模式键被拒绝，旧设置别名不会使漫画停留在更早列表页。','保留持久化回归；完整原生导航和手势交互仍需逐项验收。',['frontend/src/modes/modeSessions.ts','frontend/src/router/index.ts'],'设置路由、旧别名、分组滚动与模式隔离测试通过')
issue('A11','待验证','独立资源聚合清理已接入','两个任务所有者先同时取消再等待；独立资源清理报错后仍继续，成功后才关库与日志。','超时或未排空时保留数据库；极端宿主退出行为仍需验证。','继续覆盖错误及硬超时，不能把正常退出通过等同所有失败路径通过。',['app/service/lifecycle.go','app/service/lifecycle_test.go'],'Go 错误注入及 Windows 正常退出通过')


issue('A12','已调整','备份恢复与文件发布具有单一所有者','一致快照导出；恢复所有类别同一事务，成功后发布失效；导入／导出共用限额，不生成自身不可读的文件。','故障或取消不留下部分恢复，指定文件写失败保留旧文件；并发归档副本独立清理。','复用 files 的复制、唯一发布与原子替换，前端读取归 requestScope。',['app/backup/service.go','app/db/backup_writer.go','app/db/backup_snapshot.go','app/files/stream.go','app/backup/transaction_test.go','app/backup/export_limit_test.go'],'SQLite 失败注入／取消回滚通过；Windows / Android 真实备份往返通过')
issue('A13','已调整','更新下载归宿主请求与共享网络预算','跨模式保留，退出取消并等待；单下载准入，共享校验和、测速、正文与哈希上下文。','取消清理部分文件并停止镜像回退，进度不会混入另一个下载。','保留真实 HTTP 与浏览器回归；安装和回滚仍须独立环境验证。',['app/update/service.go','app/updater/update_download.go','app/updater/request_context_test.go','frontend/src/stores/updateState.ts'],'Go 阻塞 HTTP、准入与迟到进度回归，浏览器安装失败恢复通过')
issue('A14','已调整','豆瓣后台补全上下文贯穿到 HTTP 与存储','视频模式拥有调度器，补全批次拥有独立执行身份；停止同时取消限速、搜索与详情。','不再等下一条记录才停，循环和执行收尾前不提前报告停止；手动补全存储错误返回调用方。','复用已有 Context 搜索和详情实现，保留兼容同步入口。',['app/douban/updater.go','app/douban/scheduler.go','app/douban/updater_ownership_test.go'],'旧实现阻塞 HTTP 取消失败已复现；修复和新所有者重启回归通过')
issue('A15','已调整','文件读取与主题取色可取消','来源、备份、主题与字幕共享 FileReader 生命周期；阅读源文本也复用同一入口并保留自身限额。','关闭主题编辑器后的迟到图片不再重新发布全局预览；取色图片和监听随所有者释放。','领域格式和文案仍由调用方处理，不另建平行文件工具库。',['frontend/src/api/fileTransfer.ts','frontend/src/views/settings/ThemeEditorModal.vue','frontend/src/utils/imagePalette.ts','scripts/test-file-transfer-browser.py'],'主题迟到图片已浏览器复现并修复；桌面／390px 及解码取消回归通过')

for it in issues: it['evidence']=[p for p in it['evidence'] if (ROOT/p).exists()]
data=dict(meta=meta,packages=sorted(packages.values(),key=lambda x:x['name']),features=features,issues=issues,routes=routes,bindings=bindings,tables=tables,functions=functions,descs=descs,files=[rel(p) for p in files])
(OUT/'inventory.json').write_text(json.dumps(data,ensure_ascii=False,indent=2),encoding='utf-8')
report=['# CCZJ Video 架构审查地图','',f"快照：{meta['date']}；commit `{meta['commit']}` 加未提交工作区。",'', '[打开交互图示](index.html) · [结构清单 JSON](inventory.json)','',f"结构扫描：{meta['files']} 个源码/验证文件、{meta['packages']} 个 Go 包、{meta['routes']} 条静态页面路由、{meta['bindings']} 个 App 导出绑定候选、{len(features)} 条功能责任链。",'', '范围：全量结构扫描与关键链路源码阅读。未逐函数确认全部功能正确性。当前已运行 Go 测试、前端构建及生命周期原生验收；本批完整验证结果见整改台账。详细验证范围见 docs/project-remediation.md。结构数量不是质量评分。','', '## 整体结构','', '```mermaid','flowchart TB','  UI[视频 / 小说 / 漫画模式壳层] --> API[前端 api / 请求 / DTO / 事件]','  API --> RPC[Wails service.App]','  UI --> HTTP[媒体与图片数据通道]','  RPC --> V[media / detail / source / collection / collect]','  RPC --> R[library / provider / quality]','  RPC --> S[download / danmaku / cast / backup / plugin / update]','  HTTP --> P[proxy / HLS / readingImages]','  V --> DB[SQLite db / 迁移]','  R --> DB','  S --> DB','  V --> N[预算 / 冷却 / 统计 / 路由 / 地址判据]','  R --> N','  P --> N','```','', '图表示责任关系；直接导入关系以 HTML 的模块依赖页为准。','', '## 功能实现','']
for f in features:
    report += [f"### {f['group']} · {f['title']}",'', ' → '.join(f['steps']), '',f['note'],'', '入口：'+ '、'.join(f'`{p}`' for p in f['files']), '']
report += ['## 调整位置','']
for i in issues:
    report += [f"### {i['id']} / {i['priority']} / {i['title']}",'',i['status'],'', '**事实：**'+i['fact'],'','**影响：**'+i['impact'],'','**建议：**'+i['proposal'],'','证据：'+ '、'.join(f'`{p}`' for p in i['evidence']),'']
report += ['## 包覆盖状态','', '| 包 | 责任 | 覆盖状态 |','| --- | --- | --- |']
for p in data['packages']:
    report += [f"| `{p['name']}` | {descs.get(p['name'],descs.get(p['name'].split('/')[0],''))} | 结构已扫描；关键路径见上文；未逐函数验收 |"]
report += ['', '## 验证边界','', 'HTML 交互验证见同目录 ui-validation.json；应用 Go 测试、Windows 退出和视频记录绑定、Android 重建及 HTTP 取消验收范围见 docs/project-remediation.md。最新完整验证 74 个前端测试文件／350 项测试及 Go 验证通过；没有逐函数或逐页面完成全部业务验收。', '',f"源码清单 SHA-256：`{meta['hash']}`。",'']
(OUT/'README.md').write_text('\n'.join(report),encoding='utf-8')
print(json.dumps(dict(meta={key:value for key,value in meta.items() if key!='dirty'},missingReferences=missing,features=len(features),issues=len(issues)),ensure_ascii=False,indent=2))
