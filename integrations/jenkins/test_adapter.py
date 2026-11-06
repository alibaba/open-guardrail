"""Jenkins context mapping tests; no Jenkins controller or credentials are used."""

import unittest
from unittest import mock

from integrations.jenkins import adapter


class JenkinsAdapterTest(unittest.TestCase):
    def setUp(self):
        self.environment = {
            "JOB_NAME": "checkout/canary",
            "BUILD_NUMBER": "42",
            "BUILD_TAG": "jenkins-checkout-canary-42",
        }
        self.options = {
            "instance_id": "jenkins-test-1",
            "checkpoint": "post-canary",
            "service": "checkout",
            "environment": "test",
            "candidate": "registry.example/checkout@sha256:abc",
            "observation": {"status": "healthy"},
            "key_file": "/private/jenkins-key.json",
            "cli": "guardrailctl",
        }

    def test_maps_jenkins_identity_without_credentials_in_changeset(self):
        context = adapter.context_from_jenkins(self.environment, **self.options)
        self.assertEqual(context.change_set["candidate"], "registry.example/checkout@sha256:abc")
        self.assertEqual(context.change_set["delivery"], {
            "type": "jenkins", "instanceId": "jenkins-test-1",
            "jobName": "checkout/canary", "buildNumber": "42",
            "buildTag": "jenkins-checkout-canary-42",
            "checkpoint": "post-canary",
        })
        self.assertNotIn("jenkins-key", str(context.change_set))
        self.assertNotIn("jenkins-key", context.operation_identity)
        self.assertEqual(context.operation_identity,
                         adapter.context_from_jenkins(self.environment, **self.options).operation_identity)

    def test_identity_changes_per_build_checkpoint_and_controller(self):
        original = adapter.context_from_jenkins(self.environment, **self.options).operation_identity
        for field, value in (("BUILD_NUMBER", "43"), ("JOB_NAME", "another-job")):
            changed = dict(self.environment, **{field: value})
            self.assertNotEqual(original,
                                adapter.context_from_jenkins(changed, **self.options).operation_identity)
        for field, value in (("checkpoint", "pre-release"), ("instance_id", "jenkins-test-2")):
            changed = dict(self.options, **{field: value})
            self.assertNotEqual(original,
                                adapter.context_from_jenkins(self.environment, **changed).operation_identity)

    def test_requires_real_jenkins_build_identity(self):
        for missing in ("JOB_NAME", "BUILD_NUMBER", "BUILD_TAG"):
            with self.subTest(missing=missing):
                environment = dict(self.environment)
                del environment[missing]
                with self.assertRaisesRegex(ValueError, "Jenkins"):
                    adapter.context_from_jenkins(environment, **self.options)
        with self.assertRaisesRegex(ValueError, "Jenkins"):
            adapter.context_from_jenkins(dict(self.environment, BUILD_NUMBER="４２"), **self.options)

    def test_poll_reports_pending_without_claiming_allow(self):
        argv = ["adapter", "--phase", "poll", "--state-dir", "/unused"]
        with mock.patch.object(adapter.sys, "argv", argv), \
             mock.patch.dict(adapter.os.environ, {"GUARDRAIL_KEY_FILE": __file__,
                                              "OPEN_GUARDRAIL_URL": "https://guardrail.example"}), \
             mock.patch.object(adapter.gate, "poll_gate", return_value=False):
            self.assertEqual(adapter.main(), 2)


if __name__ == "__main__":
    unittest.main()
