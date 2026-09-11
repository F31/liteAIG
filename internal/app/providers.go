package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/F31/liteAIG/internal/connectors/model/anthropic"
	"github.com/F31/liteAIG/internal/connectors/model/azure"
	"github.com/F31/liteAIG/internal/connectors/model/bedrock"
	"github.com/F31/liteAIG/internal/connectors/model/gemini"
	"github.com/F31/liteAIG/internal/connectors/model/openai"
	"github.com/F31/liteAIG/internal/connectors/model/vertex"
	"github.com/F31/liteAIG/internal/gateway/execution"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/kernel/runtime"
	"github.com/F31/liteAIG/internal/platform/egress"
	"github.com/F31/liteAIG/internal/platform/secrets"
)

// providerResolver builds real upstream model connectors per deployment from
// the runtime snapshot (provider endpoint/type + credential secret ref) and
// caches them. No fake invokers: an unresolvable deployment is a routing
// exclusion, not a silent success.
type providerResolver struct {
	http    *http.Client
	secrets secrets.Provider
	policy  egress.Policy

	mu       sync.Mutex
	invokers map[string]contracts.InteractionInvoker
}

func newProviderResolver(client *http.Client, secrets secrets.Provider, policy egress.Policy) *providerResolver {
	if client == nil {
		client = &http.Client{}
	}
	return &providerResolver{http: client, secrets: secrets, policy: policy, invokers: map[string]contracts.InteractionInvoker{}}
}

func (r *providerResolver) Resolve(snapshot *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment) (contracts.InteractionInvoker, bool) {
	return r.ResolveCredential(snapshot, deployment, deployment.CredentialID)
}

func (r *providerResolver) ResolveCredential(snapshot *runtime.TenantRuntimeSnapshot, deployment runtime.Deployment, credentialID string) (contracts.InteractionInvoker, bool) {
	if snapshot == nil {
		return nil, false
	}
	provider, ok := snapshot.Provider(deployment.ProviderID)
	if !ok || provider.Status != "enabled" || provider.Endpoint == "" {
		return nil, false
	}
	credential, ok := snapshot.Credential(credentialID)
	if !ok || credential.Status != "enabled" || credential.SecretRef == "" {
		return nil, false
	}

	cacheKey := deployment.ID + ":" + credentialID + ":" + provider.Endpoint + ":" + deployment.UpstreamModel
	r.mu.Lock()
	if invoker, cached := r.invokers[cacheKey]; cached {
		r.mu.Unlock()
		return invoker, true
	}
	r.mu.Unlock()

	invoker, err := r.build(provider, credential)
	if err != nil {
		return nil, false
	}
	invoker = deploymentModelInvoker{inner: invoker, upstreamModel: deployment.UpstreamModel}
	r.mu.Lock()
	r.invokers[cacheKey] = invoker
	r.mu.Unlock()
	return invoker, true
}

func (r *providerResolver) build(provider runtime.Provider, credential runtime.Credential) (contracts.InteractionInvoker, error) {
	if err := egress.ValidateTarget(provider.Endpoint, r.policy); err != nil {
		return nil, err
	}
	switch provider.Type {
	case "openai", "openai-compatible":
		config := openai.DefaultConfig()
		config.BaseURL = provider.Endpoint
		config.SecretRef = credential.SecretRef
		config.AllowInsecureHTTP = allowsInsecureHTTP(provider.Endpoint)
		return openai.New(config, r.http, r.secrets)
	case "anthropic":
		config := anthropic.DefaultConfig()
		config.BaseURL = provider.Endpoint
		config.SecretRef = credential.SecretRef
		config.AllowInsecureHTTP = allowsInsecureHTTP(provider.Endpoint)
		return anthropic.New(config, r.http, r.secrets)
	case "azure-openai":
		config := azure.DefaultConfig()
		config.BaseURL = provider.Endpoint
		config.SecretRef = credential.SecretRef
		config.AllowInsecureHTTP = allowsInsecureHTTP(provider.Endpoint)
		return azure.New(config, r.http, r.secrets)
	case "bedrock":
		region := regionFromEndpoint(provider.Endpoint, "bedrock-runtime")
		config := bedrock.DefaultConfig()
		config.BaseURL = provider.Endpoint
		config.Region = region
		config.SecretRef = credential.SecretRef
		config.AllowInsecureHTTP = allowsInsecureHTTP(provider.Endpoint)
		return bedrock.New(config, r.http, r.secrets)
	case "vertex":
		config := vertex.DefaultConfig()
		config.BaseURL = provider.Endpoint
		config.Location = locationFromEndpoint(provider.Endpoint)
		config.SecretRef = credential.SecretRef
		config.AllowInsecureHTTP = allowsInsecureHTTP(provider.Endpoint)
		return vertex.New(config, r.http, r.secrets)
	case "gemini":
		config := gemini.DefaultConfig()
		config.BaseURL = provider.Endpoint
		config.SecretRef = credential.SecretRef
		config.AllowInsecureHTTP = allowsInsecureHTTP(provider.Endpoint)
		return gemini.New(config, r.http, r.secrets)
	default:
		return nil, fmt.Errorf("unsupported provider type %q", provider.Type)
	}
}

// regionFromEndpoint extracts the AWS region from a Bedrock Runtime endpoint
// host (https://bedrock-runtime.{region}.amazonaws.com), falling back to
// us-east-1 when the host is not in the expected shape.
func regionFromEndpoint(endpoint, servicePrefix string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "us-east-1"
	}
	host := parsed.Hostname()
	prefix := servicePrefix + "."
	if strings.HasPrefix(host, prefix) {
		remainder := strings.TrimPrefix(host, prefix)
		if index := strings.Index(remainder, "."); index > 0 {
			return remainder[:index]
		}
		if remainder != "" {
			return remainder
		}
	}
	return "us-east-1"
}

// locationFromEndpoint extracts the Vertex location from the regional
// aiplatform endpoint host (https://{location}-aiplatform.googleapis.com),
// falling back to us-central1.
func locationFromEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "us-central1"
	}
	host := parsed.Hostname()
	if strings.HasSuffix(host, "-aiplatform.googleapis.com") {
		location := strings.TrimSuffix(host, "-aiplatform.googleapis.com")
		if location != "" {
			return location
		}
	}
	return "us-central1"
}

// allowsInsecureHTTP permits plain HTTP only for loopback endpoints (local
// development and test providers); external providers must be HTTPS.
func allowsInsecureHTTP(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type deploymentModelInvoker struct {
	inner         contracts.InteractionInvoker
	upstreamModel string
}

func (i deploymentModelInvoker) Invoke(ctx context.Context, request contracts.InvocationRequest) (*contracts.InvocationResponse, error) {
	request.Request = upstreamRequest(request.Request, i.upstreamModel)
	return i.inner.Invoke(ctx, request)
}

func (i deploymentModelInvoker) Stream(ctx context.Context, request contracts.InvocationRequest, writer contracts.StreamWriter) error {
	request.Request = upstreamRequest(request.Request, i.upstreamModel)
	return i.inner.Stream(ctx, request, writer)
}

func (i deploymentModelInvoker) Health(ctx context.Context, target contracts.TargetRef) contracts.HealthStatus {
	return i.inner.Health(ctx, target)
}

func (i deploymentModelInvoker) Capabilities(ctx context.Context, target contracts.TargetRef) contracts.CapabilitySet {
	return i.inner.Capabilities(ctx, target)
}

func (i deploymentModelInvoker) NormalizeError(err error) *contracts.UpstreamError {
	return i.inner.NormalizeError(err)
}

func upstreamRequest(request *interaction.UnifiedRequest, model string) *interaction.UnifiedRequest {
	if request == nil || model == "" {
		return request
	}
	clone := *request
	clone.Model = model
	return &clone
}

var _ execution.Resolver = (*providerResolver)(nil)
var _ execution.CredentialResolver = (*providerResolver)(nil)
