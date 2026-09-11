package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/F31/liteAIG/internal/identity/apikey"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/tenancy"
)

type Service struct {
	repository      Repository
	keys            apikey.Repository
	registry        *runtime.ActiveRegistry
	ids             contracts.IDGenerator
	clock           contracts.Clock
	guardrailLoader GuardrailLoader
	lkgPublisher    LKGPublisher
	bundlePublisher BundlePublisher
	system          SystemConfigRepository
	events          contracts.EventSink
}

// SystemConfigRepository exposes the persisted singleton SystemConfig. It is
// optional: compositions without a system store compile tenants with empty
// system defaults (the historical behavior).
type SystemConfigRepository interface {
	GetSystemConfig(context.Context) (SystemConfig, error)
	SetSystemConfig(context.Context, SystemConfig, string) (SystemConfig, error)
}

// SetSystemRepository wires the system defaults store. Tenant publish,
// rollback, and reconcile then compile with System→Tenant defaults applied.
func (s *Service) SetSystemRepository(repo SystemConfigRepository) {
	s.system = repo
}

// SetEventSink wires the optional redacted domain-event sink used for
// system-level configuration notifications.
func (s *Service) SetEventSink(events contracts.EventSink) {
	s.events = events
}

// systemDefaults returns the persisted system config, or the empty system
// config when no system store is wired. A read failure aborts the tenant
// compile: silently falling back to no system defaults would bypass policy
// enforcement.
func (s *Service) systemDefaults(ctx context.Context) (SystemConfig, error) {
	if s.system == nil {
		return SystemConfig{}, nil
	}
	return s.system.GetSystemConfig(ctx)
}

// SystemConfig returns the persisted singleton system config. An unwired
// system store yields the empty system config (no defaults).
func (s *Service) SystemConfig(ctx context.Context) (SystemConfig, error) {
	if s.system == nil {
		return SystemConfig{}, nil
	}
	return s.system.GetSystemConfig(ctx)
}

// SetSystemConfig upserts the singleton system config. The write is only
// valid through the system_admin-gated Admin API surface; the Service exposes
// it without authorization so the presentation layer owns that decision.
//
// After persisting, every tenant's latest published version is re-compiled
// and re-activated so the new System-to-Tenant defaults take effect immediately
// (not only on the next tenant publish). Reconcile is best-effort: a tenant
// that no longer validates under the new defaults keeps its previous active
// snapshot and is reported via ReconcileAll's error; the persisted system
// config still wins for future publishes.
func (s *Service) SetSystemConfig(ctx context.Context, document SystemConfig, actorID string) (SystemConfig, error) {
	if s.system == nil {
		return SystemConfig{}, fmt.Errorf("system config repository not wired")
	}
	switch document.TenantDefaults.ResidencyEnforcement {
	case "", "advisory", "strict":
	default:
		return SystemConfig{}, fmt.Errorf("invalid residency_enforcement %q: want advisory or strict", document.TenantDefaults.ResidencyEnforcement)
	}
	if _, err := s.system.SetSystemConfig(ctx, document, actorID); err != nil {
		return SystemConfig{}, err
	}
	s.emitSystemConfigUpdate(ctx, actorID)
	_, _ = s.ReconcileAll(ctx)
	return document, nil
}

func (s *Service) emitSystemConfigUpdate(ctx context.Context, actorID string) {
	if s.events == nil {
		return
	}
	id, err := s.ids.New()
	if err != nil {
		return
	}
	_ = s.events.Emit(ctx, contracts.DomainEvent{
		ID:         id,
		Kind:       "system_config.update",
		OccurredAt: s.clock.Now(),
		Attributes: map[string]string{"action": "update", "severity": "medium", "actor_id": actorID},
	})
}

func NewService(repository Repository, keys apikey.Repository, registry *runtime.ActiveRegistry, ids contracts.IDGenerator, clock contracts.Clock) *Service {
	return &Service{repository: repository, keys: keys, registry: registry, ids: ids, clock: clock}
}

// GuardrailLoader returns the tenant's active fast-published guardrail policy,
// if any. ReconcileAll applies it onto the compiled snapshot so fast publishes
// survive a process restart.
type GuardrailLoader func(context.Context, string) (runtime.GuardrailPolicy, bool)

// SetGuardrailLoader wires the fast-published guardrail policy source used by
// ReconcileAll at startup.
func (s *Service) SetGuardrailLoader(loader GuardrailLoader) {
	s.guardrailLoader = loader
}

// LKGPublisher persists a compiled snapshot as the tenant's Last Known Good
// bundle. It is invoked best-effort after every successful activation; a
// persistence failure must not fail the publish.
type LKGPublisher func(context.Context, *runtime.TenantRuntimeSnapshot) error

// SetLKGPublisher wires the Last Known Good persistence hook.
func (s *Service) SetLKGPublisher(publisher LKGPublisher) {
	s.lkgPublisher = publisher
}

// BundlePublisher publishes the immutable, Control-Plane-signed RuntimeBundle
// for a tenant after every successful activation. It is invoked after the
// registry activation so Data Planes can pull the exact signed bundle that was
// activated. A failure must not fail the publish (the in-memory registry is
// already the source of truth); it is best-effort delivery.
type BundlePublisher func(context.Context, *runtime.TenantRuntimeSnapshot) error

// SetBundlePublisher wires the signed-bundle delivery hook.
func (s *Service) SetBundlePublisher(publisher BundlePublisher) {
	s.bundlePublisher = publisher
}

// activate atomically activates a compiled snapshot and, best-effort, persists
// it as the tenant's Last Known Good bundle and publishes the signed bundle.
func (s *Service) activate(ctx context.Context, snapshot *runtime.TenantRuntimeSnapshot) {
	s.registry.ActivateTenant(snapshot.TenantRef, snapshot)
	if s.lkgPublisher != nil {
		_ = s.lkgPublisher(ctx, snapshot)
	}
	if s.bundlePublisher != nil {
		_ = s.bundlePublisher(ctx, snapshot)
	}
}

func (s *Service) Publish(ctx context.Context, scope tenancy.TenantScope, draftID string, expectedRevision int64, actorID string) (*Version, []Diagnostic, error) {
	draft, err := s.repository.GetDraft(ctx, scope, draftID)
	if err != nil {
		return nil, nil, err
	}
	system, err := s.systemDefaults(ctx)
	if err != nil {
		return nil, nil, err
	}
	diagnostics := ValidateForTenantWithSystemDefaults(system, draft.Config, scope.TenantID)
	if HasErrors(diagnostics) {
		return nil, diagnostics, fmt.Errorf("config validation failed")
	}
	now := s.clock.Now()
	keys, err := s.keys.ListActive(ctx, scope)
	if err != nil {
		return nil, diagnostics, err
	}
	if _, err := CompileWithSystemDefaultsAndAPIKeys(system, draft.Config, 0, now, keys); err != nil {
		return nil, diagnostics, err
	}
	versionID, err := s.ids.New()
	if err != nil {
		return nil, diagnostics, err
	}
	auditID, err := s.ids.New()
	if err != nil {
		return nil, diagnostics, err
	}
	version, err := s.repository.PublishDraft(ctx, scope, PublishRecord{DraftID: draftID, VersionID: versionID, AuditID: auditID, ActorID: actorID, ExpectedRevision: expectedRevision, PublishedAt: now})
	if err != nil {
		return nil, diagnostics, err
	}
	snapshot, err := CompileWithSystemDefaultsAndAPIKeys(system, version.Config, version.Version, version.PublishedAt, keys)
	if err != nil {
		return nil, diagnostics, err
	}
	s.activate(ctx, snapshot)
	return version, diagnostics, nil
}

func (s *Service) Rollback(ctx context.Context, scope tenancy.TenantScope, sourceVersion int64, actorID string) (*Version, error) {
	now := s.clock.Now()
	source, err := s.repository.GetVersion(ctx, scope, sourceVersion)
	if err != nil {
		return nil, err
	}
	system, err := s.systemDefaults(ctx)
	if err != nil {
		return nil, err
	}
	if HasErrors(ValidateForTenantWithSystemDefaults(system, source.Config, scope.TenantID)) {
		return nil, fmt.Errorf("config validation failed")
	}
	keys, err := s.keys.ListActive(ctx, scope)
	if err != nil {
		return nil, err
	}
	if _, err := CompileWithSystemDefaultsAndAPIKeys(system, source.Config, 0, now, keys); err != nil {
		return nil, err
	}
	versionID, err := s.ids.New()
	if err != nil {
		return nil, err
	}
	auditID, err := s.ids.New()
	if err != nil {
		return nil, err
	}
	version, err := s.repository.RollbackVersion(ctx, scope, RollbackRecord{SourceVersion: sourceVersion, VersionID: versionID, AuditID: auditID, ActorID: actorID, PublishedAt: now})
	if err != nil {
		return nil, err
	}
	snapshot, err := CompileWithSystemDefaultsAndAPIKeys(system, version.Config, version.Version, version.PublishedAt, keys)
	if err != nil {
		return nil, err
	}
	s.activate(ctx, snapshot)
	return version, nil
}

// Reconcile re-compiles and atomically re-activates a published version without
// creating new version history, then records an audit event.
func (s *Service) Reconcile(ctx context.Context, scope tenancy.TenantScope, sourceVersion int64, actorID string) (*Version, error) {
	source, err := s.repository.GetVersion(ctx, scope, sourceVersion)
	if err != nil {
		return nil, err
	}
	system, err := s.systemDefaults(ctx)
	if err != nil {
		return nil, err
	}
	if HasErrors(ValidateForTenantWithSystemDefaults(system, source.Config, scope.TenantID)) {
		return nil, fmt.Errorf("config validation failed")
	}
	keys, err := s.keys.ListActive(ctx, scope)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	snapshot, err := CompileWithSystemDefaultsAndAPIKeys(system, source.Config, source.Version, now, keys)
	if err != nil {
		return nil, err
	}
	s.activate(ctx, snapshot)
	auditID, err := s.ids.New()
	if err != nil {
		return nil, err
	}
	if err := s.repository.Audit(ctx, scope, AuditRecord{ID: auditID, ActorID: actorID, Action: "config.reconcile", ResourceID: source.ID, Version: source.Version, OccurredAt: now}); err != nil {
		return nil, err
	}
	return source, nil
}

// ReconcileAll re-compiles and re-activates the latest published version of
// every tenant at process startup, restoring in-memory runtime snapshots that
// do not survive a restart. A tenant whose config no longer validates is
// skipped (and reported) so a single bad tenant cannot prevent the process
// from booting the others.
func (s *Service) ReconcileAll(ctx context.Context) (int, error) {
	versions, err := s.repository.ListLatestVersions(ctx)
	if err != nil {
		return 0, err
	}
	var failures []string
	activated := 0
	for _, version := range versions {
		scope := tenancy.TenantScope{TenantID: version.TenantID}
		var overlay *runtime.GuardrailPolicy
		if s.guardrailLoader != nil {
			if policy, ok := s.guardrailLoader(ctx, scope.TenantID); ok {
				overlay = &policy
			}
		}
		if err := s.compileAndActivate(ctx, scope, version, overlay); err != nil {
			failures = append(failures, "tenant "+scope.TenantID+": "+err.Error())
			continue
		}
		activated++
	}
	if len(failures) > 0 {
		return activated, fmt.Errorf("reconcile: %s", strings.Join(failures, "; "))
	}
	return activated, nil
}

// compileAndActivate validates the version's document (with an optional
// fast-published guardrail overlay), compiles it, and atomically activates the
// tenant snapshot.
func (s *Service) compileAndActivate(ctx context.Context, scope tenancy.TenantScope, version Version, overlay *runtime.GuardrailPolicy) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	document := version.Config
	if overlay != nil {
		rules := make([]GuardrailRule, 0, len(overlay.Rules))
		for _, item := range overlay.Rules {
			rules = append(rules, GuardrailRule{ID: item.ID, Kind: item.Kind, Pattern: item.Pattern, Action: item.Action, Replacement: item.Replacement})
		}
		document.Guardrail = GuardrailPolicy{Mode: overlay.Mode, Rules: rules}
	}
	system, err := s.systemDefaults(ctx)
	if err != nil {
		return fmt.Errorf("load system config: %w", err)
	}
	if HasErrors(ValidateForTenantWithSystemDefaults(system, document, scope.TenantID)) {
		return fmt.Errorf("config validation failed")
	}
	keys, err := s.keys.ListActive(ctx, scope)
	if err != nil {
		return fmt.Errorf("list api keys: %w", err)
	}
	snapshot, err := CompileWithSystemDefaultsAndAPIKeys(system, document, version.Version, version.PublishedAt, keys)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	s.activate(ctx, snapshot)
	return nil
}

// OverlayGuardrail recompiles the tenant's latest published version with the
// supplied fast-published guardrail policy and atomically activates the result.
// Fast publishes do not create config version history: the policy lives in the
// guardrail policy store and ReconcileAll re-applies it at startup.
func (s *Service) OverlayGuardrail(ctx context.Context, scope tenancy.TenantScope, policy runtime.GuardrailPolicy) error {
	versions, err := s.repository.ListVersions(ctx, scope)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return ErrNoPublishedVersion
	}
	return s.compileAndActivate(ctx, scope, versions[len(versions)-1], &policy)
}

// Draft returns a draft for the console editor.
func (s *Service) Draft(ctx context.Context, scope tenancy.TenantScope, id string) (*Draft, error) {
	return s.repository.GetDraft(ctx, scope, id)
}

// CreateDraft starts a new editable draft based on the latest published version.
func (s *Service) CreateDraft(ctx context.Context, scope tenancy.TenantScope, actorID string) (*Draft, error) {
	versions, err := s.repository.ListVersions(ctx, scope)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, ErrNoPublishedVersion
	}
	latest := versions[len(versions)-1]
	draftID, err := s.ids.New()
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	draft := &Draft{
		ID: draftID, TenantID: scope.TenantID, BaseVersion: latest.Version, Revision: 1,
		Status: "editing", Config: latest.Config, CreatedBy: actorID, UpdatedBy: actorID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repository.CreateDraft(ctx, scope, *draft); err != nil {
		return nil, err
	}
	return draft, nil
}

// Drafts lists the tenant's drafts for the console.
func (s *Service) Drafts(ctx context.Context, scope tenancy.TenantScope) ([]Draft, error) {
	return s.repository.ListDrafts(ctx, scope)
}

// UpdateDraft persists a draft revision with optimistic concurrency.
func (s *Service) UpdateDraft(ctx context.Context, scope tenancy.TenantScope, id string, revision int64, document TenantConfig, actorID string) (*Draft, error) {
	return s.repository.UpdateDraft(ctx, scope, id, revision, document, actorID)
}

// Diff returns the semantic diff between a draft and the latest active version.
func (s *Service) Diff(ctx context.Context, scope tenancy.TenantScope, id string) ([]DiffEntry, error) {
	draft, err := s.repository.GetDraft(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	versions, err := s.repository.ListVersions(ctx, scope)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return SemanticDiff(TenantConfig{}, draft.Config)
	}
	latest := versions[len(versions)-1]
	before := latest.Config
	if latest.Version == draft.BaseVersion {
		return SemanticDiff(before, draft.Config)
	}
	// Base differs from latest; diff against latest to surface the rebase delta.
	return SemanticDiff(before, draft.Config)
}

// Versions lists published configuration versions for the console.
func (s *Service) Versions(ctx context.Context, scope tenancy.TenantScope) ([]Version, error) {
	return s.repository.ListVersions(ctx, scope)
}

// Rebase re-bases a draft onto the latest active version. Returns a conflict
// result when the draft cannot be rebased automatically.
func (s *Service) Rebase(ctx context.Context, scope tenancy.TenantScope, draftID, actorID string) (RebaseOutcome, error) {
	draft, err := s.repository.GetDraft(ctx, scope, draftID)
	if err != nil {
		return RebaseOutcome{}, err
	}
	versions, err := s.repository.ListVersions(ctx, scope)
	if err != nil {
		return RebaseOutcome{}, err
	}
	if len(versions) == 0 {
		return RebaseOutcome{Rebased: true, Revision: draft.Revision}, nil
	}
	latest := versions[len(versions)-1]
	if latest.Version == draft.BaseVersion {
		return RebaseOutcome{Rebased: true, Revision: draft.Revision}, nil
	}
	// The draft is based on an older version; persist it onto the latest base
	// only if it still validates against the current tenant config.
	system, err := s.systemDefaults(ctx)
	if err != nil {
		return RebaseOutcome{Conflict: true, Revision: draft.Revision}, nil
	}
	if HasErrors(ValidateForTenantWithSystemDefaults(system, draft.Config, scope.TenantID)) {
		return RebaseOutcome{Conflict: true, Revision: draft.Revision}, nil
	}
	updated, err := s.repository.UpdateDraft(ctx, scope, draftID, draft.Revision, draft.Config, actorID)
	if err != nil {
		return RebaseOutcome{Conflict: true, Revision: draft.Revision}, nil
	}
	return RebaseOutcome{Rebased: true, Revision: updated.Revision}, nil
}

// RebaseOutcome is the console-facing rebase result.
type RebaseOutcome struct {
	Rebased  bool
	Conflict bool
	Revision int64
}
