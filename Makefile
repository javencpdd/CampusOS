.PHONY: build run dev dev-all test lint clean contracts contracts-check docs-links readme-check version-check architecture-check reliability-check outbox-check failure-injection-check v12-failure-injection-check structured-thread-check mutual-aid-check secondhand-check identity-email-check identity-challenge-check identity-registration-check identity-session-check identity-recovery-check identity-admin-account-check email-delivery-check category-hierarchy-check frontend-budget data-governance-check generated-files-check v12-migration-check database-check backup restore-drill release-check migrate-up migrate-down migrate-reset migrate-status docker-up docker-infra-up docker-tools-up docker-down web-dev web-build admin-dev admin-build docs-dev docs-build

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
	python3 skills/campusos-data-architecture-sync/scripts/check_architecture_sync.py --root .

reliability-check:
	python3 scripts/check-reliability-boundaries.py

outbox-check:
	./scripts/check-outbox.sh

failure-injection-check:
	./scripts/check-failure-injection.sh

v12-failure-injection-check:
	./scripts/check-v12-failure-injection.sh

identity-email-check:
	./scripts/check-identity-email.sh

identity-challenge-check:
	./scripts/check-identity-challenge.sh

identity-registration-check:
	./scripts/check-identity-registration.sh

identity-session-check:
	./scripts/check-identity-session.sh

identity-recovery-check:
	./scripts/check-identity-recovery.sh

identity-admin-account-check:
	./scripts/test-v12-admin-account-migration.sh

email-delivery-check:
	./scripts/check-email-delivery.sh

category-hierarchy-check:
	./scripts/check-category-hierarchy.sh

structured-thread-check:
	./scripts/check-structured-thread.sh

mutual-aid-check:
	./scripts/check-mutual-aid.sh

secondhand-check:
	./scripts/check-secondhand.sh

frontend-budget:
	python3 scripts/check-frontend-bundles.py

data-governance-check:
	python3 scripts/check-data-governance.py
	./scripts/test-v10-layout-migration.sh

generated-files-check:
	python3 scripts/check-generated-files.py

v12-migration-check:
	./scripts/test-v10-module-separation-migration.sh
	./scripts/test-v11-reliability-migration.sh
	./scripts/test-v12-identity-migration.sh
	./scripts/test-v12-identity-challenge-migration.sh
	./scripts/test-v12-identity-session-migration.sh
	./scripts/test-v12-identity-recovery-migration.sh
	./scripts/test-v12-category-hierarchy-migration.sh
	./scripts/test-v12-structured-threads-migration.sh
	./scripts/test-v12-mutual-aid-migration.sh
	./scripts/test-v12-secondhand-migration.sh
	./scripts/test-v12-identity-challenge-policy-migration.sh
	./scripts/test-v12-reliability-worker-convergence-migration.sh
	./scripts/test-v12-admin-account-migration.sh

database-check:
	./scripts/database-check.sh all
	$(MAKE) v12-migration-check

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
