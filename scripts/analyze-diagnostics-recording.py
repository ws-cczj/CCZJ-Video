"""Analyse a local diagnostic ZIP/JSONL without extracting or executing its contents."""
import argparse
import collections
import datetime as dt
import json
from pathlib import Path
import statistics
import zipfile

MIB = 1024 ** 2


def read_inputs(path):
    if path.is_dir():
        path = max(path.glob('diagnostics-session-*.zip'), key=lambda item: item.stat().st_mtime)
    if path.suffix == '.zip':
        with zipfile.ZipFile(path) as archive:
            timeline = [item for item in archive.infolist() if item.filename.endswith('.jsonl')]
            reports = [item for item in archive.infolist() if item.filename.endswith('.json')]
            if len(timeline) != 1 or len(reports) != 1:
                raise ValueError('Expected one timeline and one final report')
            if timeline[0].file_size > 64 * MIB or reports[0].file_size > 4 * MIB:
                raise ValueError('Diagnostic inputs exceed analysis size bounds')
            # zip.read validates CRC, and no archive paths are used for extraction.
            raw = archive.read(timeline[0]).decode('utf-8')
            report = json.loads(archive.read(reports[0]))
    else:
        raw, report = path.read_text(encoding='utf-8'), {}
    records, incomplete_tail = [], False
    lines = raw.splitlines()
    for index, line in enumerate(lines):
        if not line.strip():
            continue
        try:
            records.append(json.loads(line))
        except json.JSONDecodeError:
            if index != len(lines) - 1:
                raise
            incomplete_tail = True
    samples = [item['resources'] for item in records if item.get('type') == 'sample']
    if len(samples) < 2:
        raise ValueError('Need at least two native resource samples')
    if any(b['sampled_at_ms'] < a['sampled_at_ms'] for a, b in zip(samples, samples[1:])):
        raise ValueError('Resource sample timestamps moved backwards')
    end = next((item['status'] for item in reversed(records) if item.get('type') == 'end'), None)
    if end and end['samples'] != len(samples):
        raise ValueError('Recorded sample count differs from the end marker')
    return path, records, samples, report, incomplete_tail


def clock(value):
    return dt.datetime.fromtimestamp(value / 1000).strftime('%H:%M:%S')


def metric(samples, key, scale=1):
    values = [item[key] / scale for item in samples if item.get(key) is not None]
    return {'first': values[0], 'last': values[-1], 'min': min(values), 'max': max(values), 'median': statistics.median(values)} if values else None


def resource_scope(samples):
    scopes = {row.get('native_scope') or ('process_tree' if row.get('native_available') else 'unavailable') for row in samples}
    if len(scopes) != 1:
        return 'mixed', None, '不同采样范围，请分别分析'
    scope = next(iter(scopes))
    if scope == 'app_process':
        if any(row.get('proportional_bytes') is not None for row in samples):
            return scope, 'proportional_bytes', 'Android 应用进程 PSS（不含隔离 WebView/GPU）'
        return scope, 'working_set_bytes', 'Android 应用进程 RSS（不含隔离 WebView/GPU）'
    if scope == 'process_tree':
        return scope, 'tree_private_bytes', '应用进程树私有提交'
    return scope, None, '原生进程内存不可用'


def architecture_metrics(events):
    """Describe observed player ownership without treating missing data as zero.

    Perf snapshots are cumulative; first_frame events hold individual load times.
    This function does not compare unrelated recordings or infer memory savings.
    """
    sessions, revisions, presentations = {}, set(), set()
    for event in events:
        data = event.get('data')
        if not isinstance(data, dict):
            continue
        if data.get('ui_architecture_revision'):
            revisions.add(data['ui_architecture_revision'])
        if data.get('presentation'):
            presentations.add(data['presentation'])
        identity = data.get('player_session_id')
        if not identity:
            continue
        session = sessions.setdefault(identity, {
            'revision': data.get('ui_architecture_revision'),
            'presentation': data.get('presentation'), 'event_counts': {},
            'first_frame_ms': [], 'maximum_active_players': None,
            'maximum_resource_providers': None, 'history': None,
            'source_catalog': None, 'closed_at_ms': None,
            'issue_groups': {}, 'presentation_adapters': [],
        })
        kind = event.get('kind', 'unknown')
        session['event_counts'][kind] = session['event_counts'].get(kind, 0) + 1
        if kind == 'first_frame' and isinstance(data.get('ms'), (int, float)):
            session['first_frame_ms'].append(data['ms'])
        if kind == 'player_closed':
            session['closed_at_ms'] = event.get('at_ms')
        if kind == 'playback_page_closed':
            session['history'] = data.get('history')
            session['source_catalog'] = data.get('source_catalog')
        browser = data.get('browser_resources') or {}
        architecture = browser.get('architecture') or {}
        player = next((row.get('snapshot') for row in browser.get('players', [])
                       if row.get('session_id') == identity), None) or browser.get('player') or {}
        presentation = player.get('presentation') or {}
        if presentation and presentation not in session['presentation_adapters']:
            session['presentation_adapters'].append(presentation)
        issues = list(data.get('issues') or []) + list(player.get('issues') or [])
        if kind == 'player_issue':
            issues.append(data)
        for issue in issues:
            if not isinstance(issue, dict) or not isinstance(issue.get('count'), (int, float)):
                continue
            key = json.dumps([issue.get('operation'), issue.get('code'), issue.get('first_at_ms')])
            previous = session['issue_groups'].get(key)
            if previous is None or issue['count'] >= previous.get('count', 0):
                session['issue_groups'][key] = {field: issue.get(field) for field in
                    ('operation', 'code', 'recoverable', 'count', 'first_at_ms', 'last_at_ms')}

        for field, value in [
            ('maximum_active_players', architecture.get('active_player_count')),
            ('maximum_resource_providers', browser.get('registered_player_count')),
        ]:
            if isinstance(value, (int, float)):
                session[field] = max(value, session[field]) if session[field] is not None else value
    for session in sessions.values():
        session['issue_groups'] = list(session['issue_groups'].values())
        history = session['history'] or {}
        incoming, visible = history.get('progress_events'), history.get('progress_ui_updates')
        session['progress_ui_update_ratio'] = visible / incoming if incoming and visible is not None else None
    return {'revisions': sorted(revisions), 'presentations': sorted(presentations),
            'has_session_context': bool(sessions), 'sessions': sessions}


def playback_snapshot(report, events):
    """A native JSONL export may not contain a separate final frontend report."""
    playback = report.get('frontend', {}).get('playback')
    if isinstance(playback, dict) and playback:
        return playback, 'final_report'
    for event in reversed(events):
        data = event.get('data')
        if not isinstance(data, dict):
            continue
        if event.get('kind') == 'recording_stop' and isinstance(data.get('playback'), dict) and data['playback']:
            return data['playback'], 'recording_stop'
        if isinstance(data.get('frames'), dict) and isinstance(data.get('browser_resources'), dict):
            return data, 'timeline_snapshot'
    return {}, 'unavailable'


def dropped_frame_percent(playback):
    frames = playback.get('frames') or {}
    total, dropped = frames.get('total'), frames.get('dropped')
    if not isinstance(total, (int, float)) or total <= 0 or not isinstance(dropped, (int, float)):
        return None
    return 100 * dropped / total


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('input', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--gpu-pid', type=int)
    parser.add_argument('--renderer-pid', type=int)
    parser.add_argument('--mode-label', default='播放', help='User-confirmed playback mode, for the chart caption')
    args = parser.parse_args()
    path, records, samples, report, incomplete_tail = read_inputs(args.input)
    events = [item for item in records if item.get('type') == 'event']
    scope, memory_key, memory_label = resource_scope(samples)
    first, last = samples[0]['sampled_at_ms'], samples[-1]['sampled_at_ms']
    close = next((item['at_ms'] for item in reversed(events) if item['kind'] == 'player_closed'), None)
    gap_seconds = [(b['sampled_at_ms'] - a['sampled_at_ms']) / 1000 for a, b in zip(samples, samples[1:])]
    frontend = report.get('frontend', {})
    playback, playback_source = playback_snapshot(report, events)
    stats = {
        key: metric(samples, key, MIB if key.endswith('_bytes') else 1)
        for key in ['tree_private_bytes', 'tree_working_set_bytes', 'private_bytes', 'working_set_bytes', 'proportional_bytes', 'private_resident_bytes', 'file_descriptors', 'heap_alloc_bytes', 'go_sys_bytes', 'cpu_percent', 'goroutines', 'handles', 'system_memory_percent']
    }
    if scope != 'process_tree':
        for key in ['tree_private_bytes', 'tree_working_set_bytes', 'private_bytes', 'handles']:
            stats[key] = None
    stable = [item for item in samples if first + 12 * 60000 <= item['sampled_at_ms'] < (close or last) - 60000]
    after = [item for item in samples if close and item['sampled_at_ms'] > close]
    drop_rate = dropped_frame_percent(playback)
    summary = {
        'input': str(path), 'sample_count': len(samples), 'duration_seconds': (last - first) / 1000,
        'resource_scope': scope, 'memory_metric': memory_key, 'memory_label': memory_label,
        'sample_gap_seconds': {'min': min(gap_seconds), 'max': max(gap_seconds), 'median': statistics.median(gap_seconds)},
        'complete': records[-1].get('type') == 'end' and not incomplete_tail,
        'player_closed_at_ms': close, 'post_close_seconds': (last - close) / 1000 if close else None,
        'metrics': stats, 'stable_window': metric(stable, memory_key, MIB) if stable and memory_key else None,
        'post_close': metric(after, memory_key, MIB) if after and memory_key else None,
        'event_counts': dict(collections.Counter(item['kind'] for item in events)),
        'architecture': architecture_metrics(events),
        'playback': playback, 'playback_source': playback_source, 'drop_rate_percent': drop_rate, 'frontend_cache': frontend.get('cache', []),
        'pid_roles': {'gpu': args.gpu_pid, 'renderer': args.renderer_pid},
        'mode_label': args.mode_label,
    }
    args.output.mkdir(parents=True, exist_ok=True)
    (args.output / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding='utf-8')

    import matplotlib
    matplotlib.use('Agg')
    from matplotlib import font_manager
    from matplotlib import pyplot as plt
    font = Path('C:/Windows/Fonts/msyh.ttc')
    if font.exists():
        font_manager.fontManager.addfont(str(font))
        plt.rcParams['font.family'] = font_manager.FontProperties(fname=str(font)).get_name()
    plt.rcParams.update({'axes.unicode_minus': False, 'font.size': 10, 'axes.spines.top': False, 'axes.spines.right': False})
    minutes = [(item['sampled_at_ms'] - first) / 60000 for item in samples]
    fig, axes = plt.subplots(3, 1, figsize=(11.5, 8.4), sharex=True, gridspec_kw={'height_ratios': [2.4, 1, 1]}, constrained_layout=True)
    fig.suptitle(f'实际{args.mode_label}资源记录 · {clock(first)}–{clock(last)}', fontsize=16, fontweight='bold')
    native_peak = 10
    if memory_key:
        values = [row.get(memory_key) / MIB if row.get(memory_key) is not None else float('nan') for row in samples]
        axes[0].plot(minutes, values, label=memory_label, color='#202a44', linewidth=2)
        native_peak = max(native_peak, (stats[memory_key] or {}).get('max', 0))
    else:
        axes[0].text(.03, .8, memory_label, transform=axes[0].transAxes)
    for pid, label, color in [(args.gpu_pid, 'WebView GPU 进程', '#d18b18'), (args.renderer_pid, 'WebView 页面渲染进程', '#2675bc')]:
        if pid and scope == 'process_tree':
            values = [next((p['private_bytes'] / MIB for p in item['processes'] if p['pid'] == pid), float('nan')) for item in samples]
            axes[0].plot(minutes, values, label=label, color=color, linewidth=1.7)
    if scope == 'process_tree':
        axes[0].plot(minutes, [item['tree_working_set_bytes'] / MIB for item in samples], label='进程树工作集合计（含共享页）', color='#387bb4', linewidth=1.5, linestyle='--')
        axes[0].plot(minutes, [item['private_bytes'] / MIB for item in samples], label='主进程私有提交', color='#2a8c72', linewidth=1.5)
        native_peak = max(native_peak, (stats['tree_working_set_bytes'] or {}).get('max', 0))
    elif scope == 'app_process' and memory_key != 'working_set_bytes':
        axes[0].plot(minutes, [item['working_set_bytes'] / MIB for item in samples], label='应用进程 RSS', color='#387bb4', linewidth=1.5, linestyle='--')
        native_peak = max(native_peak, (stats['working_set_bytes'] or {}).get('max', 0))
    axes[0].set(ylabel='进程内存（MiB）', ylim=(0, native_peak * 1.12))
    if memory_key:
        axes[0].legend(loc='upper left', ncol=2, frameon=False)
    axes[1].plot(minutes, [item['heap_alloc_bytes'] / MIB for item in samples], color='#2a8c72', linewidth=1.3)
    axes[1].set(ylabel='Go 堆（MiB）', ylim=(0, max(10, stats['heap_alloc_bytes']['max'] * 1.2)))
    axes[2].plot(minutes, [item.get('cpu_percent') if item.get('cpu_percent') is not None else float('nan') for item in samples], color='#8163b5', linewidth=1.3)
    axes[2].set(ylabel='CPU（整机 %）', xlabel='记录开始后经过的时间（分钟）', ylim=(0, max(15, (stats['cpu_percent'] or {}).get('max', 0) * 1.2)))
    play = next((item['at_ms'] for item in events if item['kind'] == 'playback_load'), None)
    for ax in axes:
        ax.grid(axis='y', color='#e8edf2')
        ax.set_axisbelow(True)
        if play:
            ax.axvline((play - first) / 60000, color='#94a3b8', linestyle=':', linewidth=1)
        if close:
            ax.axvline((close - first) / 60000, color='#bf4b4b', linestyle='--', linewidth=1)
            ax.axvspan((close - first) / 60000, minutes[-1], color='#f3d3d3', alpha=.5)
    if close:
        axes[0].text((close - first) / 60000 - .15, axes[0].get_ylim()[1] * .95, f'关闭播放器\n后续观察 {(last-close)/1000:.0f} 秒', ha='right', va='top', color='#aa3f3f', fontsize=9)
    axes[2].text(0, -.36, '私有内存不等于显卡显存。红色区域是关闭后观察窗口；此记录不能单独证明或排除内存泄漏。', transform=axes[2].transAxes, color='#526176', fontsize=9)
    fig.savefig(args.output / 'memory-timeline.png', dpi=160, facecolor='white')
    plt.close(fig)
    print(json.dumps({'samples': len(samples), 'duration_minutes': round(summary['duration_seconds'] / 60, 2), 'resource_scope': scope, 'memory_metric': memory_key, 'memory_mib': stats.get(memory_key), 'post_close_seconds': summary['post_close_seconds'], 'drop_rate_percent': drop_rate, 'output': str(args.output)}, ensure_ascii=True))


if __name__ == '__main__':
    main()
