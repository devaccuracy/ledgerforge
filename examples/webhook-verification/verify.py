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
import re
import time


MAX_BODY_BYTES = 1024 * 1024


def verify(raw_body, headers, secret, *, now=None, tolerance=300):
    """Validate raw notification bytes; business-event deduplication is separate."""
    if not secret or len(raw_body) > MAX_BODY_BYTES:
        raise ValueError("missing secret or excessive body size")
    normalized = {key.lower(): value for key, value in headers.items()}
    timestamp = normalized.get("x-ledgerforge-timestamp", "")
    signature = normalized.get("x-ledgerforge-signature", "")
    if not re.fullmatch(r"[0-9]+", timestamp) or not re.fullmatch(r"[0-9a-f]{64}", signature):
        raise ValueError("missing or malformed signature headers")
    current = int(time.time()) if now is None else now
    if abs(current - int(timestamp)) > tolerance:
        raise ValueError("timestamp outside freshness window")
    expected = hmac.new(secret.encode(), timestamp.encode() + b"." + raw_body, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(expected, signature):
        raise ValueError("invalid webhook signature")
