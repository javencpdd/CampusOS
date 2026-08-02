# 贡献、Pull Request 与 CI/CD

> 当前基线：`v0.13.0`  
> 适用对象：后端、Web、Admin、Docs、SDK、插件和资源包贡献者

## 1. 修改前

```bash
git status --short
git branch --show-current
```

不要覆盖工作树中与当前任务无关的改动。先按
[模块与插件边界](/guide/module-plugin-resource-boundaries) 确认代码和数据归属。

## 2. 按改动范围验证

| 改动 | 最小验证 |
| --- | --- |
| Go/Core/Feature | 定向测试、`GOCACHE=/tmp/campusos-go-cache go test ./... -count=1` |
| Web | `pnpm lint`、`pnpm format:check`、组件测试、`pnpm build` |
| Admin | 组件测试、`pnpm build` |
| Docs | `make readme-check`、`make docs-links`、docs-site build |
| Migration | migration drill、`make database-check`、`make data-governance-check` |
| Plugin/Resource | CLI inspect、包/权限负向测试、`make architecture-check` |
| Docker | `make docker-deploy-check` 和受影响的备份恢复流程 |
| 任意文本/资源包 | `python scripts/check-line-endings.py --include-untracked` |

所有改动都执行：

```bash
git diff --check
python scripts/check-line-endings.py --include-untracked
```

换行检查默认只读，并依据 `.gitattributes` 同时校验 UTF-8、LF/CRLF 与二进制声明。只有需要修复当前工作区时
才使用 `--fix`；脚本不会执行 `git add`。CI 会在 `ubuntu-latest` 和 `windows-latest` 分别执行只读检查。

版本封板和高风险改动使用：

```bash
RUN_RESTORE_DRILL=true RUN_BROWSER_SMOKE=true make release-check
```

只记录实际执行过的命令。没有证据时不要写“已验收”。

## 3. 提交辅助脚本

两个 Bash 脚本始终读取当前 Git 分支，切换分支后不需要修改脚本：

| 脚本 | 作用 |
| --- | --- |
| `sh/git_commit.sh` | 查看状态/diff、`git add -A`、commit，并按确认 push 当前分支 |
| `sh/git_pr.sh` | 检查分支、工作区和 GitHub CLI，push 当前分支并创建 PR |

Windows 应在 Git Bash/WSL2 中运行；PowerShell 可以显式调用 Git for Windows：

```powershell
$gitExe = (Get-Command git.exe -ErrorAction Stop).Source
$gitRoot = Split-Path (Split-Path $gitExe -Parent) -Parent
$gitBash = Join-Path $gitRoot 'bin\bash.exe'

& $gitBash ./sh/git_commit.sh 'feat: describe the change'
gh auth status
& $gitBash ./sh/git_pr.sh -t 'feat: describe the change' --base main --dry-run
& $gitBash ./sh/git_pr.sh -t 'feat: describe the change' --base main
```

`$gitBash` 是当前 PowerShell 会话变量，每次新开终端都需要重新执行前三行初始化。默认安装路径下也可直接使用
`& 'C:\Program Files\Git\bin\bash.exe' ./sh/git_commit.sh 'feat: describe the change'`。变量未初始化时，
`& $gitBash ...` 会被 PowerShell 拒绝；去掉 `&` 同样不是合法调用。

PowerShell 的 `Set-ExecutionPolicy` 不影响 `.sh`；也不要依赖可能指向 WSL 的裸 `bash`。环境变量使用
`$env:CAMPUSOS_GIT_REMOTE = 'my-fork'` 后再调用 `$gitBash`。完整 Windows 命令、退出码检查和故障处理见仓库
Help 的 PR 脚本说明。

标准流程：

```bash
git branch --show-current
git status --short
./sh/git_commit.sh -s
./sh/git_commit.sh "feat: describe the change"

gh auth login
gh auth status
./sh/git_pr.sh -t "feat: describe the change" --base main --dry-run
./sh/git_pr.sh -t "feat: describe the change" --base main
```

`git_commit.sh` 会暂存全部改动，新分支 push 时通过 `-u` 建立 upstream，并默认拒绝直接 push
`main`、`master`、`develop`。`git_pr.sh` 默认要求工作区干净、使用
`.github/PULL_REQUEST_TEMPLATE/pull_request_template.md`，从 `origin/HEAD` 推断 base，并拒绝 head/base
相同。使用 fork 或其他 remote 时传 `--remote <name>`；目标不是默认主干时传 `--base <branch>`。

辅助脚本不会替你选择测试范围，也不会自动证明改动安全。仓库内详细说明见
`docs/help/github使用相关/PR提交脚本使用说明.md`。

## 4. Pull Request 应说明什么

PR 至少包含：

- 实际完成的行为，不写未实现愿景。
- 主要代码、API、数据或文档变化。
- 兼容和迁移处理。
- 实际测试结果。
- 风险、回滚方式和已知限制。

迁移、权限、可靠写入和插件供应链改动还要提供负向测试。前端改动应说明检查过的手机、平板和桌面视口。

## 5. 当前 CI

GitHub CI 文件是：

```text
.github/workflows/ci_test.yml
```

触发范围包括 `main`、`develop`、受支持的功能/修复分支和指向主分支的 Pull Request。它包含：

### Cross-platform Text Matrix

- 在 `ubuntu-latest` 和 `windows-latest` 分别检出同一提交。
- 校验 `.gitattributes` 的文本/二进制分类、UTF-8 编码和 LF/CRLF 规则。
- 不在 CI 中自动修复，发现漂移直接失败。

### Backend Test

- PostgreSQL 16 service。
- migration 和 migration 状态。
- schema/data audit 与数据库合同。
- 全量 Go 测试和后端构建。
- API/授权合同漂移检查。
- 文档链接和模块边界检查。

### Frontend Matrix

- Web lint、格式检查、组件测试和构建。
- Admin 组件测试和构建。
- docs-site 构建。
- TypeScript Plugin SDK 构建。

同一分支只保留最新一次 CI。CI 使用只读仓库权限，不获得部署 Secret。

## 6. 当前 CD

发布工作流是：

```text
.github/workflows/deploy.yml
```

推送 `v*` 标签或手动触发后，它会重新执行 migration、Go 测试、合同和文档检查，构建 Linux `amd64`
后端、Web、Admin、Docs 和 TypeScript SDK，并上传发布包。

SSH 部署只有配置 production Environment 和以下 Secret 后才执行：

```text
HOST
USERNAME
SSH_KEY
SSH_PORT（可选）
```

非敏感 Variables：

```text
DEPLOY_PATH
DEPLOY_RESTART_COMMAND
```

当前 workflow 的 Linux `amd64` 发布包不等于 Windows 或 Linux `arm64` 已完成发行认证。容器化交付应按
[Docker 部署与迁移](/deployment/docker) 独立验证。

## 7. 合并前检查

1. CI 全部通过。
2. migration、API、权限、配置和目录变化已记录。
3. 文档与代码使用相同术语和版本。
4. 没有密钥、Token、真实验证码、用户数据或本机绝对路径进入提交。
5. 没有删除权限、审计、路径、配额、内容清洗或数据库约束来绕过测试。
6. 进度文档包含实际证据和回滚方式。

## 8. 继续阅读

- [开发者学习路线](/guide/developer-learning-path)
- [构建与发布](/deployment/release)
- [文档状态与历史替代](/project/document-lifecycle)
- 仓库内 `docs/help/系统设计相关/开发运行与验证指南.md`
