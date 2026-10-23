"""Real diagnostics panel lifecycle, memory display and report export checks.

Requires an isolated Vite server and Python Playwright; only Wails bindings are mocked.
"""
import argparse
import json
from pathlib import Path
import re

from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--url', default='http://127.0.0.1:9248')
    parser.add_argument('--channel', default=None)
    parser.add_argument('--lifecycle-only', action='store_true')
    args = parser.parse_args()
    bindings = (ROOT / 'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    names = re.findall(r'export function (\w+)\(', bindings)
    special = {'GetRuntimeMetrics', 'GetDiagnostics', 'ListIdentityMergeCandidates', 'ExportDiagnosticsReport', 'GetDiagnosticsRecordingStatus', 'StartDiagnosticsRecording', 'StopDiagnosticsRecording', 'AddDiagnosticsRecordingMarker'}
    mock = '\n'.join(f'export async function {name}() {{ return {"[]" if name in ("ListPlugins", "RescanPlugins") else chr(34)+chr(34)}; }}' for name in names if name not in special)
    mock += '''
      export async function GetRuntimeMetrics() {
        window.__metricCalls = (window.__metricCalls || 0) + 1;
        const value = structuredClone(window.__metrics);
        value.resources.sampled_at_ms = Date.now();
        value.recording = structuredClone(window.__recording);
        return value;
      }
      export async function GetDiagnostics() { return window.__diagnostics; }
      export async function ListIdentityMergeCandidates() { window.__candidateCalls = (window.__candidateCalls || 0) + 1; return {groups: []}; }
      export async function ExportDiagnosticsReport(frontend) {
        window.__exported = JSON.parse(frontend);
        return {path:'C:/diagnostics/report.json',bundle_path:window.__recording.path && !window.__recording.active ? 'C:/diagnostics/session.zip' : '',heap_profile_path:'C:/diagnostics/heap.pprof',goroutine_profile_path:'C:/diagnostics/goroutines.txt'};
      }
      export async function GetDiagnosticsRecordingStatus() { return structuredClone(window.__recording); }
      export async function StartDiagnosticsRecording() {
        Object.assign(window.__recording, {active:true,path:'C:/diagnostics/recording.jsonl',started_at_ms:Date.now(),samples:1});
        return structuredClone(window.__recording);
      }
      export async function StopDiagnosticsRecording() {
        Object.assign(window.__recording, {active:false,reason:'manual',ended_at_ms:Date.now()});
        return structuredClone(window.__recording);
      }
      export async function AddDiagnosticsRecordingMarker(kind, payload) {
        if (!window.__recording.active) return false;
        window.__recording.markers++;
        window.__recordingMarkers.push({kind,data:JSON.parse(payload)});
        return true;
      }
    '''
    metrics = {'uptime_seconds': 120, 'goroutines': 20, 'cache': [], 'network': {'categories': []},
               'resources': {'sampled_at_ms': 1, 'native_available': True, 'private_bytes': 100*1024**2,
                             'tree_private_bytes': 350*1024**2, 'working_set_bytes': 80*1024**2,
                             'system_total_bytes': 16*1024**3, 'system_available_bytes': 4*1024**3,
                             'system_memory_percent': 75, 'working_set_percent': 0.5, 'cpu_percent': 2.5,
                             'heap_alloc_bytes': 20*1024**2, 'heap_retained_bytes': 5*1024**2,
                             'goroutines': 20, 'handles': 120, 'process_count': 4, 'num_gc': 3, 'notes': [],
                             'processes': [{'pid': 123, 'name': 'cczjVideo.exe', 'private_bytes': 100*1024**2, 'working_set_bytes': 80*1024**2, 'handles': 70}]}}
    diagnostics = {'env': {'app_version': 'dev', 'goos': 'windows', 'goarch': 'amd64', 'schema_version': 10},
                   'storage': {}, 'douban': {}, 'collect': {}, 'sources': [{'source_key': str(i), 'name': f'Source {i:02d}', 'enabled': True, 'api_url': f'https://source-{i}.example/api', 'last_error_kind': 'network' if i == 0 else '', 'last_error': 'Connection timed out' if i == 0 else ''} for i in range(40)], 'tables': [], 'notes': []}
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True, channel=args.channel)
        try:
            page = browser.new_page(viewport={'width': 1280, 'height': 900})
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
            page.add_init_script('window.__metrics = '+json.dumps(metrics)+';window.__diagnostics = '+json.dumps(diagnostics)+';window.__recording = {active:false,path:"",samples:0,markers:0,started_at_ms:0,ended_at_ms:0,error:"",reason:""};window.__recordingMarkers = [];')
            page.goto(args.url + '/tests/browser/diagnostics.html')
            page.wait_for_load_state('networkidle')
            expect(page.locator('.diag')).to_be_visible()
            page.wait_for_function('(window.__metricCalls || 0) >= 1')
            page.evaluate('window.diagnosticsTest.state.active = false')
            page.wait_for_timeout(100)
            before = page.evaluate('window.__metricCalls')
            page.wait_for_timeout(2400)
            assert page.evaluate('window.__metricCalls') == before, 'hidden retained panel kept polling backend metrics'
            page.evaluate('window.diagnosticsTest.state.active = true')
            page.wait_for_function('(window.__metricCalls || 0) > '+str(before))
            if not args.lifecycle_only:
                expect(page.locator('.resource-monitor')).to_contain_text('350.0 MB')
                expect(page.locator('.resource-monitor')).to_contain_text('2.5%')
                page.get_by_role('button', name='导出诊断报告', exact=True).click()
                page.wait_for_function('!!window.__exported')
                assert len(page.evaluate('window.__exported.resources.history')) <= 150
                assert page.evaluate('window.__candidateCalls || 0') == 0, 'merge candidates loaded before their section opened'
                header_before = page.locator('.diag-page-header').bounding_box()
                page.locator('.diag-nav [data-section="collection"]').click()
                expect(page.locator('.source-table-wrap tbody tr')).to_have_count(40)
                page.locator('.diag-content').evaluate('(el) => el.scrollTop = el.scrollHeight')
                header_after = page.locator('.diag-page-header').bounding_box()
                assert header_before['y'] == header_after['y'], 'header scrolled out of the workspace'
                assert page.locator('.main-content').evaluate('(el) => el.scrollHeight <= el.clientHeight + 1'), 'outer page gained another vertical scrollbar'
                assert page.locator('.diag-table-wrap').evaluate_all('(els) => els.every(el => el.scrollHeight <= el.clientHeight + 1)'), 'tables kept nested vertical scrollbars'
                page.get_by_role('searchbox', name='搜索采集源或接口地址').fill('Source 00')
                expect(page.locator('.source-table-wrap tbody tr')).to_have_count(1)
                page.get_by_role('searchbox').fill('')
                page.get_by_role('button', name='只看异常', exact=True).click()
                expect(page.locator('.source-table-wrap tbody tr')).to_have_count(1)
                expect(page.locator('.diag-source-error')).to_contain_text('Connection timed out')
                page.locator('.diag-nav [data-section="douban"]').click()
                page.wait_for_function('window.__candidateCalls === 1')
                page.locator('.diag-nav [data-section="overview"]').click()
                expect(page.locator('.resource-monitor')).to_be_visible()
                page.get_by_role('button', name='导出诊断报告', exact=True).click()
                assert len(page.evaluate('window.__exported.resources.history')) > 0, 'section changes lost the resource history'
                page.get_by_role('button', name='开始记录', exact=True).click()
                expect(page.get_by_role('button', name='结束并导出', exact=True)).to_be_visible()
                page.wait_for_function('window.__recordingMarkers.some(e => e.kind === "recording_start")')
                page.evaluate('window.diagnosticsTest.state.active = false; window.diagnosticsTest.perf.beginLoad("media.example"); window.diagnosticsTest.perf.markFirstFrame(); window.diagnosticsTest.perf.addStall(1200); window.diagnosticsTest.perf.addFrames(1000,10); window.diagnosticsTest.perf.addWatched(15);')
                page.wait_for_function('window.__recordingMarkers.some(e => e.kind === "stall")')
                hidden_calls = page.evaluate('window.__metricCalls')
                page.wait_for_function('window.__recordingMarkers.some(e => e.kind === "playback_snapshot")', timeout=10000)
                assert page.evaluate('window.__metricCalls') == hidden_calls, 'recording restarted hidden panel polling'
                page.evaluate('window.diagnosticsTest.state.active = true; window.__exported = null')
                page.get_by_role('button', name='结束并导出', exact=True).click()
                page.wait_for_function('!!window.__exported')
                expect(page.locator('.diag-export-bar')).to_contain_text('session.zip')
                assert page.evaluate('window.__exported.playback.frames.dropped') == 10
                assert page.evaluate('window.__exported.playback.watched_seconds') == 15
                assert page.evaluate('window.__exported.recording.active') is False
                assert page.evaluate('window.__recordingMarkers.some(e => e.kind === "recording_stop")')
                marker_count = page.evaluate('window.__recordingMarkers.length')
                page.wait_for_timeout(5300)
                assert page.evaluate('window.__recordingMarkers.length') == marker_count, 'ended session left its frontend recording observer running'
                preview = ROOT / 'dist/audit/diagnostics-preview.png'
                preview.parent.mkdir(parents=True, exist_ok=True)
                page.screenshot(path=str(preview), full_page=False)
                page.evaluate('window.__metrics.resources.native_available = false; window.__metrics.resources.sampled_at_ms++')
                page.wait_for_timeout(2300)
                expect(page.locator('.resource-monitor')).to_contain_text('不可用')
            page.evaluate('window.diagnosticsTest.state.mounted = false')
            page.wait_for_timeout(100)
            before = page.evaluate('window.__metricCalls')
            page.wait_for_timeout(2400)
            assert page.evaluate('window.__metricCalls') == before, 'unmounted panel kept polling'
            if not args.lifecycle_only:
                page.goto(args.url + '/tests/browser/diagnostics.html?settings')
                page.wait_for_load_state('networkidle')
                expect(page.locator('.settings-page.is-diagnostics .diag')).to_be_visible()
                assert page.locator('.main-content').evaluate('(el) => el.scrollHeight <= el.clientHeight + 1'), 'Settings diagnostic page exceeded host height'
                page.locator('.diag-nav [data-section="collection"]').click()
                page.locator('.diag-content').evaluate('(el) => el.scrollTop = el.scrollHeight')
                expect(page.get_by_role('button', name='导出诊断报告', exact=True)).to_be_in_viewport()
                assert page.locator('.main-content').evaluate('(el) => el.scrollTop === 0'), 'Settings host scrolled instead of section content'
                page.locator('.diag-nav [data-section="overview"]').click()
                page.screenshot(path=str(ROOT / 'dist/audit/diagnostics-settings-preview.png'))
                # Match the narrower content area beside the application's sidebar.
                page.locator('.main-content').evaluate("el => el.style.width = '980px'")
                assert page.locator('.diag-nav').evaluate("el => getComputedStyle(el).flexDirection") == 'row', 'navigation ignored its available content width'
                page.locator('.main-content').evaluate("el => el.style.width = ''")
                page.set_viewport_size({'width': 760, 'height': 800})
                page.locator('.diag-nav [data-section="network"]').click()
                expect(page.get_by_role('button', name='导出诊断报告', exact=True)).to_be_in_viewport()
                assert page.locator('.main-content').evaluate('(el) => el.scrollWidth <= el.clientWidth + 1'), 'narrow page overflows horizontally'
                page.screenshot(path=str(ROOT / 'dist/audit/diagnostics-narrow-preview.png'))
            assert not errors, errors
            print('Diagnostics browser checks passed: page layout, background playback events across hidden views, stop/export ZIP, full playback snapshot, timer cleanup, native metrics and lifecycle')
        finally:
            browser.close()


if __name__ == '__main__':
    main()
