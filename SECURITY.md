# Security Policy

OpenGuardrail is a development preview. Security reports are welcome.

## Supported versions

Only the latest commit on the default branch is maintained during the preview.
There are no supported release branches or backported security fixes yet.
There is no production support or fixed security response-time commitment.

## Reporting a vulnerability

Use **GitHub Private Vulnerability Reporting** in
[alibaba/open-guardrail](https://github.com/alibaba/open-guardrail):

1. Open the [private vulnerability reporting form](https://github.com/alibaba/open-guardrail/security/advisories/new)
   (**Security > Advisories > Report a vulnerability**).
2. Complete the private report with the information below.
3. Continue the investigation and disclosure discussion in that private advisory.

See [GitHub's reporting instructions](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/report-privately)
for the complete procedure. Repository administrators and security managers with
advisory access receive these reports. The project uses this channel rather than
a separate security mailbox.

Keep vulnerability details out of public issues, discussions and conduct reports.
Community conduct reports follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

Include the affected revision, impact, reproduction steps, relevant configuration
and any suggested mitigation. Remove credentials, personal data, private hostnames
and production records from the report. Maintainers acknowledge reports privately
and coordinate disclosure after a fix or mitigation is available.

## Preview security boundaries

- Bind directly to loopback for local evaluation. A non-loopback deployment requires
  an HTTPS public URL and a trusted TLS reverse proxy.
- The local administrator is a preview identity mechanism. Failed logins consume a
  persistent shared attempt budget and trigger temporary exponential backoff, but
  deployments must still apply source-based rate limiting at the trusted reverse
  proxy.
- Project machine keys expire after 90 days by default (configurable from one
  minute to 365 days). Administrators can list metadata, revoke keys or rotate
  their secrets. Rotation requires the current key version and immediately
  invalidates the old secret; use separate keys when consumers need an overlap.
  Revocation prevents subsequent authentication; it does not cancel requests
  that already authenticated. Run Submission Credentials remain separately
  scoped and can be rotated while the Run is open.
- Every current storage backend reports `multiInstance: false`. Run only one service
  instance per database.
- Store database credentials, bootstrap tokens, sessions and machine keys in the
  deployment platform's secret mechanism. Never commit them or include them in logs.

These limitations are also documented in the administrator and architecture guides.
