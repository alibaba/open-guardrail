"""Check the query boundaries that make the local recovery investigation meaningful."""

import unittest

from first_batch import EXAMPLE, observe, read


class FirstBatchQueries(unittest.TestCase):
    def setUp(self):
        self.data = read(EXAMPLE / "first-batch-data.json")
        self.scope = self.data["changeSet"]

    def test_logs_exclude_other_instances_and_the_end_of_the_window(self):
        logs = self.data["windows"][0]["logs"]
        logs.append(dict(logs[0], at=self.scope["to"]))
        found = observe(self.data, self.scope, "logs")
        self.assertEqual([row["orderId"] for row in found], ["order-42"])

    def test_empty_logs_are_distinct_from_an_unavailable_source(self):
        window = self.data["windows"][0]
        window["logs"] = []
        self.assertEqual(observe(self.data, self.scope, "logs"), [])
        del window["logs"]
        self.assertIn("unavailable", observe(self.data, self.scope, "logs"))

    def test_other_environments_and_candidates_do_not_supply_evidence(self):
        other = dict(self.scope, environment="other", candidate="other")
        self.assertIn("unavailable", observe(self.data, other, "metrics"))
        self.assertEqual(observe(self.data, other, "records", "order-42"), [])
        self.assertEqual(observe(self.data, other, "changes"), [])
