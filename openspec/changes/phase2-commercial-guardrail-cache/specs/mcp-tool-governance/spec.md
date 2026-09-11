# mcp-tool-governance Specification

## ADDED Requirements

### Requirement: MCP 2026-07-28 tool adapter
The system SHALL provide an MCP 2026-07-28 adapter using stateless Streamable HTTP with `Mcp-Method`/`Mcp-Name` headers and `server/discover`, invoked through the Tool connector contract. The adapter SHALL only connect and invoke; authorization, guardrails, budgets, and routing SHALL execute in the pipeline.

#### Scenario: Discover and call an MCP tool
- **WHEN** an admitted request targets a configured MCP tool
- **THEN** the MCP server is discovered and the tool invoked through the connector contract
- **AND** the invocation passes through pipeline governance before reaching the server

### Requirement: Tool registry and catalog
The system SHALL maintain a tenant-scoped Tool Registry with MCP Servers and a Tool Catalog compiled into the Tenant Runtime Snapshot. Tool declarations SHALL carry id, name, schema, capability tags, data classification, and endpoint.

#### Scenario: Tool catalog is snapshot-driven
- **WHEN** a request selects a tool
- **THEN** the tool's identity, schema, and policy come from the captured snapshot
- **AND** no per-request repository access is required

### Requirement: Tool ACL enforcement
Tool ACL SHALL be enforced at the `TOOL_REQUEST` checkpoint in the pipeline and MUST NOT be bypassable by Prompt Injection in the tool arguments. Unauthorized tool calls SHALL be rejected before any upstream invocation.

#### Scenario: Unauthorized tool call rejected
- **WHEN** a tool call violates the Tool ACL
- **THEN** the call is rejected at `TOOL_REQUEST`
- **AND** no MCP server is invoked

#### Scenario: Prompt injection cannot bypass ACL
- **WHEN** tool arguments contain an injected directive attempting to invoke a denied tool
- **THEN** the ACL decision is based on the declared tool identity, not the argument content
- **AND** the denied invocation is blocked

### Requirement: Schema and DLP on tool arguments
Tool arguments SHALL be validated against the declared JSON Schema, and sensitive argument content SHALL be checked by configured DLP rules before invocation.

#### Scenario: Schema-invalid tool call rejected
- **WHEN** tool arguments do not match the declared schema
- **THEN** the call is rejected with a validation outcome
- **AND** no upstream invocation occurs

### Requirement: Tool result provenance and re-guardrail
Tool results SHALL carry Content Provenance of type `tool_result` marked untrusted and SHALL re-enter the Guardrail pipeline before being used as model context or returned.

#### Scenario: Tool result re-guardrailed
- **WHEN** a tool result is returned by an MCP server
- **THEN** it is treated as untrusted `tool_result` provenance
- **AND** it is re-evaluated by the applicable Guardrail before use

### Requirement: Tool call events and attribution
Every tool invocation SHALL emit a Tool Call Event attributed to Tenant, Project, request, session, agent (if any), user (if trusted), and tool identity, recording `tool_call_count` and cost. Events SHALL support session/task linkage for later agentic observability.

#### Scenario: Tool call attributed
- **WHEN** an agent triggers a tool call within a session
- **THEN** the Tool Call Event records the tenant, project, request, session, agent, and tool
- **AND** the event links to the task and request identifiers
