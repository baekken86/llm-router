.PHONY: build web clean dev

build: web
	go build -o llm-router ./cmd/llm-router

web:
	cd web && npm install && npm run build

dev:
	cd web && npm run dev

clean:
	rm -rf web/dist web/node_modules llm-router
