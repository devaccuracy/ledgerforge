# Security policy

Report suspected vulnerabilities privately to
[security@ledgerforge.io](mailto:security@ledgerforge.io). Do not post exploits,
credentials or sensitive financial data in public issues.

## Supported versions

| Version | Security support |
| --- | --- |
| Latest stable LedgerForge release | Supported; fixes ship in a new patch release. |
| `main` | Reports accepted; development builds have no stability commitment. |
| Older releases and prereleases | Upgrade to the latest stable release; no routine backports. |

Use release binaries or images from this repository and keep the Go runtime and
service dependencies current. Support covers the repository's code, published
images and deployment defaults; operators remain responsible for deployment
configuration and third-party infrastructure.

## Private reporting

Include the affected version or commit, component, impact, reproduction steps,
and a minimal proof of concept when possible. Redact personal data and secrets.
If evidence is sensitive, email a short description first and arrange a private
transfer with the maintainers. Send follow-ups to the same address.

Reports about authentication, authorization, monetary correctness, transaction
integrity, webhook delivery and deployment exposure are in scope. A reproducible
resource-exhaustion attack is also welcome. Reports about modified deployments
may need reproduction on a supported version before a fix can be assessed.

## Response and disclosure

Maintainers aim to:

- Acknowledge a report within **3 business days**.
- Provide an initial assessment and next steps within **7 business days**.
- Send progress updates at least every **14 days** while a confirmed issue is open.
- Agree on disclosure timing with the reporter, targeting **90 days** from
  confirmation where feasible and prioritizing actively exploited issues.

These are response targets, not a guarantee of resolution by a particular date.
If you receive no acknowledgement after three business days, follow up privately.
Disclosure may happen sooner when users need urgent mitigations, or later by
agreement when a fix requires additional validation. Publish an advisory with
affected versions, upgrade or mitigation steps, and reporter credit if desired.
