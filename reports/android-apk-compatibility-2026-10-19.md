# 小米 15 APK 安装兼容性排查

用户设备为小米 15、HyperOS 3。排查时没有已连接且可用的真机，因此下面区分包级验证与真机验证。

## 包级证据

`aapt dump badging` 检查原来的 `bin/CCZJ-Video-android-amd64-preview.apk` 和 `bin/CCZJ-Video-android-amd64-acceptance.apk`，两者均为：

```text
sdkVersion:'24'
targetSdkVersion:'35'
native-code: 'x86_64'
```

ZIP 内均只有 `lib/x86_64/libwails.so`，没有 `arm64-v8a`。原 preview APK 的 v2 签名验证通过。对于 ARM64 手机，缺少匹配的原生库已构成安装兼容性问题；尚未通过手机安装器日志取得具体错误码。

Android 对 APK 中原生库的架构匹配规则见 [Android ABI 文档](https://developer.android.com/ndk/guides/abis)。

## 修正

- 实际重新编译 AArch64 Go shared library，生成 `bin/CCZJ-Video-android-arm64-preview.apk`，没有通过改文件名转换架构。
- PowerShell 构建脚本和 Gradle 默认目标改为 ARM64。
- `android:build` / `android:run` 使用 `ANDROID_ARCH`，默认 `arm64`；MuMu 显式使用 `ANDROID_ARCH=amd64`。这是对原有 `ARCH` 参数的迁移，README 和 Android 构建说明已同步。Task 自带的 `ARCH` 表示电脑架构，仅在模板上使用 `default "arm64"` 不能覆盖其 `amd64` 值；实际 dry-run 已验证这一点。
- `android:dev` 保留原来的 MuMu 流程与 `ARCH` 参数。
- 修复 Windows PowerShell 5 在探测 Java 8 时因 `javac -version` 写入 stderr 而提前退出的问题，使脚本能够继续找到同目录下的 JDK 21；机器默认 Java 不变。

## 验证结果

原包执行以下检查返回 exit code 1，输出 `FAIL: arm64-v8a native library`；对新包检查同一条目通过：

```powershell
python -c "import sys,zipfile; p='bin/CCZJ-Video-android-amd64-preview.apk'; z=zipfile.ZipFile(p); ok='lib/arm64-v8a/libwails.so' in z.namelist(); print(('PASS' if ok else 'FAIL')+': arm64-v8a native library'); sys.exit(0 if ok else 1)"
```

这验证原生库架构缺失，不能替代真机安装。

最终从仓库根目录运行 `wails3 task android:build` 成功。包含前端 TypeScript 检查、生产前端构建、Android Go shared library 编译、Gradle APK 构建；日志在 `dist/android-arm64-task-build.log`。默认 build/run、显式 MuMu build/run、dev 默认及 ARM64 覆盖值均通过实际 CLI dry-run 检查。

最终 APK 信息：

| 项目 | 结果 |
| --- | --- |
| 包名 | `com.cczj.video.preview` |
| 架构 | 仅 `arm64-v8a`；ELF machine 为 AArch64 |
| 系统版本 | 最低 API 24 / Android 7.0，target API 35 |
| 签名 | `apksigner verify --verbose` 通过，v2 签名 |
| 文件完整性 | ZIP CRC 检查通过 |
| 原生库对齐 | 所有 ELF LOAD 段均按 `0x4000` / 16 KB 对齐，偏移与地址满足同余要求 |
| APK 对齐 | `zipalign -c -P 16 4` 通过 |
| 大小 | 41,699,755 字节 |
| SHA256 | `6cb019a99a2e4a4aa1d1319b46b2fef67f45ddacd6805a4ee2f3aa6d747144f8` |

16 KB 对齐检查依据 [Android 页面大小说明](https://developer.android.com/guide/practices/page-sizes) 和 [zipalign 文档](https://developer.android.com/tools/zipalign)。这是二进制布局检查，不等同于已在 16 KB 真机运行。

未运行小米真机安装、启动或播放验证，也未将 x86_64 模拟器验证等同于真机结果。APK 仍是开发签名的实验预览版。
