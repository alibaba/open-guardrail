"""Safety contracts for the real-cluster acceptance harness; no cluster is used."""

from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

from integrations.argo import smoke


class SmokePreflightTest(unittest.TestCase):
    def runner(self, work):
        return smoke.Smoke(SimpleNamespace(work_dir=str(work), namespace="guardrail-test",
            context="kind-test", host_ip="192.0.2.1", cli="/unused/cli", server_bin="/unused/server"))

    def test_refuses_existing_namespace_before_creating_private_data(self):
        with tempfile.TemporaryDirectory() as parent:
            work = Path(parent) / "private"
            runner = self.runner(work)
            with mock.patch.object(runner, "command", return_value=SimpleNamespace(stdout="https://127.0.0.1:6443")), \
                 mock.patch.object(runner, "kubectl", return_value=SimpleNamespace(stdout="namespace/guardrail-test")) as kube:
                with self.assertRaisesRegex(ValueError, "namespace already exists"):
                    runner.preflight()
            self.assertFalse(work.exists())
            self.assertTrue(all(call.args[0] == "get" for call in kube.call_args_list))

    def test_remote_api_is_refused_before_any_namespace_operation(self):
        with tempfile.TemporaryDirectory() as parent:
            runner = self.runner(Path(parent) / "private")
            with mock.patch.object(runner, "command", return_value=SimpleNamespace(stdout="https://remote.example:6443")), \
                 mock.patch.object(runner, "kubectl") as kube:
                with self.assertRaisesRegex(ValueError, "loopback"):
                    runner.preflight()
            kube.assert_not_called()

    def test_refuses_private_credentials_inside_repository(self):
        with self.assertRaisesRegex(ValueError, "outside the repository"):
            self.runner(smoke.gate.ROOT / "private-smoke-data")


if __name__ == "__main__":
    unittest.main()
