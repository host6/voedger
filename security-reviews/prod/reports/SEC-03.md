# Exposure of Resource to Wrong Sphere (`SEC-03`)

- **Scope:** uspecs/specs/prod
- **CWE:** [668](https://cwe.mitre.org/data/definitions/668.html) · **STRIDE:** E, T · **ASVS L2:** V14.1
- **Review mode:** Architecture

## Method

Read `uspecs/specs/prod/routing/arch-debug.md` (cross-subsystem components, cross-cutting concerns) and compared the intended control sphere of the admin endpoint against the public listener described in `uspecs/specs/prod/routing/arch.md` and `uspecs/specs/prod/apps/arch-deployment.md`.

## Problems

### PROD-SEC-03-001

- Artifact: `arch-doc` `uspecs/specs/prod/routing/arch-debug.md`
- Severity: High

Runtime-diagnostics resources intended for the operator/admin control sphere are also mounted on the public, internet-facing sphere. The admin endpoint is "restricted only by the loopback bind address (`httpu.LocalhostIP:AdminPort`)" (`arch-debug.md:73`), establishing that `/debug/*` is designed as a loopback-only operator resource. However, `arch-debug.md:109` states `registerDebugHandlers` "is therefore part of the handler graph on every listener that runs a `routerService` — both the admin endpoint ... and the public HTTP/HTTPS listener", and `arch-debug.md:77` confirms the same `/debug/*` routes are reachable by any `*Client` on the public listener.

Mounting a loopback-scoped diagnostics resource onto the public listener places it in the wrong control sphere, granting access to actors (arbitrary internet callers) who should not reach it.

## Remediation

Confine `/debug/*` to the admin (loopback) sphere only: skip `registerDebugHandlers` for the public listener, or guard registration with a configuration flag that defaults to admin-only. Keep the diagnostics resource within the operator control sphere by construction rather than relying on external network ACLs.
