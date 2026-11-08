#!/usr/bin/env python3
"""Exercise architecture rules against real temporary Go modules."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("check-architecture.py").resolve()
MODULE = "example.test/network"


def check_graph(edges):
    with tempfile.TemporaryDirectory(prefix="connector-architecture-") as temp:
        root = Path(temp)
        (root / "go.mod").write_text(f"module {MODULE}\n\ngo 1.26.0\n")
        packages = set(edges)
        for dependencies in edges.values():
            packages.update(dependencies)
        for package in packages:
            directory = root / package
            directory.mkdir(parents=True, exist_ok=True)
            imports = "".join(f'import _ "{MODULE}/{dependency}"\n' for dependency in edges.get(package, ()))
            (directory / "package.go").write_text("package fixture\n" + imports)
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--root", str(root)],
            env=dict(os.environ, GOWORK="off", GOFLAGS="-mod=readonly"),
            capture_output=True, text=True,
        )


class ArchitectureTests(unittest.TestCase):
    def test_allowed_composition_and_similar_names(self):
        result = check_graph({
            "internal/cli": ("internal/bootstrap", "internal/progress"),
            "internal/bootstrap": ("internal/workflow", "config"),
            "internal/workflow": ("connector", "internal/topologyreport"),
            "connector": ("storage", "internal/terminal", "vendorspec"),
            "config": ("archive", "internal/cliformat"),
        })
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_reverse_dependencies_include_nested_packages(self):
        for source, target in (
            ("connector/session", "internal/cli/commands"),
            ("config/decode", "internal/bootstrap/settings"),
            ("vendors/custom", "internal/workflow/report"),
            ("connector/session", "vendors/custom"),
            ("internal/bootstrap/settings", "internal/cli/commands"),
            ("internal/workflow/backup", "internal/progress/render"),
        ):
            with self.subTest(source=source, target=target):
                result = check_graph({source: (target,)})
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn(f"{source} -> {target}:", result.stderr)

    def test_internal_helper_cannot_hide_reverse_dependency(self):
        result = check_graph({
            "config/decode": ("internal/shared",),
            "internal/shared": ("internal/cli/commands",),
        })
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("config/decode -> internal/cli/commands:", result.stderr)

    def test_unresolvable_graph_fails_closed(self):
        result = check_graph({"connector": ("config",), "config": ("connector",)})
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn("cannot resolve the Go package graph", result.stderr)


if __name__ == "__main__":
    unittest.main()
