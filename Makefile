.PHONY: build run dev test lint clean migrate-up migrate-down migrate-reset

# 构建
build:
	go build -o bin/campusos-server ./cmd/server/main.go

# 运行
run: build
	./bin/campusos-server

# 开发热重载
dev:
	air

# 测试
test:
	go test ./... -v -count=1

# 测试覆盖率
test-coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# 代码检查
lint:
	golangci-lint run ./...

# 清理
clean:
	rm -rf bin/ tmp/ coverage.out coverage.html

# 数据库迁移
migrate-up:
	@echo "==> 执行数据库迁移"
	@for f in $$(ls migrations/*.up.sql | sort); do \
		echo "==> $$f"; \
		PGPASSWORD=campusos_dev psql -h localhost -U campusos -d campusos -v ON_ERROR_STOP=1 -f "$$f" || exit 1; \
	done

migrate-down:
	@echo "==> 回滚数据库迁移"
	@for f in $$(ls migrations/*.down.sql | sort -r); do \
		echo "==> $$f"; \
		PGPASSWORD=campusos_dev psql -h localhost -U campusos -d campusos -v ON_ERROR_STOP=1 -f "$$f" || exit 1; \
	done

migrate-reset: migrate-down migrate-up

# Docker
docker-up:
	docker compose up -d redis nats

docker-down:
	docker compose down

# 前端
web-dev:
	cd web && pnpm dev

web-build:
	cd web && pnpm build
