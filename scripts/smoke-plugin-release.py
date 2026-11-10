#!/usr/bin/env python3
"""Install release assets outside the checkout and verify the packaged entry points."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--assets-dir', type=Path, required=True)
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    assets = args.assets_dir.resolve()
    node = shutil.which('node')
    if not node:
        parser.error('Node.js 22+ is required for the plugin trial')
    spec = importlib.util.spec_from_file_location('installer', assets / 'install.py')
    installer = importlib.util.module_from_spec(spec)
    # The validation process must leave the published asset directory unchanged.
    sys.dont_write_bytecode = True
    spec.loader.exec_module(installer)
    with tempfile.TemporaryDirectory(prefix='guardrail-independent-install-') as temporary:
        root = Path(temporary)
        installed = installer.install(assets, root / 'versions with spaces', args.version)
        empty_path = root / 'empty-path'
        empty_path.mkdir()
        environment = dict(os.environ, PATH=str(empty_path), HOME=str(root),
                           OPEN_GUARDRAIL_BINARIES=str(installed / 'native'),
                           OPEN_GUARDRAIL_PYTHON=sys.executable)
        environment.pop('PYTHONPATH', None)
        environment.pop('PYTHONHOME', None)
        for command in ('doctor.mjs', 'trial.mjs'):
            options = ['--live'] if command == 'doctor.mjs' else []
            # Doctor permits startup + version query; trial permits ten serial
            # calls (420s + 9 * 90s), with cleanup and scheduling margin.
            timeout = 480 if command == 'doctor.mjs' else 1500
            completed = subprocess.run([node, str(installed / 'plugin' / command), *options],
                                       env=environment, cwd=root, text=True, timeout=timeout, stdout=subprocess.PIPE)
            print(completed.stdout.strip(), flush=True)
            completed.check_returncode()
            result = json.loads(completed.stdout)
            if command == 'trial.mjs' and result.get('passed') is not True:
                raise ValueError('installation trial did not pass')
    print('Independent installation passed with no checkout, Go, npm or host credentials on PATH.')


if __name__ == '__main__':
    main()
