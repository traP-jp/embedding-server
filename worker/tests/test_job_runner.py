import unittest
from unittest.mock import Mock

import httpx

from job_runner import claim_jobs, run_jobs


class JobRunnerTest(unittest.TestCase):
    def test_partial_claim_failure_preserves_claimed_jobs(self) -> None:
        api = Mock()
        job = {"id": "claimed", "payload": {"text": "hello"}}
        api.claim.side_effect = [job, httpx.ReadTimeout("timeout")]
        self.assertEqual(claim_jobs(api, 4), [job])

    def test_first_claim_failure_is_propagated(self) -> None:
        api = Mock()
        api.claim.side_effect = httpx.ReadTimeout("timeout")
        with self.assertRaises(httpx.ReadTimeout):
            claim_jobs(api, 4)

    def test_vector_count_mismatch_fails_every_prepared_job(self) -> None:
        api, embedder = Mock(), Mock()
        embedder.embed_many.return_value = [[0.1]]
        jobs = [{"id": str(i), "payload": {"text": "hello"}} for i in range(2)]
        self.assertEqual(run_jobs(api, embedder, Mock(), Mock(), jobs), 0)
        api.complete.assert_not_called()
        self.assertEqual(api.fail.call_count, 2)
