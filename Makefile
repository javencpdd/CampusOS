.PHONY: build run dev test lint clean migrate-up migrate-down migrate-reset migrate-status docker-up docker-infra-up docker-tools-up docker-down web-dev web-build

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
	./scripts/migrate.sh up

migrate-down:
	./scripts/migrate.sh down

migrate-reset:
	./scripts/migrate.sh reset

migrate-status:
	./scripts/migrate.sh status

# Docker
docker-up:
	./scripts/docker-up.sh

docker-infra-up: docker-up

docker-tools-up:
	docker compose up -d pgadmin

docker-down:
	docker compose down

# 前端
web-dev:
	cd web && pnpm dev

web-build:
	cd web && pnpm build
