# Improper Access Control (`IAM-06`)

- **Scope:** uspecs/specs/prod
- **CWE:** [284](https://cwe.mitre.org/data/definitions/284.html) · **STRIDE:** E · **ASVS L2:** V4.1
- **Review mode:** Architecture

## Method

Read `uspecs/specs/prod/routing/arch-debug.md` and compared its access-control posture against the principal/role model in `uspecs/specs/prod/auth/arch.md` and `uspecs/specs/prod/auth/arch-authz.md`, and the request pipeline in `uspecs/specs/prod/apps/arch-processing.md`.

## Problems

### PROD-IAM-06-001

- Artifact: `arch-doc` `uspecs/specs/prod/routing/arch-debug.md`
- Severity: High

The debug subsystem applies no access-control check to restrict the `/debug/*` resource to authorized actors on the public listener. `arch-debug.md:10` describes the actor as "Any caller able to open a TCP connection to either listener" and states the routes carry "no authentication, role check, or method restriction". Unlike the rest of the routing context, these handlers bypass the principal-based authorization model documented in `arch-authz.md`: `arch-debug.md:109` confirms the routes "are not gated by `[Query limiter]`, `[Request validator]`, authentication, or any other operator".

Because `registerDebugHandlers` runs before `registerReverseProxyHandler` (`arch-debug.md:81`), the `/debug/*` routes take precedence on whichever listener they are mounted, so the public listener grants unauthenticated, unauthorized access to a runtime-diagnostics resource that should be restricted to operators.

## Remediation

Restrict access to `/debug/*` to an authorized sphere: confine the handlers to the loopback admin endpoint, or enforce an admin-principal authorization check in the public-listener handler graph consistent with the routing context's authorization model. Do not rely on route ordering or external network controls as the sole access-control mechanism.
