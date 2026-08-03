# CampusOS Git 提交与 PR 脚本使用说明

> 适用脚本：`sh/git_commit.sh`、`sh/git_pr.sh`
> 文档日期：2026-08-02
> 目标：说明如何安全提交当前分支、推送 upstream 并创建 GitHub Pull Request

## 1. 脚本定位

两个脚本职责分离：

| 脚本 | 负责 | 不负责 |
| --- | --- | --- |
| `sh/git_commit.sh` | 状态/diff、`git add -A`、commit、按确认 push 当前分支 | 不创建 PR、不替代测试 |
| `sh/git_pr.sh` | 分支/工作区/`gh` 检查、push、`gh pr create` | 不生成 commit、不自动修复 CI |

切换 Git 分支后不需要修改脚本。两个脚本都在运行时读取当前分支；

默认 remote 是 `origin`。当前分支为`djw-window` 时，push 使用 `origin/djw-window`。

PR base 默认从 `origin/HEAD` 推断，本仓库当前为`main`。

PR 脚本负责：

| 步骤 | 说明 |
| --- | --- |
| 检查 GitHub CLI | 需要本机安装 `gh` |
| 检查 GitHub 登录 | 需要先执行 `gh auth login` |
| 检查当前分支 | 不允许直接从 `main`、`master`、`develop` 创建 PR |
| 检查工作区 | 默认要求工作区干净，避免未提交改动被误认为已进入 PR |
| 推送分支 | 默认执行 `git push -u origin 当前分支` |
| 创建 PR | 调用 `gh pr create`，默认读取仓库 PR 模板 |

## 2. Windows、Git Bash 和 WSL2

### 2.1 在 PowerShell 中调用 Git Bash（推荐）

`$gitBash` 不是 PowerShell 内置变量，也不是仓库自动注入的变量。每次新开 PowerShell/Windows Terminal 会话后，都要先执行下面的初始化片段，再执行 `& $gitBash ...`。如果只复制调用命令，`$gitBash` 为 `$null`，PowerShell会报“`&` 后面的表达式不是有效命令”。

先在仓库根目录运行以下初始化片段。它会从当前 `git.exe` 的安装位置寻找同一套 Git Bash，避免系统中的
`bash.exe` 实际指向 WSL：

```powershell
Set-Location 'C:\Users\19046\Desktop\Code\CampusOS'

$gitExe = (Get-Command git.exe -ErrorAction Stop).Source
$gitRoot = Split-Path (Split-Path $gitExe -Parent) -Parent
$gitBash = Join-Path $gitRoot 'bin\bash.exe'
if (-not (Test-Path -LiteralPath $gitBash)) {
    throw "没有找到 Git for Windows Bash：$gitBash"
}
```

如果 Git 安装在默认目录，也可以完全不使用变量，直接执行这一条自包含命令：

```powershell
& 'C:\Program Files\Git\bin\bash.exe' ./sh/git_commit.sh '更新参考 CampusOS/docs/进度'
```

不要写成 `$gitBash ./sh/git_commit.sh ...`。PowerShell 调用保存在变量中的命令路径时必须保留前面的 `&`；但前提仍是 `$gitBash` 已经赋值且指向真实文件。

随后所有脚本参数直接放在 `& $gitBash` 后面，不需要使用容易产生多层引号问题的 `bash -lc`：

```powershell
# 查看帮助、当前分支和变更，不会写入仓库
& $gitBash ./sh/git_commit.sh --help
& $gitBash ./sh/git_commit.sh -s
& $gitBash ./sh/git_commit.sh -d

# 暂存全部改动、创建 commit，并按提示决定是否 push
& $gitBash ./sh/git_commit.sh '更新参考 CampusOS/docs/进度'

# 只推送当前分支并建立 upstream
& $gitBash ./sh/git_commit.sh -p

# 预览 PR 命令；不会 push，也不会创建 PR
& $gitBash ./sh/git_pr.sh -t '更新参考 CampusOS/docs/进度' --base main --dry-run

# 推送当前分支并创建 PR
& $gitBash ./sh/git_pr.sh -t '更新参考 CampusOS/docs/进度' --base main
```

每次执行后可在 PowerShell 检查 Bash 脚本的退出码：

```powershell
if ($LASTEXITCODE -ne 0) {
    throw "CampusOS Git 脚本执行失败，退出码：$LASTEXITCODE"
}
# If we reach here, $LASTEXITCODE is 0 — genuine success
Write-Host "CampusOS Git 脚本执行成功"
```

默认安装位置确实是 `C:\Program Files\Git` 时，也可以使用较短写法：

```powershell
& 'C:\Program Files\Git\bin\bash.exe' ./sh/git_commit.sh -s
& 'C:\Program Files\Git\bin\bash.exe' ./sh/git_pr.sh --help
```



PS: 在linux环境中，就不需要 `& 'C:\Program Files\Git\bin\bash.exe'`  或者 `& $gitBash`

直接使用 `./sh/git_commit.sh` 或 `./sh/git_pr.sh` 即可



### 2.2 Windows 完整提交与 PR 流程

下面的 PowerShell 命令可以直接按顺序执行：

```powershell
Set-Location 'C:\Users\19046\Desktop\Code\CampusOS'

git branch --show-current
git status --short

$gitExe = (Get-Command git.exe -ErrorAction Stop).Source
$gitRoot = Split-Path (Split-Path $gitExe -Parent) -Parent
$gitBash = Join-Path $gitRoot 'bin\bash.exe'

# 注意：该脚本会执行 git add -A，先确认没有混入无关改动
& $gitBash ./sh/git_commit.sh 'fix: resolve WebUI issues'
if ($LASTEXITCODE -ne 0) { throw 'commit 脚本执行失败' }

# GitHub CLI 登录发生在 Windows 当前用户环境中
gh auth status
# 尚未登录时执行：gh auth login

& $gitBash ./sh/git_pr.sh -t 'fix: resolve WebUI issues' --base main --dry-run
if ($LASTEXITCODE -ne 0) { throw 'PR 预览失败' }

& $gitBash ./sh/git_pr.sh -t 'fix: resolve WebUI issues' --base main
if ($LASTEXITCODE -ne 0) { throw 'PR 创建失败' }
```

`git_commit.sh` 默认在 commit 后询问是否 push。如果已经选择了 push，随后 `git_pr.sh` 再次执行 push 是安全的，
通常只会显示远程分支已经是最新状态。

### 2.3 Windows 下设置脚本环境变量

Bash 文档中的 `NAME=value ./script.sh` 不能原样复制到 PowerShell。PowerShell 要先设置 `$env:`：

```powershell
# 使用 fork remote
$env:CAMPUSOS_GIT_REMOTE = 'my-fork'
try {
    & $gitBash ./sh/git_commit.sh -p
} finally {
    Remove-Item Env:CAMPUSOS_GIT_REMOTE -ErrorAction SilentlyContinue
}
```

维护者确需直接推送受保护分支时，PowerShell 写法如下；普通贡献者不应启用：

```powershell
$env:CAMPUSOS_ALLOW_PROTECTED_PUSH = 'true'
try {
    & $gitBash ./sh/git_commit.sh -p
} finally {
    Remove-Item Env:CAMPUSOS_ALLOW_PROTECTED_PUSH -ErrorAction SilentlyContinue
}
```

### 2.4 直接使用 Git Bash

从开始菜单打开 Git Bash，路径要写成 Git Bash 风格：

```bash
cd /c/Users/19046/Desktop/Code/CampusOS
./sh/git_commit.sh -s
./sh/git_commit.sh "fix: resolve WebUI issues"
gh auth status
./sh/git_pr.sh -t "fix: resolve WebUI issues" --base main --dry-run
./sh/git_pr.sh -t "fix: resolve WebUI issues" --base main
```

### 2.5 使用 WSL2

WSL2 中 Windows 的 `C:` 盘路径是 `/mnt/c`：

```bash
cd /mnt/c/Users/19046/Desktop/Code/CampusOS
./sh/git_commit.sh -s
./sh/git_pr.sh --help
```

WSL 内需要能够执行 `git` 和 `gh`，其全局 Git 身份、凭据和 GitHub CLI 登录状态可能与 Windows 独立。不要在
一个未配置身份或未登录 `gh` 的 WSL 环境中直接创建提交。仓库放在 `C:` 盘且主要使用 Windows 工具时，Git Bash
通常更简单。

单独输入 `sh` 只会尝试进入或启动一个 POSIX Shell，不会自动提交或创建 PR。在 PowerShell 中也不要依赖裸
`bash` 命令，因为它可能指向 WSL；使用上面解析得到的 `$gitBash` 最明确。

## 3. 前置要求

后续章节为了避免重复，命令块主要使用 Git Bash/WSL 写法。Windows PowerShell 用户应把
`./sh/git_commit.sh ...` 替换为 `& $gitBash ./sh/git_commit.sh ...`，把 `./sh/git_pr.sh ...` 替换为
`& $gitBash ./sh/git_pr.sh ...`；普通 `git`、`gh` 命令可以直接在 PowerShell 执行。

### 3.1 确认分支和工作区

```bash
git branch --show-current
git status --short
git remote -v
git branch -vv
```

不要在 detached HEAD 下提交或创建 PR。提交脚本会暂存全部已跟踪和未跟踪改动，因此工作区存在无关文件时
应先拆分或清理，不要直接运行快速提交。

### 3.2 安装 GitHub CLI

确认本机已有 `gh`：

```bash
gh --version
```

如果未安装，需要先安装 GitHub CLI。

### 3.3 登录 GitHub

```bash
gh auth login
gh auth status
```

`gh auth status` 正常后再运行 PR 脚本。

### 3.4 先提交本地改动

PR 只能包含已经 commit 并 push 到远程分支的内容。推荐流程：

```bash
./sh/git_commit.sh "更新参考CampusOS/docs/进度"
./sh/git_pr.sh -t "更新参考CampusOS/docs/进度"
```

如果当前工作区还有未提交改动，`git_pr.sh` 默认会停止，防止遗漏。

## 4. `git_commit.sh` 用法

只查看分支、upstream 和文件状态：

```bash
./sh/git_commit.sh -s
```

查看完整 diff：

```bash
./sh/git_commit.sh -d
```

交互输入提交信息：

```bash
./sh/git_commit.sh
```

直接指定提交信息：

```bash
./sh/git_commit.sh "docs: update Windows Docker guide"
./sh/git_commit.sh -m "fix: handle current branch safely"
```

只 push 当前分支：

```bash
./sh/git_commit.sh -p
```

push 使用：

```text
git push -u <remote> <当前分支>
```

因此新分支会自动建立 upstream，切换分支后不会继续推送旧分支。默认 remote 为 `origin`；需要使用其他
remote 时设置：

```bash
CAMPUSOS_GIT_REMOTE=my-fork ./sh/git_commit.sh -p
```

脚本默认拒绝直接 push `main`、`master`、`develop`。维护者确有需要时必须显式设置
`CAMPUSOS_ALLOW_PROTECTED_PUSH=true`；普通贡献流程不应使用该开关。

## 5. `git_pr.sh` 基本用法

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

## 6. PR 描述来源

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

## 7. 常用参数

| 参数 | 说明 |
| --- | --- |
| `-t, --title TEXT` | PR 标题 |
| `-b, --base BRANCH` | PR 目标分支 |
| `-r, --remote REMOTE` | push 和 base 推断使用的 remote，默认 `origin` |
| `--body TEXT` | PR 描述文本 |
| `--body-file FILE` | PR 描述文件 |
| `-f, --fill` | 使用 commit 信息自动填充 |
| `-d, --draft` | 创建 Draft PR |
| `-w, --web` | 打开浏览器创建 PR |
| `--no-push` | 不自动 push 当前分支 |
| `--allow-dirty` | 允许工作区存在未提交改动 |
| `--dry-run` | 只打印将执行的命令 |
| `-h, --help` | 查看帮助 |

## 8. 推荐工作流

```text
完成开发或文档修改
        ↓
按改动范围运行本地验证
        ↓
./sh/git_commit.sh "提交信息"
        ↓
确认 push 的分支与 remote
        ↓
./sh/git_pr.sh -t "PR 标题" --base main --dry-run
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

## 9. 常见问题

### 9.1 提示 GitHub CLI 未登录

执行：

```bash
gh auth login
```

再用：

```bash
gh auth status
```

确认登录状态。

### 9.2 提示工作区存在未提交改动

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

### 9.3 已经 push 过，不想脚本再次 push

```bash
./sh/git_pr.sh -t "PR 标题" --no-push
```

### 9.4 不确定目标分支

脚本会优先从 `origin/HEAD` 推断目标分支，其次尝试 `main`、`develop`。如果项目实际目标分支不同，显式指定：

```bash
./sh/git_pr.sh -t "PR 标题" --base main
```

### 9.5 切换分支后是否需要修改脚本

不需要。先确认：

```bash
git branch --show-current
git branch -vv
```

`git_commit.sh` push 当前分支；`git_pr.sh` 用当前分支作为 head。只有 PR 目标不是 `origin/HEAD` 时才需要
显式传 `--base`，只有使用 fork/其他远程仓库时才需要 `--remote` 或 `CAMPUSOS_GIT_REMOTE`。

### 9.6 当前分支与目标分支相同

脚本会拒绝创建这种 PR。请切换到功能分支：

```bash
git switch -c docs/windows-docker-guide
```

或核对后指定正确 base。

### 9.7 PowerShell 提示“无法将 .sh 识别为命令”

不要直接执行 `./sh/git_commit.sh`，也不需要修改 PowerShell Execution Policy。重新按 2.1 节解析 Git Bash：

```powershell
$gitExe = (Get-Command git.exe -ErrorAction Stop).Source
$gitRoot = Split-Path (Split-Path $gitExe -Parent) -Parent
$gitBash = Join-Path $gitRoot 'bin\bash.exe'
& $gitBash ./sh/git_commit.sh --help
```

如果错误是“`&` 后面的表达式不是有效命令”，先检查变量：

```powershell
if (-not $gitBash) {
    Write-Error '$gitBash 尚未初始化，请先执行 2.1 节初始化片段'
} else {
    Test-Path -LiteralPath $gitBash
}
```

进入第一个分支表示变量尚未初始化；`Test-Path` 返回 `False` 表示保存的路径无效。重新执行上面的初始化片段，
或者直接使用完整路径调用。

如果 `$gitBash` 不存在，说明当前 `git.exe` 不是完整的 Git for Windows 安装，或者安装目录布局不同。此时先用
`Get-Command git.exe` 确认来源，不要随意调用可能指向 WSL 的裸 `bash`。

### 9.8 PowerShell 能执行 `gh`，但 PR 脚本提示缺少 `gh`

先确认 Git Bash 继承的 PATH 中也能找到 GitHub CLI：

```powershell
gh --version
& $gitBash -c 'command -v gh && gh --version'
```

如果第一条成功、第二条失败，关闭并重新打开 Windows Terminal/PowerShell，使安装 GitHub CLI 后的新 PATH 生效；
仍失败时把 GitHub CLI 安装目录加入用户 PATH。不要把访问令牌直接写进脚本或仓库。

### 9.9 Windows 下 `--body-file` 路径不存在

优先使用仓库相对路径，例如：

```powershell
& $gitBash ./sh/git_pr.sh -t 'docs: update guide' --body-file .github/PULL_REQUEST_TEMPLATE/pull_request_template.md
```

必须使用绝对路径时改成 Git Bash 路径格式，例如 `C:\Users\19046\pr.md` 对应
`/c/Users/19046/pr.md`。不要把带反斜杠的 Windows 路径直接交给 Bash 的 `--body-file`。

## 10. 与 CI 的关系

脚本只是创建 PR，不替代 CI。PR 创建后，`.github/workflows/ci_test.yml` 会在 GitHub 上执行后端测试、数据库迁移和前端构建。合并前仍应等待 CI 结果。

更多说明见：

| 文档 | 说明 |
| --- | --- |
| `docs/help/系统设计相关/开发运行与验证指南.md` | 当前本地验证、提交与 CI 入口 |
| `docs-site/contributing/workflow.md` | 官方贡献、PR 与 CI/CD 完整流程 |
