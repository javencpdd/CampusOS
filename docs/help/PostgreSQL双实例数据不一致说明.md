# PostgreSQL 双实例数据不一致说明

> 日期：2026-07-02
> 场景：宿主机 `psql -p 5432` 查询数据正常，但 `docker exec campusos-postgres psql ...` 查询数据不全

## 1. 结论

这不是 `psql` 查询命令本身的问题，而是当前机器上同时存在两套 PostgreSQL 实例：

| 连接方式 | 实际连接到哪里 | 当前数据表现 |
| --- | --- | --- |
| `PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5432 -U campusos -d campusos` | 宿主机本机 PostgreSQL | 数据较完整 |
| `docker exec -it campusos-postgres psql -U campusos -d campusos` | Docker 容器 `campusos-postgres` 内的 PostgreSQL | 数据较少 |
| `PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5433 -U campusos -d campusos` | Docker 容器映射到宿主机的 PostgreSQL | 与 `docker exec` 看到的数据一致 |

原因是当前 `.env` 中已经配置：

```env
POSTGRES_PORT=5433
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable
```

也就是说，CampusOS 的 Docker PostgreSQL 现在映射在宿主机 `5433`，不是 `5432`。

## 2. 为什么两个命令看到的数据不一样

### 2.1 宿主机连接命令

```bash
PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5432 -U campusos -d campusos
```

这个命令的意思是：

```text
从宿主机连接 127.0.0.1:5432 上的 PostgreSQL。
```

当前 `127.0.0.1:5432` 被宿主机本机 PostgreSQL 占用，不是 Docker 容器 `campusos-postgres`。

你看到的输出：

```text
psql (16.14 (Ubuntu 16.14-1.pgdg22.04+1))
SSL connection ...
```

这也说明它更像是在连接宿主机 Ubuntu 安装的 PostgreSQL。

### 2.2 Docker 容器内连接命令

```bash
docker exec -it campusos-postgres psql -U campusos -d campusos
```

这个命令的意思是：

```text
先进入 campusos-postgres 容器，再在容器内部连接 PostgreSQL。
```

它一定连接 Docker 容器里的 PostgreSQL，不会连接宿主机 `127.0.0.1:5432`。

因此，如果宿主机 PostgreSQL 和 Docker PostgreSQL 都有一个叫 `campusos` 的数据库，它们名字一样，但数据不是同一份。

## 3. 本机验证结果

本机当前容器端口映射为：

```text
campusos-postgres  0.0.0.0:5433->5432/tcp
```

只读检查结果：

| 连接 | server | migration 数量 | users | threads | user_space_contents |
| --- | --- | ---: | ---: | ---: | ---: |
| 宿主机 `127.0.0.1:5432` | `16.14 (Ubuntu 16.14-1.pgdg22.04+1)` | 10 | 5 | 4 | 2 |
| 宿主机 `127.0.0.1:5433` | Docker PostgreSQL 16.14 | 10 | 1 | 0 | 0 |
| `docker exec campusos-postgres` | Docker PostgreSQL 16.14 | 10 | 1 | 0 | 0 |

这说明：

```text
127.0.0.1:5432 != campusos-postgres 容器数据库
127.0.0.1:5433 == campusos-postgres 容器数据库
```

## 4. 正确使用方式

### 4.1 如果以 Docker PostgreSQL 为准

推荐开发时统一使用 Docker PostgreSQL。此时命令应使用 `5433`：

```bash
PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5433 -U campusos -d campusos
```

后端 `.env` 应保持：

```env
POSTGRES_PORT=5433
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5433/campusos?sslmode=disable
```

pgAdmin 中注册服务器仍然填：

```text
Host = postgres
Port = 5432
Database = campusos
Username = campusos
Password = campusos_dev
```

注意：pgAdmin 在 Docker 网络里访问 PostgreSQL 容器，所以使用容器内端口 `5432`，不是宿主机映射端口 `5433`。

### 4.2 如果以宿主机 PostgreSQL 为准

如果你想继续使用宿主机 `127.0.0.1:5432` 这份数据，那么后端 `.env` 应改为：

```env
DATABASE_DSN=postgres://campusos:campusos_dev@localhost:5432/campusos?sslmode=disable
```

这种情况下：

| 命令 | 是否能看到宿主机数据 |
| --- | --- |
| `psql -h 127.0.0.1 -p 5432 ...` | 能 |
| `docker exec campusos-postgres psql ...` | 不能 |

因为 `docker exec` 永远查询 Docker 容器里的数据库。

## 5. 如何把宿主机数据迁移到 Docker 数据库

如果你确认宿主机 `5432` 里的数据才是需要保留的数据，可以先备份，再导入 Docker PostgreSQL。

### 5.1 从宿主机 5432 备份

```bash
mkdir -p backups
PGPASSWORD=campusos_dev pg_dump \
  -h 127.0.0.1 \
  -p 5432 \
  -U campusos \
  -d campusos \
  -F c \
  -f backups/campusos-host5432.dump
```

### 5.2 恢复到 Docker 5433

恢复前确认 Docker 数据库可以被覆盖：

```bash
PGPASSWORD=campusos_dev pg_restore \
  -h 127.0.0.1 \
  -p 5433 \
  -U campusos \
  -d campusos \
  --clean \
  --if-exists \
  backups/campusos-host5432.dump
```

恢复后验证：

```bash
PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5433 -U campusos -d campusos -c \
  "SELECT count(*) AS users FROM users;"

docker exec campusos-postgres psql -U campusos -d campusos -c \
  "SELECT count(*) AS users FROM users;"
```

两个结果应该一致。

## 6. 排查命令

查看宿主机端口监听：

```bash
ss -ltnp | grep -E ':5432|:5433'
```

查看 Docker 容器端口映射：

```bash
docker compose ps postgres
docker inspect campusos-postgres --format '{{json .NetworkSettings.Ports}}'
```

确认宿主机 `5432` 连接的是谁：

```bash
PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5432 -U campusos -d campusos -c \
  "SELECT inet_server_addr(), inet_server_port(), version();"
```

确认 Docker 映射端口 `5433`：

```bash
PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5433 -U campusos -d campusos -c \
  "SELECT inet_server_addr(), inet_server_port(), version();"
```

确认容器内部数据库：

```bash
docker exec campusos-postgres psql -U campusos -d campusos -c \
  "SELECT current_database(), current_user, version();"
```

## 7. 建议

为避免后续继续混淆，建议只选择一个数据库作为开发主库：

| 推荐方案 | 说明 |
| --- | --- |
| Docker PostgreSQL 作为主库 | 推荐。保持 `.env` 使用 `POSTGRES_PORT=5433` 和 `DATABASE_DSN=...localhost:5433...` |
| 宿主机 PostgreSQL 作为主库 | 可以，但不要再用 `docker exec campusos-postgres` 判断业务数据 |

如果继续使用 Docker PostgreSQL，后续查询数据时优先使用：

```bash
PGPASSWORD=campusos_dev psql -h 127.0.0.1 -p 5433 -U campusos -d campusos
```

或：

```bash
docker exec -it campusos-postgres psql -U campusos -d campusos
```
