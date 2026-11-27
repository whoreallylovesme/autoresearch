from __future__ import annotations

import os
import tempfile

TEST_TOKEN = "unit-test-operator-token-7f3a9c5e1b8d4f6a2c0e"
_STORAGE = tempfile.TemporaryDirectory(prefix="autoresearch-ui-tests-")
os.environ["AUTORESEARCH_OPERATOR_TOKEN"] = TEST_TOKEN
os.environ.pop("AUTORESEARCH_OPERATOR_TOKEN_FILE", None)
os.environ["AUTORESEARCH_UI_ALLOWED_HOSTS"] = "127.0.0.1"
os.environ["AUTORESEARCH_UI_ALLOWED_ORIGINS"] = "http://127.0.0.1"
os.environ["AUTORESEARCH_UI_SESSION_MAX_AGE"] = "60"
os.environ["NICEGUI_STORAGE_PATH"] = _STORAGE.name
