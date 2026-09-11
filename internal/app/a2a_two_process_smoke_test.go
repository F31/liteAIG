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
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/F31/liteAIG/internal/platform/storage/sqlite"
	"github.com/F31/liteAIG/migrations"
)

// TestA2APushWorkerTwoProcessDeliversExactlyOnce is the true two-OS-process
// black-box smoke: two real `liteaig` processes share one SQLite file, race the
// `platform:a2a_push_outbox` singleton lease and the outbox `pending -> sending`
// claim, and must deliver every queued callback exactly once. It also asserts a
// clean SIGTERM drain exit for both processes. Directly sqlrepo is not used so
// the binary is the only consumer of the shared database.
func TestA2APushWorkerTwoProcessDeliversExactlyOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping two-OS-process smoke under -short")
	}
	bin, cleanupBin := buildLiteaigBinary(t)
	defer cleanupBin()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "two-proc.db")
	seedSidecarAndRows(t, dbPath)

	const deliveries = 12
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

	seedOutbox(t, dbPath, callback.URL, deliveries)

	// Two independent processes, distinct listener ports, one shared database.
	cmdA := startLiteProcess(t, bin, dbPath, "18101", "18102", "18103")
	cmdB := startLiteProcess(t, bin, dbPath, "18111", "18112", "18113")
	// Cleanup performs the SIGTERM graceful-shutdown assertions exactly once,
	// even when an earlier assertion fails.
	t.Cleanup(func() { stopLiteProcess(t, cmdA, "A"); stopLiteProcess(t, cmdB, "B") })

	deadline := time.Now().Add(30 * time.Second)
	for total.Load() < deliveries && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if got := total.Load(); got != deliveries {
		t.Fatalf("callback hits = %d, want %d (stderr A: %s\nstderr B: %s)", got, deliveries, tail(cmdA), tail(cmdB))
	}
	mu.Lock()
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("delivery %q delivered %d times, want exactly once", id, count)
		}
	}
	if len(seen) != deliveries {
		t.Fatalf("unique delivery ids = %d, want %d", len(seen), deliveries)
	}
	mu.Unlock()
}

// buildLiteaigBinary compiles the real binary once for the smoke, honoring an
// operator-supplied path (e.g. CI prebuild) via LITEAIG_TEST_BIN.
func buildLiteaigBinary(t *testing.T) (string, func()) {
	t.Helper()
	if path := os.Getenv("LITEAIG_TEST_BIN"); path != "" {
		return path, func() {}
	}
	bin := filepath.Join(t.TempDir(), "liteaig")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", bin, "github.com/F31/liteAIG/cmd/liteaig")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build liteaig binary: %v\n%s", err, output)
	}
	return bin, func() { _ = os.Remove(bin) }
}

func seedSidecarAndRows(t *testing.T, dbPath string) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+".masterkey", []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// seedOutbox provisions completed durable tasks and pending push rows so the
// two booted processes can only drain them.
func seedOutbox(t *testing.T, dbPath, callbackURL string, n int) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	payload := `{"jsonrpc":"2.0","id":"1","messageId":"m","result":{"message":{"parts":[{"kind":"text","text":"two-proc"}]}},"task":{"status":"completed"}}`
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id, public_ref, name) VALUES (?, ?, ?)`, "tenant-two-proc", "t", "t"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		taskID := fmt.Sprintf("task-%02d", i)
		if _, err := db.ExecContext(ctx, `INSERT INTO a2a_tasks(task_id, tenant_id, project_id, request_id, idempotency_key, status) VALUES (?, ?, ?, ?, ?, 'completed')`, taskID, "tenant-two-proc", "project-1", taskID, "key-"+taskID); err != nil {
			t.Fatal(err)
		}
		pushID := fmt.Sprintf("push-%02d", i)
		if _, err := db.ExecContext(ctx, `INSERT INTO a2a_push_outbox(id, tenant_id, task_id, callback_url, bearer_token, payload, status, attempts, max_attempts, next_attempt_at) VALUES (?, ?, ?, ?, '', ?, 'pending', 0, 3, CURRENT_TIMESTAMP)`, pushID, "tenant-two-proc", taskID, callbackURL, payload); err != nil {
			t.Fatal(err)
		}
	}
}

func startLiteProcess(t *testing.T, bin, dbPath, adminPort, readyPort, gatewayPort string) *exec.Cmd {
	t.Helper()
	args := []string{
		"--db", "file:" + dbPath,
		"--admin-addr", "127.0.0.1:" + adminPort,
		"--ready-addr", "127.0.0.1:" + readyPort,
		"--gateway-addr", "127.0.0.1:" + gatewayPort,
		"--smtp-host", "127.0.0.1",
		"--smtp-port", "18025",
		"--smtp-tls-mode", "none",
		"--smtp-from", "noreply@liteaig.local",
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderrBuffer{}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait until the admin origin is serving (same probe the Playwright config uses).
	probe := fmt.Sprintf("http://127.0.0.1:%s/api/admin/oidc/config", adminPort)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(probe)
		if err == nil {
			resp.Body.Close()
			return cmd
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	t.Fatalf("liteaig process (port %s) did not become ready: %s", adminPort, tail(cmd))
	return nil
}

func stopLiteProcess(t *testing.T, cmd *exec.Cmd, name string) {
	t.Helper()
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("process %s did not exit cleanly on SIGTERM: %v\n%s", name, err, tail(cmd))
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Errorf("process %s hung during graceful shutdown", name)
	}
}

// stderrBuffer retains the last 2 KiB of stderr for diagnostics.
type stderrBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (s *stderrBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	if len(s.buf) > 2048 {
		s.buf = s.buf[len(s.buf)-2048:]
	}
	return len(p), nil
}

func (s *stderrBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.buf)
}

func tail(cmd *exec.Cmd) string {
	if cmd == nil {
		return ""
	}
	if buf, ok := cmd.Stderr.(*stderrBuffer); ok {
		return buf.String()
	}
	return ""
}
