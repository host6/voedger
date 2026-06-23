# Security Check Plan — uspecs/specs/prod

- **Scope:** uspecs/specs/prod (`prod`)
- **Review mode:** Architecture
- **Date:** 2026-06-22

## `IAM-01` Improper Authentication

When an actor claims to have a given identity, the product does not prove or insufficiently proves that the claim is correct.

**STRIDE:** S, **CWE:** [287](https://cwe.mitre.org/data/definitions/287.html)

| Artifact                                           | Verdict | Severity | Problems |
| :------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/auth/arch-authn.md`  | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/authn--td.md`   | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/authn.feature`  | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-tokens.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch.md`        | ✅      |          | 0        |

## `IAM-02` Missing Authentication for Critical Function

The product does not perform any authentication for functionality that requires a provable user identity or consumes significant resources.

**STRIDE:** S, E, **CWE:** [306](https://cwe.mitre.org/data/definitions/306.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-debug.md`   | ❌      | High     | 1        |
| `arch-doc` `uspecs/specs/prod/auth/authn--td.md`       | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md` | ✅      |          | 0        |

**Results:** [reports/IAM-02.md](reports/IAM-02.md)

## `IAM-03` Missing Authorization

The product does not perform an authorization check when an actor attempts to access a resource or perform an action.

**STRIDE:** E, **CWE:** [862](https://cwe.mitre.org/data/definitions/862.html)

| Artifact                                                   | Verdict | Severity | Problems |
| :--------------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/auth/arch-authz.md`          | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md`     | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-dml.feature`       | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-view-read.feature` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-blob-read.feature` | ✅      |          | 0        |

## `IAM-04` Incorrect Authorization

The product performs an authorization check but does so incorrectly, allowing attackers to bypass intended access restrictions.

**STRIDE:** E, **CWE:** [863](https://cwe.mitre.org/data/definitions/863.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/auth/arch-authz.md`      | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-membership.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-dml.feature`   | ✅      |          | 0        |

## `IAM-05` Improper Privilege Management

The product does not properly assign, modify, track, or check privileges, creating an unintended sphere of control.

**STRIDE:** E, **CWE:** [269](https://cwe.mitre.org/data/definitions/269.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/auth/arch-authz.md`      | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-tokens.md`     | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-membership.md` | ✅      |          | 0        |

## `IAM-06` Improper Access Control

The product does not restrict or incorrectly restricts access to a resource from an unauthorized actor.

**STRIDE:** E, **CWE:** [284](https://cwe.mitre.org/data/definitions/284.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/auth/arch.md`            | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-authz.md`      | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/routing/arch-debug.md`   | ❌      | High     | 1        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md` | ✅      |          | 0        |

**Results:** [reports/IAM-06.md](reports/IAM-06.md)

## `IAM-07` Authorization Bypass Through User-Controlled Key

Authorization is based on a key the user can control, allowing access to other users' data by modifying the key (IDOR).

**STRIDE:** E, **CWE:** [639](https://cwe.mitre.org/data/definitions/639.html)

| Artifact                                                | Verdict | Severity | Problems |
| :------------------------------------------------------ | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/auth/arch-authz.md`       | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md`  | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-dml.feature`    | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/storage/structs--arch.md` | ✅      |          | 0        |

## `API-01` Improper Input Validation

The product does not validate or incorrectly validates input that affects the control flow or data handling of the program.

**STRIDE:** T, **CWE:** [20](https://cwe.mitre.org/data/definitions/20.html)

| Artifact                                                   | Verdict | Severity | Problems |
| :--------------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-ingress.md`     | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-dml.feature`       | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-view-read.feature` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/authn--td.md`           | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/storage/appttl--arch.md`     | ✅      |          | 0        |

## `API-02` SQL Injection

The product constructs an SQL command from externally-influenced input without neutralizing special elements that could modify the intended command.

**STRIDE:** T, **CWE:** [89](https://cwe.mitre.org/data/definitions/89.html)

| Artifact                                                   | Verdict | Severity | Problems |
| :--------------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/apps/vsql-dml.feature`       | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-view-read.feature` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/storage/structs--arch.md`    | ✅      |          | 0        |

## `API-04` Command Injection

The product constructs a command from externally-influenced input without neutralizing special elements that could modify the intended command.

**STRIDE:** T, E, **CWE:** [77](https://cwe.mitre.org/data/definitions/77.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/apps/vsql-dml.feature`   | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md` | ✅      |          | 0        |

## `API-07` Path Traversal

The product uses externally-influenced input to construct a pathname without neutralizing sequences that resolve to a location outside the restricted directory.

**STRIDE:** T, I, **CWE:** [22](https://cwe.mitre.org/data/definitions/22.html)

| Artifact                                                     | Verdict | Severity | Problems |
| :----------------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-reverse-proxy.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/routing/arch-tls.md`           | [-]     |          |          |
| `arch-doc` `uspecs/specs/prod/apps/vsql-blob-read.feature`   | ✅      |          | 0        |

## `API-08` Unrestricted Upload of File with Dangerous Type

The product allows the upload of files without sufficiently restricting type, size, or destination, enabling abuse.

**STRIDE:** T, E, **CWE:** [434](https://cwe.mitre.org/data/definitions/434.html)

| Artifact                                                   | Verdict | Severity | Problems |
| :--------------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-ingress.md`     | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md`     | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/vsql-blob-read.feature` | [-]     |          |          |

## `API-09` Cross-Site Request Forgery (CSRF)

The product does not sufficiently verify that a well-formed, valid, consistent request was intentionally provided by the user who submitted it.

**STRIDE:** S, **CWE:** [352](https://cwe.mitre.org/data/definitions/352.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-ingress.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-tokens.md`     | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-authn.md`      | ✅      |          | 0        |

## `API-10` Server-Side Request Forgery (SSRF)

The product fetches a remote resource using a URL influenced by an actor without sufficiently ensuring the destination is the intended one.

**STRIDE:** S, T, **CWE:** [918](https://cwe.mitre.org/data/definitions/918.html)

| Artifact                                                     | Verdict | Severity | Problems |
| :----------------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-reverse-proxy.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/routing/arch-tls.md`           | [-]     |          |          |
| `arch-doc` `uspecs/specs/prod/arch-user-email-sending.md`    | ✅      |          | 0        |

## `API-11` Deserialization of Untrusted Data

The product deserializes untrusted data without sufficiently verifying that the resulting data will be valid.

**STRIDE:** T, E, **CWE:** [502](https://cwe.mitre.org/data/definitions/502.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-ingress.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-tokens.md`     | ✅      |          | 0        |

## `API-12` Cleartext Transmission of Sensitive Information

The product transmits sensitive or security-critical data in cleartext in a communication channel that can be sniffed.

**STRIDE:** I, T, **CWE:** [319](https://cwe.mitre.org/data/definitions/319.html)

| Artifact                                                     | Verdict | Severity | Problems |
| :----------------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-tls.md`           | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/routing/arch-ingress.md`       | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/routing/arch-reverse-proxy.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/arch-user-email-sending.md`    | ✅      |          | 0        |

## `DATA-01` Exposure of Sensitive Information to an Unauthorized Actor

The product exposes sensitive information to an actor that is not explicitly authorized to have access to it.

**STRIDE:** I, **CWE:** [200](https://cwe.mitre.org/data/definitions/200.html)

| Artifact                                                | Verdict | Severity | Problems |
| :------------------------------------------------------ | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-debug.md`    | ❌      | High     | 1        |
| `arch-doc` `uspecs/specs/prod/auth/arch-tokens.md`      | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/storage/structs--arch.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/logging--td.md`      | ✅      |          | 0        |

**Results:** [reports/DATA-01.md](reports/DATA-01.md)

## `DATA-02` Missing Encryption of Sensitive Data

The product does not encrypt sensitive data before storage or transmission.

**STRIDE:** I, **CWE:** [311](https://cwe.mitre.org/data/definitions/311.html)

| Artifact                                                | Verdict | Severity | Problems |
| :------------------------------------------------------ | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/storage/structs--arch.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/authn--td.md`        | ✅      |          | 0        |

## `DATA-03` Insertion of Sensitive Information into Log File

The product writes sensitive information to a log file that is accessible to actors who are not authorized to read it.

**STRIDE:** I, **CWE:** [532](https://cwe.mitre.org/data/definitions/532.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/apps/logging--td.md`     | ❌      | Medium   | 1        |
| `arch-doc` `uspecs/specs/prod/routing/arch-ingress.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-tokens.md`     | ✅      |          | 0        |

**Results:** [reports/DATA-03.md](reports/DATA-03.md)

## `SEC-01` Use of Hard-coded Credentials

The product contains hard-coded credentials used for authentication or for encrypting/decrypting data.

**STRIDE:** I, **CWE:** [798](https://cwe.mitre.org/data/definitions/798.html)

| Artifact                                                  | Verdict | Severity | Problems |
| :-------------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/auth/authn--td.md`          | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/auth/arch-tokens.md`        | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/arch-user-email-sending.md` | ✅      |          | 0        |

## `SEC-02` Uncontrolled Resource Consumption

The product does not properly control the allocation and maintenance of a limited resource, enabling exhaustion.

**STRIDE:** D, **CWE:** [400](https://cwe.mitre.org/data/definitions/400.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-ingress.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/storage/appttl--arch.md` | ✅      |          | 0        |

## `SEC-03` Exposure of Resource to Wrong Sphere

The product exposes a resource to the wrong control sphere, granting access to actors who should not have it.

**STRIDE:** E, T, **CWE:** [668](https://cwe.mitre.org/data/definitions/668.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-debug.md`   | ❌      | High     | 1        |
| `arch-doc` `uspecs/specs/prod/routing/arch.md`         | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-deployment.md` | ✅      |          | 0        |

**Results:** [reports/SEC-03.md](reports/SEC-03.md)

## `SEC-04` Allocation of Resources Without Limits or Throttling

The product allocates a reusable resource on behalf of an actor without imposing restrictions on size or rate.

**STRIDE:** D, **CWE:** [770](https://cwe.mitre.org/data/definitions/770.html)

| Artifact                                               | Verdict | Severity | Problems |
| :----------------------------------------------------- | :------ | :------- | :------- |
| `arch-doc` `uspecs/specs/prod/routing/arch-ingress.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/apps/arch-processing.md` | ✅      |          | 0        |
| `arch-doc` `uspecs/specs/prod/storage/appttl--arch.md` | ✅      |          | 0        |

## Not applicable

Rules excluded from this architecture review, with justification.

| Rule       | Title                                            | Justification                                                                                                             |
| :--------- | :----------------------------------------------- | :------------------------------------------------------------------------------------------------------------------------ |
| API-03     | OS Command Injection                             | No specification in scope describes spawning OS processes from request data; the platform dispatches typed VSQL/commands. |
| API-05     | Code Injection                                   | The WASM extension runtime that executes app code is not part of the `prod` scope specifications under review.            |
| API-06     | Cross-site Scripting                             | Backend emits JSON and `text/event-stream` responses only; no server-side HTML/web-page generation in scope.              |
| DEP-01     | Use of Unmaintained Third Party Components       | Dependency maintenance is a build/supply-chain concern verified in code review, not addressed by architecture specs.      |
| DEP-02     | Inclusion of Functionality from Untrusted Sphere | Build/packaging provenance is a supply-chain concern verified in code review, not addressed by architecture specs.        |
| MEM-01..09 | Memory-safety weaknesses                         | The `prod` stack is Go (managed runtime); no native/CGo components are specified, so this class is eliminated by design.  |

## Brief Summary

- ✅ `IAM-01` Improper Authentication
- ❌ `IAM-02` Missing Authentication for Critical Function — High · [results](reports/IAM-02.md)
- ✅ `IAM-03` Missing Authorization
- ✅ `IAM-04` Incorrect Authorization
- ✅ `IAM-05` Improper Privilege Management
- ❌ `IAM-06` Improper Access Control — High · [results](reports/IAM-06.md)
- ✅ `IAM-07` Authorization Bypass Through User-Controlled Key
- ✅ `API-01` Improper Input Validation
- ✅ `API-02` SQL Injection
- ✅ `API-04` Command Injection
- ✅ `API-07` Path Traversal
- ✅ `API-08` Unrestricted Upload of File with Dangerous Type
- ✅ `API-09` Cross-Site Request Forgery (CSRF)
- ✅ `API-10` Server-Side Request Forgery (SSRF)
- ✅ `API-11` Deserialization of Untrusted Data
- ✅ `API-12` Cleartext Transmission of Sensitive Information
- ❌ `DATA-01` Exposure of Sensitive Information to an Unauthorized Actor — High · [results](reports/DATA-01.md)
- ✅ `DATA-02` Missing Encryption of Sensitive Data
- ❌ `DATA-03` Insertion of Sensitive Information into Log File — Medium · [results](reports/DATA-03.md)
- ✅ `SEC-01` Use of Hard-coded Credentials
- ✅ `SEC-02` Uncontrolled Resource Consumption
- ❌ `SEC-03` Exposure of Resource to Wrong Sphere — High · [results](reports/SEC-03.md)
- ✅ `SEC-04` Allocation of Resources Without Limits or Throttling

_Reviewed 23 applicable rules on 2026-06-22: 18 satisfied, 5 gaps; 14 rules N/A. Headline: the unauthenticated `net/http/pprof` debug surface mounted on the public listener (`routing/arch-debug.md`) drives four High-severity gaps (IAM-02, IAM-06, DATA-01, SEC-03), and unredacted full-header logging (`apps/logging--td.md`) leaks the `Authorization` bearer token into logs (DATA-03)._
