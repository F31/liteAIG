# Graph Report - .  (2026-08-27)

## Corpus Check
- Corpus is ~20,822 words - fits in a single context window. You may not need a graph.

## Summary
- 24 nodes · 21 edges · 6 communities (4 shown, 2 thin omitted)
- Extraction: 95% EXTRACTED · 5% INFERRED · 0% AMBIGUOUS · INFERRED: 1 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Runtime and Configuration|Runtime and Configuration]]
- [[_COMMUNITY_Governed Interaction Pipeline|Governed Interaction Pipeline]]
- [[_COMMUNITY_Routing and Resilience|Routing and Resilience]]
- [[_COMMUNITY_Phase 0 User Outcome|Phase 0 User Outcome]]
- [[_COMMUNITY_Identity and Isolation|Identity and Isolation]]
- [[_COMMUNITY_Product Positioning|Product Positioning]]

## God Nodes (most connected - your core abstractions)
1. `Fixed Seven-stage Pipeline` - 4 edges
2. `Golden Scenario A Multi-model Unified Egress` - 4 edges
3. `Stable Kernel` - 3 edges
4. `Tenant Runtime Snapshot` - 3 edges
5. `Unified Interaction Model` - 3 edges
6. `Explainable Smart Routing` - 3 edges
7. `Modular Monolith` - 2 edges
8. `Runtime Registry` - 2 edges
9. `Logical Model` - 2 edges
10. `Usage Ledger` - 2 edges

## Surprising Connections (you probably didn't know these)
- `Stable Kernel` --conceptually_related_to--> `Fixed Seven-stage Pipeline`  [EXTRACTED]
  docs/LiteAIG_AI_Gateway_V7.2.md → docs/LiteAIG_AI_Gateway_V7.2.md  _Bridges community 0 → community 1_
- `Golden Scenario A Multi-model Unified Egress` --references--> `Logical Model`  [EXTRACTED]
  docs/LiteAIG_AI_Gateway_V7.2.md → docs/LiteAIG_AI_Gateway_V7.2.md  _Bridges community 2 → community 3_
- `Golden Scenario A Multi-model Unified Egress` --conceptually_related_to--> `Usage Ledger`  [EXTRACTED]
  docs/LiteAIG_AI_Gateway_V7.2.md → docs/LiteAIG_AI_Gateway_V7.2.md  _Bridges community 1 → community 3_

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Phase 0 Governed Multi-model Vertical Slice** — docs_liteaig_ai_gateway_v7_2_phase_0, docs_liteaig_ai_gateway_v7_2_scenario_a, docs_liteaig_ai_gateway_v7_2_setup_wizard, docs_liteaig_ai_gateway_v7_2_logical_model, docs_liteaig_ai_gateway_v7_2_request_explorer [EXTRACTED 1.00]

## Communities (6 total, 2 thin omitted)

### Community 0 - "Runtime and Configuration"
Cohesion: 0.29
Nodes (7): Versioned Configuration Lifecycle, Do Not Build Boundary, Stable Kernel, Modular Monolith, Runtime Registry, Tenant Runtime Snapshot, Virtual API Key

### Community 1 - "Governed Interaction Pipeline"
Cohesion: 0.33
Nodes (6): Agentic Governance and Audit, Connector Contract, Ingress Protocol Adapter, Fixed Seven-stage Pipeline, Unified Interaction Model, Usage Ledger

### Community 2 - "Routing and Resilience"
Cohesion: 0.50
Nodes (4): Explainable Smart Routing, Logical Model, Request Explorer, Resilience Engine

### Community 3 - "Phase 0 User Outcome"
Cohesion: 0.67
Nodes (3): Phase 0 P0-Core, Golden Scenario A Multi-model Unified Egress, Five-minute Setup Wizard

## Knowledge Gaps
- **13 isolated node(s):** `LiteAIG v7.2`, `Enterprise AI Governance and Traffic Control Plane`, `Ingress Protocol Adapter`, `Connector Contract`, `Canonical Identity and Principal` (+8 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **2 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Fixed Seven-stage Pipeline` connect `Governed Interaction Pipeline` to `Runtime and Configuration`?**
  _High betweenness centrality (0.470) - this node is a cross-community bridge._
- **Why does `Golden Scenario A Multi-model Unified Egress` connect `Phase 0 User Outcome` to `Governed Interaction Pipeline`, `Routing and Resilience`?**
  _High betweenness centrality (0.344) - this node is a cross-community bridge._
- **Why does `Stable Kernel` connect `Runtime and Configuration` to `Governed Interaction Pipeline`?**
  _High betweenness centrality (0.340) - this node is a cross-community bridge._
- **What connects `LiteAIG v7.2`, `Enterprise AI Governance and Traffic Control Plane`, `Ingress Protocol Adapter` to the rest of the system?**
  _14 weakly-connected nodes found - possible documentation gaps or missing edges._