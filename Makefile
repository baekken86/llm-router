.PHONY: build web clean dev test test-setup test-teardown

build: web
	go build -o llm-router ./cmd/llm-router

web:
	cd web && npm install && npm run build

dev:
	cd web && npm run dev

clean:
	rm -rf web/dist web/node_modules llm-router

test-setup:
	@mkdir -p /tmp/llm-router-test
	@LLM_ROUTER_PORT=8080 \
	 LLM_ROUTER_DB=/tmp/llm-router-test/test.db \
	 LLM_ROUTER_ADMIN_PASSWORD=testpass123 \
	 LLM_ROUTER_ENCRYPTION_KEY=0000000000000000000000000000000000000000000000000000000000000000 \
	 ./llm-router proxy --no-tui & echo $$! > /tmp/llm-router-test/server.pid
	@sleep 2

test-teardown:
	@if [ -f /tmp/llm-router-test/server.pid ]; then \
		kill $$(cat /tmp/llm-router-test/server.pid) 2>/dev/null || true; \
		rm -f /tmp/llm-router-test/server.pid; \
	fi
	@rm -rf /tmp/llm-router-test

test:
	cd web && BASE_URL=http://localhost:19922 npx playwright test
