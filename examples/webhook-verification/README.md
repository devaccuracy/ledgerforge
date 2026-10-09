<!--
Copyright 2026 LedgerForge Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# Verify a notification signature

Run the self-contained receiver regression tests:

```sh
python3 -m unittest discover -s examples/webhook-verification -p 'test_*.py'
```

`verify.py` accepts the **raw body bytes**, HTTP headers and the receiver's
selected signing secret. It checks HMAC-SHA256 with constant-time comparison,
header shape, body size and a five-minute past/future freshness window before
business processing. The tests show that a dedicated signing secret works
without the API master secret, a legacy fallback works only when that is the
sender's selected secret, and changing JSON whitespace invalidates the signature.

Configure `notification.webhook.signing_secret` on the sender and securely
provision the same dedicated secret at the receiver. The sender falls back to
`server.secret_key` only when the dedicated setting is empty. Configured
signature/timestamp headers cannot override generated values.

This module is a verification function, not a web server. Enforce request size
limits before reading a body. After verification, persist a stable event ID and
your business result atomically, then acknowledge 2xx; the signature check does
not deduplicate retries or prevent a fresh signed duplicate. See
[deployment](../../docs/deployment.md) for delivery and destination controls.
