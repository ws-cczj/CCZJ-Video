"""Build the local Wails app on macOS; prepare-only also works on Windows.

Device output is for re-signing with the user's own Apple account. No Apple
credentials or provisioning profiles are needed by this build process.
"""
import argparse
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import subprocess
import sys
import zipfile

ROOT = Path(__file__).resolve().parents[2]
GENERATED = ROOT / "build/ios/generated"
MIN_IOS = "16.0"


def run(*args, env=None, capture=False):
    result = subprocess.run([str(arg) for arg in args], cwd=ROOT, env=env,
                            check=True, text=True, encoding="utf-8", errors="replace",
                            stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.STDOUT if capture else None)
    return result.stdout.strip() if capture else None


def prepare(bundle_id):
    version = json.loads((ROOT / "version.json").read_text(encoding="utf-8"))["version"]
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise ValueError("iOS requires a numeric major.minor.patch version")
    if not re.fullmatch(r"[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+){2,}", bundle_id):
        raise ValueError("Use a reverse-DNS bundle identifier, without spaces")
    expected = run("go", "list", "-m", "-f", "{{.Version}}", "github.com/wailsapp/wails/v3", capture=True)
    if expected not in run("wails3", "version", capture=True):
        raise RuntimeError(f"Install the matching Wails CLI: {expected}")
    GENERATED.mkdir(parents=True, exist_ok=True)
    # JSON is valid YAML and avoids an extra parser/dependency. Version has one
    # authority, version.json, rather than a second maintained iOS version.
    config = GENERATED / "config.yml"
    config.write_text(json.dumps({"ios": {"bundleID": bundle_id, "displayName": "CCZJ Video",
        "version": version, "minIOSVersion": MIN_IOS}}, indent=2), encoding="utf-8")
    run("wails3", "ios", "overlay:gen", "-out", GENERATED / "overlay.json", "-config", config)
    run("wails3", "ios", "xcode:gen", "-outdir", GENERATED / "xcode", "-config", config)
    plist_path = GENERATED / "xcode/main/Info.plist"
    with plist_path.open("rb") as source:
        info = plistlib.load(source)
    info["CFBundleIdentifier"] = bundle_id
    info["CFBundleShortVersionString"] = version
    info["CFBundleVersion"] = version
    # Native HLS streams only through our capability-protected loopback bridge.
    # Go handles external source requests; do not exempt arbitrary web hosts.
    info["NSAppTransportSecurity"] = {"NSAllowsLocalNetworking": True,
        "NSExceptionDomains": {"127.0.0.1": {"NSExceptionAllowsInsecureHTTPLoads": True}}}
    # DLNA discovery is deliberately unavailable on iOS until its multicast
    # entitlement and permission flow have been implemented and verified.
    with plist_path.open("wb") as destination:
        plistlib.dump(info, destination)
    return version, info


def build(target, version, info, skip_frontend):
    if sys.platform != "darwin":
        raise RuntimeError("iOS compilation needs macOS and full Xcode; use --prepare-only on Windows")
    sdk = "iphoneos" if target == "device" else "iphonesimulator"
    arch = "arm64" if target == "device" or platform.machine() == "arm64" else "amd64"
    clang_arch = "x86_64" if arch == "amd64" else arch
    clang_target = f"{clang_arch}-apple-ios{MIN_IOS}" + ("-simulator" if target == "simulator" else "")
    sdk_path = run("xcrun", "--sdk", sdk, "--show-sdk-path", capture=True)
    clang = run("xcrun", "--sdk", sdk, "--find", "clang", capture=True)
    if not skip_frontend:
        run("npm", "--prefix", "frontend", "run", "build")
    out = ROOT / "bin/ios" / target
    out.mkdir(parents=True, exist_ok=True)
    archive = out / "CCZJVideo.a"
    env = os.environ.copy()
    env.update(GOWORK="off", GOOS="ios", GOARCH=arch, CGO_ENABLED="1", CC=clang,
        CGO_CFLAGS=f'-isysroot "{sdk_path}" -target {clang_target}',
        CGO_LDFLAGS=f'-isysroot "{sdk_path}" -target {clang_target}')
    run("go", "build", "-buildmode=c-archive", "-overlay", GENERATED / "overlay.json",
        "-tags", "ios,production", "-buildvcs=false", "-trimpath", "-ldflags",
        f"-w -s -X cczjVideo/app/updater.Version={version}", "-o", archive, ".", env=env)
    app = out / "CCZJVideo.app"
    # Only replace our generated bundle, never an arbitrary path or user install.
    if app.exists():
        if app.is_symlink() or app.resolve().parent != out.resolve():
            raise RuntimeError("Refusing to replace an unexpected app bundle path")
        shutil.rmtree(app)
    app.mkdir()
    executable = app / info["CFBundleExecutable"]
    frameworks = ["Foundation", "UIKit", "WebKit", "Security", "CoreFoundation",
        "UniformTypeIdentifiers", "LocalAuthentication", "UserNotifications", "AVFoundation",
        "CoreLocation", "CoreMotion", "SystemConfiguration", "Network"]
    framework_flags = [arg for name in frameworks for arg in ("-framework", name)]
    run(clang, "-target", clang_target, "-isysroot", sdk_path, *framework_flags, "-lresolv",
        "-o", executable, GENERATED / "xcode/main/main.m", f"-Wl,-force_load,{archive}")
    shutil.copy2(GENERATED / "xcode/main/Info.plist", app / "Info.plist")
    info["CFBundleSupportedPlatforms"] = ["iPhoneOS" if target == "device" else "iPhoneSimulator"]
    info["UIDeviceFamily"] = [1, 2]
    info["DTPlatformName"] = sdk
    if target == "simulator":
        info.pop("UIRequiredDeviceCapabilities", None)
    run("xcrun", "ibtool", "--compile", app / "LaunchScreen.storyboardc",
        "--minimum-deployment-target", MIN_IOS, GENERATED / "xcode/main/LaunchScreen.storyboard")
    icon_info = out / "icons.plist"
    run("xcrun", "actool", "--compile", app, "--app-icon", "AppIcon", "--platform", sdk,
        "--minimum-deployment-target", MIN_IOS, "--product-type", "com.apple.product-type.application",
        "--target-device", "iphone", "--target-device", "ipad", "--output-partial-info-plist", icon_info,
        GENERATED / "xcode/main/Assets.xcassets")
    with icon_info.open("rb") as source:
        info.update(plistlib.load(source))
    with (app / "Info.plist").open("wb") as destination:
        plistlib.dump(info, destination)
    # Simulator uses ad-hoc signing. Device builds carry a local integrity
    # signature only and MUST be re-signed before installation on an iPhone.
    run("codesign", "--force", "--sign", "-", app)
    run("codesign", "--verify", "--strict", app)
    if target == "device":
        ipa = out / f"CCZJ-Video-ios-{version}-resign.ipa"
        with zipfile.ZipFile(ipa, "w", zipfile.ZIP_DEFLATED) as bundle:
            for path in sorted(app.rglob("*")):
                if path.is_file():
                    bundle.write(path, Path("Payload") / app.name / path.relative_to(app))
        print(f"IPA (requires personal signing): {ipa}", flush=True)
    print(f"APP: {app}", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=("device", "simulator"), default="device")
    parser.add_argument("--bundle-id", default="com.cczj.video")
    parser.add_argument("--prepare-only", action="store_true")
    parser.add_argument("--skip-frontend", action="store_true")
    args = parser.parse_args()
    if not args.prepare_only and sys.platform != "darwin":
        parser.error("Build on macOS with Xcode, or pass --prepare-only")
    version, info = prepare(args.bundle_id)
    if not args.prepare_only:
        build(args.platform, version, info, args.skip_frontend)


if __name__ == "__main__":
    main()
