.PHONY: dev dev-console dev-backend test check build console-build

# One-shot local startup: Lite backend + Console dev server (Change 3).
# The Console dev server proxies /api/admin -> LITEAIG_ADMIN_ADDR.
dev: dev-backend dev-console

dev-backend:
	LITEAIG_ADMIN_ADDR="http://localhost:8081" go run ./cmd/liteaig --db "file:liteaig-dev.db" --admin-addr :8081 --ready-addr :8080

dev-console:
	cd web/console && LITEAIG_ADMIN_ADDR="http://localhost:8081" npm run dev

test:
	go test -race -timeout 180s ./...

check:
	go vet ./...
	go run ./cmd/architecture-test
	cd web/console && npm run check

console-build:
	cd web/console && npm run build

build:
	go build ./...
