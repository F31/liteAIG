package sqlrepo

import (
	"database/sql"
	"time"

	"github.com/F31/liteAIG/internal/kernel/contracts"
)

// Deps carries the shared dependencies several adapters need.
type Deps struct {
	IDs   contracts.IDGenerator
	Clock contracts.Clock // optional; nil keeps the real system clock for time-sensitive adapters
}

// Store is the single construction point for the SQL repository set. The
// composition root holds one Store and never calls individual adapters.
type Store struct {
	db *sql.DB

	Tenancy          *TenancyRepository
	Bootstrap        *BootstrapRepository
	Config           *ConfigRepository
	APIKeys          *APIKeyRepository
	Accounting       *AccountingRepository
	ResourceCatalog  *ResourceCatalogRepository
	SecurityEvents   *SecurityEventStore
	GuardrailPolicy  *GuardrailPolicyStore
	ToolCalls        *ToolCallStore
	LocalCredentials *LocalCredentialStore
	Pepper           *PepperStore
	Audit            *AuditRepository
	AuditRetention   *AuditRetentionRepository
	BudgetAudit      *BudgetAuditRepository
	Organization     *OrganizationRepository
	Alerts           *AlertStore
	AlertAudit       *AlertAuditRecorder
	SecretVault      *SecretVault
	Delegations      *DelegationStore
	Approvals        *ApprovalStore
	Federation       *FederationStore
	Coordination     *CoordinationLeaseStore
	Evidence         *EvidenceRepository
	A2ATask          *A2ATaskStore
	A2APushOutbox    *A2APushOutboxStore
	BatchMappings    *BatchMappingStore
	SystemConfig     *SystemConfigRepository
	EventOutbox      *DomainEventOutbox
}

// Open constructs every repository against one database handle.
func Open(db *sql.DB, deps Deps) *Store {
	if db == nil {
		panic("sqlrepo: Open requires a non-nil *sql.DB")
	}
	return &Store{
		db:               db,
		Tenancy:          NewTenancyRepository(db),
		Bootstrap:        NewBootstrapRepository(db),
		Config:           NewConfigRepository(db),
		APIKeys:          NewAPIKeyRepository(db),
		Accounting:       NewAccountingRepository(db),
		ResourceCatalog:  NewResourceCatalogRepository(db),
		SecurityEvents:   NewSecurityEventStore(db),
		GuardrailPolicy:  NewGuardrailPolicyStore(db),
		ToolCalls:        NewToolCallStore(db),
		LocalCredentials: NewLocalCredentialStore(db),
		Pepper:           NewPepperStore(db),
		Audit:            NewAuditRepository(db),
		AuditRetention:   NewAuditRetentionRepository(db),
		BudgetAudit:      NewBudgetAuditRepository(db),
		Organization:     NewOrganizationRepository(db),
		Alerts:           NewAlertStore(db),
		AlertAudit:       NewAlertAuditRecorder(db, deps.IDs),
		SecretVault:      NewSecretVault(db),
		Delegations:      NewDelegationStore(db),
		Approvals:        NewApprovalStore(db),
		Federation:       NewFederationStore(db),
		Coordination:     NewCoordinationLeaseStore(db, clockFor(deps.Clock)),
		Evidence:         NewEvidenceRepository(db),
		A2ATask:          NewA2ATaskStore(db),
		A2APushOutbox:    NewA2APushOutboxStore(db),
		BatchMappings:    NewBatchMappingStore(db),
		SystemConfig:     NewSystemConfigRepository(db),
		EventOutbox:      NewDomainEventOutbox(db),
	}
}

func clockFor(clock contracts.Clock) func() time.Time {
	if clock == nil {
		return time.Now
	}
	return clock.Now
}

// SecretResolver wires the secret vault with the key-material decryptor.
func (s *Store) SecretResolver(open func([]byte) ([]byte, error)) *SecretResolver {
	return NewSecretResolver(s.SecretVault, open)
}

// DB exposes the underlying handle for connection-lifecycle duties only
// (migrations, health checks); domain reads must go through the repositories.
func (s *Store) DB() *sql.DB { return s.db }
