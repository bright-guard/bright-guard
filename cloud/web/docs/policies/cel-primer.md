# Writing a policy: CEL primer

Bright Guard policies are written in [CEL](https://github.com/google/cel-spec),
the same expression language Google uses for IAM conditions and Cloud Armor
rules. CEL is small, fast, and side-effect-free — a policy is a boolean
expression that says "should this invocation be flagged?" and Bright Guard
takes the configured action when the expression returns `true`.

Every policy has three pieces:

- **Name** — human label shown in the SPA.
- **Action** — `deny` (block the invocation) or `warn` (record a decision
  without blocking).
- **Expression** — the CEL expression.

## The shortest policy

Block any tool whose name contains `delete`:

```
capability.kind == "tool" && capability.name.contains("delete")
```

Save it on `/app/policies` with action `deny`. Within one heartbeat (≤30s)
every gateway picks up the bundle and starts enforcing it.

## What variables are in scope

CEL expressions evaluate against a fixed environment. The full list is
generated from source in the [CEL environment reference](../reference/cel-env.md);
the highlights:

| Variable | Type | Common uses |
|---|---|---|
| `caller` | `map<string, dyn>` | `caller.agent == "my-bot"`, `caller.user == "ci@example.com"` |
| `server` | `map<string, string>` | `server.name`, `server.transport == "streamable-http"` |
| `capability` | `map<string, string>` | `capability.kind` (`"tool"` / `"resource"` / `"prompt"`), `capability.name`, `capability.description` |
| `at` | `timestamp` | `at.getHours() < 6` — wall-clock UTC of the invocation |
| `status` | `string` | `"ok"` / `"error"` / `"denied"`; useful for warn rules that fire only on already-blocked calls |

Anything else (`true && some_other_var`) is a compile-time error. The
**Simulate** button on the policy detail page validates the expression
against historical observations so you can see what would have matched
before you save.

## Cost limit

Every evaluation runs under a hard cost cap (currently **50 000 units**;
roughly one unit per AST operation). The example expressions in this page
cost <10. If a policy exceeds the limit it errors out — the invocation is
**not** denied; the policy is treated as having not matched. The error is
logged to Cloud Logging so a runaway expression doesn't fail-closed across
the fleet.

Practically, the cost cap is only hit by expressions that walk large nested
structures inside `caller.*`. Keep policies focused on a handful of fields.

## Worked examples

### Block production writes from CI

```
caller.agent == "ci-runner" &&
capability.kind == "tool" &&
(capability.name.startsWith("create_") ||
 capability.name.startsWith("delete_") ||
 capability.name.startsWith("update_"))
```

Action: `deny`.

### Warn on off-hours access

```
(at.getHours() < 7 || at.getHours() > 19) &&
server.name == "prod-database"
```

Action: `warn`. Doesn't block — but every matched invocation gets the
`would have been warned` chip in the activity timeline.

### Deny when an upstream tool's description mentions deletion

```
capability.kind == "tool" &&
capability.description.contains("delete")
```

A coarse-grained safety net for new tools you haven't audited yet.

### Allow-list a small set of read-only tools

```
!(capability.kind == "tool" &&
  (capability.name == "search_issues" ||
   capability.name == "get_issue" ||
   capability.name == "list_projects"))
```

Action: `deny`. Reads like "anything that isn't in this allow-list, deny".

## How matches show up

When a policy matches an invocation, the row in **Activity** sprouts a chip:

- **ENFORCED** — the invocation was blocked. Status is `denied`; the chip is
  red and reads "by &lt;policy name&gt;".
- **would have been blocked** — the invocation went through, but a `deny`
  policy matched. The chip is amber.
- **would have been warned** — a `warn` policy matched. Amber chip.

See [The activity timeline](../activity-timeline.md) for the full chip
vocabulary.

## Disabled features

The following CEL extensions are deliberately **not** enabled:

- `cel.ext.Strings()` — its regex helpers (`matches`, `replace`) are a
  backtracking DoS surface when fed tenant input. Use `.startsWith()`,
  `.endsWith()`, and `.contains()` instead.

If you have a use case that genuinely needs regex, file an issue — we may
ship a narrowly-scoped helper that runs under a separate cost budget.
