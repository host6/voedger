---
name: skill-security-check
description: Plan and run a security threat-modeling check for a scope — the `plan` command builds the check plan, the `run` command executes it
user-invocable: true
disable-model-invocation: true
---

# Security Check

A two-command skill for security threat-modeling review of a backend subsystem, driven by the
Security Threat-Modeling Rule Index (`subsystem-security-glossary.md`, next to this skill):

| Command | Argument      | What it does                                                                                                                                  |
| :------ | :------------ | :-------------------------------------------------------------------------------------------------------------------------------------------- |
| `plan`  | `<scope>`     | Investigate the scope, map applicable rules to artifacts, emit a **check plan** of what to check and against what. Plans only — never checks. |
| `run`   | `<plan-path>` | Execute a plan: check each artifact, fill verdicts, write per-rule reports for problems, and summarize.                                       |

## Invocation

Parse user input as `<command> <argument>`, where `<command>` is `plan` or `run`:

> Security check **plan** for scope `<scope>`.
> Security check **run** for plan `<plan-path>`.

If the command is omitted, infer it: a scope (subsystem / layer / directory / spec context) means
`plan`; a path to a `check-plan.md` means `run`.

## Shared input — the rule index

Both commands always read the rule index from its known location, the `subsystem-security-glossary.md`
next to this skill: the catalogue of rules with their STRIDE tags, CWE links, ASVS L2 refs, and
review-mode evidence pointers (`Spec Artifact`, `Method / Tool`), plus the artifact-type vocabulary
and the risk matrix. Do not ask the user to supply it.

---

# Command: `plan`

Given a **scope**, investigate it, decompose it into concrete **artifacts** (files, folders, libs,
architecture docs, config, build definitions), map every applicable **rule** to its artifacts, and
emit a **check plan**: one `##` section per rule, each opening with a brief CWE-based description and
STRIDE / CWE metadata, followed by an artifact-level table whose verdict / severity / problems cells
are placeholders. **Plans only — it never performs a check or writes a verdict.**

## `plan` inputs

- `<scope>` — a subsystem, an architectural layer, a directory, a spec context, or a cross-cutting
  concern (such as routing). This is the only argument the command accepts.

## `plan` procedure

1. **Load the rule index** from `subsystem-security-glossary.md`.
2. **Determine the review mode** implied by the scope: _architecture_ (specifications), _code_
   (implementation), or _both_. This selects which evidence pointer (`Spec Artifact` vs.
   `Method / Tool`) drives artifact discovery for each rule.
3. **Investigate the scope.** Locate everything that belongs to it and classify each item into a
   typed **artifact** per the glossary vocabulary — `file`, `folder`, `lib`, `arch-doc`, `config`,
   `build`, or `route-table`. Use codebase search plus directory listing; **verify every path
   exists** — never invent paths.
4. **Map rules → artifacts.** For each rule in the index decide:
   - whether it applies to the scope (otherwise list it under `Not Applicable` with a one-line
     justification);
   - which concrete artifacts are relevant to checking it. A rule may map to several artifacts, and
     one artifact may appear under several rules.
5. **Emit the plan** as a single Markdown file (format below), one `##` section per rule, and create
   the reports directory. Do not fill every verdict / severity / problems cell.
   **Do not execute any check, write a results file, or place a results link** — the `run` command
   creates `reports/{rule-id}.md` and appends its link to the rule's section only if it finds problems.

## `plan` output locations

- Scope folder: `<projectRoot>/security-reviews/{scope-id}/`, where `{scope-id}` is the kebab-case scope name.
- Plan file: `<projectRoot>/security-reviews/{scope-id}/check-plan.md`.

## Plan file format

A header block (scope, review mode, date), then one `##` section per **applicable** rule, a
`Not Applicable` list, and a `Brief Summary` placeholder:

```markdown
# Security Check Plan — <scope>

- **Scope:** <scope> (`{scope-id}`)
- **Review mode:** Architecture / Code / Both
- **Date:** YYYY-MM-DD

## `<rule-id>` <rule name>

<1-2 sentence description of the weakness, summarized from its CWE entry.>

**STRIDE:** <letters>, **CWE:** [<id>](https://cwe.mitre.org/data/definitions/<id>.html)

| Artifact            | Verdict | Severity | Problems |
| :------------------ | :------ | :------- | :------- |
| `<type>` `path/one` |         |          |          |

## Not Applicable

- `<rule-id>` <rule name> — <one-line justification>.

## Brief Summary

_To be completed by `run`: a per-rule list (✅ satisfied / ❌ unsatisfied), the Satisfied / Gap / N/A counts, and the headline finding._
```

## `plan` conventions

- One `##` section per **applicable** rule; the heading is `` `<rule-id>` `` (the index `ID`, e.g.
  `IAM-01`) followed by the rule name, for traceability.
- Each section opens with a 1-2 sentence description summarized from its CWE entry, then a metadata
  line carrying the **STRIDE letters** and **CWE link** (copied verbatim from the matching index
  row). `plan` places no results link; `run` appends one to `reports/{rule-id}.md` only on problems.
- The table has one row per mapped artifact: `Artifact` is `` `<type>` `` followed by its verified
  path. Artifact `type` is from the glossary vocabulary (`file`, `folder`, `lib`, `arch-doc`,
  `config`, `build`, `route-table`).
- `Verdict`, `Severity`, and `Problems` values are empty, one per artifact row;
  `run` fills them.
- Every artifact path must be verified to exist before it is written into the plan.
- `plan` never fills a verdict, never writes a report, and never runs a tool — it emits the plan and stops.

---

# Command: `run`

Given a **check-plan file** produced by `plan`, execute the security check by following it: for each
applicable **rule section**, check each listed **artifact**, fill that artifact's `Verdict` /
`Severity` / `Problems` cells, and **only when the rule has problems** write the rule's **results
file** and append a link to it in the rule's section, then complete the plan's `Brief Summary`.

## `run` inputs

- `<plan-path>` — the path to the `check-plan.md` to execute. This is the only argument required; the
  scope, review mode, and output directory are read from the plan itself. Do not infer a plan from a
  scope — the plan must be provided. Results paths are resolved relative to the plan file's directory.

## `run` preconditions

- The given `<plan-path>` must exist and be a `plan`-produced `check-plan.md`. If it is missing or
  malformed, **stop** and instruct the user to run `plan` first — do not invent a plan here.

## `run` procedure

1. **Load the plan** and parse the header and every rule section with its artifact table.
2. **For each rule section**, in plan order, **for each artifact row**:
   - **Re-verify the artifact path exists.** If it no longer exists, treat it as a problem noting the
     stale reference.
   - **Check the rule against this artifact** per the review mode:
     - _Architecture_ (`arch-doc` and DS / FD / TD specs): read the artifact and assess whether the
       design addresses/mitigates the rule; cite the specific sections.
     - _Code_ (`file` / `folder` / `config` / `build` / `lib` / `route-table`): perform code review
       and/or apply the rule's `Method / Tool` from the index; capture concrete evidence
       (`file:line`, command output). Only run tools that are available; if one is missing, say so.
   - **Fill the artifact row cells** (rules below): `Verdict`, `Severity`, `Problems`.
3. **Write the results file and append its link** — **only when the rule has at least one problem
   across its artifacts**: write `reports/{rule-id}.md` using the format below, and append a
   `**Results:** [reports/{rule-id}.md](reports/{rule-id}.md)` line to the rule's section, below its
   artifact table. Rules with no problems get neither file nor link. Never fabricate evidence — cite
   only what was actually read or run.
4. **Complete the `Brief Summary`** (format below): a per-rule list marking each checked rule with a
   green ✅ when satisfied or a red ❌ when unsatisfied, followed by the Satisfied / Gap / N/A counts
   and the headline finding.

## `run` cell-filling rules

Cells are filled **per artifact row**:

- **Verdict** — `✅` Satisfied (no problems for that artifact), `❌` Gap (one or more problems),
  `[-]` N/A (the rule does not apply to that artifact), or **empty** Not reviewed (left only when a
  check could not be performed; state why in the results file or summary).
- **Severity** — only for `❌` artifacts: `Low` / `Medium` / `High` / `Critical` from the glossary
  4×4 Likelihood × Impact risk matrix. Leave blank otherwise.
- **Problems** — the integer count of problems for that artifact. Write `0` when none. The per-problem
  detail lives in the rule's results file, whose link `run` appends to the rule's section on problems.

## `run` summary format

Replace the plan's `Brief Summary` placeholder with a per-rule list followed by a totals line. Mark
each checked rule with a green ✅ when **satisfied** (every artifact `✅` / `[-]`, no gaps) or a red
❌ when **unsatisfied** (any `❌` gap); link unsatisfied rules to their results file:

```markdown
## Brief Summary

- ✅ `<rule-id>` <rule name>
- ❌ `<rule-id>` <rule name> — <severity> · [results](reports/<rule-id>.md)

_Reviewed N rules on YYYY-MM-DD: S satisfied, G gaps, A N/A. Headline: <one-line finding>._
```

Rules left empty (Not reviewed) are listed without a mark and counted separately in the totals line.

## `run` results file format

One results file per rule **with at least one problem**, at `reports/{rule-id}.md`:

```markdown
# <rule name> (`<rule-id>`)

- **Scope:** <scope>
- **CWE:** [<id>](https://cwe.mitre.org/data/definitions/<id>.html) · **STRIDE:** <letters> · **ASVS L2:** <ref>
- **Review mode:** Architecture / Code

## Method

<spec sections read, or tools/commands run (with versions)>

## Problems

<one `###` block per problem, ordered by **severity from high to low** (Critical → High → Medium →
Low); the heading is `<scope uppercased>-<rule-id>-<NNN>` where `<NNN>` is a 3-digit problem number assigned in that
order starting at `001`>

### <scope uppercased>-<rule-id>-001

- Artifact: <the affected artifact (`<type>` `path`)>
- Severity: <severity>

<problem concrete evidence (`file:line`, quoted spec text, command output), and why it is a problem>

## Remediation

<concrete fix per problem>
```

## `run` conventions

- Execute strictly what the plan lists; do not add or drop rules or artifacts. If the plan is
  incomplete, report it rather than silently extending it.
- Fill every artifact row's cells; a results file is written and its link appended **only** for rules
  with at least one problem artifact — all-Satisfied / all-N/A rules get no results file and no link.
- The cell-filling rules and the glossary risk matrix are authoritative for `Verdict` / `Severity` /
  `Problems`.
- All evidence must be real and cited; an artifact that cannot be checked is left empty with the reason.
