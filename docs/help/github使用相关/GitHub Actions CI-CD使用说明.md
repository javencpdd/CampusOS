# CampusOS GitHub Actions CI/CD 使用说明

> 适用文件：`.github/workflows/test.yml`、`.github/workflows/deploy.yml`
> 文档日期：2026-06-27
> 目标：说明 test 作为 CI 流程、deploy 作为 CD 流程时如何触发、验证、配置和排错

## 1. 工作流定位

当前 `.github/workflows` 中保留两条主要流水线：

| 文件 | 流程 | 作用 |
| --- | --- | --- |
| `test.yml` | CI | 每次提交或 PR 时验证后端测试、数据库迁移、后端构建、web/admin 前端构建 |
| `deploy.yml` | CD | 推送 `v*` 标签或手动触发时，构建发布包并通过 SSH 部署到远程服务器 |

开发者 push 代码到 GitHub
        ↓
GitHub Actions 自动触发
        ↓
CI：安装依赖、检查代码、运行测试、构建项目
        ↓
如果 CI 成功
        ↓
CD：连接服务器
        ↓
上传代码或构建产物
        ↓
安装生产依赖
        ↓
重启服务
        ↓
部署完成

## 2. CI：test.yml

### 2.1 触发条件

`test.yml` 会在以下场景触发：

| 触发方式 | 说明 |
| --- | --- |
| push 到 `main` | 主分支提交必须跑 CI |
| push 到 `develop` | 开发分支提交必须跑 CI |
| push 到 `feature/**`、`fix/**`、`codex/**` | 功能、修复和 Codex 工作分支也会跑 CI |
| PR 指向 `main` 或 `develop` | 合并前验证 |
| `workflow_dispatch` | GitHub 页面手动运行 |

### 2.2 CI 做了什么

CI 被拆成两个 job：

| Job | 内容 |
| --- | --- |
| `Backend Test` | 启动 PostgreSQL service，安装 `psql`，执行 `make migrate-up`、`make migrate-status`、`go test ./...`、`go build` |
| `Frontend Build` | 用 matrix 分别进入 `web` 和 `admin`，执行 `pnpm install --frozen-lockfile` 与 `pnpm build` |

后端 job 使用 GitHub Actions service container 提供 PostgreSQL，不依赖远程服务器，也不依赖本地 Docker Compose。

### 2.3 CI 中的重要变量

| 变量 | 当前值 | 说明 |
| --- | --- | --- |
| `NODE_VERSION` | `22` | web/admin 构建使用的 Node.js 版本 |
| `PNPM_VERSION` | `8` | 与当前 `pnpm-lock.yaml` lockfile 版本匹配 |
| `DB_HOST` | `localhost` | GitHub runner 连接 PostgreSQL service |
| `DB_PORT` | `5432` | PostgreSQL service 暴露端口 |
| `DB_USER` | `campusos` | 测试数据库用户 |
| `DB_PASSWORD` | `campusos_dev` | 测试数据库密码 |
| `DATABASE_DSN` | `postgres://...` | 后端默认数据库连接串 |

## 3. CD：deploy.yml

### 3.1 触发条件

`deploy.yml` 会在以下场景触发：

| 触发方式 | 说明 |
| --- | --- |
| push `v*` 标签 | 推荐正式发布方式，例如 `v0.3.0-dev.1` |
| `workflow_dispatch` | GitHub 页面手动部署当前分支 |

推荐发布命令：

```bash
git tag v0.3.0-dev.1
git push origin v0.3.0-dev.1
```

### 3.2 CD 做了什么

CD 包含两个 job：

| Job | 内容 |
| --- | --- |
| `Build Release Package` | 验证数据库迁移、后端测试、后端 Linux amd64 构建、web/admin 前端构建、组装 tar.gz 发布包 |
| `Deploy To Server` | 下载发布包，通过 SSH 上传到远程服务器，解压到版本目录，并把 `current` 软链接切换到新版本 |

发布包包含：

```text
bin/campusos-server
web/dist/
admin/dist/
migrations/
scripts/
docker-compose.yml
.env.example
```

它不包含 `node_modules`、源码缓存或本地构建临时目录。

## 4. GitHub Secrets 与 Variables 配置

进入 GitHub 仓库页面：

```text
Settings -> Secrets and variables -> Actions
```

### 4.1 必填 Secrets

| Secret | 示例 | 说明 |
| --- | --- | --- |
| `HOST` | `203.0.113.10` | 远程服务器地址。这里的 `secrets.HOST` 假设存放在 GitHub 平台的 Actions Secrets 中 |
| `USERNAME` | `deploy` | SSH 登录用户名 |
| `SSH_KEY` | 私钥内容 | 用于登录服务器的 SSH 私钥，建议使用专门的部署 key |

### 4.2 可选 Secrets

| Secret | 默认值 | 说明 |
| --- | --- | --- |
| `SSH_PORT` | `22` | 服务器 SSH 端口 |

### 4.3 可选 Variables

Variables 适合放非敏感配置：

| Variable | 默认值 | 说明 |
| --- | --- | --- |
| `DEPLOY_PATH` | `/opt/campusos` | 远程部署根目录 |
| `DEPLOY_RESTART_COMMAND` | 自动尝试 `sudo systemctl restart campusos` | 自定义重启命令，例如 `sudo systemctl restart campusos` 或 `cd /opt/campusos/current && ./scripts/restart.sh` |

如果没有配置 `DEPLOY_RESTART_COMMAND`，workflow 会先检查远程是否存在 `campusos.service`。如果存在，就执行：

```bash
sudo systemctl restart campusos
```

如果不存在，就只完成上传、解压和 `current` 软链接切换，不强制重启。

## 5. 远程服务器准备

推荐创建专门的部署用户：

```bash
sudo useradd -m -s /bin/bash deploy
sudo mkdir -p /opt/campusos
sudo chown -R deploy:deploy /opt/campusos
```

把 GitHub Secrets 中 `SSH_KEY` 对应的公钥写入：

```bash
sudo mkdir -p /home/deploy/.ssh
sudo nano /home/deploy/.ssh/authorized_keys
sudo chown -R deploy:deploy /home/deploy/.ssh
sudo chmod 700 /home/deploy/.ssh
sudo chmod 600 /home/deploy/.ssh/authorized_keys
```

如果使用 systemd，可以准备一个 `campusos.service`，让 `DEPLOY_RESTART_COMMAND` 重启它。服务文件的实际内容要根据服务器上的 `.env`、工作目录、数据库地址和静态资源托管方式调整。

## 6. 远程目录结构

部署后默认目录结构如下：

```text
/opt/campusos/
├── releases/
│   ├── campusos-v0.3.0-dev.1-12.tar.gz
│   └── campusos-v0.3.0-dev.1-12/
└── current -> /opt/campusos/releases/campusos-v0.3.0-dev.1-12
```

`current` 是软链接。服务启动脚本或 systemd 服务建议指向：

```text
/opt/campusos/current/bin/campusos-server
```

这样每次部署只需要切换软链接，再重启服务。

## 7. 常见问题

### 7.1 CI 中迁移失败

先看 `Run database migrations` step。常见原因：

| 现象 | 处理 |
| --- | --- |
| `psql: command not found` | 检查 `Install PostgreSQL client` step 是否成功 |
| `connection refused` | 检查 PostgreSQL service 是否 healthy |
| SQL 语法错误 | 本地执行 `make migrate-up` 复现 |

### 7.2 前端依赖安装失败

当前 workflow 使用 `pnpm install --frozen-lockfile`，如果 `package.json` 和 `pnpm-lock.yaml` 不一致，CI 会失败。

处理方式：

```bash
cd web
pnpm install

cd ../admin
pnpm install
```

然后提交更新后的 lockfile。

### 7.3 CD 提示缺少 HOST

说明 GitHub Secrets 没有配置 `HOST`，或者配置在了错误的仓库/环境中。确认位置：

```text
Settings -> Secrets and variables -> Actions -> Secrets
```

至少要有：

```text
HOST
USERNAME
SSH_KEY
```

### 7.4 SSH 连接失败

检查：

| 检查项 | 命令或位置 |
| --- | --- |
| 服务器地址 | `secrets.HOST` |
| 用户名 | `secrets.USERNAME` |
| 端口 | `secrets.SSH_PORT`，默认 `22` |
| 公钥是否写入服务器 | `/home/<user>/.ssh/authorized_keys` |
| 私钥格式是否完整 | `secrets.SSH_KEY` 必须包含完整多行私钥 |

### 7.5 部署成功但服务没有重启

如果远程没有 `campusos.service`，workflow 会只完成文件部署。可以在 GitHub Variables 中配置：

```text
DEPLOY_RESTART_COMMAND=sudo systemctl restart campusos
```

或者配置为自己的脚本：

```text
DEPLOY_RESTART_COMMAND=cd /opt/campusos/current && ./scripts/restart.sh
```

## 8. 当前建议

- CI 作为合并前门槛，至少要求 `Backend Test` 和两个前端 build 全部通过。
- CD 先用于测试服务器或预发布服务器，等 systemd、`.env`、数据库迁移策略稳定后再作为生产发布入口。
- `HOST`、`USERNAME`、`SSH_KEY` 放 Secrets；`DEPLOY_PATH`、`DEPLOY_RESTART_COMMAND` 放 Variables。
- 远程服务器上的真实 `.env` 不应该提交到 GitHub，应由服务器本地维护或后续引入更正式的密钥管理方案。
