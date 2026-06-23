# Insertion of Sensitive Information into Log File (`DATA-03`)

- **Scope:** uspecs/specs/prod
- **CWE:** [532](https://cwe.mitre.org/data/definitions/532.html) · **STRIDE:** I · **ASVS L2:** V7.1
- **Review mode:** Architecture

## Method

Read `uspecs/specs/prod/apps/logging--td.md` (request-context attributes and the `formatHeaders` usage in the request-context constructor) and cross-checked `uspecs/specs/prod/auth/arch-tokens.md` and `uspecs/specs/prod/routing/arch-ingress.md` for how the bearer credential is carried.

## Problems

### PROD-DATA-03-001

- Artifact: `arch-doc` `uspecs/specs/prod/apps/logging--td.md`
- Severity: Medium

The request-context constructor captures **all** request headers into a log attribute with no redaction. `logging--td.md:156` defines the `headers` attribute as "all request headers formatted as a single string for production debugging of real IP propagation", and `logging--td.md:633` shows `logAttrib_Headers: formatHeaders(req.Header)` writing the full header set into the logging context map.

Authenticated requests carry the principal token in the `Authorization` header (see `arch-tokens.md`). Capturing the complete header set therefore writes the bearer token verbatim into logs. Anyone with read access to those logs obtains a usable credential, and the design specifies no allow-list or redaction of `Authorization` (or `Cookie`) before logging.

## Remediation

Redact sensitive headers before logging: maintain a deny-list (at minimum `Authorization`, `Cookie`, `Set-Cookie`) whose values are replaced with a placeholder in `formatHeaders`, or switch to an explicit allow-list of headers needed for real-IP debugging (e.g. `X-Forwarded-For`, `X-Real-IP`, `Forwarded`). Document the redaction guarantee in the technical design.
