package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/access/protocol/openai"
	"github.com/F31/liteAIG/internal/federation"
	gatewayserver "github.com/F31/liteAIG/internal/gateway/server"
	"github.com/F31/liteAIG/internal/kernel"
	"github.com/F31/liteAIG/internal/kernel/contracts"
	"github.com/F31/liteAIG/internal/kernel/interaction"
	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/internal/platform/storage/sqlrepo"
	"github.com/F31/liteAIG/internal/tenancy"
	"github.com/F31/liteAIG/migrations"
)

// concurrencyStubCore satisfies the gateway Core surface without doing any
// governance work; the concurrent drain path never touches it.
type concurrencyStubCore struct{}

func (concurrencyStubCore) Admit(context.Context, gatewayserver.AdmitInput) (*kernel.RequestContext, error) {
	return nil, nil
}
func (concurrencyStubCore) Run(context.Context, *kernel.RequestContext, contracts.StreamWriter) error {
	return nil
}
func (concurrencyStubCore) Models(context.Context, string, string) (*openai.ModelsResponse, error) {
	return &openai.ModelsResponse{}, nil
}
func (concurrencyStubCore) Tool(context.Context, gatewayserver.AdmitInput) (*interaction.UnifiedResponse, error) {
	return &interaction.UnifiedResponse{}, nil
}

// TestA2APushWorkerTwoRacingWorkersDeliverExactlyOnce approximates the
// Standard-tier multi-process smoke inside the repo: two singleton workers
// sharing one SQL database drain a common push outbox. The singleton lease
// (`platform:a2a_push_outbox`) plus the outbox-level `pending -> sending`
// atomic claim must guarantee every callback is delivered exactly once, with no
// duplicate delivery ids observed by the callback receiver.
func TestA2APushWorkerTwoRacingWorkersDeliverExactlyOnce(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "a2a-concurrent.db")
	db, err := sqlite.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := sqlrepo.Open(db, sqlrepo.Deps{})
	tenant := "tenant-concurrent"

	tenantScope := tenancy.TenantScope{TenantID: tenant}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO tenants(id, public_ref, name) VALUES (?, ?, ?)`, tenant, tenant, tenant); err != nil {
		t.Fatal(err)
	}
	const deliveries = 20
	for i := 0; i < deliveries; i++ {
		taskID := fmt.Sprintf("task-%02d", i)
		if err := store.A2ATask.Create(context.Background(), tenantScope, sqlrepo.A2ATask{
			TaskID: taskID, TenantID: tenant, ExternalAgentID: "ext", ProjectID: "project-1",
			RequestID: taskID, IdempotencyKey: "key-" + taskID, Status: sqlrepo.A2ATaskCompleted,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var hits atomic.Int32
	var mu sync.Mutex
	seen := map[string]int{}
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		id := r.Header.Get("X-LiteAIG-A2A-Push-ID")
		mu.Lock()
		seen[id]++
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(callback.Close)
	for i := 0; i < deliveries; i++ {
		if err := store.A2APushOutbox.EnqueueForTask(context.Background(), federation.A2APushDelivery{
			ID: fmt.Sprintf("push-%02d", i), TaskID: fmt.Sprintf("task-%02d", i),
			CallbackURL: callback.URL, Payload: []byte(`{"ok":true}`), MaxAttempts: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Both workers share the same SQL store (== same outbox) and the same lease
	// table, mirroring two processes racing over one database.
	newWorkerServer := func() *gatewayserver.Server {
		return gatewayserver.New(concurrencyStubCore{}, gatewayserver.Config{
			A2APushOutbox: store.A2APushOutbox,
		})
	}
	serverA := newWorkerServer()
	serverB := newWorkerServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerA := &singletonTask{leases: store.Coordination, scope: platformA2APushScope, ttl: singletonLeaseTTL}
	workerB := &singletonTask{leases: store.Coordination, scope: platformA2APushScope, ttl: singletonLeaseTTL}
	go workerA.run(ctx, 20*time.Millisecond, func(ctx context.Context) error { serverA.DrainA2APushOutbox(ctx); return nil })
	go workerB.run(ctx, 20*time.Millisecond, func(ctx context.Context) error { serverB.DrainA2APushOutbox(ctx); return nil })

	deadline := time.Now().Add(10 * time.Second)
	for hits.Load() < deliveries && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)

	if got := hits.Load(); got != deliveries {
		t.Fatalf("callback hits = %d, want %d", got, deliveries)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != deliveries {
		t.Fatalf("unique delivery ids = %d, want %d (seen %d)", len(seen), deliveries, hits.Load())
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("delivery %q delivered %d times, want exactly once", id, count)
		}
	}
	summary, err := store.A2APushOutbox.Summary(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Delivered != deliveries || summary.Pending != 0 || summary.Sending != 0 || summary.Failed != 0 {
		t.Fatalf("final outbox summary = %+v, want %d delivered", summary, deliveries)
	}
}
