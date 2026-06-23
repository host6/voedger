# Exposure of Sensitive Information to an Unauthorized Actor (`DATA-01`)

- **Scope:** uspecs/specs/prod
- **CWE:** [200](https://cwe.mitre.org/data/definitions/200.html) · **STRIDE:** I · **ASVS L2:** V8.2
- **Review mode:** Architecture

## Method

Read `uspecs/specs/prod/routing/arch-debug.md` (entry points, scenarios, cross-cutting concerns) and reviewed the data classification implied by the exposed `pprof` handlers. Cross-checked `uspecs/specs/prod/auth/arch-tokens.md`, `uspecs/specs/prod/storage/structs--arch.md`, and `uspecs/specs/prod/apps/logging--td.md` for comparison.

## Problems

### PROD-DATA-01-001

- Artifact: `arch-doc` `uspecs/specs/prod/routing/arch-debug.md`
- Severity: High

The unauthenticated `pprof` handlers on the public listener expose sensitive runtime internals to any caller. `arch-debug.md:15` enumerates the reachable handlers including `/debug/cmdline` (process command line, which can disclose flags, paths, and embedded arguments), `/debug/symbol` (`arch-debug.md:59`, resolves program counters to function names), and the heap/goroutine profiles via `/debug/{cmd}` (`arch-debug.md:67`). Heap profiles and execution traces can contain fragments of in-memory application data, and symbol/cmdline output reveals internal structure useful for crafting further attacks.

`arch-debug.md:10` confirms "every reachable caller can collect the same profiles" with no authentication, and `arch-debug.md:77` confirms the same routes are mounted on the public listener. This exposes sensitive information to actors not authorized to have it.

## Remediation

Do not serve `pprof` profiles, command line, or symbol data to unauthenticated public callers. Restrict the `/debug/*` handlers to the loopback admin endpoint or require an admin principal, so runtime internals are disclosed only to authorized operators.
