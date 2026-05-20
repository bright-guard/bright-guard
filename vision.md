# Bright Guard — Vision

## Vision

**Bright Guard is the control plane for AI infrastructure.**

As enterprises adopt AI agents and the Model Context Protocol (MCP) to connect those agents to internal systems, a new class of infrastructure is emerging — one that today operates with no inventory, no policy, and no audit trail. MCP endpoints behave like APIs, but they are discovered ad-hoc, exposed without governance, and accessed by agents that can chain capabilities in ways no one has approved.

Bright Guard exists to give enterprises **visibility, governance, and enforcement** over this new surface — using the network itself (DNS, traffic, workload identity) as the foundational signal. We turn MCP from an ungoverned sprawl into a managed control plane that security, networking, and platform teams can operate together.

## Mission

Make every MCP endpoint, capability, and interaction in an enterprise **discoverable, governable, and enforceable** — with a single policy model that applies consistently across cloud, endpoint, and network.

## The Problem

AI agents are now first-class consumers of enterprise data and services, and MCP is becoming the default protocol that connects them. This creates three gaps that existing security tooling does not close:

1. **No inventory.** Security and IT teams cannot enumerate the MCP servers running in their environment, who deployed them, or what they expose.
2. **No governance.** There is no policy layer that says *which* agents, users, or workloads may invoke *which* capabilities on *which* endpoints — let alone one that respects network context.
3. **No audit.** When an agent exfiltrates data, misuses a credential, or invokes a capability it shouldn't, there is no trail and no enforcement point to stop it.

The networking layer — DNS, traffic, workload identity — already sees all of this. Bright Guard turns that vantage point into a product.

## Strategic Positioning

Bright Guard is the **AI governance and control plane** that sits between AI agents and the systems they reach. It is:

- **Network-native** — DNS and traffic are the primary signals, not agent-side instrumentation.
- **Policy-first** — discovery and audit serve a policy model, not the other way around.
- **Environment-unified** — one control plane across cloud, Kubernetes, and endpoints.

| Buyer role | Decision |
|---|---|
| Economic buyer | CISO / Security leadership |
| Technical buyer | Enterprise Architect / Platform Engineering |
| Primary operator | Network / DevOps |
| Policy owner | GRC / AI Governance |

## Core Capabilities

At the highest level, Bright Guard delivers three capabilities that build on each other:

1. **Visibility** — discover MCP servers, enumerate their capabilities, and track how they are used.
2. **Governance & Policy** — define and enforce policies by endpoint, capability, user, asset, and network context.
3. **Simulation & Operational Control** — validate policy changes before rollout, audit outcomes after, and enforce consistently across every environment.

## Use Cases

### 1. MCP Server Discovery

Build a continuous, authoritative catalog of every MCP server in the environment.

- **Primary signal:** DNS — known MCP service names, lookup patterns, and resolution behavior.
- **Augmenting signals:** HTTP and proxy telemetry, workload metadata, third-party feeds.
- **Output:** an inventory of MCP endpoints with owner, location, exposure, and first-seen / last-seen state.

### 2. Capability Discovery

For each discovered endpoint, enumerate the capabilities it exposes — tools, resources, and prompts — so that policy can be written against what an endpoint *actually does*, not just where it lives.

- Establishes the surface area of each endpoint.
- Detects capability drift when servers add, remove, or change what they offer.

### 3. Monitoring & Audit Trail

Continuously monitor MCP endpoints and the agents, users, and workloads that interact with them.

- Usage telemetry per endpoint, per capability, per caller.
- A durable audit trail suitable for incident response and compliance reporting.

### 4. Policy Definition & Enforcement

Define and apply policies that govern endpoint behavior, access, and data handling.

- **Subjects:** endpoints, capabilities, users, agents, workloads.
- **Controls:** allow / deny, rate limits, data-handling constraints, credential requirements.
- **Enforcement:** at the network layer, at the endpoint agent, or at a cloud gateway.

### 5. Policy Simulation (Pre- and Post-Mortem)

Reduce the risk of rolling out a new policy.

- **Pre-mortem:** simulate a proposed policy against recent traffic to predict what it would have blocked or allowed.
- **Post-mortem:** after enforcement, report what *was* blocked and what *would have been* blocked under alternate rules.

### 6. Asset- and User-Level Policy

Policies are not just endpoint-based. Bright Guard binds policies to **context**:

- Specific users or service identities.
- Specific assets — workloads, hosts, clusters.
- Specific agents and agent classes.

### 7. Network-Aware Enforcement

Use the network position as a first-class policy dimension.

- Apply different policies based on subnet, VPC, Kubernetes namespace, or cluster.
- Leverage DNS and workload communication visibility to enforce rules where traffic already flows.

### 8. Internal vs. External Exposure Control

Detect and control MCP endpoints that are internal but reachable externally, and prevent unsafe data flows out of the enterprise.

- Flag internal endpoints accidentally exposed to the public internet.
- Block data flows from internal MCP servers to public AI endpoints when policy forbids them.

### 9. Credential and API-Key Governance

Detect and enforce policy on the credentials AI agents present.

- Identify unauthorized personal API keys in use against enterprise endpoints.
- Enforce that only approved credentials, scopes, or identity providers may access governed endpoints.

### 10. Distributed Enforcement Layer

Administrators can deploy enforcement where it fits their environment:

- **Cloud-based enforcement** — gateway or proxy mode, optionally certificate-based.
- **Endpoint agent enforcement** — local enforcement co-resident with the workload.

Both modes share one policy model and one audit stream.

### 11. Unified Policy Across Environments

A single policy authored once applies consistently across cloud, Kubernetes, on-prem, and endpoint enforcement points. There is one control plane, not one per environment.

## Personas

Bright Guard serves three persona clusters. Each cluster has a clear focus and a clear buying motion.

### Cluster A — Infrastructure & Operations

Owns the network and runtime where MCP traffic actually lives. Their primary focus is **discovery, observability, and enforcement deployment**.

#### IT / Network Administrator
- **Owns:** network infrastructure, DNS, connectivity.
- **Goals:** discover MCP servers, understand traffic and dependencies, ensure controlled connectivity.
- **Needs:** DNS-based discovery, network-driven policy (subnets, clusters), workload-communication visibility.
- **Why they care:** MCP traffic runs over the network — they are the natural owner of the control point.

#### DevOps / Platform Engineering
- **Owns:** Kubernetes, cloud platforms, runtime infrastructure.
- **Goals:** keep MCP workloads reliable, apply environment-aware policy, deploy enforcement consistently.
- **Needs:** network-aware policies per cluster and environment, enforcement-agent deployment, workload and dependency visibility.
- **Why they care:** enforcement and observability must integrate with the runtime they already operate.

### Cluster B — Security & Governance

Owns enterprise risk and policy. Their primary focus is **policy, risk, compliance, and enforcement outcomes**.

#### Security Leader (CISO / CSO / Security Operations)
- **Owns:** enterprise security posture and risk.
- **Goals:** prevent data exfiltration via AI, enforce governance across agents and endpoints, detect unsafe behavior.
- **Needs:** endpoint/user/asset-level policy, audit trails, exposure control, credential enforcement.
- **Why they care:** AI + MCP creates a new attack surface with no existing control plane.

#### AI Governance / GRC Lead
- **Owns:** policy for AI usage, acceptable use, and compliance.
- **Goals:** define rules for how agents and MCP systems may operate, ensure compliance, validate policy impact before rollout.
- **Needs:** a policy-definition framework, pre/post-mortem simulation, compliance reporting.
- **Why they care:** they own the policy model and the compliance mandate, not the infrastructure that runs it.

#### Identity & Access Management (IAM)
- **Owns:** user and service identity, authentication, authorization.
- **Goals:** control who and what may access MCP endpoints, govern credential and API-key usage.
- **Needs:** identity-bound policy, credential enforcement, integration with existing identity systems.
- **Why they care:** MCP governance is fundamentally an access-control problem.

### Cluster C — Architecture & AI Platform

Owns how MCP fits into enterprise architecture and how AI agents operate within it. Their primary focus is **control-plane design, integration, and system behavior**.

#### Enterprise Architect (incl. AI / Cloud Architect)
- **Owns:** system architecture across cloud, AI, and enterprise systems.
- **Goals:** integrate MCP into enterprise architecture, ensure scalable consistent control, define the AI control-plane strategy.
- **Needs:** a unified control plane, consistent enforcement across cloud and endpoint, integration with existing networking and security stacks.
- **Why they care:** Bright Guard becomes a foundational layer of their AI architecture.

#### Application Security / DevSecOps
- **Owns:** application, API, and pipeline security.
- **Goals:** control how MCP endpoints are exposed and consumed, enforce runtime policy, prevent credential and API misuse.
- **Needs:** capability discovery, endpoint behavior policy, detection of unauthorized API-key use.
- **Why they care:** MCP endpoints behave like dynamic APIs and fall directly into AppSec scope.

#### AI / Agent Platform Owner *(emerging)*
- **Owns:** internal AI agents, agent workflows, and their MCP usage.
- **Goals:** manage how agents interact with MCP systems, keep agents within defined boundaries.
- **Needs:** agent-level governance, agent-to-MCP interaction visibility, data-flow control.
- **Why they care:** this is a new role emerging with AI adoption, and it has no existing tooling.

## Where We're Headed

Bright Guard's near-term focus is the foundation: **DNS-based discovery, capability enumeration, and an audit trail** that gives the network and security teams a shared view of MCP in their environment. From that foundation we layer policy, enforcement, and simulation — building toward a single unified AI control plane that spans every environment an enterprise operates.
