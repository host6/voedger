# Security Threat-Modeling Rule Index

## Purpose

This document is the **rule index and glossary** for security threat-modeling reviews of backend cloud subsystems. It is a reference catalogue only — it carries no per-review status and is never filled in. Each review's outcome is recorded by the **security-check plan** emitted by the `plan` command and completed by the `run` command of the [security-check skill](SKILL.md).

It supports **two review modes** against one rule set, anchored in **Microsoft STRIDE**, **OWASP ASVS L2**, and **CWE Top-25**:

- **Architecture review** — assess whether the subsystem's **specifications** (Domain specs, Functional Design / Gherkin features, Technical Design) address each rule by design.
- **Code review** — assess whether the subsystem's **implementation** (source, configuration, build definitions) satisfies each rule, using the listed methods and tools.

## Review Modes & Evidence Sources

Each rule carries two evidence pointers. The **`Spec Artifact`** column points to where the design should address the rule (architecture review); the **`Method / Tool`** column points to how the implementation is verified (code review).

For architecture review, inspect the subsystem's specification artifacts under `uspecs/specs/{domain}/{context}/`:

| Code   | Artifact Type        | Files                                                   | Provides Evidence Of                                                                      |
| :----- | :------------------- | :------------------------------------------------------ | :---------------------------------------------------------------------------------------- |
| **DS** | Domain specification | `domain.md`                                             | Actors, roles, entities, sensitive-data classification, trust boundaries.                 |
| **FD** | Functional Design    | `*.feature`, `*--reqs.md`                               | Observable behavior: auth/validation scenarios, error handling, input/output rules.       |
| **TD** | Technical Design     | `tech.md`, `arch.md`, `arch-{subsystem}.md`, `*--td.md` | Mechanisms: protocols, data stores, encryption, secrets handling, tech stack, interfaces. |

For code review, apply the verification technique named in the `Method / Tool` column (e.g., Code Review, SAST, DAST, Pen Test, Config/Log Review, secret/dependency scanners) to the implementation.

## Glossary

- **STRIDE**: A threat-modeling methodology developed by Microsoft for identifying security threats across six categories. See: [Microsoft STRIDE threat categories](https://learn.microsoft.com/en-us/azure/security/develop/threat-modeling-tool-threats).
  - **S**poofing: Impersonating a person or program.
  - **T**ampering: Modifying data or code.
  - **R**epudiation: Claiming not to have performed an action.
  - **I**nformation Disclosure: Exposing data to unauthorized parties.
  - **D**enial of Service: Impacting availability.
  - **E**levation of Privilege: Gaining unauthorized access levels.
- **OWASP ASVS L2 (Level 2 - Standard)**: The [Application Security Verification Standard](https://owasp.org/www-project-application-security-verification-standard/) for applications that handle sensitive data.
- **CWE Top-25**: A list of the [most widespread and critical software weaknesses](https://cwe.mitre.org/top25/) that can lead to serious vulnerabilities.
- **Rule**: a required security property named by its official MITRE CWE title and identified by a stable `rule-id` (e.g. `IAM-01`), grouped by architectural layer in the [Rule Index](#rule-index).
- **Artifact**: the concrete unit a rule is checked against — a `file`, a `folder`, a `lib` (library/dependency), an `arch-doc` (architecture/spec document), a `config` file, a `build` definition, or a `route-table`. The planner classifies each in-scope artifact by type.

## Risk Scoring Matrix

Risk is assessed qualitatively using a 4×4 matrix of **Likelihood** vs. **Impact**.

### Matrix Definitions

| Likelihood         | Description                                                             |
| :----------------- | :---------------------------------------------------------------------- |
| **Almost Certain** | Expected to occur in most circumstances (e.g., weekly/monthly).         |
| **Likely**         | Will probably occur in many circumstances (e.g., once or twice a year). |
| **Unlikely**       | Could occur at some time but is infrequent.                             |
| **Rare**           | May occur only in exceptional circumstances.                            |

| Impact         | Description                                                                        |
| :------------- | :--------------------------------------------------------------------------------- |
| **Critical**   | Massive financial/reputational loss, total data breach, or permanent service loss. |
| **Major**      | Significant impact on customers, large-scale data exposure, or long downtime.      |
| **Minor**      | Limited impact on users, small-scale data exposure, or short service disruption.   |
| **Negligible** | No significant impact on customers or data integrity.                              |

### Severity Levels

| Likelihood \ Impact | Negligible | Minor  | Major    | Critical |
| :------------------ | :--------- | :----- | :------- | :------- |
| **Almost Certain**  | Medium     | High   | Critical | Critical |
| **Likely**          | Medium     | Medium | High     | Critical |
| **Unlikely**        | Low        | Medium | Medium   | High     |
| **Rare**            | Low        | Low    | Medium   | Medium   |

## Rule Index

Rules are grouped by architectural layer. All 25 entries of the [CWE Top-25 2025](https://cwe.mitre.org/top25/archive/2025/2025_cwe_top25.html) are included (marked **★**); rules without ★ are not in the 2025 Top-25 but remain relevant for backend cloud subsystems. The **`Rule`** column names the rule by its official MITRE CWE title; **`Spec Artifact`** (DS / FD / TD) drives architecture review and **`Method / Tool`** drives code review.

### 1. Identity & Access Management

| ID     | Rule                                             | STRIDE | ASVS L2 | CWE                                                     | Spec Artifact | Method / Tool         |
| :----- | :----------------------------------------------- | :----- | :------ | :------------------------------------------------------ | :------------ | :-------------------- |
| IAM-01 | Improper Authentication                          | S      | V2.2    | [287](https://cwe.mitre.org/data/definitions/287.html)  | TD, FD        | Code Review, DAST     |
| IAM-02 | Missing Authentication for Critical Function     | S, E   | V2.1    | [★306](https://cwe.mitre.org/data/definitions/306.html) | TD, FD        | Code Review, Pen Test |
| IAM-03 | Missing Authorization                            | E      | V4.2    | [★862](https://cwe.mitre.org/data/definitions/862.html) | DS, TD, FD    | Code Review, Pen Test |
| IAM-04 | Incorrect Authorization                          | E      | V4.3    | [★863](https://cwe.mitre.org/data/definitions/863.html) | TD, FD        | Code Review, Pen Test |
| IAM-05 | Improper Privilege Management                    | E      | V4.1    | [269](https://cwe.mitre.org/data/definitions/269.html)  | DS, TD        | Config Review         |
| IAM-06 | Improper Access Control                          | E      | V4.1    | [★284](https://cwe.mitre.org/data/definitions/284.html) | DS, TD, FD    | Code Review, Pen Test |
| IAM-07 | Authorization Bypass Through User-Controlled Key | E      | V4.2    | [★639](https://cwe.mitre.org/data/definitions/639.html) | TD, FD        | Code Review, Pen Test |

### 2. Transport & API Security

| ID     | Rule                                                                                       | STRIDE | ASVS L2 | CWE                                                     | Spec Artifact | Method / Tool         |
| :----- | :----------------------------------------------------------------------------------------- | :----- | :------ | :------------------------------------------------------ | :------------ | :-------------------- |
| API-01 | Improper Input Validation                                                                  | T      | V5.1    | [★20](https://cwe.mitre.org/data/definitions/20.html)   | FD, TD        | Code Review, SAST     |
| API-02 | Improper Neutralization of Special Elements used in an SQL Command ('SQL Injection')       | T      | V5.2    | [★89](https://cwe.mitre.org/data/definitions/89.html)   | TD            | Code Review, SAST     |
| API-03 | Improper Neutralization of Special Elements used in an OS Command ('OS Command Injection') | T, E   | V5.2    | [★78](https://cwe.mitre.org/data/definitions/78.html)   | TD            | Code Review, SAST     |
| API-04 | Improper Neutralization of Special Elements used in a Command ('Command Injection')        | T, E   | V5.2    | [★77](https://cwe.mitre.org/data/definitions/77.html)   | TD            | Code Review, SAST     |
| API-05 | Improper Control of Generation of Code ('Code Injection')                                  | T, E   | V5.2    | [★94](https://cwe.mitre.org/data/definitions/94.html)   | TD            | Code Review, SAST     |
| API-06 | Improper Neutralization of Input During Web Page Generation ('Cross-site Scripting')       | T      | V5.3    | [★79](https://cwe.mitre.org/data/definitions/79.html)   | TD, FD        | Code Review, DAST     |
| API-07 | Improper Limitation of a Pathname to a Restricted Directory ('Path Traversal')             | T, I   | V12.1   | [★22](https://cwe.mitre.org/data/definitions/22.html)   | TD            | Code Review, SAST     |
| API-08 | Unrestricted Upload of File with Dangerous Type                                            | T, E   | V12.2   | [★434](https://cwe.mitre.org/data/definitions/434.html) | FD, TD        | Code Review, Pen Test |
| API-09 | Cross-Site Request Forgery (CSRF)                                                          | S      | V4.2    | [★352](https://cwe.mitre.org/data/definitions/352.html) | TD, FD        | Code Review, DAST     |
| API-10 | Server-Side Request Forgery (SSRF)                                                         | S, T   | V10.3   | [★918](https://cwe.mitre.org/data/definitions/918.html) | TD            | Code Review, DAST     |
| API-11 | Deserialization of Untrusted Data                                                          | T, E   | V5.5    | [★502](https://cwe.mitre.org/data/definitions/502.html) | TD            | Code Review, SAST     |
| API-12 | Cleartext Transmission of Sensitive Information                                            | I, T   | V9.1    | [319](https://cwe.mitre.org/data/definitions/319.html)  | TD            | Scan (testssl.sh)     |

### 3. Data & Storage

| ID      | Rule                                                       | STRIDE | ASVS L2 | CWE                                                     | Spec Artifact | Method / Tool           |
| :------ | :--------------------------------------------------------- | :----- | :------ | :------------------------------------------------------ | :------------ | :---------------------- |
| DATA-01 | Exposure of Sensitive Information to an Unauthorized Actor | I      | V8.2    | [★200](https://cwe.mitre.org/data/definitions/200.html) | DS, TD        | Code Review, Log Review |
| DATA-02 | Missing Encryption of Sensitive Data                       | I      | V8.1    | [311](https://cwe.mitre.org/data/definitions/311.html)  | TD            | Config Review           |
| DATA-03 | Insertion of Sensitive Information into Log File           | I      | V7.1    | [532](https://cwe.mitre.org/data/definitions/532.html)  | TD            | Log Review, SAST        |

### 4. Infrastructure & Secrets

| ID     | Rule                                                 | STRIDE | ASVS L2 | CWE                                                     | Spec Artifact | Method / Tool                      |
| :----- | :--------------------------------------------------- | :----- | :------ | :------------------------------------------------------ | :------------ | :--------------------------------- |
| SEC-01 | Use of Hard-coded Credentials                        | I      | V14.3   | [798](https://cwe.mitre.org/data/definitions/798.html)  | TD            | Secret Scan (gitleaks, trufflehog) |
| SEC-02 | Uncontrolled Resource Consumption                    | D      | V13.2   | [400](https://cwe.mitre.org/data/definitions/400.html)  | TD            | Load Test, Config Review           |
| SEC-03 | Exposure of Resource to Wrong Sphere                 | E, T   | V14.1   | [668](https://cwe.mitre.org/data/definitions/668.html)  | TD            | Network Map, Config Review         |
| SEC-04 | Allocation of Resources Without Limits or Throttling | D      | V13.2   | [★770](https://cwe.mitre.org/data/definitions/770.html) | TD            | Load Test, Config Review           |

### 5. Dependency & Supply Chain

| ID     | Rule                                                     | STRIDE | ASVS L2 | CWE                                                      | Spec Artifact | Method / Tool                 |
| :----- | :------------------------------------------------------- | :----- | :------ | :------------------------------------------------------- | :------------ | :---------------------------- |
| DEP-01 | Use of Unmaintained Third Party Components               | T, E   | V14.2   | [1104](https://cwe.mitre.org/data/definitions/1104.html) | TD            | Snyk / OWASP dependency-check |
| DEP-02 | Inclusion of Functionality from Untrusted Control Sphere | T, E   | V14.2   | [829](https://cwe.mitre.org/data/definitions/829.html)   | TD            | Config Review, Build Audit    |

### 6. Memory Safety (Native / CGo Code)

> **Note**: Applies only to subsystems whose Technical Design specifies native (C/C++/CGo) components. Mark **N/A** for purely managed-language (Go, Java, Python) subsystems where the runtime eliminates this class of weakness — the tech stack stated in the Technical Design determines applicability.

| ID     | Rule                                                                    | STRIDE  | ASVS L2 | CWE                                                     | Spec Artifact | Method / Tool          |
| :----- | :---------------------------------------------------------------------- | :------ | :------ | :------------------------------------------------------ | :------------ | :--------------------- |
| MEM-01 | Out-of-bounds Write                                                     | T, E    | N/A     | [★787](https://cwe.mitre.org/data/definitions/787.html) | TD            | SAST, AddressSanitizer |
| MEM-02 | Out-of-bounds Read                                                      | I       | N/A     | [★125](https://cwe.mitre.org/data/definitions/125.html) | TD            | SAST, AddressSanitizer |
| MEM-03 | Use After Free                                                          | T, E    | N/A     | [★416](https://cwe.mitre.org/data/definitions/416.html) | TD            | SAST, Valgrind         |
| MEM-04 | Improper Restriction of Operations within the Bounds of a Memory Buffer | T, E, D | N/A     | [119](https://cwe.mitre.org/data/definitions/119.html)  | TD            | SAST, AddressSanitizer |
| MEM-05 | NULL Pointer Dereference                                                | D       | N/A     | [★476](https://cwe.mitre.org/data/definitions/476.html) | TD            | SAST, Code Review      |
| MEM-06 | Integer Overflow or Wraparound                                          | T, E    | N/A     | [190](https://cwe.mitre.org/data/definitions/190.html)  | TD            | SAST, Code Review      |
| MEM-07 | Buffer Copy without Checking Size of Input ('Classic Buffer Overflow')  | T, E    | N/A     | [★120](https://cwe.mitre.org/data/definitions/120.html) | TD            | SAST, AddressSanitizer |
| MEM-08 | Stack-based Buffer Overflow                                             | T, E    | N/A     | [★121](https://cwe.mitre.org/data/definitions/121.html) | TD            | SAST, AddressSanitizer |
| MEM-09 | Heap-based Buffer Overflow                                              | T, E    | N/A     | [★122](https://cwe.mitre.org/data/definitions/122.html) | TD            | SAST, AddressSanitizer |
