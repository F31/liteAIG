# basic-routing-resilience Specification (Delta)

## ADDED Requirements

### Requirement: Hard constraints then soft score selection
Resolution SHALL filter to hard-constraint-eligible deployments first and SHALL then order the eligible set by the compiled soft score (latency, cost, load, cache-affinity) when a soft scoring policy is configured. When no soft scoring weights are configured, the existing priority/weighted/round-robin ordering SHALL apply unchanged. Every route decision SHALL carry candidate evidence including exclusions and per-component scores.

#### Scenario: Soft score orders eligible deployments
- **WHEN** a route policy configures soft scoring weights
- **THEN** eligible deployments are ordered by their normalized soft score
- **AND** the selected and fallback deployments reflect that order

#### Scenario: Defaults preserve priority behavior
- **WHEN** a route policy has no soft scoring weights
- **THEN** selection falls back to the prior priority/weighted/round-robin behavior
- **AND** no behavioral regression occurs

#### Scenario: Fallback still explainable
- **WHEN** the first selected deployment fails during execution
- **THEN** fallback proceeds in soft-score order
- **AND** Request Explorer explains each attempt with its score evidence
