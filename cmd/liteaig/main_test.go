package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunModes(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{mode: "all", want: "mode=all gateway=true control=true"},
		{mode: "gateway", want: "mode=gateway gateway=true control=false"},
		{mode: "control", want: "mode=control gateway=false control=true"},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			closed := make(chan struct{})
			close(closed)
			if code := run([]string{"--mode=" + tt.mode, "--ready-addr=127.0.0.1:0"}, &stdout, &stderr, closed); code != 0 {
				t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("run() output = %q, want substring %q", stdout.String(), tt.want)
			}
		})
	}
}

func TestRunModesStartOnlySelectedPlanes(t *testing.T) {
	tests := []struct {
		mode        string
		wantAdmin   string
		wantGateway string
	}{
		{mode: "all", wantAdmin: "management=true", wantGateway: "gateway=true"},
		{mode: "gateway", wantAdmin: "management=false", wantGateway: "gateway=true"},
		{mode: "control", wantAdmin: "management=true", wantGateway: "gateway=false"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			closed := make(chan struct{})
			close(closed)
			args := []string{
				"--mode=" + tt.mode,
				"--db=file:" + t.TempDir() + "/lite.db",
				"--ready-addr=127.0.0.1:0",
				"--admin-addr=127.0.0.1:0",
				"--gateway-addr=127.0.0.1:0",
			}
			if code := run(args, &stdout, &stderr, closed); code != 0 {
				t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
			}
			out := stdout.String()
			if !strings.Contains(out, tt.wantAdmin) || !strings.Contains(out, tt.wantGateway) {
				t.Fatalf("run() output = %q, want %q and %q", out, tt.wantAdmin, tt.wantGateway)
			}
		})
	}
}

func TestRunRejectsInvalidMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--mode=worker"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "invalid runtime mode") {
		t.Fatalf("run() stderr = %q", stderr.String())
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); got != "liteaig "+version+"\n" {
		t.Fatalf("run() output = %q", got)
	}
}

func TestWebhookCLIValidation(t *testing.T) {
	for _, withDB := range []bool{false, true} {
		args := []string{"--webhook-url=http://169.254.169.254/", "--ready-addr=127.0.0.1:0"}
		want := "requires --db"
		if withDB {
			args = append(args, "--db=file:webhook-cli?mode=memory&cache=shared")
			want = "not allowed"
		}
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), want) {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{addr: "127.0.0.1:8081", want: true},
		{addr: "[::1]:8081", want: true},
		{addr: ":8081", want: false},
		{addr: "0.0.0.0:8081", want: false},
		{addr: "[::]:8081", want: false},
		{addr: "192.168.1.10:8081", want: false},
	}
	for _, tt := range tests {
		if got := isLoopbackAddr(tt.addr); got != tt.want {
			t.Fatalf("isLoopbackAddr(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestRunWarnsOnNonLoopbackAdminAddr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	closed := make(chan struct{})
	close(closed)
	args := []string{
		"--mode=control",
		"--db=file:" + t.TempDir() + "/lite.db",
		"--ready-addr=127.0.0.1:0",
		"--admin-addr=0.0.0.0:0",
	}
	if code := run(args, &stdout, &stderr, closed); code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "reachable off-loopback") {
		t.Fatalf("stderr = %q, want off-loopback warning", stderr.String())
	}
}
