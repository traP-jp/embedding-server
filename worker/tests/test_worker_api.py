from __future__ import annotations

import unittest
from unittest.mock import patch

import httpx

from worker_api import ApiClient


class ApiClientAuthTest(unittest.TestCase):
    def test_internal_request_uses_bearer_key(self) -> None:
        response = httpx.Response(
            204,
            request=httpx.Request("POST", "https://api.example.com/internal/worker/jobs/claim"),
        )
        with patch("worker_api.httpx.post", return_value=response) as post:
            ApiClient("https://api.example.com", "internal-secret").claim()

        self.assertEqual(
            post.call_args.kwargs["headers"],
            {"Authorization": "Bearer internal-secret"},
        )


if __name__ == "__main__":
    unittest.main()
