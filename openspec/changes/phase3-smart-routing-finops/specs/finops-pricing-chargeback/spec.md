# finops-pricing-chargeback Specification

## ADDED Requirements

### Requirement: Versioned pricing and two amount sets
The system SHALL support Pricing Versions containing rate lists keyed by model/provider/currency, and SHALL derive two amounts from each Usage fact: Provider Cost (from the ledger) and Customer Charge (from the applicable Price Version). The applicable Price Version SHALL be recorded with the fact.

#### Scenario: Charge differs from provider cost
- **WHEN** a usage fact is priced under a Price Version whose customer rate differs from the provider rate
- **THEN** the ledger records both provider_cost and customer_charge
- **AND** the pricing version used is attributable

#### Scenario: Pricing golden
- **WHEN** a known usage fact is priced under a fixed Price Version
- **THEN** the resulting customer_charge matches the golden expected value
- **AND** the benchmark asserts the version is used

### Requirement: Multi-currency conversion
Usage facts SHALL record a settlement currency, and the system SHALL convert provider and customer amounts into the tenant settlement currency using a configurable, validated rate source. The original currency and amounts SHALL be preserved.

#### Scenario: Cross-currency chargeback
- **WHEN** a provider charges in USD and the tenant settles in EUR
- **THEN** chargeback reflects the EUR amount using the configured rate
- **AND** the USD amount remains recorded

### Requirement: Chargeback and showback
Chargeback/Showback SHALL be derived from attribution-qualified usage only (verified, key_bound, or delegated trust for User/OrgUnit/Agent chargeback), never from untrusted client labels. Reports SHALL support Tenant, Project, OrgUnit, User, Application, Agent, and Task scopes.

#### Scenario: Untrusted label excluded from chargeback
- **WHEN** a request carries an untrusted client user label
- **THEN** no User/OrgUnit chargeback is attributed to it
- **AND** Tenant/Project/Key chargeback still applies
