package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/postgres"
	"github.com/F31/liteAIG/migrations"
)

func TestA2APushWorkerTwoGatewayProcessesPostgresRedisDeliversExactlyOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Standard-tier process smoke under -short")
	}
	dsn := os.Getenv("LITEAIG_TEST_POSTGRES_DSN")
	redisURL := os.Getenv("LITEAIG_TEST_REDIS_URL")
	if redisURL == "" && os.Getenv("LITEAIG_TEST_REDIS_ADDR") != "" {
		redisURL = "redis://" + os.Getenv("LITEAIG_TEST_REDIS_ADDR") + "/0"
	}
	if dsn == "" || redisURL == "" {
		t.Skip("LITEAIG_TEST_POSTGRES_DSN and LITEAIG_TEST_REDIS_URL/ADDR are required")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LITEAIG_MASTER_KEY", base64.StdEncoding.EncodeToString(key))
	bin, cleanupBin := buildLiteaigBinary(t)
	defer cleanupBin()

	const deliveries = 16
	total := &atomic.Int32{}
	var mu sync.Mutex
	seen := map[string]int{}
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total.Add(1)
		id := r.Header.Get("X-LiteAIG-A2A-Push-ID")
		mu.Lock()
		seen[id]++
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(callback.Close)
	seedPostgresOutbox(t, dsn, callback.URL, deliveries)

	cmdA := startLiteGatewayProcess(t, bin, dsn, redisURL, "18301", "18302")
	cmdB := startLiteGatewayProcess(t, bin, dsn, redisURL, "18311", "18312")
	t.Cleanup(func() { stopLiteProcess(t, cmdA, "standard-a"); stopLiteProcess(t, cmdB, "standard-b") })

	deadline := time.Now().Add(30 * time.Second)
	for total.Load() < deliveries && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if got := total.Load(); got != deliveries {
		t.Fatalf("callback hits = %d, want %d (stderr A: %s\nstderr B: %s)", got, deliveries, tail(cmdA), tail(cmdB))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != deliveries {
		t.Fatalf("unique delivery ids = %d, want %d", len(seen), deliveries)
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("delivery %q delivered %d times, want exactly once", id, count)
		}
	}
}

func seedPostgresOutbox(t *testing.T, dsn, callbackURL string, n int) {
	t.Helper()
	ctx := context.Background()
	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	tenantID := "00000000-0000-0000-0000-00000000a2a1"
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id, public_ref, name) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, tenantID, "standard-a2a", "standard-a2a"); err != nil {
		t.Fatal(err)
	}
	payload := `{"jsonrpc":"2.0","id":"1","messageId":"m","result":{"message":{"parts":[{"kind":"text","text":"standard"}]}},"task":{"status":"completed"}}`
	for i := 0; i < n; i++ {
		taskID := fmt.Sprintf("standard-task-%02d", i)
		if _, err := db.ExecContext(ctx, `INSERT INTO a2a_tasks(task_id, tenant_id, project_id, request_id, idempotency_key, status) VALUES ($1, $2, $3, $4, $5, 'completed')`, taskID, tenantID, "project-1", taskID, "standard-key-"+taskID); err != nil {
			t.Fatal(err)
		}
		pushID := fmt.Sprintf("standard-push-%02d", i)
		if _, err := db.ExecContext(ctx, `INSERT INTO a2a_push_outbox(id, tenant_id, task_id, callback_url, bearer_token, payload, status, attempts, max_attempts, next_attempt_at) VALUES ($1, $2, $3, $4, '', $5, 'pending', 0, 3, CURRENT_TIMESTAMP)`, pushID, tenantID, taskID, callbackURL, payload); err != nil {
			t.Fatal(err)
		}
	}
}

func startLiteGatewayProcess(t *testing.T, bin, dsn, redisURL, readyPort, gatewayPort string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin,
		"--mode", "gateway",
		"--db", dsn,
		"--coordinator", redisURL,
		"--ready-addr", "127.0.0.1:"+readyPort,
		"--gateway-addr", "127.0.0.1:"+gatewayPort,
		"--smtp-host", "127.0.0.1",
		"--smtp-port", "18025",
		"--smtp-tls-mode", "none",
		"--smtp-from", "noreply@liteaig.local",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderrBuffer{}
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf("http://127.0.0.1:%s/healthz", readyPort)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(probe)
		if err == nil {
			resp.Body.Close()
			return cmd
		}
		if strings.Contains(tail(cmd), "lite:") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	t.Fatalf("liteaig gateway process (port %s) did not become ready: %s", readyPort, tail(cmd))
	return nil
}
