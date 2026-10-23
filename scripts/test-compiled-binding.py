"""Check Android diagnostics resolve the API policy wrapper after bundling."""
import importlib.util
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location('diagnostics', Path(__file__).with_name('collect-android-diagnostics.py'))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)

class BindingTests(unittest.TestCase):
    def check_bundle(self, source, expected):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binding = root/'frontend/bindings/cczjVideo/app/service/app.js'
            binding.parent.mkdir(parents=True)
            binding.write_text('export function GetSetting(key) { return $Call.ByID(123,key); }', encoding='utf-8')
            asset = root/'frontend/dist/assets/app-fixture.js'
            asset.parent.mkdir(parents=True)
            asset.write_text(source, encoding='utf-8')
            previous = MODULE.ROOT
            try:
                MODULE.ROOT = root
                self.assertEqual(MODULE.compiled_binding('GetSetting'), ('/assets/app-fixture.js', expected))
            finally: MODULE.ROOT = previous

    def test_wrapper_wins_even_when_raw_binding_is_also_exported(self):
        for quote in ['"', "'", '`']:
            self.check_bundle('function raw(a){return call(123,a)}const wrapped=bind('+quote+'GetSetting'+quote+',raw);export{raw as old,wrapped as safe}', 'safe')

    def test_legacy_build_remains_supported(self):
        self.check_bundle('function raw(a){return call(123,a)}export{raw as old}', 'old')

if __name__ == '__main__': unittest.main()
