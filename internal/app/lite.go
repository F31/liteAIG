package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/F31/liteAIG/internal/controlplane/backend"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/F31/liteAIG/internal/access/auth"
	"github.com/F31/liteAIG/internal/access/protocol/a2a"
	toolmcp "github.com/F31/liteAIG/internal/connectors/tool/mcp"
	"github.com/F31/liteAIG/internal/controlplane/adminapi"
	"github.com/F31/liteAIG/internal/controlplane/approval"
	"github.com/F31/liteAIG/internal/controlplane/config"
	"github.com/F31/liteAIG/internal/controlplane/setup"
	"github.com/F31/liteAIG/internal/federation"
	"github.com/F31/liteAIG/internal/finops/accounting"
	"github.com/F31/liteAIG/internal/gateway/admission"
	gatewayapproval "github.com/F31/liteAIG/internal/gateway/approval"
	"github.com/F31/liteAIG/internal/gateway/playground"
	gatewayserver "github.com/F31/liteAIG/internal/gateway/server"
	"github.com/F31/liteAIG/internal/guardrail"
	"github.com/F31/liteAIG/internal/guardrail/benchmark"
	"github.com/F31/liteAIG/internal/identity"
	"github.com/F31/liteAIG/internal/identity/apikey"
	identityoidc "github.com/F31/liteAIG/internal/identity/oidc"
	"github.com/F31/liteAIG/internal/identity/password"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/observability"
	"github.com/F31/liteAIG/internal/observability/alert"
	"github.com/F31/liteAIG/internal/observability/audit"
	"github.com/F31/liteAIG/internal/platform/bundle"
	"github.com/F31/liteAIG/internal/platform/egress"
	"github.com/F31/liteAIG/internal/platform/id"
	"github.com/F31/liteAIG/internal/platform/lkg"
	"github.com/F31/liteAIG/internal/platform/mail"
	"github.com/F31/liteAIG/internal/platform/secrets"
	"github.com/F31/liteAIG/internal/platform/storage/postgres"
	"github.com/F31/liteAIG/internal/platform/storage/spool"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/platform/webkit"
	"github.com/F31/liteAIG/internal/policy/rate"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
	"github.com/F31/liteAIG/web/console"
	"go.opentelemetry.io/otel/propagation"
)

// LiteOptions configures the runnable Lite management plane.
type LiteOptions struct {
	DSN         string
	AdminAddr   string
	GatewayAddr string
	BaseURL     string       // gateway base URL used in SDK examples
	Lifecycle   *Lifecycle   // shared drain lifecycle (created by Lite when nil)
	DrainConfig DrainConfig  // used when Lifecycle is nil
	OIDC        *OIDCOptions // optional SSO; nil or empty Issuer keeps local login only
	Analytics   *AnalyticsOptions
	WebhookURL  string       // opt-in metadata notifications via the Lite egress policy
	Mail        *MailOptions // optional SMTP relay; nil or empty Host keeps the email reset flow disabled

	OTLPTracesEndpoint  string // optional OTLP/HTTP traces endpoint; empty falls back to LITEAIG_OTLP_ENDPOINT
	OTLPMetricsEndpoint string // optional OTLP/HTTP metrics endpoint
	OTLPLogsEndpoint    string // optional OTLP/HTTP logs endpoint

	// GatewayRPM caps gateway requests per tenant+project per minute; 0 uses
	// the built-in 600 RPM default. GatewayBurst is the token bucket capacity
	// (0 means the burst equals GatewayRPM). GatewayTPM caps tokens per
	// tenant+project per minute; 0 disables TPM metering.
	GatewayRPM   int
	GatewayBurst int
	GatewayTPM   int64

	// RuntimeBundle delivery (Stage 11). ControlPlane side: BundleSigner signs
	// every published snapshot and BundleToken authenticates the pull endpoint
	// for Data Planes. Gateway side: ControlURL points at a split-mode Control
	// Plane whose signed bundles are pulled when the local registry is empty;
	// BundlePublicKeyHex verifies those signatures.
	BundleSigner       *bundle.Signer
	BundleToken        string
	ControlURL         string
	BundlePublicKeyHex string

	// CoordinatorURL points at the Redis/Valkey coordinator used by Standard
	// tier for budget ledger, concurrency leases, and inflight counters; empty
	// keeps the self-contained in-memory implementations (Lite).
	CoordinatorURL string

	// EgressAllowCIDRs is the operator opt-in list of internal networks
	// reachable by the upstream connectors (in-cluster provider endpoints in a
	// self-hosted Kubernetes deployment). All other private/internal ranges stay
	// denied, so the default Single-operator posture is unchanged when empty.
	EgressAllowCIDRs []string

	// AgentCard, when non-nil, is published by the gateway at
	// /.well-known/agent-card.json. It is the operator-supplied static Agent
	// Card for this process; there is no control-plane card editor. Dynamic
	// per-tenant cards and lifecycle discovery integration are future work.
	AgentCard *a2a.AgentCard
}

// MailOptions configures the transactional mail relay used by the
// email-verified password reset flow and, when AlertTo is set, the alert email
// notification channel.
type MailOptions struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string // starttls (default), implicit, none
	// AlertTo, when non-empty and the relay is configured, subscribes alert
	// notifications to this recipient.
	AlertTo string
}

// AnalyticsOptions forwards redacted domain events to a ClickHouse-compatible
// HTTP analytics sink. Nil or an empty BaseURL keeps a no-op sink (no events
// leave the process).
type AnalyticsOptions struct {
	BaseURL string // ClickHouse HTTP endpoint, e.g. http://localhost:8123
	Table   string // target table, e.g. liteaig.analytics_events
}

// OIDCOptions enables OIDC authorization-code + PKCE login on the Console.
// The IdP's JWKS is fetched from <Issuer>/.well-known/jwks.json.
type OIDCOptions struct {
	Issuer                string // OIDC issuer; empty disables SSO
	ClientID              string
	Audience              string // expected id_token aud; defaults to ClientID
	AuthorizationEndpoint string
	TokenEndpoint         string
	RedirectURI           string
	StateTTL              time.Duration
	AdminSubjects         []string // sub allow-list mapped to tenant_admin
	OperatorSubjects      []string // sub allow-list mapped to tenant_operator
}

// Lite is the runnable Lite management plane composition root.
type Lite struct {
	handler        http.Handler
	gateway        http.Handler
	registry       *runtime.ActiveRegistry
	lifecycle      *Lifecycle
	close          func() error
	cipher         *secrets.Cipher
	secretProvider secrets.Provider
	secretVault    *sqlrepo.SecretVault
	// federationLifecycle is the process-shared federation relationship store
	// backing the inbound federated identity resolver and the admin overlay.
	federationLifecycle *federation.Lifecycle
}

// Lifecycle exposes the drain lifecycle wired through the Lite handlers.
func (l *Lite) Lifecycle() *Lifecycle { return l.lifecycle }

// FederationLifecycle exposes the shared federation relationship lifecycle so
// operators and tests can seed/verify trust state through the same instance
// the data-plane resolver reads.
func (l *Lite) FederationLifecycle() *federation.Lifecycle { return l.federationLifecycle }

// SecretProvider resolves local:// secret references (pepper, provider
// credentials, env) for the data plane and provider connectors.
func (l *Lite) SecretProvider() secrets.Provider { return l.secretProvider }

// SecretVault exposes the envelope-encrypted secret store.
func (l *Lite) SecretVault() *sqlrepo.SecretVault { return l.secretVault }

// Handler exposes the combined Admin API + session + Console handler.
func (l *Lite) Handler() http.Handler { return l.handler }

// GatewayHandler exposes the OpenAI/Anthropic-compatible data plane. It is nil
// until the data plane is composed (always the case for a fully built Lite).
func (l *Lite) GatewayHandler() http.Handler { return l.gateway }

// HasActiveRuntime reports whether at least one tenant runtime snapshot is
// loaded. The data plane cannot serve traffic until the startup reconcile
// (or a later publish) has activated a tenant.
func (l *Lite) HasActiveRuntime() bool { return l.registry.HasAnyTenant() }

// Close closes the underlying storage.
func (l *Lite) Close() error {
	if l.close != nil {
		return l.close()
	}
	return nil
}

// localPepperVersion is the seeded key-pepper version for the Lite profile.
const localPepperVersion = 1

// localPepperRef is the secret reference resolved by the static provider.
const localPepperRef = "local://pepper/v1"

// configReconcileInterval is how often every replicas sharing the same
// database re-applies the latest published config snapshots to its in-process
// registry. A publish activates only the acting replica's registry, so peers
// would otherwise serve stale (or no) runtime and never become ready.
const configReconcileInterval = 5 * time.Second

// auditRetentionInterval is how often the retention sweeper rebuilds the
// expired-audit cut set for tenants with an explicit retention policy.
const auditRetentionInterval = time.Hour

func sealA2APushBearer(cipher *secrets.Cipher) func([]byte) (string, error) {
	return func(plain []byte) (string, error) {
		if len(plain) == 0 {
			return "", nil
		}
		sealed, err := cipher.Encrypt(plain)
		if err != nil {
			return "", err
		}
		return "sealed:" + base64.RawURLEncoding.EncodeToString(sealed), nil
	}
}

func openA2APushBearer(cipher *secrets.Cipher) func(string) ([]byte, error) {
	return func(value string) ([]byte, error) {
		encoded, ok := strings.CutPrefix(value, "sealed:")
		if !ok {
			return []byte(value), nil
		}
		sealed, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		return cipher.Decrypt(sealed)
	}
}

func sealA2APushPayload(cipher *secrets.Cipher) func([]byte) ([]byte, error) {
	return func(plain []byte) ([]byte, error) {
		if len(plain) == 0 {
			return nil, nil
		}
		sealed, err := cipher.Encrypt(plain)
		if err != nil {
			return nil, err
		}
		return []byte("sealed:" + base64.RawURLEncoding.EncodeToString(sealed)), nil
	}
}

func openA2APushPayload(cipher *secrets.Cipher) func([]byte) ([]byte, error) {
	return func(value []byte) ([]byte, error) {
		encoded, ok := strings.CutPrefix(string(value), "sealed:")
		if !ok {
			return append([]byte(nil), value...), nil
		}
		sealed, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		return cipher.Decrypt(sealed)
	}
}

// liteFoundation is the storage and identity base every Lite service shares.
// openLiteFoundation closes the database itself on failure, so callers only
// clean up the resources they create.
type liteFoundation struct {
	db             *sql.DB
	cipher         *secrets.Cipher
	pepper         *identity.Pepper
	clock          systemClock
	ids            *id.Generator
	store          *sqlrepo.Store
	secretProvider secrets.Provider
	secretVault    *sqlrepo.SecretVault
	hasher         *password.Argon2id
}

func openLiteFoundation(ctx context.Context, dsn string) (*liteFoundation, error) {
	db, err := openLiteStorage(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open lite storage: %w", err)
	}
	fail := func(err error) (*liteFoundation, error) {
		_ = db.Close()
		return nil, err
	}
	if err := migrations.Apply(ctx, db); err != nil {
		return fail(fmt.Errorf("apply lite migrations: %w", err))
	}
	masterKey, err := loadLiteMasterKey(dsn)
	if err != nil {
		return fail(err)
	}
	cipher, err := secrets.NewCipher(masterKey)
	if err != nil {
		return fail(fmt.Errorf("build secret cipher: %w", err))
	}
	ids := id.NewGenerator(nil)
	store := sqlrepo.Open(db, sqlrepo.Deps{IDs: ids, Clock: systemClock{}})
	pepper, err := identity.EnsurePepper(ctx, store.Pepper, cipher, localPepperVersion, localPepperRef)
	if err != nil {
		return fail(err)
	}
	hasher, err := password.NewArgon2id(password.DefaultArgon2idParams(), nil)
	if err != nil {
		return fail(err)
	}
	return &liteFoundation{
		db: db, cipher: cipher, pepper: pepper,
		clock:          systemClock{},
		ids:            ids,
		store:          store,
		secretProvider: secrets.NewCompositeProvider(pepper, store.SecretResolver(cipher.Decrypt), secrets.NewEnvProvider()),
		secretVault:    store.SecretVault,
		hasher:         hasher,
	}, nil
}

func openLiteStorage(ctx context.Context, dsn string) (*sql.DB, error) {
	if strings.HasPrefix(strings.TrimSpace(dsn), "postgres://") || strings.HasPrefix(strings.TrimSpace(dsn), "postgresql://") {
		return postgres.Open(ctx, dsn)
	}
	return sqlite.Open(ctx, dsn)
}

// alertNotifiersFor builds the optional per-alert email delivery channel. It
// is wired only when a relay and an alert recipient are both configured.
func alertNotifiersFor(options LiteOptions) ([]alert.Notifier, error) {
	if options.Mail == nil || options.Mail.Host == "" || options.Mail.AlertTo == "" {
		return nil, nil
	}
	sender, err := mail.NewSMTPSender(mail.Config{
		Host: options.Mail.Host, Port: options.Mail.Port,
		Username: options.Mail.Username, Password: options.Mail.Password,
		From: options.Mail.From, TLSMode: options.Mail.TLSMode,
	})
	if err != nil {
		return nil, err
	}
	return []alert.Notifier{&alert.EmailNotifier{Sender: sender, To: options.Mail.AlertTo}}, nil
}

// liteRuntime is the compiled in-memory runtime plus the control-plane
// services that publish into it, restored from the database at boot.
type liteRuntime struct {
	registry      *runtime.ActiveRegistry
	configService *config.Service
	apikeyService *apikey.Service
	alertService  *alert.Service
	bundleHub     *bundle.Hub
}

// bootstrapLiteRuntime activates the process-global runtime, restores the
// published tenant snapshots (with the Last Known Good fallback) and builds
// the control-plane services that publish into the registry.
func bootstrapLiteRuntime(ctx context.Context, f *liteFoundation, dsn string, signer *bundle.Signer, notifiers []alert.Notifier) (*liteRuntime, error) {
	registry := &runtime.ActiveRegistry{}
	// Activate the process-global runtime so bearer-token authentication can
	// resolve the seeded key pepper for the Lite profile.
	registry.ActivateGlobal(runtime.NewGlobalRuntime(runtime.GlobalRuntimeData{
		Version: 1, PublishedAt: f.clock.Now(),
		PepperRefs: map[int]string{localPepperVersion: localPepperRef},
	}))
	apikeyService, err := apikey.NewService(f.store.APIKeys, f.pepper, apikey.NewGenerator(nil), f.ids, f.clock, f.cipher)
	if err != nil {
		return nil, err
	}
	configService := config.NewService(f.store.Config, f.store.APIKeys, registry, f.ids, f.clock)
	configService.SetSystemRepository(f.store.SystemConfig)
	// Fast-published guardrail policies live in their own store; re-apply the
	// active one onto each compiled snapshot at startup so a restart does not
	// silently drop it from the data plane.
	guardrailStore := f.store.GuardrailPolicy
	configService.SetGuardrailLoader(func(ctx context.Context, tenantID string) (runtime.GuardrailPolicy, bool) {
		policy, err := guardrailStore.GetActive(ctx, tenancy.TenantScope{TenantID: tenantID})
		if err != nil || policy == nil {
			return runtime.GuardrailPolicy{}, false
		}
		rules := make([]runtime.GuardrailRule, 0, len(policy.Rules))
		for _, rule := range policy.Rules {
			rules = append(rules, runtime.GuardrailRule{ID: rule.ID, Kind: rule.Kind, Pattern: rule.Pattern, Action: rule.Action, Replacement: rule.Replacement})
		}
		return runtime.GuardrailPolicy{Mode: string(policy.ChangeType), SecurityEpoch: policy.SecurityEpoch, Rules: rules}, true
	})
	// Last Known Good: persist every successful activation as an LKG bundle so
	// the data plane can boot from the last good config when the Control Plane
	// state is missing or unreadable. File-backed deployments only; in-memory
	// DSNs have no durable store to fall back to.
	var lkgStore *lkg.Store
	if dbPath, ok := liteDataPath(dsn); ok {
		if store, lkgErr := lkg.New(filepath.Join(filepath.Dir(dbPath), "liteaig-lkg")); lkgErr == nil {
			lkgStore = store
			configService.SetLKGPublisher(func(ctx context.Context, snapshot *runtime.TenantRuntimeSnapshot) error {
				data := snapshot.Data()
				return lkgStore.Save(ctx, snapshot.TenantRef, lkg.Bundle{
					SchemaVersion: lkgSchemaVersion,
					TenantID:      data.TenantID,
					TenantRef:     data.TenantRef,
					ConfigVersion: data.Version,
					SecurityEpoch: data.SecurityEpoch,
					SnapshotData:  &data,
				})
			})
		} else {
			log.Printf("lite: LKG store unavailable: %v", lkgErr)
		}
	}
	// Signed RuntimeBundle delivery (Stage 11): every activation is signed and
	// published to the bundle hub so split-mode Data Planes can pull it over the
	// authenticated control-plane endpoint.
	var bundleHub *bundle.Hub
	if signer != nil {
		bundleHub = bundle.NewHub()
		configService.SetBundlePublisher(func(ctx context.Context, snapshot *runtime.TenantRuntimeSnapshot) error {
			signed, err := signer.Sign(snapshot)
			if err != nil {
				return err
			}
			bundleHub.Publish(snapshot, signed)
			return nil
		})
	}
	// Restore in-memory runtime snapshots for every published tenant. These
	// snapshots do not survive a process restart; without this the data plane
	// would reject all traffic (401/403) until the next manual publish.
	if reconciled, reconcileErr := configService.ReconcileAll(ctx); reconcileErr != nil {
		log.Printf("lite: startup reconcile: %d tenant(s) restored: %v", reconciled, reconcileErr)
	}
	// LKG boot fallback: for any tenant whose Control-Plane state did not
	// activate a runtime (missing or unreadable published config), boot the
	// last known good bundle. This never overrides a healthy Control-Plane
	// activation.
	if lkgStore != nil {
		if refs, err := lkgStore.TenantRefs(); err == nil {
			for _, ref := range refs {
				if _, active := registry.Tenant(ref); active {
					continue
				}
				bundle, bundleErr := lkgStore.LoadVerified(ctx, ref, verifyLKGBundle)
				if bundleErr != nil || bundle.SnapshotData == nil {
					log.Printf("lite: LKG boot for %s unavailable: %v", ref, bundleErr)
					continue
				}
				snapshot := runtime.NewTenantSnapshot(*bundle.SnapshotData)
				registry.ActivateTenant(snapshot.TenantRef, snapshot)
				log.Printf("lite: booted tenant %s from Last Known Good bundle v%d", ref, bundle.ConfigVersion)
			}
		}
	}
	alertService, err := alert.NewService(f.store.Alerts, notifiers, f.store.AlertAudit, f.ids, f.clock)
	if err != nil {
		return nil, err
	}
	return &liteRuntime{registry: registry, configService: configService, apikeyService: apikeyService, alertService: alertService, bundleHub: bundleHub}, nil
}

// buildLiteAdmin wires the session manager, the Admin API server, the login
// endpoints (OIDC + local accounts + email-verified password reset) and the
// outer handler stack with its drain gate.
func buildLiteAdmin(controlBackend *backend.ControlBackend, f *liteFoundation, options LiteOptions, egressClient *http.Client, lifecycle *Lifecycle, auditEvent func(ctx context.Context, tenantID, actorID, action, resourceType, resourceID string) error, systemAuditEvent func(ctx context.Context, actorID, action, resourceType, resourceID string) error, events contracts.EventSink, bundleHub *bundle.Hub) (http.Handler, error) {
	sessions, err := adminapi.NewSessionManager(adminapi.SessionConfig{CookieName: "lia_session", TTL: sessionTTL, Secure: true}, f.clock, nil)
	if err != nil {
		return nil, err
	}
	// The setup wizard is anonymous by design (it creates the first admin),
	// so it is gated behind a bootstrap token when the admin port is reachable
	// off-loopback. The token is printed once to the server log for operators
	// who initialize from a remote console.
	bootstrapToken, err := newBootstrapToken()
	if err != nil {
		return nil, err
	}
	log.Printf("lite: setup bootstrap token (send as X-Bootstrap-Token for remote console initialization): %s", bootstrapToken)
	server := adminapi.New(adminapi.AllOf(controlBackend), sessions).WithUserManagement(f.store.LocalCredentials, f.hasher, f.ids).WithTenantMemberships(f.store.Organization).WithSystemConfig(controlBackend).WithBootstrapToken(bootstrapToken).WithEventSink(events)
	oidcLogin, err := newOIDCLogin(sessions, options.OIDC, egressClient)
	if err != nil {
		return nil, err
	}
	// auditLocalAuth is the local-account subset (actor, action, resource)
	// consumed by the session endpoints and password-reset service; it resolves
	// the single-tenant ID from the first tenant.
	auditLocalAuth := func(ctx context.Context, actor, action, resourceID string) error {
		tenant, err := f.store.Tenancy.FirstTenant(ctx)
		if err != nil || tenant == nil {
			return err
		}
		return auditEvent(ctx, tenant.ID, actor, action, "local_user", resourceID)
	}
	server = server.WithAudit(auditEvent).WithSystemAudit(systemAuditEvent).WithSessionInvalidation(sessions.InvalidateForAdmin)
	if oidcLogin != nil {
		oidcLogin.Audit = auditLocalAuth
		// Lite is single-tenant and the bootstrap tenant ID is random, so an
		// OIDC session must resolve to the real tenant instead of the "default"
		// placeholder the claim mapping produces.
		oidcLogin.ResolveTenant = func(ctx context.Context) (string, error) {
			tenant, err := f.store.Tenancy.FirstTenant(ctx)
			if err != nil || tenant == nil {
				return "", err
			}
			return tenant.ID, nil
		}
	}
	// Email-verified password reset: only wired when an SMTP relay is
	// configured; otherwise the login screen falls back to the local
	// emergency reset.
	var passwordReset adminapi.PasswordResetService
	if options.Mail != nil && options.Mail.Host != "" {
		sender, mailErr := mail.NewSMTPSender(mail.Config{
			Host: options.Mail.Host, Port: options.Mail.Port,
			Username: options.Mail.Username, Password: options.Mail.Password,
			From: options.Mail.From, TLSMode: options.Mail.TLSMode,
		})
		if mailErr != nil {
			return nil, mailErr
		}
		passwordReset = newPasswordResetService(f.store.LocalCredentials, f.hasher, sender, sessions, auditLocalAuth, f.clock)
	}
	sessionEndpoints := adminapi.SessionEndpoints{
		Sessions:       sessions,
		Verifier:       adminapi.LocalVerifier{Store: f.store.LocalCredentials, Passwords: f.hasher},
		OIDC:           oidcLogin,
		MaxBodyBytes:   1 << 20,
		BootstrapToken: bootstrapToken,
		// Brute-force protection: 5 failed logins per account and 20 total
		// attempts per client per 15 minutes.
		LoginLimiter:     adminapi.NewLoginLimiter(15*time.Minute, 5, 20),
		EventSink:        events,
		EventIDs:         f.ids,
		PasswordReset:    passwordReset,
		ResetRateLimiter: adminapi.NewResetRateLimiter(15*time.Minute, 10, 5, time.Minute),
		Audit:            auditLocalAuth,
	}
	handler, err := buildLiteHandler(server, sessionEndpoints, f.store, bundleHub, options.BundleToken)
	if err != nil {
		return nil, err
	}
	// Outermost: refuses new traffic once draining begins.
	handler.Use(drainGate(lifecycle))
	return handler, nil
}

// wireAccountingSpool adds the durable accounting spool for file-backed
// databases. The returned repository is either the spooled one (write through
// the local WAL, background at-least-once flush) or the direct repository for
// in-memory DSNs; closeSpool and spoolFlush are nil in the direct case.
func wireAccountingSpool(dsn string, accountingRepo accounting.Repository, registry *runtime.ActiveRegistry, events contracts.EventSink, clock contracts.Clock) (accounting.Repository, func() error, func(context.Context) error, error) {
	dbPath, ok := liteDataPath(dsn)
	if !ok {
		return accountingRepo, nil, nil, nil
	}
	spoolDir := filepath.Join(filepath.Dir(dbPath), "liteaig-spool", "accounting")
	s, err := spool.New(spool.Config{Dir: spoolDir})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open accounting spool: %w", err)
	}
	flusher := accounting.NewFlusher(s, accountingRepo, accounting.FlusherConfig{Batch: 100, Interval: time.Second}, nil, clock, events)
	flushCtx, cancelFlush := context.WithCancel(context.Background())
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		flusher.Run(flushCtx)
	}()
	closeSpool := func() error {
		cancelFlush()
		<-flushDone
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), spoolDrainTimeout)
		_, _ = flusher.RunOnce(drainCtx)
		cancelDrain()
		return s.Close()
	}
	spoolFlush := func(ctx context.Context) error {
		drainCtx, cancelDrain := context.WithTimeout(ctx, spoolDrainTimeout)
		defer cancelDrain()
		_, err := flusher.RunOnce(drainCtx)
		return err
	}
	var spooled accounting.Repository = &spoolAccounting{direct: accountingRepo, spool: s, registry: registry}
	return spooled, closeSpool, spoolFlush, nil
}

// NewLite composes the runnable Lite profile: SQLite storage, migrations,
// seeded key pepper, repositories, domain services, Admin API, session
// endpoints, and the embedded Console.
func NewLite(ctx context.Context, options LiteOptions) (*Lite, error) {
	// Single egress posture for all upstream connector traffic (model, MCP,
	// A2A, webhooks, and the setup wizard's provider probe).
	// EgressAllowCIDRs lets a self-hosted operator reach in-cluster Service
	// endpoints; everything else keeps the default SSRF guardrails (no internal
	// address ranges, no redirects, capped bodies).
	appPolicy, policyErr := egress.LitePolicyWithAllowedCIDRs(options.EgressAllowCIDRs)
	if policyErr != nil {
		return nil, fmt.Errorf("egress allow cidrs: %w", policyErr)
	}
	if options.WebhookURL != "" {
		if err := egress.ValidateTarget(options.WebhookURL, appPolicy); err != nil {
			return nil, fmt.Errorf("webhook target: %w", err)
		}
	}
	if options.BaseURL == "" {
		options.BaseURL = "http://localhost:8081"
	}
	foundation, err := openLiteFoundation(ctx, options.DSN)
	if err != nil {
		return nil, err
	}
	db := foundation.db
	notifiers, err := alertNotifiersFor(options)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	liteRuntime, err := bootstrapLiteRuntime(ctx, foundation, options.DSN, options.BundleSigner, notifiers)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	registry := liteRuntime.registry
	configService := liteRuntime.configService
	apikeyService := liteRuntime.apikeyService
	alertService := liteRuntime.alertService
	tenancyRepo := foundation.store.Tenancy
	configRepo := foundation.store.Config
	accountingRepo := foundation.store.Accounting
	alertStore := foundation.store.Alerts
	credentialStore := foundation.store.LocalCredentials
	securityEvents := foundation.store.SecurityEvents
	toolCalls := foundation.store.ToolCalls

	bootstrapService, err := setup.NewService(setup.DefaultConfig(), foundation.store.Bootstrap, foundation.hasher, foundation.ids, foundation.ids, foundation.clock)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	// Single egress posture computed at the top of NewLite: egressClient and
	// providerSetup honor EgressAllowCIDRs (self-hosted K8s in-cluster
	// providers) while keeping the default SSRF guardrails (no internal address
	// ranges, no redirects, capped bodies).
	egressClient := egress.Client(appPolicy)
	prober := newProviderProber(egressClient, foundation.secretProvider)
	providerSetup := newRealProviderSetup(foundation.ids, foundation.clock, tenancyRepo, configService, configRepo, foundation.secretVault, foundation.cipher, egressClient, appPolicy)
	live := backend.NewLiveBus()
	// Standard tier coordination: when a --coordinator redis:// URL is set, the
	// budget ledger, concurrency leases, and inflight counters are backed by
	// Redis/Valkey; otherwise the self-contained in-memory variants (Lite) are
	// used. The budget ledger fail mode follows §14.4 (hard = fail_closed by
	// default; soft projects may be declared to fail open).
	coord, err := openCoordinator(ctx, coordinatorOptions{
		URL: options.CoordinatorURL,
		Alerts: func(kind, message string) {
			live.PublishLive(contracts.LiveEvent{Outcome: kind, DeploymentID: "coordinator", LatencyMS: 0})
		},
	}, foundation.clock)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	settlementCurrency := func(ctx context.Context, tenantID string) (string, error) {
		tenant, err := tenancyRepo.GetTenant(ctx, tenancy.TenantScope{TenantID: tenantID})
		if err != nil || tenant.SettlementCurrency == "" {
			return "USD", nil
		}
		return tenant.SettlementCurrency, nil
	}
	// Drain lifecycle: once draining starts, new requests are refused (503),
	// in-flight work and long streams are waited out, and the accounting
	// spool is flushed before exit. A composition root (main) may share its
	// own lifecycle so readiness and drain observe one state machine.
	lifecycle := options.Lifecycle
	if lifecycle == nil {
		var lifecycleErr error
		lifecycle, lifecycleErr = NewLifecycle(options.DrainConfig, nil, nil)
		if lifecycleErr != nil {
			_ = db.Close()
			return nil, lifecycleErr
		}
	}
	// Human approval checkpoint: high-risk tool/agent actions defined in the
	// published config (tool_policies.require_approval / agents.require_approval)
	// block on the data plane until an operator decides in the Console.
	approvalService := approval.NewService(foundation.store.Approvals, foundation.clock.Now)
	// Approvals gate real actions, so every decision must trace to an active
	// local user of the tenant; forged or cross-tenant actor IDs are rejected.
	// With OIDC enabled the session's AdminID is an IdP-verified subject that
	// is never provisioned as a local account, so the IdP is the authority.
	approvalService.SetApproverVerifier(func(ctx context.Context, scope tenancy.TenantScope, approver string) error {
		if approver == "" {
			return approval.ErrUnknownApprover
		}
		credential, err := credentialStore.FindLocalUserByID(ctx, approver)
		if err == nil && credential.Status == "active" && credential.TenantID == scope.TenantID {
			return nil
		}
		if options.OIDC != nil && options.OIDC.Issuer != "" {
			return nil
		}
		return approval.ErrUnknownApprover
	})
	approvalCheckpoint := gatewayapproval.Handler{Checker: &approvalGate{service: approvalService}}
	events, flushAnalytics, err := newAnalyticsSink(options.Analytics)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	notificationSettings, err := notificationSettingsForStartup(ctx, options.WebhookURL, tenancyRepo, alertStore)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	events, closeNotifications, reloadNotifications, err := newReloadableNotificationSink(notificationSettings, events)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	notificationSink := events
	events = newDurableEventSink(foundation.store.EventOutbox, events)
	composed := false
	tracer, traceErr := observability.NewOTLPTracer(ctx, firstNonEmpty(options.OTLPTracesEndpoint, os.Getenv("LITEAIG_OTLP_TRACES_ENDPOINT"), os.Getenv("LITEAIG_OTLP_ENDPOINT")))
	if traceErr != nil {
		log.Print("OTLP tracing disabled: invalid configuration or initialization failure")
	}
	otlpMetrics, metricsErr := observability.NewOTLPMetrics(ctx, firstNonEmpty(options.OTLPMetricsEndpoint, os.Getenv("LITEAIG_OTLP_METRICS_ENDPOINT")))
	if metricsErr != nil {
		log.Print("OTLP metrics disabled: invalid configuration or initialization failure")
	}
	otlpLogger, logsErr := observability.NewOTLPLogger(ctx, firstNonEmpty(options.OTLPLogsEndpoint, os.Getenv("LITEAIG_OTLP_LOGS_ENDPOINT")))
	if logsErr != nil {
		log.Print("OTLP logs disabled: invalid configuration or initialization failure")
	}
	if otlpLogger != nil {
		_ = otlpLogger.Emit(ctx, observability.LogRecord{Message: "liteaig runtime composed", Severity: "info", Attributes: map[string]string{"mode": "lite"}})
		// Structured security/operational event export: every domain event is
		// forwarded to the OTLP logs channel (allowlisted attributes) before it
		// reaches the durable outbox. Best-effort: log-export failures never
		// fail the request or the durable event path.
		events = logThenEventSink{
			log:  observability.OTLPLogSink{Logger: otlpLogger},
			next: events,
		}
	}
	var metricSink *observability.MetricSink
	if otlpMetrics != nil {
		metricSink = observability.NewMetricSink(func(metric observability.Metric) {
			_ = otlpMetrics.Record(context.Background(), metric)
		})
	}
	defer func() {
		if !composed {
			_ = tracer.Shutdown(context.Background())
			_ = otlpMetrics.Shutdown(context.Background())
			_ = otlpLogger.Shutdown(context.Background())
			closeNotifications()
		}
	}()
	alertService.SetEventSink(events)
	approvalService.SetEventSink(events)
	configService.SetEventSink(events)
	pipeline, err := newLitePipeline(accountingRepo, live, foundation.ids, foundation.clock, egressClient, foundation.secretProvider, appPolicy, settlementCurrency, lifecycle.Streams(), approvalCheckpoint, events, rate.Policy{RequestsPerMinute: options.GatewayRPM, Burst: options.GatewayBurst}, options.GatewayTPM, prober.Probe, coord)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	pipeline.tracer = tracer
	pipeline.metricSink = metricSink
	playgroundService, err := playground.New(pipeline, foundation.ids, foundation.clock)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	// Data plane: bearer-token auth + admission + the governed pipeline behind
	// an OpenAI/Anthropic-compatible HTTP gateway.
	authenticator := auth.NewAPIKeyAuthenticator(registry, foundation.secretProvider, foundation.clock)
	admissionService, err := admission.New(admission.Config{MaxBodyBytes: gatewayMaxBodyBytes}, authenticator, foundation.ids, foundation.clock)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	// The federation lifecycle backing both the inbound federated identity
	// resolver (data plane) and the admin federation overlay is a single
	// process-shared instance over the relationship store, so a relationship
	// activated on the admin side is immediately resolvable on the gateway.
	federationLifecycle := federation.NewLifecycleWithStore(foundation.clock.Now, foundation.store.Federation)
	// Lite is single-tenant: the federated caller scope resolves to the sole
	// tenant and its compiled runtime snapshot.
	federatedAuth := &inboundFederatedAuth{
		resolver: federation.NewResolver(federationLifecycle),
		tenants: func(ctx context.Context) ([]tenancy.TenantScope, error) {
			tenant, tenantErr := tenancyRepo.FirstTenant(ctx)
			if tenantErr != nil {
				if errors.Is(tenantErr, tenancy.ErrNotFound) {
					return nil, nil
				}
				return nil, tenantErr
			}
			return []tenancy.TenantScope{{TenantID: tenant.ID}}, nil
		},
		snapshotFor: func(ctx context.Context, tenantID string) (*runtime.TenantRuntimeSnapshot, error) {
			snapshot, ok := registry.TenantByID(tenantID)
			if !ok || snapshot == nil {
				return nil, tenancy.ErrNotFound
			}
			return snapshot, nil
		},
	}
	pushSigningSecret, _ := foundation.pepper.Resolve(ctx, localPepperRef)
	gatewayServer := gatewayserver.New(newLiteGatewayCore(admissionService, pipeline, authenticator, toolCalls, foundation.store.A2ATask, egressClient, foundation.secretProvider, federatedAuth), gatewayserver.Config{
		MaxBodyBytes:         gatewayMaxBodyBytes,
		LocalAgentCard:       options.AgentCard,
		Middleware:           []webkit.Middleware{drainGate(lifecycle)},
		CallbackClient:       egressClient,
		A2APushOutbox:        foundation.store.A2APushOutbox,
		BatchMappings:        foundation.store.BatchMappings,
		A2APushSigningSecret: pushSigningSecret,
		A2APushSealBearer:    sealA2APushBearer(foundation.cipher),
		A2APushOpenBearer:    openA2APushBearer(foundation.cipher),
		A2APushSealPayload:   sealA2APushPayload(foundation.cipher),
		A2APushOpenPayload:   openA2APushPayload(foundation.cipher),
		A2APushSealURL: func(value string) (string, error) {
			return sealA2APushBearer(foundation.cipher)([]byte(value))
		},
		A2APushOpenURL: func(value string) (string, error) {
			plain, err := openA2APushBearer(foundation.cipher)(value)
			return string(plain), err
		},
	})
	var background sync.WaitGroup
	runBackground := func(run func()) {
		background.Add(1)
		go func() {
			defer background.Done()
			run()
		}()
	}
	pushCtx, pushCancel := context.WithCancel(context.Background())
	defer func() {
		if !composed {
			pushCancel()
		}
	}()
	pushWorker := &singletonTask{leases: foundation.store.Coordination, scope: platformA2APushScope, ttl: singletonLeaseTTL}
	runBackground(func() {
		pushWorker.run(pushCtx, time.Second, func(ctx context.Context) error {
			gatewayServer.DrainA2APushOutbox(ctx)
			return nil
		})
	})
	eventOutboxCtx, eventOutboxCancel := context.WithCancel(context.Background())
	defer func() {
		if !composed {
			eventOutboxCancel()
		}
	}()
	eventOutboxWorker := newOutboxDeliveryWorker(foundation.store.EventOutbox, notificationSink, foundation.clock)
	eventOutbox := &singletonTask{leases: foundation.store.Coordination, scope: platformEventOutboxScope, ttl: singletonLeaseTTL}
	runBackground(func() {
		eventOutbox.run(eventOutboxCtx, time.Second, func(ctx context.Context) error {
			return eventOutboxWorker.Drain(ctx)
		})
	})
	gatewayHandler := gatewayServer.Handler()
	if tracer != nil {
		next := gatewayHandler
		gatewayHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	firstCall := &liteFirstCall{service: playgroundService, registry: registry}
	wizard, err := setup.NewWizard(setup.WizardConfig{
		GatewayBaseURL: options.BaseURL, DefaultLogicalModel: "default-chat", FirstKeyName: "first-key",
	}, resumableBootstrapper{bootstrap: bootstrapService, resetter: foundation.store.Bootstrap}, providerSetup, apikeyService, firstCall)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	approvals := &liteApprovals{service: approvalService}
	agentGraph := &liteAgentGraph{accounting: accountingRepo}
	cardClient := a2a.NewClient(egressClient, a2a.NewMemoryCardStore(10*time.Minute))
	fed := &liteFederation{lifecycle: federationLifecycle, registry: registry, cardClient: cardClient, pushOutbox: foundation.store.A2APushOutbox}
	auditRecorder := sqlrepo.NewAuditRecorder(foundation.db, foundation.ids)
	// auditEvent writes a tenant-scoped audit row. Callers pass the tenant ID
	// explicitly because unauthenticated flows (emergency reset, failed
	// logins) resolve it before the actor is known.
	auditEvent := func(ctx context.Context, tenantID, actorID, action, resourceType, resourceID string) error {
		if tenantID == "" || actorID == "" {
			return nil
		}
		return auditRecorder.Record(ctx, tenantID, actorID, action, resourceType, resourceID)
	}
	systemAuditEvent := func(ctx context.Context, actorID, action, resourceType, resourceID string) error {
		if actorID == "" {
			return nil
		}
		return auditRecorder.RecordSystem(ctx, actorID, action, resourceType, resourceID)
	}
	controlBackend := backend.NewControlBackend(
		configService, accountingRepo, foundation.store.ResourceCatalog, alertService, alertStore, registry,
		liteCircuits{}, live,
		func(ctx context.Context, scope tenancy.TenantScope, limit int) ([]backend.SecurityEventView, error) {
			return listSecurityEvents(ctx, securityEvents, scope, limit)
		},
		func(ctx context.Context, scope tenancy.TenantScope, limit int) ([]backend.ToolCallView, error) {
			return listToolCalls(ctx, toolCalls, scope, limit)
		},
	)
	controlBackend.SetNotificationReloader(func(_ context.Context, settings alert.NotificationSettings) error {
		return reloadNotifications(&settings)
	})
	// Wire the runnable Lite paths into the ControlBackend capability slots;
	// AllOf below only compiles while every capability stays satisfied.
	capabilities := &liteCapabilities{
		setup:          backend.WizardSetupAdapter{Wizard: wizard},
		keys:           apikeyService,
		playground:     &litePlayground{service: playgroundService, registry: registry, tenancy: tenancyRepo},
		tenancy:        tenancyRepo,
		tenantAdmin:    foundation.store.Tenancy,
		audit:          foundation.store.Audit,
		auditRetention: foundation.store.AuditRetention,
		ids:            foundation.ids,
		clock:          foundation.clock,
		config:         configService,
		registry:       registry,
		delegation:     foundation.store.Delegations,
		cipher:         foundation.cipher,
		vault:          foundation.secretVault,
		egress:         egressClient,
		prober:         prober,
		auditEvent:     auditEvent,
	}
	controlBackend.SetSetupWizard(capabilities.setup.Setup)
	controlBackend.SetCreateKey(capabilities.CreateKey)
	controlBackend.SetRevokeKey(capabilities.RevokeKey)
	controlBackend.SetListKeys(capabilities.ListKeys)
	controlBackend.SetRevealKey(capabilities.RevealKey)
	controlBackend.SetDiscoverMCPTools(capabilities.DiscoverMCPTools)
	controlBackend.SetCreateCredential(capabilities.CreateCredential)
	controlBackend.SetRotateCredential(capabilities.RotateCredential)
	controlBackend.SetDisableCredential(capabilities.DisableCredential)
	controlBackend.SetDeleteCredential(capabilities.DeleteCredential)
	controlBackend.SetPlayground(capabilities.playground.Run)
	controlBackend.SetPlaygroundStream(capabilities.playground.PlaygroundStream)
	controlBackend.SetTenants(capabilities.Tenants)
	controlBackend.SetCreateTenant(capabilities.CreateTenant)
	controlBackend.SetUpdateTenantStatus(capabilities.UpdateTenantStatus)
	controlBackend.SetProjectsLister(capabilities.Projects)
	controlBackend.SetCreateProject(capabilities.CreateProject)
	controlBackend.SetAuditLister(capabilities.Audit)
	controlBackend.SetAuditRetentionRead(capabilities.GetAuditRetention)
	controlBackend.SetAuditRetentionWrite(capabilities.SetAuditRetention)
	controlBackend.SetHealthView(capabilities.Health)
	controlBackend.SetApprovalLister(approvals.List)
	controlBackend.SetApprovalAction(approvals.Decide)
	controlBackend.SetFederationSuspend(fed.Suspend)
	controlBackend.SetAgentGraph(agentGraph.Build)
	controlBackend.SetFederationOverlay(fed.Overlay)
	controlBackend.SetFederationDiscover(fed.Discover)
	controlBackend.SetFederationReview(fed.Review)
	controlBackend.SetPushDeliveries(fed.PushDeliveries)
	controlBackend.SetDelegations(capabilities.Delegations)
	controlBackend.SetGrantDelegation(capabilities.GrantDelegation)
	controlBackend.SetRevokeDelegation(capabilities.RevokeDelegation)
	// Evidence Export (Stage 14): a read-only, tenant-scoped archive assembled
	// on demand from durable tables. The exporter itself never selects prompt/
	// response bodies or secrets, so the archive is sensitive-free by
	// construction; the route is additionally gated by evidence.export.
	controlBackend.SetEvidenceExporter(func(ctx context.Context, scope tenancy.TenantScope, from, to time.Time) (benchmark.Archive, error) {
		records, err := foundation.store.Evidence.Collect(ctx, scope, from, to)
		if err != nil {
			return benchmark.Archive{}, err
		}
		return benchmark.NewExporter(foundation.clock.Now).Export(scope.TenantID, from, to, records), nil
	})
	// Guardrail fast publishes persist to the policy store, record a config
	// audit event, and (in FastPublishGuardrail) activate on the data plane.
	controlBackend.SetGuardrailRegistry(guardrail.NewPolicyRegistryWithStore(&guardrailAuditRecorder{repo: configRepo, ids: foundation.ids, clock: foundation.clock}, foundation.store.GuardrailPolicy))

	handler, err := buildLiteAdmin(controlBackend, foundation, options, egressClient, lifecycle, auditEvent, systemAuditEvent, events, liteRuntime.bundleHub)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	// Durable accounting spool: file-backed databases get a local WAL next to
	// the database. Tenants with a spool policy (hard/soft) write through it;
	// a background flusher ingests into the database at-least-once, so an
	// outage (or a crash, replayed on the next boot) does not lose accounting.
	pipelineAccounting, closeSpool, spoolFlushFunc, err := wireAccountingSpool(options.DSN, accountingRepo, registry, events, foundation.clock)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	lifecycle.SetDrainFinalizer(func(ctx context.Context) error {
		var spoolErr error
		if spoolFlushFunc != nil {
			spoolErr = spoolFlushFunc(ctx)
		}
		return errors.Join(spoolErr, tracer.Shutdown(ctx), otlpMetrics.Shutdown(ctx), otlpLogger.Shutdown(ctx))
	})
	pipeline.accounting = pipelineAccounting
	// Split-mode RuntimeBundle delivery (Stage 11): a mode=gateway process with
	// a ControlURL pulls signed bundles from the Control Plane and atomically
	// activates them (cold-start full fetch then periodic sync). A NACK or an
	// unreachable Control Plane keeps whatever runtime is already loaded.
	var syncCancel context.CancelFunc
	if options.ControlURL != "" && options.BundleToken != "" {
		publicKey, keyErr := bundle.VerifyPublicKey(options.BundlePublicKeyHex)
		if keyErr != nil {
			_ = db.Close()
			return nil, keyErr
		}
		client := bundle.NewClient(options.ControlURL, options.BundleToken, egressClient)
		syncer := newBundleSyncer(client, publicKey, registry, foundation.clock, 15*time.Second)
		syncCtx, cancel := context.WithCancel(context.Background())
		syncCancel = cancel
		runBackground(func() { syncer.run(syncCtx) })
	}
	// Config converge loop: every replica that shares the same database
	// re-applies the latest published version of every tenant. A publish only
	// activates the acting replica's in-process registry, so without this each
	// peer would keep serving the runtime it booted with (or none) and never
	// report ready. Unlike the leader-gated reservation sweeper, the reconcile
	// loop runs on every replica unconditionally.
	var reconcileCancel context.CancelFunc
	reconcileCtx, cancelReconcile := context.WithCancel(context.Background())
	reconcileCancel = cancelReconcile
	runBackground(func() {
		ticker := time.NewTicker(configReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-reconcileCtx.Done():
				return
			case <-ticker.C:
				if reconciled, reconcileErr := configService.ReconcileAll(reconcileCtx); reconcileErr != nil {
					log.Printf("lite: config reconcile: %d tenant(s) converged: %v", reconciled, reconcileErr)
				}
			}
		}
	})

	// Reservation Sweeper (§14.3): the platform leader expires abandoned budget
	// reservations every reservationSweepInterval. Leadership comes from the
	// DB-backed coordination_leases store, so multiple Control Planes running
	// against the same database never sweep the same reservations twice.
	var sweeperCancel context.CancelFunc
	sweeper := &singletonTask{leases: foundation.store.Coordination, scope: platformSweeperScope, ttl: singletonLeaseTTL}
	sweepCtx, cancel := context.WithCancel(context.Background())
	sweeperCancel = cancel
	runBackground(func() {
		sweeper.run(sweepCtx, reservationSweepInterval, func(ctx context.Context) error {
			pipeline.budget.Sweep()
			return nil
		})
	})
	// Audit retention sweeper: every replica sharing the database purges
	// expired audit events for tenants with an explicit retention policy.
	// Retention is opt-in and the purge is idempotent, so no leader gate is
	// required (replicas may race on the same rows without double-deleting).
	var retentionCancel context.CancelFunc
	retentionCtx, cancelRetention := context.WithCancel(context.Background())
	retentionCancel = cancelRetention
	runBackground(func() {
		ticker := time.NewTicker(auditRetentionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-retentionCtx.Done():
				return
			case <-ticker.C:
				if removed, retentionErr := capabilities.purgeExpiredAudit(retentionCtx, time.Now()); retentionErr != nil {
					log.Printf("lite: audit retention purge: %v", retentionErr)
				} else {
					recordAuditPurge(removed, time.Now())
					if removed > 0 {
						log.Printf("lite: audit retention purged %d expired event(s)", removed)
					}
				}
				now := time.Now()
				removed, enabled, mappingErr := sweepFileMappingRetention(retentionCtx, configService, foundation.store.BatchMappings, now)
				if mappingErr != nil {
					log.Printf("lite: file mapping retention purge: %v", mappingErr)
				} else if enabled {
					recordFileMappingPurge(removed, now)
					if removed > 0 {
						log.Printf("lite: file mapping retention purged %d expired mapping(s)", removed)
					}
				}
			}
		}
	})
	closeFunc := func() error {
		var firstErr error
		pushCancel()
		eventOutboxCancel()
		if reconcileCancel != nil {
			reconcileCancel()
		}
		if retentionCancel != nil {
			retentionCancel()
		}
		if syncCancel != nil {
			syncCancel()
		}
		if sweeperCancel != nil {
			sweeperCancel()
		}
		background.Wait()
		closeNotifications()
		_ = tracer.Shutdown(context.Background())
		_ = otlpMetrics.Shutdown(context.Background())
		_ = otlpLogger.Shutdown(context.Background())
		if flushAnalytics != nil {
			drainCtx, cancelDrain := context.WithTimeout(context.Background(), spoolDrainTimeout)
			if err := flushAnalytics(drainCtx); err != nil && firstErr == nil {
				firstErr = err
			}
			cancelDrain()
		}
		if closeSpool != nil {
			if err := closeSpool(); err != nil {
				firstErr = err
			}
		}
		coord.close()
		if err := db.Close(); firstErr == nil {
			firstErr = err
		}
		return firstErr
	}
	composed = true
	return &Lite{
		handler: handler, gateway: gatewayHandler, registry: registry, lifecycle: lifecycle, close: closeFunc,
		cipher: foundation.cipher, secretProvider: foundation.secretProvider, secretVault: foundation.secretVault,
		federationLifecycle: federationLifecycle,
	}, nil
}

const sessionTTL = 12 * time.Hour

// liteCapabilities holds the runnable Lite control-plane paths that the
// generic ControlBackend leaves unwired: the API key lifecycle, provider
// credentials, live MCP discovery, projects, the audit trail, and real
// upstream health probes. NewLite wires each capability into the
// ControlBackend's matching slot (wizard setup, playground and the
// federation overlay are wired as plain method values).
type liteCapabilities struct {
	setup          backend.WizardSetupAdapter
	keys           *apikey.Service
	playground     *litePlayground
	tenancy        tenancy.Repository
	tenantAdmin    tenancy.TenantAdmin
	audit          audit.Repository
	auditRetention audit.RetentionStore
	ids            contracts.IDGenerator
	clock          contracts.Clock
	config         *config.Service
	registry       *runtime.ActiveRegistry
	delegation     *sqlrepo.DelegationStore
	cipher         *secrets.Cipher
	vault          *sqlrepo.SecretVault
	egress         *http.Client
	// prober real-probes upstream providers for the Health view (may be nil).
	prober *providerProber
	// auditEvent records sensitive tenant mutations (nil disables auditing).
	auditEvent func(ctx context.Context, tenantID, actorID, action, resourceType, resourceID string) error
}

func (b *liteCapabilities) CreateKey(ctx context.Context, scope tenancy.TenantScope, input apikey.CreateInput, actorID string) (*apikey.CreateResult, error) {
	if input.TenantRef == "" {
		snapshot, ok := b.registry.TenantByID(scope.TenantID)
		if !ok || snapshot.TenantRef == "" {
			return nil, tenancy.ErrNotFound
		}
		input.TenantRef = snapshot.TenantRef
	}
	result, err := b.keys.Create(ctx, scope, input)
	if err != nil {
		return nil, err
	}
	if err := b.refreshTenantRuntime(ctx, scope, actorID); err != nil {
		return nil, fmt.Errorf("%w: %v", backend.ErrRuntimeRefreshFailed, err)
	}
	return result, nil
}

func (b *liteCapabilities) RevokeKey(ctx context.Context, scope tenancy.TenantScope, id string, actorID string) error {
	if err := b.keys.Revoke(ctx, scope, id); err != nil {
		return err
	}
	if err := b.refreshTenantRuntime(ctx, scope, actorID); err != nil {
		// The revocation is persisted; only the data-plane re-activation
		// failed. Report it so the operator knows the key may still be
		// honored until the next reconcile/publish/restart.
		return fmt.Errorf("%w: %v", backend.ErrRuntimeRefreshFailed, err)
	}
	return nil
}

func (b *liteCapabilities) ListKeys(ctx context.Context, scope tenancy.TenantScope) ([]backend.KeyResource, error) {
	records, err := b.keys.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	view := make([]backend.KeyResource, 0, len(records))
	for _, record := range records {
		view = append(view, backend.KeyResource{ID: record.ID, Name: record.Name, ProjectID: record.ProjectID, Fingerprint: record.Fingerprint, Status: record.Status, CreatedAt: record.CreatedAt, Revealable: len(record.KeyCiphertext) > 0})
	}
	return view, nil
}

func (b *liteCapabilities) RevealKey(ctx context.Context, scope tenancy.TenantScope, id string) (string, error) {
	return b.keys.Reveal(ctx, scope, id)
}

func (b *liteCapabilities) Delegations(ctx context.Context, scope tenancy.TenantScope) ([]backend.DelegationGrantView, error) {
	if b.delegation == nil {
		return nil, errors.New("delegation store not wired")
	}
	grants, err := b.delegation.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	views := make([]backend.DelegationGrantView, 0, len(grants))
	for _, grant := range grants {
		views = append(views, delegationView(grant))
	}
	return views, nil
}

func (b *liteCapabilities) GrantDelegation(ctx context.Context, scope tenancy.TenantScope, input backend.DelegationGrantInput, actor string) (backend.DelegationGrantView, error) {
	if b.delegation == nil {
		return backend.DelegationGrantView{}, errors.New("delegation store not wired")
	}
	permissions := compactStrings(input.Permissions)
	if input.DelegatorID == "" || input.DelegateeID == "" || input.DelegatorID == input.DelegateeID || len(permissions) == 0 {
		return backend.DelegationGrantView{}, setup.ErrInvalidInput
	}
	id, err := b.ids.New()
	if err != nil {
		return backend.DelegationGrantView{}, err
	}
	grant := identity.DelegationGrant{ID: id, TenantID: scope.TenantID, DelegatorID: input.DelegatorID, DelegateeID: input.DelegateeID, Permissions: permissions, CreatedBy: actor, CreatedAt: b.clock.Now()}
	stored, err := b.delegation.Put(ctx, scope, grant)
	if err != nil {
		return backend.DelegationGrantView{}, err
	}
	if b.auditEvent != nil {
		_ = b.auditEvent(ctx, scope.TenantID, actor, "delegation.grant", "delegation_grant", stored.ID)
	}
	return delegationView(stored), nil
}

func (b *liteCapabilities) RevokeDelegation(ctx context.Context, scope tenancy.TenantScope, id, actor string) error {
	if b.delegation == nil {
		return errors.New("delegation store not wired")
	}
	ok, err := b.delegation.Delete(ctx, scope, id)
	if err != nil {
		return err
	}
	if !ok {
		return tenancy.ErrNotFound
	}
	if b.auditEvent != nil {
		_ = b.auditEvent(ctx, scope.TenantID, actor, "delegation.revoke", "delegation_grant", id)
	}
	return nil
}

func delegationView(grant identity.DelegationGrant) backend.DelegationGrantView {
	return backend.DelegationGrantView{ID: grant.ID, DelegatorID: grant.DelegatorID, DelegateeID: grant.DelegateeID, Permissions: grant.Permissions, CreatedBy: grant.CreatedBy, CreatedAt: grant.CreatedAt}
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

// refreshTenantRuntime re-compiles the tenant's active published version with
// the latest API keys so creates/revocations take effect on the data plane
// immediately, without creating a new config version. Reconcile failures are
// usually transient lock contention, so it retries briefly before reporting.
func (b *liteCapabilities) refreshTenantRuntime(ctx context.Context, scope tenancy.TenantScope, actorID string) error {
	if b.config == nil || b.registry == nil {
		return nil
	}
	snapshot, ok := b.registry.TenantByID(scope.TenantID)
	if !ok || snapshot.Version == 0 {
		return nil
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 150 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if _, lastErr = b.config.Reconcile(ctx, scope, snapshot.Version, actorID); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// auditTenant records a successful sensitive mutation on the data-plane-adjacent
// backend paths. Failures are logged, not returned: the mutation already
// committed.
func (b *liteCapabilities) auditTenant(ctx context.Context, scope tenancy.TenantScope, actor, action, resourceType, resourceID string) {
	if b.auditEvent == nil || actor == "" {
		return
	}
	if err := b.auditEvent(ctx, scope.TenantID, actor, action, resourceType, resourceID); err != nil {
		log.Printf("lite: audit write failed (%s resource=%s): %v", action, resourceID, err)
	}
}

func (b *liteCapabilities) DiscoverMCPTools(ctx context.Context, scope tenancy.TenantScope, id string) ([]backend.DiscoveredTool, error) {
	snapshot, ok := b.registry.TenantByID(scope.TenantID)
	if !ok {
		return nil, tenancy.ErrNotFound
	}
	var url string
	for _, server := range snapshot.MCPServers() {
		if server.ID == id {
			url = server.URL
			break
		}
	}
	if url == "" {
		return nil, tenancy.ErrNotFound
	}
	connector, err := toolmcp.New(toolmcp.Config{BaseURL: url, Timeout: 10 * time.Second}, b.egress, nil)
	if err != nil {
		return nil, err
	}
	discovered, err := connector.Discover(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]backend.DiscoveredTool, 0, len(discovered))
	for _, tool := range discovered {
		result = append(result, backend.DiscoveredTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	return result, nil
}

func (b *liteCapabilities) CreateCredential(ctx context.Context, scope tenancy.TenantScope, input backend.CredentialCreateInput, actor string) error {
	if input.Status == "" {
		input.Status = "enabled"
	}
	draft, err := b.config.CreateDraft(ctx, scope, actor)
	if err != nil {
		return err
	}
	providerExists := false
	for _, provider := range draft.Config.Providers {
		if provider.ID == input.ProviderID {
			providerExists = true
			break
		}
	}
	if !providerExists {
		return tenancy.ErrNotFound
	}
	credentialID, err := b.ids.New()
	if err != nil {
		return err
	}
	secretRef := "local://credential/" + credentialID
	ciphertext, err := b.cipher.Encrypt([]byte(input.Secret))
	if err != nil {
		return err
	}
	if err := b.vault.Put(ctx, secretRef, scope.TenantID, ciphertext); err != nil {
		return err
	}
	draft.Config.Credentials = append(draft.Config.Credentials, config.Credential{
		ID: credentialID, TenantID: scope.TenantID, ProviderID: input.ProviderID,
		OwnerScope: "TENANT_PRIVATE", SecretRef: secretRef, Status: input.Status,
	})
	updated, err := b.config.UpdateDraft(ctx, scope, draft.ID, draft.Revision, draft.Config, actor)
	if err != nil {
		return err
	}
	_, _, returnErr := b.config.Publish(ctx, scope, updated.ID, updated.Revision, actor)
	if returnErr == nil {
		b.auditTenant(ctx, scope, actor, "credential.create", "provider_credential", credentialID)
	}
	return returnErr
}

func (b *liteCapabilities) RotateCredential(ctx context.Context, scope tenancy.TenantScope, id string, secret []byte, actor string) error {
	snapshot, ok := b.registry.TenantByID(scope.TenantID)
	if !ok {
		return tenancy.ErrNotFound
	}
	credential, ok := snapshot.Credential(id)
	if !ok {
		return tenancy.ErrNotFound
	}
	ciphertext, err := b.cipher.Encrypt(secret)
	if err != nil {
		return err
	}
	// Replace is a single upsert that overwrites the material and marks the
	// previous one rotated atomically; there is no crash window where the
	// reference is left unreadable between a Rotate and a Put.
	if err := b.vault.Replace(ctx, credential.SecretRef, scope.TenantID, ciphertext); err != nil {
		return err
	}
	b.auditTenant(ctx, scope, actor, "credential.rotate", "provider_credential", id)
	return nil
}

func (b *liteCapabilities) DisableCredential(ctx context.Context, scope tenancy.TenantScope, id, actor string) error {
	snapshot, ok := b.registry.TenantByID(scope.TenantID)
	if !ok {
		return tenancy.ErrNotFound
	}
	credential, ok := snapshot.Credential(id)
	if !ok {
		return tenancy.ErrNotFound
	}
	draft, err := b.config.CreateDraft(ctx, scope, actor)
	if err != nil {
		return err
	}
	found := false
	for i := range draft.Config.Credentials {
		if draft.Config.Credentials[i].ID == id {
			draft.Config.Credentials[i].Status = "disabled"
			found = true
			break
		}
	}
	if !found {
		return tenancy.ErrNotFound
	}
	updated, err := b.config.UpdateDraft(ctx, scope, draft.ID, draft.Revision, draft.Config, actor)
	if err != nil {
		return err
	}
	_, _, err = b.config.Publish(ctx, scope, updated.ID, updated.Revision, actor)
	if err != nil {
		return err
	}
	if err := b.vault.Disable(ctx, credential.SecretRef); err != nil {
		return err
	}
	b.auditTenant(ctx, scope, actor, "credential.disable", "provider_credential", id)
	return nil
}

func (b *liteCapabilities) DeleteCredential(ctx context.Context, scope tenancy.TenantScope, id, actor string) error {
	snapshot, ok := b.registry.TenantByID(scope.TenantID)
	if !ok {
		return tenancy.ErrNotFound
	}
	credential, ok := snapshot.Credential(id)
	if !ok {
		return tenancy.ErrNotFound
	}
	for _, deployment := range snapshot.Deployments() {
		if deployment.CredentialID == id {
			return errors.New("credential is still referenced by a model endpoint")
		}
	}
	draft, err := b.config.CreateDraft(ctx, scope, actor)
	if err != nil {
		return err
	}
	filtered := draft.Config.Credentials[:0]
	for _, item := range draft.Config.Credentials {
		if item.ID != id {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == len(draft.Config.Credentials) {
		return tenancy.ErrNotFound
	}
	draft.Config.Credentials = filtered
	updated, err := b.config.UpdateDraft(ctx, scope, draft.ID, draft.Revision, draft.Config, actor)
	if err != nil {
		return err
	}
	if _, _, err := b.config.Publish(ctx, scope, updated.ID, updated.Revision, actor); err != nil {
		return err
	}
	if err := b.vault.Disable(ctx, credential.SecretRef); err != nil {
		return err
	}
	b.auditTenant(ctx, scope, actor, "credential.delete", "provider_credential", id)
	return nil
}

func (b *liteCapabilities) Tenants(ctx context.Context) ([]backend.TenantSummary, error) {
	page, err := b.tenantAdmin.ListTenants(ctx, tenancy.PageRequest{Offset: 0, Limit: 100})
	if err != nil {
		return nil, err
	}
	result := make([]backend.TenantSummary, 0, len(page.Items))
	for _, tenant := range page.Items {
		result = append(result, tenantSummary(tenant))
	}
	return result, nil
}

func (b *liteCapabilities) CreateTenant(ctx context.Context, input backend.TenantCreateInput, actorID string) (backend.TenantSummary, error) {
	if strings.TrimSpace(input.PublicRef) == "" || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.DefaultProjectName) == "" {
		return backend.TenantSummary{}, errors.New("tenant publicRef, name, and defaultProjectName are required")
	}
	tenantID, err := b.ids.New()
	if err != nil {
		return backend.TenantSummary{}, err
	}
	projectID, err := b.ids.New()
	if err != nil {
		return backend.TenantSummary{}, err
	}
	now := b.clock.Now()
	settlementCurrency := strings.ToUpper(strings.TrimSpace(input.SettlementCurrency))
	if settlementCurrency == "" {
		settlementCurrency = "USD"
	}
	enforcement := input.ResidencyEnforcement
	if enforcement == "" {
		enforcement = "advisory"
	}
	tenant := tenancy.Tenant{
		ID: tenantID, PublicRef: strings.TrimSpace(input.PublicRef), Name: strings.TrimSpace(input.Name),
		Status: "active", SettlementCurrency: settlementCurrency, DefaultProjectID: projectID, CreatedAt: now,
	}
	project := tenancy.Project{
		ID: projectID, TenantID: tenantID, Name: strings.TrimSpace(input.DefaultProjectName), Status: "active",
		ResidencyEnforcement: enforcement, AllowedDataRegions: input.AllowedDataRegions, CreatedAt: now,
	}
	if err := b.tenantAdmin.CreateTenantWithDefaultProject(ctx, tenant, project); err != nil {
		return backend.TenantSummary{}, err
	}
	b.auditTenant(ctx, tenancy.TenantScope{TenantID: tenantID}, actorID, "tenant.create", "tenant", tenantID)
	return tenantSummary(tenant), nil
}

func (b *liteCapabilities) UpdateTenantStatus(ctx context.Context, id string, input backend.TenantStatusInput, actorID string) (backend.TenantSummary, error) {
	if err := b.tenantAdmin.UpdateTenantStatus(ctx, id, input.Status); err != nil {
		return backend.TenantSummary{}, err
	}
	scope := tenancy.TenantScope{TenantID: id}
	tenant, err := b.tenancy.GetTenant(ctx, scope)
	if err != nil {
		return backend.TenantSummary{}, err
	}
	b.auditTenant(ctx, scope, actorID, "tenant.status.update", "tenant", id)
	return tenantSummary(*tenant), nil
}

func tenantSummary(tenant tenancy.Tenant) backend.TenantSummary {
	return backend.TenantSummary{
		ID: tenant.ID, PublicRef: tenant.PublicRef, Name: tenant.Name, Status: tenant.Status,
		SettlementCurrency: tenant.SettlementCurrency, DefaultProjectID: tenant.DefaultProjectID, CreatedAt: tenant.CreatedAt,
	}
}

func (b *liteCapabilities) GetAuditRetention(ctx context.Context, scope tenancy.TenantScope) (backend.AuditRetentionView, error) {
	policy, err := b.auditRetention.Get(ctx, scope.TenantID)
	if err != nil {
		return backend.AuditRetentionView{}, err
	}
	return backend.AuditRetentionView{
		TenantID: scope.TenantID, RetentionDays: policy.RetentionDays,
		UpdatedBy: policy.UpdatedBy, UpdatedAt: policy.UpdatedAt,
	}, nil
}

func (b *liteCapabilities) SetAuditRetention(ctx context.Context, scope tenancy.TenantScope, input backend.AuditRetentionInput, actorID string) (backend.AuditRetentionView, error) {
	if err := b.auditRetention.Set(ctx, scope.TenantID, input.RetentionDays, actorID); err != nil {
		return backend.AuditRetentionView{}, err
	}
	b.auditTenant(ctx, scope, actorID, "audit.retention.update", "tenant", scope.TenantID)
	value, err := b.GetAuditRetention(ctx, scope)
	if err != nil {
		return backend.AuditRetentionView{}, err
	}
	return value, nil
}

// purgeExpiredAudit applies every explicit retention policy once: audit events
// older than the configured window are removed. Retention is opt-in, so
// tenants without a policy are untouched.
func (b *liteCapabilities) purgeExpiredAudit(ctx context.Context, now time.Time) (int64, error) {
	policies, err := b.auditRetention.List(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, policy := range policies {
		if policy.RetentionDays <= 0 {
			continue
		}
		removed, err := b.auditRetention.Purge(ctx, policy.TenantID, now.Add(-time.Duration(policy.RetentionDays)*24*time.Hour))
		if err != nil {
			return total, err
		}
		total += removed
	}
	return total, nil
}

func (b *liteCapabilities) Projects(ctx context.Context, scope tenancy.TenantScope) ([]backend.ProjectSummary, error) {
	page, err := b.tenancy.ListProjects(ctx, scope, tenancy.PageRequest{Offset: 0, Limit: 100})
	if err != nil {
		return nil, err
	}
	result := make([]backend.ProjectSummary, 0, len(page.Items))
	for _, project := range page.Items {
		result = append(result, backend.ProjectSummary{
			ID: project.ID, TenantID: project.TenantID, Name: project.Name,
			Status: project.Status, ResidencyEnforcement: project.ResidencyEnforcement,
			AllowedDataRegions: project.AllowedDataRegions,
		})
	}
	return result, nil
}

func (b *liteCapabilities) Audit(ctx context.Context, scope tenancy.TenantScope, limit, offset int) ([]backend.AuditRecord, error) {
	return b.audit.List(ctx, scope.TenantID, limit, offset)
}

// Health overrides the config-derived control-plane health with real upstream
// probes: each provider that backs an enabled deployment is checked against
// its live model catalog with the deployed credential. Lite has no circuit
// breakers, so the circuits list stays empty.
func (b *liteCapabilities) Health(ctx context.Context, scope tenancy.TenantScope) (backend.HealthView, error) {
	snapshot, ok := b.registry.TenantByID(scope.TenantID)
	if !ok || snapshot.Version == 0 {
		return backend.HealthView{Ready: false, Drift: "no_active_snapshot"}, nil
	}
	view := backend.HealthView{Ready: true, Drift: "none"}
	probed := make(map[string]bool, 8)
	probeCredentialFor := func(providerID, providerType, endpoint, secretRef string) (bool, string) {
		if b.prober == nil {
			return true, ""
		}
		return b.prober.Probe(ctx, providerType, endpoint, secretRef)
	}
	for _, deployment := range snapshot.Deployments() {
		if deployment.Status != "enabled" || probed[deployment.ProviderID] {
			continue
		}
		credential, credentialOK := snapshot.Credential(deployment.CredentialID)
		if !credentialOK || credential.Status != "enabled" {
			continue
		}
		provider, providerOK := providerByID(snapshot.Providers(), deployment.ProviderID)
		if !providerOK {
			continue
		}
		probed[provider.ID] = true
		healthy, reason := probeCredentialFor(provider.ID, provider.Type, provider.Endpoint, credential.SecretRef)
		view.Providers = append(view.Providers, backend.ProviderHealth{ID: provider.ID, Type: provider.Type, Healthy: healthy, Reason: reason})
	}
	for _, provider := range snapshot.Providers() {
		if probed[provider.ID] {
			continue
		}
		reason := "no_enabled_credential"
		if provider.Status != "enabled" {
			reason = "provider_disabled"
		}
		view.Providers = append(view.Providers, backend.ProviderHealth{ID: provider.ID, Type: provider.Type, Healthy: false, Reason: reason})
	}
	return view, nil
}

func providerByID(providers []runtime.Provider, id string) (runtime.Provider, bool) {
	for _, provider := range providers {
		if provider.ID == id {
			return provider, true
		}
	}
	return runtime.Provider{}, false
}

func (b *liteCapabilities) CreateProject(ctx context.Context, scope tenancy.TenantScope, input backend.ProjectCreateInput) (backend.ProjectSummary, error) {
	if input.Name == "" {
		return backend.ProjectSummary{}, errors.New("project name is required")
	}
	enforcement := input.ResidencyEnforcement
	if enforcement == "" {
		enforcement = "advisory"
	}
	id, err := b.ids.New()
	if err != nil {
		return backend.ProjectSummary{}, err
	}
	project := tenancy.Project{
		ID: id, TenantID: scope.TenantID, Name: input.Name, Status: "active",
		ResidencyEnforcement: enforcement, AllowedDataRegions: input.AllowedDataRegions,
		CreatedAt: b.clock.Now(),
	}
	if err := b.tenancy.CreateProject(ctx, scope, project); err != nil {
		return backend.ProjectSummary{}, err
	}
	return backend.ProjectSummary{
		ID: project.ID, TenantID: project.TenantID, Name: project.Name,
		Status: project.Status, ResidencyEnforcement: project.ResidencyEnforcement,
		AllowedDataRegions: project.AllowedDataRegions,
	}, nil
}

// liteCircuits is a no-op circuit viewer for the Lite profile.
type liteCircuits struct{}

func (liteCircuits) State(string, string) string { return "" }
func (liteCircuits) Reset(string, string)        {}

// newBootstrapToken mints the one-time setup gate token (32 random bytes, hex).
func newBootstrapToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// newOIDCLogin builds the optional OIDC login path from LiteOptions. A nil or
// Issuer-less option returns (nil, nil) so the Console falls back to local
// login only. A partially configured option is a composition error.
func newOIDCLogin(sessions *adminapi.SessionManager, o *OIDCOptions, egressClient *http.Client) (*adminapi.OIDCLogin, error) {
	if o == nil || o.Issuer == "" {
		return nil, nil
	}
	audience := o.Audience
	if audience == "" {
		audience = o.ClientID
	}
	verifier := identityoidc.NewJWKSVerifier(egressClient, o.Issuer, time.Minute)
	exchanger := adminapi.HTTPTokenExchanger{Client: egressClient, TokenEndpoint: o.TokenEndpoint, ClientID: o.ClientID}
	return adminapi.NewOIDCLogin(sessions, verifier, exchanger, adminapi.OIDCLoginConfig{
		AuthorizationEndpoint: o.AuthorizationEndpoint,
		ClientID:              o.ClientID,
		Audience:              audience,
		RedirectURI:           o.RedirectURI,
		StateTTL:              o.StateTTL,
		Roles:                 identityoidc.RoleClaims{Admins: o.AdminSubjects, Operators: o.OperatorSubjects},
	})
}

// buildLiteHandler assembles the root engine: Admin API under /api/admin/
// (with its session plugin), the public setup status probe, and the Console
// SPA as the fallback.
func buildLiteHandler(server *adminapi.Server, sessionEndpoints adminapi.SessionEndpoints, store *sqlrepo.Store, bundleHub *bundle.Hub, bundleToken string) (*webkit.Engine, error) {
	root := webkit.New()
	sessionEndpoints.Register(server.Engine())
	root.HandleHTTP("/api/admin/", server)
	// More specific than the /api/admin/ subtree pattern, so it wins.
	root.Handle("GET /api/admin/setup/status", setupStatus(store))
	// Dependency-free Prometheus exposition over the request ledger.
	root.Handle("GET /metrics", liteMetrics(store))
	// Signed RuntimeBundle delivery (Stage 11). Only mounted when the tenant
	// runtime is bundled (Control Plane signs); the token keeps it machine-only.
	if bundleHub != nil && bundleToken != "" {
		handler := bundle.NewHandler(bundleHub, bundleToken)
		root.HandleHTTP("GET /api/runtime/bundles/{tenantRef}", handler)
		root.HandleHTTP("GET /api/runtime/bundles", handler)
	}
	console, err := newConsoleHandler()
	if err != nil {
		return nil, err
	}
	root.Handle("/", console)
	return root, nil
}

// setupStatus reports whether the one-time bootstrap already ran, so the
// Console can show an initialization summary instead of the wizard. It is
// public (no session): it mirrors what the login/setup screens already expose
// on a local single-tenant console.
func setupStatus(store *sqlrepo.Store) webkit.Handler {
	return func(c *webkit.Context) error {
		ctx := c.Request().Context()
		out := struct {
			Initialized   bool   `json:"initialized"`
			Ready         bool   `json:"ready"`
			TenantName    string `json:"tenantName,omitempty"`
			AdminUsername string `json:"adminUsername,omitempty"`
		}{}
		if tenant, err := store.Tenancy.FirstTenant(ctx); err == nil && tenant != nil {
			out.Initialized = true
			out.TenantName = tenant.Name
			// An install is "ready" only once a first config version is
			// published. A half-initialized state (admin created, no publish)
			// must not report success: the wizard is retryable in that state.
			if versions, err := store.Config.ListVersions(ctx, tenancy.TenantScope{TenantID: tenant.ID}); err == nil {
				out.Ready = len(versions) > 0
			}
			if users, err := store.LocalCredentials.ListLocalUsers(ctx); err == nil && len(users) > 0 {
				out.AdminUsername = users[0].Username
			}
		}
		return c.JSON(http.StatusOK, out)
	}
}

// newConsoleHandler serves the embedded Console SPA with a fallback to
// index.html for client-side routes.
func newConsoleHandler() (webkit.Handler, error) {
	sub, err := fs.Sub(console.Assets, "dist")
	if err != nil {
		return nil, fmt.Errorf("open console assets: %w", err)
	}
	files := http.FileServer(http.FS(sub))
	return func(c *webkit.Context) error {
		r := c.Request()
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if path == "index.html" || path == "sw.js" {
			c.Header().Set("Cache-Control", "no-store")
		}
		if _, err := fs.Stat(sub, path); err == nil {
			files.ServeHTTP(c.Response(), r)
			return nil
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			return c.Text(http.StatusNotFound, "console not built\n")
		}
		c.Header().Set("Content-Type", "text/html; charset=utf-8")
		c.Header().Set("Cache-Control", "no-store")
		_, err = c.Response().Write(index)
		return err
	}, nil
}

// helpers below satisfy the Backend read surfaces not backed by a repository.

func listSecurityEvents(ctx context.Context, store guardrail.SecurityEventStore, scope tenancy.TenantScope, limit int) ([]backend.SecurityEventView, error) {
	if store == nil {
		return nil, errors.New("security event store not available")
	}
	events, err := store.List(ctx, scope, limit)
	if err != nil {
		return nil, err
	}
	result := make([]backend.SecurityEventView, 0, len(events))
	for _, event := range events {
		result = append(result, backend.SecurityEventView{
			ID: event.ID, RuleID: event.RuleID, Action: event.Action, Occurred: event.OccurredAt,
		})
	}
	return result, nil
}

func listToolCalls(ctx context.Context, store *sqlrepo.ToolCallStore, scope tenancy.TenantScope, limit int) ([]backend.ToolCallView, error) {
	if store == nil {
		return nil, errors.New("tool call store not available")
	}
	events, err := store.List(ctx, scope, limit)
	if err != nil {
		return nil, err
	}
	result := make([]backend.ToolCallView, 0, len(events))
	for _, event := range events {
		result = append(result, backend.ToolCallView{
			ID: event.ID, ToolName: event.ToolName, SessionID: event.SessionID,
			TaskID: event.TaskID, AgentID: event.AgentID, Occurred: event.OccurredAt,
		})
	}
	return result, nil
}

// guardrailAuditRecorder records fast guardrail publishes in the config audit
// trail so the security review surface sees them alongside config publishes.
type guardrailAuditRecorder struct {
	repo  config.Repository
	ids   contracts.IDGenerator
	clock contracts.Clock
}

func (r *guardrailAuditRecorder) RecordGuardrailPublish(ctx context.Context, scope tenancy.TenantScope, policy guardrail.Policy, change guardrail.ChangeType, actor string) error {
	id, err := r.ids.New()
	if err != nil {
		return err
	}
	return r.repo.Audit(ctx, scope, config.AuditRecord{ID: id, ActorID: actor, Action: "guardrail.fast_publish", ResourceID: policy.ID, Version: policy.Version, OccurredAt: r.clock.Now()})
}
