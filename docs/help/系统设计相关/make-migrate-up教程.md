# make migrate-up 实现原理教程

> 适用项目：CampusOS
> 当前命令来源：项目根目录 `Makefile`
> 编写日期：2026-06-27
> 目标读者：刚接触 Makefile、PostgreSQL 迁移和 Docker 本地开发的项目维护者

## 1. 这条命令做了什么

在项目根目录执行：

```bash
make migrate-up
```

当前实现不是 `golang-migrate`，而是项目自带的轻量迁移脚本。`Makefile` 负责提供短命令入口，真正的迁移逻辑在 `scripts/migrate.sh` 和 `scripts/migrate.ps1` 中。

脚本会：

| 步骤 | 说明 |
| --- | --- |
| 1 | 创建 `schema_migrations` 版本表 |
| 2 | 扫描 `migrations/*.up.sql` |
| 3 | 按文件名顺序执行未记录过的迁移 |
| 4 | 每成功执行一个迁移，就写入 `schema_migrations` |
| 5 | 再次执行时跳过已完成的版本 |

当前 `Makefile` 中的真实实现是：

```makefile
# 数据库迁移
migrate-up:
	./scripts/migrate.sh up

migrate-down:
	./scripts/migrate.sh down

migrate-reset:
	./scripts/migrate.sh reset

migrate-status:
	./scripts/migrate.sh status
```

也就是说，`make migrate-up` 首次执行时会按顺序应用当前目录中的全部 `.up.sql` 文件，例如当前版本会应用：

```bash
000001_init_schema.up.sql
000002_add_roles.up.sql
000003_add_plugins.up.sql
000004_seed_admin.up.sql
000005_plugin_schema_alignment.up.sql
000006_add_ai_call_logs.up.sql
000007_add_user_spaces.up.sql
000008_add_user_space_contents.up.sql
000009_add_user_space_styles.up.sql
000010_fix_admin_seed_password.up.sql
```

可以用下面的命令只查看 Make 会执行什么，而不真正执行数据库迁移：

```bash
make -n migrate-up
```

当前输出会展示 Makefile 调用的迁移脚本命令，而不会真的连接数据库。它适合用来理解 `make migrate-up` 最终会执行什么。

## 2. 为什么可以这样用

它能工作的原因来自三层配合：

| 层级 | 作用 |
| --- | --- |
| Makefile | 定义 `migrate-up` 目标，负责把短命令展开成完整命令 |
| psql / docker exec psql | PostgreSQL 官方命令行客户端，负责连接数据库并执行 SQL 文件 |
| Docker Compose / PostgreSQL | 提供正在运行的 PostgreSQL 服务，通过 `${POSTGRES_PORT:-5432}` 暴露到宿主机 |

项目的 `docker-compose.yml` 中定义了 PostgreSQL：

```yaml
postgres:
  image: postgres:16-alpine
  container_name: campusos-postgres
  ports:
    - "${POSTGRES_PORT:-5432}:5432"
  environment:
    POSTGRES_DB: campusos
    POSTGRES_USER: campusos
    POSTGRES_PASSWORD: campusos_dev
```

这里最关键的是：

```yaml
ports:
  - "${POSTGRES_PORT:-5432}:5432"
```

它表示：

| 位置 | 端口 |
| --- | --- |
| 宿主机 | `localhost:${POSTGRES_PORT:-5432}`，当前开发机通常是 `localhost:5433` |
| Docker 容器内部 | `postgres:5432` |

所以即使 PostgreSQL 服务运行在 Docker 容器里，宿主机上的 `psql` 仍然可以通过映射端口连接进去。如果宿主机没有安装 `psql`，当前脚本会自动改用 `docker exec campusos-postgres psql`，直接调用 PostgreSQL 容器内部自带的客户端。

## 3. 命令逐段解释

宿主机有 `psql` 时，循环中每次真正执行的命令形态如下：

```bash
PGPASSWORD=campusos_dev psql -h localhost -p "${POSTGRES_PORT:-5432}" -U campusos -d campusos -v ON_ERROR_STOP=1 -f 某个迁移文件.sql
```

逐段含义：

| 片段 | 说明 |
| --- | --- |
| `PGPASSWORD=campusos_dev` | 给本次 `psql` 进程临时设置数据库密码 |
| `psql` | PostgreSQL 命令行客户端 |
| `-h localhost` | 连接宿主机的 `localhost` |
| `-p "${POSTGRES_PORT:-5432}"` | 连接 Docker 暴露到宿主机的端口，当前开发机通常是 `5433` |
| `-U campusos` | 使用数据库用户 `campusos` |
| `-d campusos` | 连接数据库 `campusos` |
| `-v ON_ERROR_STOP=1` | SQL 出错时立即停止，避免后续迁移继续执行 |
| `-f 某个迁移文件.sql` | 执行当前循环选中的 SQL 文件 |

其中 `PGPASSWORD=campusos_dev` 是 Linux shell 的临时环境变量写法，只对后面的这一条命令生效。它避免了执行 `psql` 时手动输入密码。

等价的写法是：

```bash
export PGPASSWORD=campusos_dev
psql -h localhost -p "${POSTGRES_PORT:-5432}" -U campusos -d campusos -v ON_ERROR_STOP=1 -f migrations/000001_init_schema.up.sql
```

但当前 Makefile 使用单行写法更简洁，也不会把 `PGPASSWORD` 长期留在当前 shell 会话里。

宿主机没有 `psql`、但 Docker PostgreSQL 容器正在运行时，脚本会自动切换为下面这种形态：

```bash
docker exec -i -e PGPASSWORD=campusos_dev campusos-postgres \
  psql -U campusos -d campusos -v ON_ERROR_STOP=1 < 某个迁移文件.sql
```

这里不能简单把 `-f migrations/xxx.sql` 传给容器内的 `psql`，因为 SQL 文件在宿主机项目目录里，不在 PostgreSQL 容器文件系统里。当前脚本会把 `-f 文件名` 转换成输入重定向，把 SQL 文件内容通过标准输入送进容器里的 `psql`。

## 4. Makefile 是如何执行它的

`Makefile` 由“目标”和“命令”组成：

```makefile
migrate-up:
	./scripts/migrate.sh up
```

这里：

| 部分 | 含义 |
| --- | --- |
| `migrate-up:` | Make 目标名称 |
| 下一行前面的 Tab | Makefile 要求命令行必须以 Tab 开头 |
| `./scripts/migrate.sh up` | 调用项目迁移脚本，由脚本扫描 SQL、检查版本表并执行未完成的迁移 |
| `schema_migrations` | 记录已经应用过的迁移版本，避免重复执行 |

项目开头还有：

```makefile
.PHONY: build run dev test lint clean migrate-up migrate-down migrate-reset migrate-status
```

`.PHONY` 表示这些名字是“伪目标”，不是文件名。这样即使目录里将来出现一个叫 `migrate-up` 的文件，执行 `make migrate-up` 时 Make 仍然会运行这个目标，而不是误判“文件已经存在，所以不用执行”。

## 5. `psql -f` 是如何执行 SQL 文件的

`psql -f` 会读取指定 SQL 文件，并把里面的语句发送到 PostgreSQL 执行。当前 `migrate-up` 会按顺序执行 10 个 `.up.sql` 文件。

当前迁移文件分工如下：

| 文件 | 作用 |
| --- | --- |
| `000001_init_schema.up.sql` | 创建用户、账号、会话、版块、主题、回复、标签、点赞、审计日志、通知、配置等核心表 |
| `000002_add_roles.up.sql` | 创建 `roles`、`user_roles`、`permissions`，并插入内置角色和权限 |
| `000003_add_plugins.up.sql` | 创建 `plugins`、`api_keys` |
| `000004_seed_admin.up.sql` | 插入默认管理员账号、管理员角色绑定和默认版块 |
| `000005_plugin_schema_alignment.up.sql` | 创建插件权限表和插件日志表 |
| `000006_add_ai_call_logs.up.sql` | 创建 AI 调用日志表 |
| `000007_add_user_spaces.up.sql` | 创建用户个人主页配置表 |
| `000008_add_user_space_contents.up.sql` | 创建个人主页同步内容表 |
| `000009_add_user_space_styles.up.sql` | 创建个人主页风格状态表 |
| `000010_fix_admin_seed_password.up.sql` | 修复默认管理员种子密码哈希 |

这些文件中既包含建表语句，也包含唯一索引、普通索引和种子数据。例如：

```sql
CREATE UNIQUE INDEX uk_users_username ON users(username) WHERE deleted_at IS NULL;
CREATE INDEX idx_threads_created_at ON threads(created_at DESC) WHERE deleted_at IS NULL;
```

执行成功后，数据库中就具备 CampusOS 当前阶段需要的核心表、权限表、插件表、插件权限/日志表、默认管理员和默认版块。

## 6. 当前项目中的前置条件

执行 `make migrate-up` 前，需要满足这些条件：

| 条件 | 检查方式 | 说明 |
| --- | --- | --- |
| PostgreSQL 服务可用 | `make docker-up` 或本机 PostgreSQL 服务 | 推荐 Docker Compose；如果本机已有 PostgreSQL 占用端口，脚本会提示跳过 |
| PostgreSQL 可连接 | `make migrate-status` | 有宿主机 `psql` 时走 `DB_HOST:DB_PORT`；没有宿主机 `psql` 时走 Docker 容器内连接 |
| `psql` 执行入口 | `psql --version` 或 `docker ps --filter name=campusos-postgres` | 两者满足一个即可：宿主机客户端可用，或 Docker PostgreSQL 容器正在运行 |
| 数据库已创建 | `campusos` | Docker 初始化时由 `POSTGRES_DB` 创建 |
| 用户已创建 | `campusos` | Docker 初始化时由 `POSTGRES_USER` 创建 |
| 密码匹配 | `campusos_dev` | 必须与 `POSTGRES_PASSWORD` 一致 |
| 当前目录正确 | 项目根目录 | 因为 SQL 文件路径是相对路径 `migrations/...` |

当前 `Makefile` 中的 `docker-up` 目标会启动迁移所需的基础服务：

```makefile
docker-up:
	./scripts/docker-up.sh
```

`scripts/docker-up.sh` 会优先检查 PostgreSQL、Redis、NATS 是否已经运行。如果宿主机端口已经被本机服务占用，它会跳过对应 Docker 服务并提示修改 `POSTGRES_PORT`、`REDIS_PORT` 或 `NATS_CLIENT_PORT`，避免新开发者在端口冲突时直接卡住。

因此首次准备数据库时，可以先执行：

```bash
make docker-up
```

如果需要 pgAdmin，再额外执行：

```bash
make docker-tools-up
```

## 7. 推荐执行流程

在 Ubuntu 或 WSL2 环境中，推荐按下面顺序执行：

```bash
# 1. 进入项目根目录
cd /home/jack/bbs/bbs01/CampusOS

# 2. 启动 PostgreSQL、Redis、NATS
make docker-up

# 3. 查看迁移状态
make migrate-status

# 4. 执行当前 Makefile 中定义的迁移
make migrate-up
```

执行完成后，可以检查表是否创建成功：

```bash
PGPASSWORD=campusos_dev psql -h localhost -p "${POSTGRES_PORT:-5432}" -U campusos -d campusos -c "\dt"
```

也可以检查某张表：

```bash
PGPASSWORD=campusos_dev psql -h localhost -p "${POSTGRES_PORT:-5432}" -U campusos -d campusos -c "\d users"
```

## 8. 为什么 migrate-up 不应该包含 down 文件

`.up.sql` 和 `.down.sql` 的语义完全不同：

| 文件类型 | 含义 | 典型操作 |
| --- | --- | --- |
| `.up.sql` | 向上迁移，创建或补齐数据库结构 | `CREATE TABLE`、`CREATE INDEX`、`INSERT seed data` |
| `.down.sql` | 向下回滚，撤销对应版本的变更 | `DELETE seed data`、`DROP TABLE` |

因此 `migrate-up` 应该只执行 `.up.sql` 文件。如果把 `.down.sql` 也放进 `migrate-up`，这个命令就会变成“先删库表和数据，再重新建库表”，每次执行都有破坏性。

更严重的是，回滚顺序不能按 `000001`、`000002`、`000003`、`000004`、`000005` 正序执行，而必须逆序执行：

```text
正确回滚顺序：
000005_plugin_schema_alignment.down.sql
000004_seed_admin.down.sql
000003_add_plugins.down.sql
000002_add_roles.down.sql
000001_init_schema.down.sql
```

原因是后面的迁移往往依赖前面的迁移。比如 `000004_seed_admin.down.sql` 要删除 `user_roles`、`accounts`、`users` 中的种子数据；如果先执行 `000001_init_schema.down.sql`，核心表会先被删除，后面的种子数据删除语句就可能因为表不存在而失败。

所以这三个命令应该分开：

| 命令 | 作用 |
| --- | --- |
| `make migrate-up` | 按正序执行所有 `.up.sql` |
| `make migrate-down` | 按逆序执行所有 `.down.sql` |
| `make migrate-reset` | 先执行 `migrate-down`，再执行 `migrate-up`，用于开发环境重建数据库 |

## 9. 当前 Makefile 的完整迁移方式

当前 `migrate-up` 通过 `scripts/migrate.sh` 执行全部上行迁移，并使用 `schema_migrations` 记录已执行版本：

```makefile
migrate-up:
	./scripts/migrate.sh up
```

脚本第一次执行时会按顺序应用：

```bash
000001_init_schema.up.sql
000002_add_roles.up.sql
000003_add_plugins.up.sql
000004_seed_admin.up.sql
000005_plugin_schema_alignment.up.sql
```

当前 `migrate-down` 使用逆序执行全部回滚迁移：

```makefile
migrate-down:
	./scripts/migrate.sh down
```

如果你要表达“先删掉旧结构，再重新建一遍”的开发环境重建动作，应使用：

```bash
make migrate-reset
```

它在 Makefile 中定义为：

```makefile
migrate-reset:
	./scripts/migrate.sh reset
```

查看迁移版本：

```bash
make migrate-status
```

注意：`migrate-reset` 会删除已有表和种子数据，只适合开发环境。不要在有真实数据的环境中随便执行。

## 10. 如果本机没有 psql 怎么办

当前 `make migrate-up` 已经内置 Docker fallback。也就是说，宿主机没有安装 PostgreSQL 客户端时，不需要手动逐个执行 SQL 文件，只要 Docker PostgreSQL 容器正在运行即可：

```bash
docker ps --filter name=campusos-postgres
make migrate-up
```

脚本会输出类似信息：

```text
==> host psql not found; using docker exec campusos-postgres psql
```

此时迁移仍然会扫描本地 `migrations/*.up.sql`，并把 SQL 文件内容通过标准输入送进容器内的 `psql`。这种方式的优点是宿主机不需要安装 `psql`，只需要 Docker 可用。

如果需要强制指定执行方式：

```bash
PSQL_MODE=host make migrate-up
PSQL_MODE=docker make migrate-up
```

如果 PostgreSQL 容器名发生变化：

```bash
POSTGRES_CONTAINER=新的容器名 PSQL_MODE=docker make migrate-up
```

## 11. 常见问题排查

### 11.1 `psql: command not found`

原因：宿主机没有安装 PostgreSQL 客户端。当前脚本会自动尝试使用 Docker 容器内的 `psql`，所以通常只需要确认容器正在运行：

```bash
docker ps --filter name=campusos-postgres
make migrate-up
```

如果希望继续使用宿主机 `psql`，再安装客户端：

```bash
sudo apt update
sudo apt install -y postgresql-client
```

### 11.2 `connection refused`

原因通常是 PostgreSQL 没有启动，或者端口没有映射到宿主机。

检查：

```bash
docker compose ps postgres
docker ps --filter name=campusos-postgres
```

启动：

```bash
docker compose up -d postgres
```

### 11.3 `password authentication failed`

原因：`PGPASSWORD`、`POSTGRES_PASSWORD` 或数据库中的用户密码不一致。

当前项目默认密码是：

```text
campusos_dev
```

如果 Docker 数据卷已经初始化过，即使修改了 `docker-compose.yml` 中的密码，旧数据卷里的数据库密码也不会自动变化。需要手动改密码，或者删除旧数据卷后重新初始化。

### 11.4 `database "campusos" does not exist`

原因：数据库没有创建成功，或连接到了错误的 PostgreSQL 实例。

检查当前容器环境：

```bash
docker inspect campusos-postgres --format '{{json .Config.Env}}'
```

也可以进入容器查看数据库：

```bash
docker exec -it campusos-postgres psql -U campusos -d postgres -c "\l"
```

### 11.5 `relation "users" already exists`

原因：数据库中已经存在旧版本手工迁移留下的对象，或当前 `schema_migrations` 版本表没有正确记录已执行版本。

当前迁移脚本会创建 `schema_migrations`，并跳过已记录的版本。如果是从旧数据库升级，`000001` 到 `000004` 的建表、索引和种子数据已经做了基础幂等处理，首次补建版本表时可以通过 `make migrate-up` 回填执行记录。

如果是开发环境想重来，可以使用：

```bash
make migrate-reset
```

它等价于先执行 `make migrate-down`，再执行 `make migrate-up`。当前 `make migrate-down` 会按逆序执行全部 `.down.sql` 文件。

更彻底的开发环境重置方式是删除 Docker 数据卷：

```bash
docker compose down -v
docker compose up -d postgres
make migrate-up
```

执行 `docker compose down -v` 会删除数据库数据卷，所有数据都会丢失，只适合开发环境。

## 12. 推荐改进方向

当前实现已经可以顺序执行所有 `.up.sql` 文件、逆序执行所有 `.down.sql` 文件，并通过 `schema_migrations` 记录已执行版本。但它仍然是项目内置的轻量脚本，生产环境长期可以继续演进为更正式的迁移工具：

| 方案 | 优点 | 说明 |
| --- | --- | --- |
| `golang-migrate` | 成熟、常见、支持版本表 | 适合生产环境 |
| Go 自研迁移命令 | 可与项目配置系统统一 | 适合做成 `campusosctl migrate up` |
| Docker 内迁移服务 | 宿主机无需安装 `psql` | 适合跨平台部署 |

推荐 v0.3-dev 之后逐步把迁移能力收敛到跨平台 Go CLI，例如：

```bash
campusosctl migrate up
campusosctl migrate down
campusosctl migrate status
```

这样 Ubuntu、Windows、WSL2、Docker 环境都可以使用同一套迁移入口。

## 13. 一句话总结

`make migrate-up` 可以这样用，是因为 Makefile 调用 `scripts/migrate.sh up`，脚本会先选择可用的 `psql` 执行入口：宿主机有 `psql` 时连接 `DB_HOST:DB_PORT`，宿主机没有 `psql` 时使用 `docker exec campusos-postgres psql`。随后脚本会创建 `schema_migrations`，按顺序执行未应用过的 SQL 迁移文件，并记录执行版本。

`migrate-up` 只应该执行 `.up.sql`；`.down.sql` 属于 `migrate-down`。如果需要开发环境重建数据库，应使用 `make migrate-reset`。当前实现已经能跑完 `000001` 到 `000010`，并能通过 `make migrate-status` 查看版本状态；后续仍可升级为 `golang-migrate` 或跨平台 Go CLI。
