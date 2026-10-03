#!/usr/bin/env python3
# Copyright (c) 2026 Proton AG
#
# This file is part of Proton Mail Bridge.
#
# Proton Mail Bridge is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# Proton Mail Bridge is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

"""Exercise the GUI build script without downloading or compiling dependencies."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


REPOSITORY = Path(__file__).resolve().parents[1]
GUI_PATH = Path("internal/frontend/bridge-gui/bridge-gui")


class BuildScriptTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="bridge build ")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.gui = self.root / GUI_PATH
        self.gui.mkdir(parents=True)
        shutil.copy2(REPOSITORY / GUI_PATH / "build.sh", self.gui / "build.sh")
        self.commands = self.root / "commands"
        self.commands.mkdir()
        self.log = self.root / "cmake.jsonl"
        self.dependencies = self.root / "dependencies.jsonl"
        self.environment = os.environ.copy()
        self.environment.pop("BRIDGE_BUILD_ENV", None)
        self.environment.update(
            PATH=f"{self.commands}{os.pathsep}{self.environment['PATH']}",
            BRIDGE_APP_VERSION="3.27.1+git",
            BRIDGE_GUI_BUILD_CONFIG="Release",
            BRIDGE_BUILD_TIME="2026-10-03T08:00:00Z",
            TEST_CMAKE_LOG=str(self.log),
            TEST_VCPKG_LOG=str(self.dependencies),
        )
        self.executable(self.commands / "git", '#!/bin/sh\nprintf "revision\\n"\n')
        self.executable(
            self.commands / "cmake",
            "#!/usr/bin/env python3\n"
            "import json, os, sys\n"
            "with open(os.environ['TEST_CMAKE_LOG'], 'a') as log:\n"
            "    log.write(json.dumps(sys.argv[1:]) + '\\n')\n"
            "sys.exit(int(os.environ.get('TEST_CMAKE_EXIT', '0')))\n",
        )
        vcpkg = self.root / "extern/vcpkg"
        vcpkg.mkdir(parents=True)
        self.executable(vcpkg / "bootstrap-vcpkg.sh", "#!/bin/sh\nexit 0\n")
        self.executable(
            vcpkg / "vcpkg",
            "#!/usr/bin/env python3\n"
            "import json, os, sys\n"
            "if os.environ.get('TEST_VCPKG_EXIT'):\n"
            "    sys.exit(int(os.environ['TEST_VCPKG_EXIT']))\n"
            "with open(os.environ['TEST_VCPKG_LOG'], 'a') as log:\n"
            "    log.write(json.dumps(sys.argv[1:]) + '\\n')\n",
        )

    @staticmethod
    def executable(path, content):
        path.write_text(content)
        path.chmod(0o755)

    def run_build(self, mac_architecture=None):
        command = ["bash", "build.sh", "install"]
        if mac_architecture is not None:
            self.environment["BRIDGE_MACOS_ARCH"] = mac_architecture
            command = ["bash", "-c", "OSTYPE=darwin; source build.sh install"]
        return subprocess.run(
            command,
            cwd=self.gui,
            env=self.environment,
            text=True,
            capture_output=True,
            check=False,
        )

    def test_build_passes_metadata_and_installs_from_paths_with_spaces(self):
        result = self.run_build()
        self.assertEqual(result.returncode, 0, result.stderr)
        invocations = [json.loads(line) for line in self.log.read_text().splitlines()]
        configure, build, install = invocations
        self.assertIn("-DBRIDGE_BUILD_TIME=2026-10-03T08:00:00Z", configure)
        self.assertIn("-DBRIDGE_BUILD_ENV=dev", configure)
        self.assertNotIn("", configure)
        self.assertEqual(build[:2], ["--build", "./cmake-build-release"])
        self.assertEqual(install[:2], ["--install", "./cmake-build-release"])

    def test_dependency_failure_stops_before_cmake(self):
        self.environment["TEST_VCPKG_EXIT"] = "1"
        result = self.run_build()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.log.exists())

    def test_mac_dependencies_support_the_universal_crash_handler(self):
        for architecture, grpc in (
            ("arm64", "grpc:arm64-osx-min-11-0"),
            ("x86_64", "grpc:x64-osx-min-10-15"),
        ):
            with self.subTest(architecture=architecture):
                result = self.run_build(mac_architecture=architecture)
                self.assertEqual(result.returncode, 0, result.stderr)
                installed = json.loads(self.dependencies.read_text().splitlines()[-1])
                self.assertIn("sentry-native:arm64-osx-min-11-0", installed)
                self.assertIn("sentry-native:x64-osx-min-10-15", installed)
                self.assertEqual([item for item in installed if item.startswith("grpc:")], [grpc])

    def test_unsupported_mac_architecture_stops_before_installing_dependencies(self):
        result = self.run_build(mac_architecture="unsupported")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.dependencies.exists())
        self.assertFalse(self.log.exists())

    def test_failed_build_preserves_diagnostics(self):
        build_directory = self.gui / "cmake-build-release"
        build_directory.mkdir()
        diagnostic = build_directory / "CMakeConfigureLog.yaml"
        diagnostic.write_text("compiler diagnostic")
        self.environment["TEST_CMAKE_EXIT"] = "1"
        result = self.run_build()
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(diagnostic.exists())


if __name__ == "__main__":
    unittest.main()
