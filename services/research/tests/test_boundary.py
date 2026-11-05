from __future__ import annotations

import ast
import importlib.util
import tempfile
import unittest
from pathlib import Path


FACADES = {"omni_research." + name for name in ("engine", "improve", "paper_fill", "capture_research", "signal_cli")}
FORBIDDEN_MODULES = {"socket", "urllib", "http", "sqlite3", "subprocess"}
DOMAIN_STDLIB = {"__future__", "dataclasses", "decimal", "fractions", "math", "typing"}
APPLICATION_STDLIB = {"__future__", "decimal", "hashlib", "json", "typing"}
ENVIRONMENT = {"os.environ", "os.getenv", "os.putenv", "os.unsetenv", "os.environb", "os.getenvb"}
FILE_CALLS = {"open", "read_bytes", "read_text", "write_bytes", "write_text"}


def boundary_violations(package: Path) -> list[str]:
    sources = {}
    for path in package.rglob("*.py"):
        parts = path.relative_to(package).with_suffix("").parts
        module = ".".join((package.name, *(parts[:-1] if parts[-1] == "__init__" else parts)))
        sources[module] = (path.name == "__init__.py", ast.parse(path.read_text(encoding="utf-8")))
    violations = []
    for module, (is_package, tree) in sources.items():
        aliases = {}
        imports = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    imports.append(alias.name)
                    aliases[alias.asname or alias.name.split(".")[0]] = alias.name if alias.asname else alias.name.split(".")[0]
            elif isinstance(node, ast.ImportFrom):
                parent = module if is_package else module.rpartition(".")[0]
                try:
                    target = importlib.util.resolve_name("." * node.level + (node.module or ""), parent) if node.level else node.module
                except ImportError:
                    violations.append(f"{module}: invalid relative import")
                    continue
                imports.append(target)
                for alias in node.names:
                    qualified = target + "." + alias.name
                    aliases[alias.asname or alias.name] = qualified
                    if qualified in sources:
                        imports.append(qualified)

        layer = module.split(".")[1] if "." in module else ""
        for target in imports:
            if target.split(".")[0] in FORBIDDEN_MODULES:
                violations.append(f"{module}: forbidden import {target}")
            if target in FACADES:
                violations.append(f"{module}: internal facade import {target}")
            if layer in {"domain", "application"}:
                internal = ("omni_research.domain",) if layer == "domain" else ("omni_research.domain", "omni_research.application")
                stdlib = DOMAIN_STDLIB if layer == "domain" else APPLICATION_STDLIB
                if not (target.split(".")[0] in stdlib or any(target == prefix or target.startswith(prefix + ".") for prefix in internal)
                        or (layer == "application" and target == "omni_research")):
                    violations.append(f"{module}: {layer} dependency {target}")

        def qualified_name(node: ast.AST) -> str:
            if isinstance(node, ast.Name):
                return aliases.get(node.id, node.id)
            if isinstance(node, ast.Attribute):
                return qualified_name(node.value) + "." + node.attr
            return ""

        for name in aliases.values():
            if name in ENVIRONMENT:
                violations.append(f"{module}: environment access {name}")
        for node in ast.walk(tree):
            if isinstance(node, ast.Attribute) and qualified_name(node) in ENVIRONMENT:
                violations.append(f"{module}: environment access {qualified_name(node)}")
            if isinstance(node, ast.Call) and layer in {"domain", "application"}:
                name = qualified_name(node.func)
                if name.rsplit(".", 1)[-1] in FILE_CALLS or name in {"__import__", "eval", "exec", "getattr"}:
                    violations.append(f"{module}: pure layer I/O or dynamic call {name}")
    return violations


class ResearchBoundaryTest(unittest.TestCase):
    def test_package_has_no_network_credential_or_operational_database_surface(self) -> None:
        package = Path(__file__).parents[1] / "omni_research"
        source = "\n".join(path.read_text(encoding="utf-8") for path in package.rglob("*.py"))
        for forbidden in ("os.environ", "socket", "urllib", "http.client", "sqlite3", "subprocess"):
            self.assertNotIn(forbidden, source)

    def test_recursive_imports_respect_research_boundaries(self) -> None:
        self.assertEqual(boundary_violations(Path(__file__).parents[1] / "omni_research"), [])

    def test_nested_and_relative_import_violations_are_rejected(self) -> None:
        cases = (
            ("domain/nested/bad.py", "from ...engine import Bar", "internal facade import"),
            ("domain/nested/bad.py", "from ... import engine as public", "internal facade import"),
            ("application/nested/bad.py", "from ...adapters.files import inputs", "application dependency"),
            ("domain/nested/bad.py", "from pathlib import Path", "domain dependency"),
            ("domain/nested/bad.py", "import json", "domain dependency"),
            ("domain/nested/bad.py", "import socket", "forbidden import"),
            ("application/nested/bad.py", "from subprocess import run", "forbidden import"),
            ("adapters/nested/bad.py", "import sqlite3", "forbidden import"),
            ("adapters/nested/bad.py", "import os as system\nvalue = system.environ", "environment access"),
            ("adapters/nested/bad.py", "from os import getenv as setting", "environment access"),
            ("application/nested/bad.py", "data = source.read_bytes()", "pure layer I/O"),
            ("domain/nested/bad.py", "__import__('pathlib')", "dynamic call"),
        )
        for path, source, expected in cases:
            with self.subTest(source=source), tempfile.TemporaryDirectory() as temporary:
                package = Path(temporary) / "omni_research"
                package.mkdir()
                (package / "engine.py").write_text("", encoding="utf-8")
                nested = package / path
                nested.parent.mkdir(parents=True)
                nested.write_text(source, encoding="utf-8")
                self.assertTrue(any(expected in violation for violation in boundary_violations(package)))

    def test_relative_domain_and_application_imports_are_allowed(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            package = Path(temporary) / "omni_research"
            for path, source in (
                ("domain/values.py", "from decimal import Decimal"),
                ("domain/nested/model.py", "from ..values import Decimal"),
                ("application/workflow.py", "from ..domain import values"),
                ("adapters/inputs/decode.py", "from ...application import workflow"),
            ):
                target = package / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(source, encoding="utf-8")
            self.assertEqual(boundary_violations(package), [])


if __name__ == "__main__":
    unittest.main()
