# Missing Authentication for Critical Function (`IAM-02`)

- **Scope:** uspecs/specs/prod
- **CWE:** [306](https://cwe.mitre.org/data/definitions/306.html) · **STRIDE:** S, E · **ASVS L2:** V2.1
- **Review mode:** Architecture

## Method

Read `uspecs/specs/prod/routing/arch-debug.md` (external actors, scenarios, cross-cutting concerns) and cross-checked against `uspecs/specs/prod/auth/authn--td.md` and `uspecs/specs/prod/apps/arch-processing.md` to confirm no authentication operator is interposed before the debug handlers on the public listener.

## Problems

### PROD-IAM-02-001

- Artifact: `arch-doc` `uspecs/specs/prod/routing/arch-debug.md`
- Severity: High

The `net/http/pprof` handlers are mounted on the public HTTP/HTTPS listener with no authentication. `arch-debug.md:10` states the debug routes "carry no authentication, role check, or method restriction, so every reachable caller can collect the same profiles", and `arch-debug.md:77` confirms `registerDebugHandlers` runs in the same `routerService.Prepare` as the public listener, so "the same `/debug/*` routes are mounted there without authentication and are reachable by any `*Client`". `arch-debug.md:109` further states these routes "are not gated by `[Query limiter]`, `[Request validator]`, authentication, or any other operator", and the scenario at `arch-debug.md:101-104` shows `https://<public-host>/debug/profile?seconds=30` served with "no authentication is performed".

These are critical functions: `pprof.Profile` / `pprof.Trace` consume significant CPU and runtime resources on demand (`/debug/profile?seconds=30` ties up profiling for 30s per call), and `pprof.Cmdline` returns process information. Exposing them unauthenticated on the public listener is missing authentication for a resource-consuming, identity-requiring function.

The design delegates protection to an operator-side concern (network ACLs, upstream reverse proxy, or an external auth layer) but the architecture specifies no in-product authentication and no secure default, so an unprotected deployment exposes the endpoints by default.

## Remediation

Gate the `/debug/*` handlers behind authentication when mounted on the public listener — either bind them exclusively to the loopback admin endpoint (do not register them in the public-listener handler graph), or require a System/admin principal token before the handler runs. If operator-side network controls remain the chosen mitigation, make non-exposure the secure default and document the required control as a mandatory deployment precondition rather than an optional concern.
