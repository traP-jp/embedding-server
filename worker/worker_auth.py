from __future__ import annotations

import hmac
import os


def internal_api_key() -> str:
    return os.environ.get("INTERNAL_API_KEY", "").strip()


def valid_bearer_token(authorization: str, expected: str) -> bool:
    if not expected:
        return False
    scheme, separator, token = authorization.partition(" ")
    if separator == "" or scheme.lower() != "bearer":
        return False
    return hmac.compare_digest(token.strip().encode("utf-8"), expected.encode("utf-8"))
