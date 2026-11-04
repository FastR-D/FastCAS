"""Release guards: reject mismatches and never upload untested archives."""
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
import zipfile
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('publish_sdk', Path(__file__).with_name('publish-sdk.py'))
publish = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publish)


class PublishingTests(unittest.TestCase):
    def test_tag_mismatch_stops_before_build(self):
        with patch.dict(os.environ, {'RELEASE_TAG': 'v9.9.9'}):
            with self.assertRaises(SystemExit):
                publish.version()

    def test_package_version_mismatch(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'sdk/typescript').mkdir(parents=True)
            (root / 'sdk/python').mkdir()
            (root / 'sdk/typescript/package.json').write_text('{"version":"0.1.0"}')
            (root / 'sdk/python/pyproject.toml').write_text('version = "0.2.0"\n')
            with patch.object(publish, 'ROOT', root), self.assertRaises(SystemExit):
                publish.version()

    def test_confirmation_defaults_to_no(self):
        with patch.dict(os.environ, {}, clear=True), patch('builtins.input', return_value=''):
            with self.assertRaises(SystemExit):
                publish.confirm('package', '0.1.0', publish.NPM)

    def test_npm_uploads_installed_archive_and_dry_run_never_uploads(self):
        for upload in (False, True):
            with self.subTest(upload=upload), tempfile.TemporaryDirectory() as directory:
                stage = Path(directory)
                tarball = stage / 'test.tgz'
                with tarfile.open(tarball, 'w:gz') as archive:
                    info = tarfile.TarInfo('package/LICENSE')
                    info.size = 3
                    archive.addfile(info, io.BytesIO(b'MIT'))
                calls = []
                def run(*args, **kwargs):
                    calls.append(args)
                    if args[:2] == ('pnpm', 'pack'):
                        return json.dumps({'filename': str(tarball)})
                    if args[:2] == ('pnpm', 'publish'):
                        config = Path(kwargs['env']['NPM_CONFIG_USERCONFIG'])
                        self.assertIn('${NODE_AUTH_TOKEN}', config.read_text())
                        self.assertNotIn('secret-test-value', config.read_text())
                    return ''
                with patch.object(publish, 'run', side_effect=run), patch.dict(os.environ, {'FASTCAS_PUBLISH_CONFIRM':'1', 'NODE_AUTH_TOKEN':'secret-test-value'}):
                    publish.npm_package(stage, '0.1.0', upload)
                installed = next(c[-1] for c in calls if c[:2] == ('pnpm', 'add'))
                uploads = [c for c in calls if c[:2] == ('pnpm', 'publish')]
                self.assertEqual(len(uploads), int(upload))
                if upload:
                    self.assertEqual(uploads[0][2], installed)
                    self.assertLess(next(i for i,c in enumerate(calls) if c[0]=='node'), calls.index(uploads[0]))

    def test_python_uploads_installed_wheel_and_blocks_broken_import(self):
        for upload, broken in ((False, False), (True, False), (True, True)):
            with self.subTest(upload=upload, broken=broken), tempfile.TemporaryDirectory() as directory:
                stage = Path(directory)
                calls = []
                def run(*args, **kwargs):
                    calls.append(args)
                    if args[:2] == ('uv', 'build'):
                        output = Path(args[args.index('--out-dir') + 1])
                        output.mkdir()
                        with zipfile.ZipFile(output / 'fastcas_sdk-0.1.0-py3-none-any.whl', 'w') as archive:
                            archive.writestr('fastcas/py.typed', '')
                            archive.writestr('fastcas_sdk-0.1.0.dist-info/licenses/LICENSE', 'MIT')
                    if '-I' in args and broken:
                        raise RuntimeError('broken wheel')
                    return ''
                with patch.object(publish, 'run', side_effect=run), patch.dict(os.environ, {'FASTCAS_PUBLISH_CONFIRM':'1'}):
                    if broken:
                        with self.assertRaises(RuntimeError):
                            publish.python_package(stage, '0.1.0', upload)
                    else:
                        publish.python_package(stage, '0.1.0', upload)
                uploads = [c for c in calls if c[:2] == ('uv', 'publish')]
                self.assertEqual(len(uploads), int(upload and not broken))
                if uploads:
                    installed = next(c[-1] for c in calls if c[:3] == ('uv', 'pip', 'install'))
                    self.assertEqual(uploads[0][-1], installed)

    def test_failed_smoke_blocks_upload(self):
        with tempfile.TemporaryDirectory() as directory:
            stage = Path(directory)
            tarball = stage / 'test.tgz'
            with tarfile.open(tarball, 'w:gz') as archive:
                archive.addfile(tarfile.TarInfo('package/LICENSE'))
            calls = []
            def run(*args, **kwargs):
                calls.append(args)
                if args[:2] == ('pnpm', 'pack'):
                    return json.dumps({'filename': str(tarball)})
                if args[0] == 'node':
                    raise RuntimeError('broken package')
                return ''
            with patch.object(publish, 'run', side_effect=run), self.assertRaises(RuntimeError):
                publish.npm_package(stage, '0.1.0', True)
            self.assertFalse(any(c[:2] == ('pnpm','publish') for c in calls))


if __name__ == '__main__':
    unittest.main()
