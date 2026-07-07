# CampusOS PR 提交脚本使用说明

> 适用脚本：`sh/git_pr.sh`
> 文档日期：2026-07-07
> 目标：说明如何用本地脚本基于当前分支创建 GitHub Pull Request

## 1. 脚本定位

`sh/git_pr.sh` 是 Pull Request 创建辅助脚本。它不负责生成 commit；提交代码仍使用：

```bash
./sh/git_commit.sh
```

PR 脚本负责：

| 步骤 | 说明 |
| --- | --- |
| 检查 GitHub CLI | 需要本机安装 `gh` |
| 检查 GitHub 登录 | 需要先执行 `gh auth login` |
| 检查当前分支 | 不允许直接从 `main`、`master`、`develop` 创建 PR |
| 检查工作区 | 默认要求工作区干净，避免未提交改动被误认为已进入 PR |
| 推送分支 | 默认执行 `git push -u origin 当前分支` |
| 创建 PR | 调用 `gh pr create`，默认读取仓库 PR 模板 |

## 2. 前置要求

### 2.1 安装 GitHub CLI

确认本机已有 `gh`：

```bash
gh --version
```

如果未安装，需要先安装 GitHub CLI。

### 2.2 登录 GitHub

```bash
gh auth login
gh auth status
```

`gh auth status` 正常后再运行 PR 脚本。

### 2.3 先提交本地改动

PR 只能包含已经 commit 并 push 到远程分支的内容。推荐流程：

```bash
./sh/git_commit.sh "docs: update readme"
./sh/git_pr.sh -t "docs: update readme"
```

如果当前工作区还有未提交改动，`git_pr.sh` 默认会停止，防止遗漏。

## 3. 基本用法

交互创建 PR：

```bash
./sh/git_pr.sh
```

指定标题创建 PR：

```bash
./sh/git_pr.sh -t "feat: add webhook management"
```

创建 Draft PR：

```bash
./sh/git_pr.sh -t "feat: add webhook management" --draft
```

指定目标分支：

```bash
./sh/git_pr.sh -t "fix: update migration docs" --base main
```

使用 commit 自动填充标题和描述：

```bash
./sh/git_pr.sh --fill
```

打开浏览器创建 PR：

```bash
./sh/git_pr.sh --web
```

只查看将执行什么，不真正 push 或创建 PR：

```bash
./sh/git_pr.sh -t "docs: preview pr command" --dry-run
```

## 4. PR 描述来源

默认情况下，脚本会使用仓库已有 PR 模板：

```text
.github/PULL_REQUEST_TEMPLATE/pull_request_template.md
```

也可以指定描述文本：

```bash
./sh/git_pr.sh -t "docs: update help" --body "更新 GitHub PR 脚本说明。"
```

或指定描述文件：

```bash
./sh/git_pr.sh -t "docs: update help" --body-file /tmp/pr-body.md
```

如果使用 `--fill`，脚本会交给 `gh pr create --fill` 根据 commit 信息自动生成标题和描述。

## 5. 常用参数

| 参数 | 说明 |
| --- | --- |
| `-t, --title TEXT` | PR 标题 |
| `-b, --base BRANCH` | PR 目标分支 |
| `--body TEXT` | PR 描述文本 |
| `--body-file FILE` | PR 描述文件 |
| `-f, --fill` | 使用 commit 信息自动填充 |
| `-d, --draft` | 创建 Draft PR |
| `-w, --web` | 打开浏览器创建 PR |
| `--no-push` | 不自动 push 当前分支 |
| `--allow-dirty` | 允许工作区存在未提交改动 |
| `--dry-run` | 只打印将执行的命令 |
| `-h, --help` | 查看帮助 |

## 6. 推荐工作流

```text
完成开发或文档修改
        ↓
按改动范围运行本地验证
        ↓
./sh/git_commit.sh "提交信息"
        ↓
./sh/git_pr.sh -t "PR 标题"
        ↓
在 GitHub 页面补充 PR 模板细节
        ↓
等待 GitHub Actions CI 通过
```

本地验证建议参考：

| 改动范围 | 建议命令 |
| --- | --- |
| Go 后端 | `GOCACHE=/tmp/campusos-go-cache go test ./... -count=1` |
| 数据库迁移 | `make migrate-up && make migrate-status` |
| 用户前台 | `cd web && pnpm build` |
| 管理后台 | `cd admin && pnpm build` |
| 文档 | 检查 Markdown 链接、路径和格式 |

## 7. 常见问题

### 7.1 提示 GitHub CLI 未登录

执行：

```bash
gh auth login
```

再用：

```bash
gh auth status
```

确认登录状态。

### 7.2 提示工作区存在未提交改动

这说明还有文件没有进入 commit。先查看：

```bash
git status --short
```

如果这些改动属于本次 PR，先提交：

```bash
./sh/git_commit.sh "提交信息"
```

如果这些改动不属于本次 PR，但你确认可以忽略，可使用：

```bash
./sh/git_pr.sh -t "PR 标题" --allow-dirty
```

### 7.3 已经 push 过，不想脚本再次 push

```bash
./sh/git_pr.sh -t "PR 标题" --no-push
```

### 7.4 不确定目标分支

脚本会优先从 `origin/HEAD` 推断目标分支，其次尝试 `main`、`develop`。如果项目实际目标分支不同，显式指定：

```bash
./sh/git_pr.sh -t "PR 标题" --base main
```

## 8. 与 CI 的关系

脚本只是创建 PR，不替代 CI。PR 创建后，`.github/workflows/ci_test.yml` 会在 GitHub 上执行后端测试、数据库迁移和前端构建。合并前仍应等待 CI 结果。

更多说明见：

| 文档 | 说明 |
| --- | --- |
| `docs/help/github使用相关/PR模板与CI自测说明.md` | PR 模板填写和本地自测 |
| `docs/help/github使用相关/GitHub Actions CI-CD使用说明.md` | CI/CD 流程和配置 |
