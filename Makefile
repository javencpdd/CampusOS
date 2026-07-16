.PHONY: build run dev dev-all test lint clean contracts contracts-check docs-links readme-check version-check architecture-check frontend-budget data-governance-check generated-files-check database-check backup restore-drill release-check migrate-up migrate-down migrate-reset migrate-status docker-up docker-infra-up docker-tools-up docker-down web-dev web-build admin-dev admin-build docs-dev docs-build

# 构建
build:
	go build -o bin/campusos-server ./cmd/server/main.go

# 运行
run: build
	./bin/campusos-server

# 开发热重载
dev:
	air

dev-all:
	./scripts/start-dev.sh

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

contracts:
	go run ./cmd/campusos-contracts --write

contracts-check:
	go run ./cmd/campusos-contracts --check

docs-links:
	python3 scripts/check-doc-links.py

readme-check:
	python3 skills/campusos-readme-update/scripts/audit_readme_structure.py --root .
	python3 skills/campusos-readme-update/scripts/check_readme_links.py --root . README.md docs/README.md

version-check:
	python3 scripts/check-version-sync.py

architecture-check:
	python3 scripts/check-architecture-boundaries.py
	python3 scripts/check-frontend-boundaries.py
	python3 scripts/test-architecture-checks.py

frontend-budget:
	python3 scripts/check-frontend-bundles.py

data-governance-check:
	python3 scripts/check-data-governance.py
	./scripts/test-v10-layout-migration.sh

generated-files-check:
	python3 scripts/check-generated-files.py

database-check:
	./scripts/database-check.sh all
	./scripts/test-v10-module-separation-migration.sh

backup:
	./scripts/backup.sh

restore-drill:
	./scripts/restore-drill.sh

release-check:
	./scripts/release-check.sh

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

admin-dev:
	cd admin && pnpm dev

admin-build:
	cd admin && pnpm build

docs-dev:
	cd docs-site && pnpm dev

docs-build:
	cd docs-site && pnpm build
