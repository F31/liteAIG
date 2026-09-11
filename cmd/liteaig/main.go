package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/F31/liteAIG/internal/app"
	"github.com/F31/liteAIG/internal/platform/bundle"
)

var version = "dev"

// bundleSchemaVersion is the RuntimeBundle schema written by the Control Plane
// signer in split mode.
const bundleSchemaVersion = "v1"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run starts the runtime and blocks until a termination signal triggers drain.
// Tests may inject a signal channel (e.g. an already-closed channel) via
// signals to exercise startup without blocking or binding a real port.
func run(args []string, stdout, stderr io.Writer, signals ...<-chan struct{}) int {
	flags := flag.NewFlagSet("liteaig", flag.ContinueOnError)
	flags.SetOutput(stderr)
	modeValue := flags.String("mode", string(app.ModeAll), "runtime mode: all, gateway, or control")
	readyAddr := flags.String("ready-addr", ":8080", "address for the readiness server")
	adminAddr := flags.String("admin-addr", "127.0.0.1:8081", "address for the Lite management plane (admin API + Console); defaults to loopback because it carries credentials")
	gatewayAddr := flags.String("gateway-addr", ":8082", "address for the Lite data plane gateway")
	gatewayRPM := flags.Int("gateway-rpm", 0, "max gateway requests per tenant+project per minute (default 600)")
	gatewayBurst := flags.Int("gateway-burst", 0, "gateway token-bucket burst (default: equal to --gateway-rpm)")
	gatewayTPM := flags.Int64("gateway-tpm", 0, "max tokens per tenant+project per minute (0 disables TPM metering)")
	// RuntimeBundle delivery (Stage 11): the Control Plane signs every published
	// snapshot; split mode=gateway processes pull signed bundles from a remote
	// Control Plane over the authenticated endpoint.
	bundleSigningKey := flags.String("bundle-signing-key", "", "hex Ed25519 private key used by the Control Plane to sign RuntimeBundles")
	bundleToken := flags.String("bundle-token", "", "shared secret that authenticates Data Plane bundle pulls")
	controlURL := flags.String("control-url", "", "Control Plane base URL for split mode=gateway bundle pulls (http://host:port)")
	bundlePublicKey := flags.String("bundle-public-key", "", "hex Ed25519 public key used by a mode=gateway process to verify Control Plane bundles")
	coordinatorURL := flags.String("coordinator", "", "Redis/Valkey coordinator URL (redis://host:port/db) for Standard tier budget ledger, concurrency leases, and inflight counters; empty keeps the in-memory Lite implementations")
	egressAllowCIDRs := flags.String("egress-allow-cidrs", "", "comma-separated private/internal networks reachable by upstream connectors (self-hosted Kubernetes in-cluster providers); all other SSRF guardrails stay enabled")
	dbDsn := flags.String("db", "", "SQLite DSN for the Lite management plane (empty disables the management plane)")
	oidcIssuer := flags.String("oidc-issuer", "", "OIDC issuer URL for SSO; empty keeps local login only (JWKS is fetched from <issuer>/.well-known/jwks.json)")
	oidcClientID := flags.String("oidc-client-id", "", "OIDC public client id")
	oidcAudience := flags.String("oidc-audience", "", "expected id_token audience (default: client id)")
	oidcAuthzEndpoint := flags.String("oidc-authz-endpoint", "", "OIDC authorization endpoint")
	oidcTokenEndpoint := flags.String("oidc-token-endpoint", "", "OIDC token endpoint")
	oidcRedirectURI := flags.String("oidc-redirect-uri", "", "OIDC redirect URI (default: <admin base URL>/login)")
	oidcAdminSubjects := flags.String("oidc-admin-subjects", "", "comma-separated subject allow-list mapped to tenant_admin")
	oidcOperatorSubjects := flags.String("oidc-operator-subjects", "", "comma-separated subject allow-list mapped to tenant_operator")
	oidcStateTTL := flags.Duration("oidc-state-ttl", 0, "PKCE state lifetime (default 5m)")
	analyticsBaseURL := flags.String("analytics-clickhouse-url", "", "ClickHouse HTTP endpoint for the analytics sink (e.g. http://localhost:8123); empty disables the sink")
	analyticsTable := flags.String("analytics-clickhouse-table", "", "ClickHouse table for the analytics sink (e.g. liteaig.analytics_events)")
	otlpTracesEndpoint := flags.String("otlp-traces-endpoint", "", "OTLP/HTTP traces endpoint; empty falls back to LITEAIG_OTLP_TRACES_ENDPOINT or LITEAIG_OTLP_ENDPOINT")
	otlpMetricsEndpoint := flags.String("otlp-metrics-endpoint", "", "OTLP/HTTP metrics endpoint; empty falls back to LITEAIG_OTLP_METRICS_ENDPOINT")
	otlpLogsEndpoint := flags.String("otlp-logs-endpoint", "", "OTLP/HTTP logs endpoint; empty falls back to LITEAIG_OTLP_LOGS_ENDPOINT")
	webhookURL := flags.String("webhook-url", "", "alert/approval/guardrail metadata webhook via Lite egress policy; empty disables notifications (requires --db)")
	smtpHost := flags.String("smtp-host", "", "SMTP relay host for the email password-reset flow; empty disables email reset")
	smtpPort := flags.Int("smtp-port", 587, "SMTP relay port (587 STARTTLS, 465 implicit TLS)")
	smtpUser := flags.String("smtp-user", "", "SMTP authentication username (empty for a relay without auth)")
	smtpPassword := flags.String("smtp-password", "", "SMTP authentication password")
	smtpFrom := flags.String("smtp-from", "", "From address for reset-code emails")
	smtpAlertTo := flags.String("smtp-alert-to", "", "recipient for alert notification emails; empty disables the email alert channel")
	smtpTLSMode := flags.String("smtp-tls-mode", "starttls", "SMTP transport security: starttls, implicit, or none (local relay)")
	drainTimeout := flags.Duration("drain-timeout", 0, "graceful drain timeout (default 30s)")
	streamDrainTimeout := flags.Duration("stream-drain-timeout", 0, "long-stream drain timeout (default 60s)")
	forceShutdownTimeout := flags.Duration("force-shutdown-timeout", 0, "hard cap on drain (default 70s)")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "liteaig %s\n", version)
		return 0
	}

	mode, err := app.ParseMode(*modeValue)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	plan := app.Compose(mode)
	if *webhookURL != "" && *dbDsn == "" {
		fmt.Fprintln(stderr, "webhook-url requires --db")
		return 2
	}
	if plan.Control && !isLoopbackAddr(*adminAddr) {
		fmt.Fprintf(stderr, "warning: admin-addr %s is reachable off-loopback; the Console exposes credentials and the setup wizard is gated by a bootstrap token — prefer 127.0.0.1 with a reverse proxy\n", *adminAddr)
	}

	config := app.DrainConfig{
		DrainTimeout:         *drainTimeout,
		StreamDrainTimeout:   *streamDrainTimeout,
		ForceShutdownTimeout: *forceShutdownTimeout,
	}
	lifecycle, err := app.NewLifecycle(config, app.NewStreamRegistry(), nil)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	// runtimeReady gates /readyz on the data plane having a loaded tenant
	// runtime. liteRef is written after composition (below) and read
	// concurrently by the readiness server, so it must be atomic.
	var liteRef atomic.Pointer[app.Lite]
	runtimeReady := func() bool {
		if !plan.Gateway {
			return true
		}
		value := liteRef.Load()
		return value == nil || value.HasActiveRuntime()
	}
	readiness := app.NewReadinessServer(*readyAddr, lifecycle, runtimeReady)
	go func() {
		if serveErr := readiness.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			fmt.Fprintln(stderr, serveErr)
		}
	}()

	var lite *app.Lite
	var admin *http.Server
	var gateway *http.Server
	if *dbDsn != "" && (plan.Control || plan.Gateway) {
		var composeErr error
		var oidcOptions *app.OIDCOptions
		if *oidcIssuer != "" {
			redirectURI := *oidcRedirectURI
			if redirectURI == "" {
				redirectURI = publicBaseURL(*adminAddr) + "/login"
			}
			oidcOptions = &app.OIDCOptions{
				Issuer:                *oidcIssuer,
				ClientID:              *oidcClientID,
				Audience:              *oidcAudience,
				AuthorizationEndpoint: *oidcAuthzEndpoint,
				TokenEndpoint:         *oidcTokenEndpoint,
				RedirectURI:           redirectURI,
				StateTTL:              *oidcStateTTL,
				AdminSubjects:         splitCommaList(*oidcAdminSubjects),
				OperatorSubjects:      splitCommaList(*oidcOperatorSubjects),
			}
		}
		var analyticsOptions *app.AnalyticsOptions
		if *analyticsBaseURL != "" && *analyticsTable != "" {
			analyticsOptions = &app.AnalyticsOptions{BaseURL: *analyticsBaseURL, Table: *analyticsTable}
		}
		var mailOptions *app.MailOptions
		if *smtpHost != "" {
			mailOptions = &app.MailOptions{
				Host: *smtpHost, Port: *smtpPort, Username: *smtpUser, Password: *smtpPassword,
				From: *smtpFrom, TLSMode: *smtpTLSMode, AlertTo: *smtpAlertTo,
			}
		}
		var signer *bundle.Signer
		if *bundleSigningKey != "" {
			parsed, parseErr := bundle.NewSignerFromHex(*bundleSigningKey, bundleSchemaVersion, 1)
			if parseErr != nil {
				fmt.Fprintln(stderr, "lite: bundle signing key:", parseErr)
				_ = readiness.Close()
				return 2
			}
			signer = parsed
		}
		lite, composeErr = app.NewLite(context.Background(), app.LiteOptions{
			DSN:                 *dbDsn,
			AdminAddr:           *adminAddr,
			GatewayAddr:         *gatewayAddr,
			BaseURL:             publicBaseURL(*gatewayAddr),
			Lifecycle:           lifecycle,
			OIDC:                oidcOptions,
			Analytics:           analyticsOptions,
			WebhookURL:          *webhookURL,
			Mail:                mailOptions,
			OTLPTracesEndpoint:  *otlpTracesEndpoint,
			OTLPMetricsEndpoint: *otlpMetricsEndpoint,
			OTLPLogsEndpoint:    *otlpLogsEndpoint,
			GatewayRPM:          *gatewayRPM,
			GatewayBurst:        *gatewayBurst,
			GatewayTPM:          *gatewayTPM,
			BundleSigner:        signer,
			BundleToken:         *bundleToken,
			ControlURL:          *controlURL,
			BundlePublicKeyHex:  *bundlePublicKey,
			CoordinatorURL:      *coordinatorURL,
			EgressAllowCIDRs:    splitCommaList(*egressAllowCIDRs),
		})
		if composeErr != nil {
			fmt.Fprintln(stderr, "lite:", composeErr)
			_ = readiness.Close()
			return 2
		}
		liteRef.Store(lite)
		if plan.Control {
			admin = &http.Server{
				Addr:              *adminAddr,
				Handler:           lite.Handler(),
				ReadHeaderTimeout: 5 * time.Second,
			}
			go func() {
				if serveErr := admin.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
					fmt.Fprintln(stderr, serveErr)
				}
			}()
		}
		if plan.Gateway {
			gateway = &http.Server{
				Addr:              *gatewayAddr,
				Handler:           lite.GatewayHandler(),
				ReadHeaderTimeout: 5 * time.Second,
			}
			go func() {
				if serveErr := gateway.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
					fmt.Fprintln(stderr, serveErr)
				}
			}()
		}
	}

	fmt.Fprintf(stdout, "liteaig %s mode=%s gateway=%t control=%t ready-addr=%s admin-addr=%s gateway-addr=%s management=%t oidc=%t analytics=%t mail=%t coordinator=%t\n", version, plan.Mode, plan.Gateway, plan.Control, *readyAddr, *adminAddr, *gatewayAddr, admin != nil, *oidcIssuer != "", *analyticsBaseURL != "" && *analyticsTable != "", *smtpHost != "", *coordinatorURL != "")

	var signalChannel <-chan struct{}
	if len(signals) > 0 {
		signalChannel = signals[0]
	} else {
		signalChannel = installSignalChannel()
	}
	if err := lifecycle.WaitForSignal(context.Background(), signalChannel); err != nil {
		fmt.Fprintln(stderr, "drain:", err)
		return 1
	}
	if admin != nil {
		_ = admin.Close()
	}
	if gateway != nil {
		_ = gateway.Close()
	}
	if lite != nil {
		_ = lite.Close()
	}
	_ = readiness.Close()
	return 0
}

func publicBaseURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}

func splitCommaList(value string) []string {
	var result []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func installSignalChannel() <-chan struct{} {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	done := make(chan struct{}, 1)
	go func() {
		<-signals
		signal.Stop(signals)
		close(done)
	}()
	return done
}

// isLoopbackAddr reports whether a listen address binds only to loopback.
// Wildcards (:addr, 0.0.0.0, ::, *) bind every interface and are not loopback.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" || host == "::" || host == "*" || host == "0.0.0.0" || host == "[::]" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
