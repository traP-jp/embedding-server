import unittest

from pydantic import ValidationError

from worker_config import Config


class ConfigTest(unittest.TestCase):
    def test_validation_error_does_not_expose_credentials(self) -> None:
        with self.assertRaises(ValidationError) as error:
            Config.model_validate({
                "WORKER_API_MODE": "invalid",
                "INTERNAL_API_KEY": "private-internal-token",
                "S3_SECRET_ACCESS_KEY": "private-s3-token",
            })
        message = str(error.exception)
        self.assertNotIn("private-internal-token", message)
        self.assertNotIn("private-s3-token", message)
