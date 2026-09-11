## ADDED Requirements

### Requirement: User and OrgUnit base model
The system SHALL maintain Users, OrgUnits with a tree hierarchy, and effective-dated User↔OrgUnit assignments. Personnel moves MUST NOT rewrite historical assignments; Usage Events SHALL snapshot the principal OrgUnit, path, and cost center at call time.

#### Scenario: Effective-dated reassignment
- **WHEN** a user is reassigned to another OrgUnit with an end date on the prior assignment
- **THEN** historical usage remains attributed to the prior OrgUnit
- **AND** new usage is attributed to the new OrgUnit

### Requirement: Trusted attribution only
User and OrgUnit attribution SHALL derive only from trusted identity sources. Client-supplied user labels SHALL NOT affect User/OrgUnit attribution, budget, or chargeback.

#### Scenario: Untrusted label ignored
- **WHEN** a request carries an untrusted client user label
- **THEN** User/OrgUnit budget and attribution are not computed from it

### Requirement: User and OrgUnit budget dimensions
When trusted identity attribution exists, Phase 1 SHALL support User and OrgUnit budget dimensions in addition to Tenant, Project, and Key dimensions. Without trusted identity, User/OrgUnit budgets MUST NOT be enforced.

#### Scenario: OrgUnit budget enforcement
- **WHEN** a trusted user with a resolved OrgUnit exceeds an OrgUnit budget
- **THEN** the budget policy applies
- **AND** requests without trusted identity are not subject to User/OrgUnit enforcement

### Requirement: Attribution in Request and Usage facts
Every request and usage fact SHALL carry the resolved user, OrgUnit, org path, cost center, and attribution trust level as a snapshot.

#### Scenario: Cross-analysis accuracy
- **WHEN** a department queries usage by OrgUnit and Project
- **THEN** results aggregate consistently with the Usage Ledger
- **AND** the attribution trust level is displayed
