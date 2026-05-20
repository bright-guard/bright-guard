// Glossary for inline term tooltips. Each entry pairs a short label with a
// 1–3 sentence definition and a target doc + anchor so the "Learn more" link
// in HelpTooltip can hand off to the page-level slide-over.
export type Term = {
  label: string;
  definition: string;
  slug: string;
  anchor?: string;
};

export const TERMS: Record<string, Term> = {
  enforced: {
    label: "ENFORCED",
    definition:
      "A policy decision that actually blocked an invocation. The shim refused the call before it reached the upstream MCP server.",
    slug: "policies/cel-primer",
    anchor: "how-matches-show-up",
  },
  denied: {
    label: "DENIED",
    definition:
      "The invocation was blocked. Either the gateway shim enforced a deny policy locally, or the control plane rejected it on ingestion.",
    slug: "activity-timeline",
    anchor: "status-chips",
  },
  audit: {
    label: "AUDIT",
    definition:
      "The invocation was observed and recorded but not blocked. Policies in audit mode log a decision without affecting the call.",
    slug: "policies/cel-primer",
    anchor: "how-matches-show-up",
  },
  warn: {
    label: "WARN",
    definition:
      "A policy matched but its action is set to warn rather than deny. The call proceeded; the decision was recorded for review.",
    slug: "policies/cel-primer",
    anchor: "how-matches-show-up",
  },
  public_exposure: {
    label: "Public exposure",
    definition:
      "The MCP server is reachable from the public internet. The classifier confirmed a successful TCP connection from outside any private network.",
    slug: "activity-timeline",
  },
  cloud_internal_exposure: {
    label: "Cloud-internal exposure",
    definition:
      "The MCP server resolves to a cloud provider's internal range (RFC1918 + metadata IPs). Reachable from VPC peers but not the public internet.",
    slug: "activity-timeline",
  },
  internal_exposure: {
    label: "Internal exposure",
    definition:
      "The MCP server is on a private network only — typically RFC1918 with no successful external probe.",
    slug: "activity-timeline",
  },
  unreachable_exposure: {
    label: "Unreachable",
    definition:
      "The classifier could not connect to the MCP server's address from any vantage point. It may be firewalled, offline, or behind an opaque tunnel.",
    slug: "activity-timeline",
  },
  unknown_exposure: {
    label: "Unknown exposure",
    definition:
      "Exposure has not been classified yet. New servers start here until the next classification sweep runs.",
    slug: "activity-timeline",
  },
  new_caller: {
    label: "NEW",
    definition:
      "A caller identity first observed within the last 7 days that has not yet been acknowledged. Use Acknowledge as known to clear the flag.",
    slug: "activity-timeline",
    anchor: "caller-identity",
  },
  cel: {
    label: "CEL",
    definition:
      "Common Expression Language — a small, sandboxed expression syntax (originally from Google's IAM stack) used here to write policies. See the primer for variables in scope.",
    slug: "policies/cel-primer",
    anchor: "what-variables-are-in-scope",
  },
  gateway: {
    label: "Gateway",
    definition:
      "A host running the Bright Guard shim. The shim proxies MCP invocations, enforces policies locally, and reports observations back to the control plane.",
    slug: "gateways/install",
  },
  policy_bundle_version: {
    label: "Policy bundle version",
    definition:
      "A monotonic version + content hash pair attached to the policy set the control plane hands to each gateway on heartbeat. Lets the shim detect a bundle change without re-fetching.",
    slug: "policies/cel-primer",
    anchor: "how-matches-show-up",
  },
};

export function termBySlug(slug: string): Term | undefined {
  return TERMS[slug];
}
