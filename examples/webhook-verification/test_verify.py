# Copyright 2026 LedgerForge Contributors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import hashlib
import hmac
import json
import unittest

from verify import MAX_BODY_BYTES, verify


class WebhookVerificationTests(unittest.TestCase):
    def setUp(self):
        self.body = b'{ "event": "transaction.applied", "data": {"id": "tx-1"} }'
        self.now = 1791500000

    def signed(self, secret="dedicated-webhook-secret", timestamp=None):
        timestamp = str(self.now if timestamp is None else timestamp)
        signature = hmac.new(secret.encode(), timestamp.encode() + b"." + self.body, hashlib.sha256).hexdigest()
        return {"X-LedgerForge-Timestamp": timestamp, "X-LedgerForge-Signature": signature}

    def test_dedicated_secret_does_not_require_master_secret(self):
        verify(self.body, self.signed(), "dedicated-webhook-secret", now=self.now)
        with self.assertRaises(ValueError):
            verify(self.body, self.signed(), "api-master-secret", now=self.now)

    def test_legacy_fallback_secret_is_valid_only_when_selected_by_sender(self):
        headers = self.signed("api-master-secret")
        verify(self.body, headers, "api-master-secret", now=self.now)
        with self.assertRaises(ValueError):
            verify(self.body, headers, "dedicated-webhook-secret", now=self.now)

    def test_exact_raw_body_is_required(self):
        reserialized = json.dumps(json.loads(self.body)).encode()
        self.assertNotEqual(reserialized, self.body)
        with self.assertRaises(ValueError):
            verify(reserialized, self.signed(), "dedicated-webhook-secret", now=self.now)

    def test_stale_and_future_signed_requests_are_rejected(self):
        for offset in [-301, 301]:
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                verify(self.body, self.signed(timestamp=self.now + offset), "dedicated-webhook-secret", now=self.now)

    def test_missing_or_forged_reserved_headers_are_rejected(self):
        for headers in [{}, {"X-LedgerForge-Timestamp": "attacker", "X-LedgerForge-Signature": "attacker"}]:
            with self.subTest(headers=headers), self.assertRaises(ValueError):
                verify(self.body, headers, "dedicated-webhook-secret", now=self.now)

    def test_unsigned_and_excessive_bodies_are_rejected(self):
        with self.assertRaises(ValueError):
            verify(self.body, self.signed(), "", now=self.now)
        with self.assertRaises(ValueError):
            verify(b"x" * (MAX_BODY_BYTES + 1), self.signed(), "dedicated-webhook-secret", now=self.now)


if __name__ == "__main__":
    unittest.main()
