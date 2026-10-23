"""Offline regression checks for analysis of real JSONL-only Android exports."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('analysis', Path(__file__).with_name('analyze-diagnostics-recording.py'))
analysis = importlib.util.module_from_spec(spec)
spec.loader.exec_module(analysis)


class RecordingAnalysis(unittest.TestCase):
    def test_jsonl_snapshot_is_used_without_final_report(self):
        first = dict(frames=dict(total=10, dropped=1), browser_resources=dict(registered_player_count=1))
        final = dict(frames=dict(total=100, dropped=2), browser_resources=dict(registered_player_count=0))
        events = [dict(kind='playback_snapshot', data=first), dict(kind='player_closed', data={'issues':[]}), dict(kind='playback_snapshot', data=final)]
        result, source = analysis.playback_snapshot({}, events)
        self.assertEqual(result, final)
        self.assertEqual(source, 'timeline_snapshot')
        self.assertEqual(analysis.dropped_frame_percent(result), 2)

    def test_missing_counters_are_unknown(self):
        self.assertIsNone(analysis.dropped_frame_percent({'frames':{'total':100}}))
        self.assertIsNone(analysis.dropped_frame_percent({'frames':{'total':0,'dropped':0}}))
        self.assertEqual(analysis.playback_snapshot({}, [dict(kind='recording_stop',data={})]), ({},'unavailable'))

    def test_final_report_has_priority(self):
        result, source = analysis.playback_snapshot({'frontend':{'playback':{'loads':3}}}, [])
        self.assertEqual(result, {'loads':3})
        self.assertEqual(source, 'final_report')


if __name__ == '__main__':
    unittest.main()
