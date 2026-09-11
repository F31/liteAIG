# finops-pricing-chargeback Specification (Delta)

## ADDED Requirements

### Requirement: External procurement cost dimension
Pricing and aggregation SHALL distinguish external agent procurement cost from internal provider/tool/agent cost. Chargeback SHALL report external procurement separately per relationship/vendor while the task total continues to include it.

#### Scenario: External cost reported separately
- **WHEN** an operator views FinOps for a task that used an external agent
- **THEN** external procurement cost is visible as a distinct dimension
- **AND** the task total still includes it
