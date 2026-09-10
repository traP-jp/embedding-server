from __future__ import annotations

import unittest

from worker_auth import valid_bearer_token


class BearerTokenTest(unittest.TestCase):
    def test_accepts_matching_bearer_token(self) -> None:
        self.assertTrue(valid_bearer_token("Bearer internal-secret", "internal-secret"))

    def test_rejects_missing_or_wrong_token(self) -> None:
        self.assertFalse(valid_bearer_token("", "internal-secret"))
        self.assertFalse(valid_bearer_token("Bearer external-secret", "internal-secret"))
        self.assertFalse(valid_bearer_token("Bearer anything", ""))


if __name__ == "__main__":
    unittest.main()
