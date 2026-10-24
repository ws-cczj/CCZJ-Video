from pathlib import Path
import json,re
ROOT=Path(__file__).resolve().parents[2]
OUT=ROOT/'dist/audit/architecture'
d=json.loads((OUT/'inventory.json').read_text(encoding='utf-8'))
# Every exported binding is assigned to its feature; shared interfaces occur on each relevant chain.
methods=[
  [
    "RestartApp",
    "QuitApp",
    "GetAppVersion",
    "IsSchedulerRunning",
    "ConfirmShutdown",
    "GracefulShutdown"
  ],
  [
    "GetSetting",
    "SetSetting"
  ],
  [
    "GetVideoList",
    "SearchVideos",
    "GetTypes",
    "GetYearsAndAreas"
  ],
  [
    "GetVideoDetail",
    "ProbePlayLines",
    "SpeedTestPlayLines",
    "GetGlobalIdForVideo",
    "FindSourcesByGlobalId",
    "CompressDetailJSONBrotli",
    "DecompressDetailJSONBrotli"
  ],
  [
    "GetVideoDetail",
    "WindowToggleMax",
    "WindowIsMax",
    "WindowSetFullscreen",
    "WindowIsFs",
    "SaveWatchHistory"
  ],
  [
    "DanmakuSearch",
    "DanmakuMatch",
    "DanmakuEpisodes",
    "DanmakuBinding",
    "DanmakuBind",
    "DanmakuUnbind",
    "DanmakuWindow"
  ],
  [
    "AddFavorite",
    "RemoveFavorite",
    "IsFavorite",
    "GetFavorites",
    "SaveWatchHistory",
    "GetRecentHistory",
    "GetHistoryPosition",
    "DeleteHistoryItem",
    "DeleteHistoryByVideo",
    "ClearAllHistory",
    "GetWatchedEpisodes",
    "GetRecommend",
    "GetSimilarVideos"
  ],
  [
    "GetAllSources",
    "GetSourceStats",
    "AddSource",
    "CreateSource",
    "UpdateSource",
    "DeleteSource",
    "GetSourceDetail",
    "RunSourceAction",
    "GetSourceParamsDoc",
    "ExportSource",
    "ImportSource",
    "ImportSourceFromBase64",
    "SearchSource",
    "SearchSourceRequest",
    "ImportSourceVideos",
    "GetGlobalTypes",
    "SetGlobalTypeCollectEnabled",
    "SyncGlobalTypes",
    "SetSourceStrategy",
    "ProbeSources",
    "ProbeSource",
    "SourceProbeTimeline"
  ],
  [
    "StartCollect",
    "PauseCollect",
    "ResumeCollect",
    "StopCollect",
    "GetCollectStatus",
    "GetCollectStatuses",
    "GetCollectSchedule",
    "SetCollectSchedule",
    "TriggerCollectNow",
    "StopBackgroundCollect",
    "SetSourceSchedule"
  ],
  [
    "DoubanSearch",
    "DoubanDetail",
    "DoubanUpdateVideo",
    "DoubanChart",
    "DoubanChartResolve",
    "DoubanTriggerNow",
    "DoubanStatus",
    "DoubanGetAll",
    "GetDoubanComments",
    "GetDoubanIntervalMinutes",
    "SetDoubanIntervalMinutes"
  ],
  [
    "StartVideoDownload",
    "GetDownloadProgress",
    "ListDownloads",
    "PauseDownload",
    "CancelDownload",
    "ResumeDownload",
    "RemoveDownload",
    "GetDownloadDir",
    "SetDownloadDir",
    "OpenFileInExplorer",
    "FileExists"
  ],
  [
    "DeleteVideo",
    "GetRecycleBin",
    "RestoreVideo",
    "PurgeVideo",
    "ClearRecycleBin",
    "ListIdentityMergeCandidates",
    "MergeGlobalVideoIdentities"
  ],
  [
    "DiscoverCastDevices",
    "PlayCastVideo",
    "ControlCastVideo"
  ],
  [
    "NovelBrowseChannels",
    "NovelBrowsePage",
    "LibrarySearch",
    "LibrarySearchPage",
    "LibrarySearchStream",
    "LibraryCatalogPage",
    "LibraryResolveRefs",
    "LibraryItems",
    "LibraryResolveRef"
  ],
  [
    "LibraryItem",
    "LibraryNovelContext",
    "LibraryRefreshNovelContext",
    "LibraryRefreshToc",
    "LibraryRefreshReadingToc",
    "NovelSourceDirectory",
    "NovelSourceAlternatives",
    "NovelChapterMatch"
  ],
  [
    "LibraryChapterText",
    "ReadingStateApply"
  ],
  [
    "ReadingStateLoad",
    "ReadingStateApply"
  ],
  [
    "ComicBrowseChannels",
    "ComicBrowsePage",
    "ComicTrending",
    "LibrarySearch",
    "LibrarySearchPage",
    "LibrarySearchStream",
    "LibraryCatalogPage",
    "LibraryResolveRefs",
    "LibraryItems",
    "LibraryResolveRef"
  ],
  [
    "LibraryItem",
    "LibraryComicContext",
    "LibraryRefreshComicContext",
    "LibraryRefreshToc",
    "LibraryRefreshReadingToc",
    "ComicSourceDirectory",
    "ComicSourceAlternatives",
    "ComicEpisodeMatch"
  ],
  [
    "LibraryChapterPages",
    "LibraryRefreshChapterPages",
    "ReadingImageURL",
    "ReadingStateApply"
  ],
  [
    "ReadingStateLoad",
    "ReadingStateApply"
  ],
  [
    "LibrarySources",
    "LibraryAddSource",
    "LibraryUpdateSource",
    "LibraryDeleteSource",
    "LibrarySetSourceEnabled",
    "LibraryTestSource",
    "LibraryPreviewSources",
    "LibraryCheckSourceCandidate",
    "LibraryImportSources"
  ],
  [
    "GetSetting",
    "SetSetting",
    "GetCloseBehavior",
    "SetCloseBehavior",
    "SetTitleBarTheme",
    "WindowSetResizable",
    "WindowGetResizable",
    "WindowSetSize",
    "WindowGetSize",
    "ApplyWindowSettings"
  ],
  [
    "ListPlugins",
    "RescanPlugins",
    "SetPluginEnabled",
    "GetPluginDirectory",
    "ReadPluginFile",
    "ReadPluginAsset",
    "OpenPluginDirectory",
    "InstallPlugin",
    "UninstallPlugin"
  ],
  [
    "BackupExport",
    "BackupImportFromBase64",
    "BackupArchives",
    "BackupRestoreArchive",
    "BackupExports",
    "BackupImportFile",
    "OpenFolder",
    "LegacyDataNotice",
    "LegacyDataMerge",
    "SchemaNotice"
  ],
  [
    "CheckUpdate",
    "DownloadUpdate",
    "InstallUpdate",
    "IgnoreVersion",
    "GetIgnoredVersion",
    "GetLastStartVersion",
    "SaveLastStartVersion",
    "GetPendingUpdateInfo",
    "ClearPendingUpdateInfo",
    "GetLastInstallReport",
    "GetSwapBackupInfo",
    "RollbackUpdate",
    "ScheduleInstallOnExit",
    "CancelInstallOnExit",
    "GetPendingInstall"
  ],
  [
    "GetRuntimeMetrics",
    "GetDiagnostics",
    "GetLogKeepDays",
    "SetLogKeepDays",
    "ExportDiagnosticsReport",
    "GetDiagnosticsRecordingStatus",
    "StartDiagnosticsRecording",
    "StopDiagnosticsRecording",
    "AddDiagnosticsRecordingMarker",
    "WriteLog",
    "GetLogList",
    "GetLogContent",
    "GetLogDir",
    "ClearLogs",
    "GetLogLevel",
    "SetLogLevel",
    "GetLogRecords",
    "GetLogStats",
    "GetLogFiles",
    "ReadLogPage",
    "ExportLogs",
    "StartLogStream",
    "StopLogStream"
  ],
  [],
  [
    "ProxyImage",
    "SetAllowPrivateNetwork",
    "GetAllowPrivateNetwork"
  ],
  [],
  []
]
domains=[['service','handoff','storagepaths'],['service','settings','lifecycle'],['media','db','collect'],['media','detail','proxy'],['proxy','media','window'],['danmaku'],['media','db'],['source','db'],['collection','source','collect'],['douban'],['download/engine','download','db'],['media','db','cache'],['cast'],['library','provider'],['library','library/novel','library/matching'],['library','provider','readingstate'],['readingstate','db'],['library','provider'],['library','library/comic','library/matching'],['library','provider','proxy'],['readingstate','db'],['library','library/quality','provider'],['settings','window'],['plugin'],['backup','db'],['update','updater','handoff'],['operation','applog','diagnosticrecording','resource','netstats'],['service','lifecycle','apperror','workflight'],['netbudget','netpolicy','netstats','outbound','netaddress','proxy'],['service','proxy','window'],['service','provider']]
resources=[
'WebView2 / Android WebView|应用窗口 · 静态资源 · 日志 · SQLite',
'modeTasks / background|停止旧工作 → 保存模式 → 文档导航',
'SQLite 视频目录 / sources|远程源站（搜索/采集按操作触发）',
'详情内存缓存 / SQLite|源站回源 · 多播放线路',
'HTML media / hls.js|/__cczj/hls · Android media · 观看进度',
'弹幕 HTTP / SQLite Store|集数绑定 · 时间窗口',
'favorites / watch_history|视频身份 · 本地进度旧键',
'sources / source_types / global_types|来源配置 · 导入导出文件',
'collect_cursor / 视频目录|modeTasks · 采集进度事件',
'豆瓣榜单 / 补全 / 冷却记录|远程站点 · 缓存 · 定时工作',
'download_tasks / 下载文件|Executor：2 并发 / 64 等待 · 断点',
'SQLite v20 / 退役坐标 / 身份合并|提交影响范围 → 缓存失效 · 用户记录保留',
'局域网设备 / SSDP / SOAP|Android 组播锁 · 系统投屏设置',
'provider 会话 / 远程小说源|library_items · 渐进搜索事件',
'library_chapters / 路由证据|小说章节匹配 · 源站目录',
'textCache 256MiB / 30天|正文 · 预取 · SQLite 阅读进度',
'reading_state / reading_state_imports|kind + sourceKey + remote identity',
'provider 会话 / 远程漫画源|趋势缓存 · library_items',
'library_chapters / 路由证据|卷 / 话次匹配 · 来源目录',
'/__cczj/img / 图片缓存|Referer + 源请求头 · 页码进度',
'reading_state / reading_state_imports|kind + sourceKey + remote identity',
'sources / 质量与健康记录|共享五路阅读闸门 · 维护任务',
'settings / 主题变量 / locales|写入队列 · 桌面窗口状态',
'plugins 文件 / 启用与信任记录|前端脚本执行 / 资源清理',
'压缩备份 / 迁移归档 / SQLite|合并恢复 · 文件选择 / 分享',
'更新包 / 完整性 / 安装报告|Windows 替换交接 · Android APK',
'滚动日志 / 诊断文件 / 指标|前端错误 · 网络统计 · 录制导出',
'生成 .js bindings / 请求身份|结构化错误 · 事件订阅 · I/O 取消',
'远程源站 / CDN / 公网代理|预算 / 冷却 / 地址判据 · 缓存',
'Activity / WebView / JNI|播放设备 · 文件 · 分享 · 安装器',
'Windows / APK / bindings / 质量报告|Go tests · 前端 tests · build · 发布']
special={0:['handoff.Await','service.NewApp','App.ServiceStartup'],27:['requestPipelines.run','runRequest','lifecycle.WithShutdown'],28:['Budget.Wrap','netpolicy.WrapTransport','outbound.Install','proxy.SafeDialContext'],29:['WailsJSBridge.invokeAsync','WailsBridge.handleRuntimeCall','RegisterAndroidMain'],30:['generate-bindings.ps1','verify.ps1','wails3 generate bindings']}
by_name={b['name']:b for b in d['bindings']}
assert set(by_name) == {name for chain in methods for name in chain}, 'public bindings missing from feature map'
for i,f in enumerate(d['features']):
    assert i<len(methods)
    fs=[]
    for name in methods[i]:
        assert name in by_name,name
        b=by_name[name].copy()
        source=(ROOT/b['file']).read_text(encoding='utf-8-sig')
        start=re.search(r'^func \(a \*App\) '+name+r'\(',source,re.M)
        assert start, name+' no longer exists in current source'
        b['line']=source[:start.start()].count('\n')+1
        end=re.search(r'^func ',source[start.end():],re.M)
        body=source[start.start():start.end()+end.start() if end else len(source)]
        b['calls']=list(dict.fromkeys(re.findall(r'\b((?:a\.[a-zA-Z]+|sourceservice|collectionservice|db|lifecycle|operation|backupservice|fileservice)\.[A-Za-z]\w*)\(',body)))
        fs.append(b)
    f.update(interfaces=fs,modules=domains[i],resource=resources[i].split('|'),special=special.get(i,[]),index=i)
d['graphNote']='关键绑定为人工归类；接口内部直接调用名来自当前 Go 方法文本扫描；跨层连线表示责任路径，不是精确完整动态调用图。'
(OUT/'graph-data.json').write_text(json.dumps(d,ensure_ascii=False,indent=2),encoding='utf-8')
t=Path(__file__).with_name('canvas.html').read_text(encoding='utf-8')
(OUT/'index.html').write_text(t.replace('__DATA__',json.dumps(d,ensure_ascii=False).replace('</','<\\/')),encoding='utf-8')
print(json.dumps({'features':len(d['features']),'mappedInterfaces':sum(len(f['interfaces']) for f in d['features']),'packages':len(d['packages'])}))
