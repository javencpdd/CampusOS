#!/bin/bash
# ============================================
# CampusOS GitHub PR 快捷脚本
# 用法：
#   ./sh/git_pr.sh
#   ./sh/git_pr.sh -t "PR 标题"
#   ./sh/git_pr.sh -t "PR 标题" --base main --draft
#   ./sh/git_pr.sh --fill
#   ./sh/git_pr.sh --no-push
#   ./sh/git_pr.sh --dry-run
#   ./sh/git_pr.sh --remote origin
# ============================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

BASE_BRANCH=""
TITLE=""
BODY_TEXT=""
BODY_FILE=""
DRAFT=false
FILL=false
WEB=false
NO_PUSH=false
ALLOW_DIRTY=false
DRY_RUN=false
REMOTE="origin"

print_header() {
    echo ""
    echo -e "${CYAN}========================================${NC}"
    echo -e "${CYAN}  CampusOS GitHub PR 工具${NC}"
    echo -e "${CYAN}========================================${NC}"
    echo ""
}

usage() {
    cat <<'EOF'
用法：
  ./sh/git_pr.sh
  ./sh/git_pr.sh -t "PR 标题"
  ./sh/git_pr.sh -t "PR 标题" --base main --draft
  ./sh/git_pr.sh --fill
  ./sh/git_pr.sh --body-file /tmp/pr.md
  ./sh/git_pr.sh --no-push
  ./sh/git_pr.sh --dry-run

参数：
  -t, --title TEXT       PR 标题；未提供时交互输入，--fill 模式除外
  -b, --base BRANCH      目标分支；默认从 REMOTE/HEAD 推断，其次 main/develop
  -r, --remote REMOTE    push 和 base 推断使用的 remote；默认 origin
      --body TEXT        PR 描述文本
      --body-file FILE   PR 描述文件；默认使用 .github/PULL_REQUEST_TEMPLATE/pull_request_template.md
  -f, --fill             使用 gh 根据 commit 自动填充标题和描述
  -d, --draft            创建 draft PR
  -w, --web              打开浏览器创建 PR
      --no-push          不自动 push 当前分支
      --allow-dirty      允许工作区存在未提交改动
      --dry-run          只打印将要执行的命令
  -h, --help             显示帮助

前置要求：
  - 已安装 GitHub CLI：gh
  - 已登录 GitHub：gh auth login
  - 当前分支已经有至少一个 commit
EOF
}

fail() {
    echo -e "${RED}错误：$*${NC}" >&2
    exit 1
}

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || fail "缺少命令：$1"
}

git_root_check() {
    git rev-parse --show-toplevel >/dev/null 2>&1 || fail "当前目录不是 Git 仓库"
}

current_branch() {
    local branch
    branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
    [ -n "$branch" ] || fail "当前处于 detached HEAD 状态，请先切换到功能分支"
    echo "$branch"
}

detect_base_branch() {
    if [ -n "$BASE_BRANCH" ]; then
        echo "$BASE_BRANCH"
        return
    fi

    local origin_head
    origin_head="$(git symbolic-ref --quiet --short refs/remotes/${REMOTE}/HEAD 2>/dev/null || true)"
    if [ -n "$origin_head" ]; then
        echo "${origin_head#${REMOTE}/}"
        return
    fi

    if git show-ref --verify --quiet "refs/remotes/${REMOTE}/main"; then
        echo "main"
        return
    fi

    if git show-ref --verify --quiet "refs/remotes/${REMOTE}/develop"; then
        echo "develop"
        return
    fi

    echo "main"
}

ensure_clean_worktree() {
    if [ "$ALLOW_DIRTY" = true ]; then
        return
    fi

    if [ -n "$(git status --porcelain)" ]; then
        echo -e "${YELLOW}当前工作区存在未提交改动：${NC}"
        git status --short
        echo ""
        fail "请先提交或暂存处理后再创建 PR；如确认这些改动不属于本 PR，可使用 --allow-dirty"
    fi
}

ensure_remote() {
    git remote get-url "$REMOTE" >/dev/null 2>&1 || fail "Git remote 不存在：$REMOTE"
}

ensure_branch_is_safe() {
    local branch="$1"
    local base="$2"

    case "$branch" in
        main|master|develop)
            fail "当前分支是 $branch，不建议直接从主干分支创建 PR。请先切换到功能分支"
            ;;
    esac

    if [ "$branch" = "$base" ]; then
        fail "当前分支与 PR 目标分支相同（$branch），请切换到功能分支或使用 --base 指定正确目标"
    fi
}

ensure_gh_auth() {
    if ! gh auth status >/dev/null 2>&1; then
        fail "GitHub CLI 未登录。请先执行：gh auth login"
    fi
}

ensure_branch_pushed() {
    local branch="$1"

    if [ "$NO_PUSH" = true ]; then
        echo -e "${YELLOW}跳过 push：--no-push 已启用${NC}"
        return
    fi

    echo -e "${BLUE}推送当前分支到 ${REMOTE}/${branch}...${NC}"
    if [ "$DRY_RUN" = true ]; then
        echo "git push -u ${REMOTE} ${branch}"
        return
    fi
    git push -u "$REMOTE" "$branch"
}

prepare_body_file() {
    if [ "$FILL" = true ] || [ "$WEB" = true ]; then
        return
    fi

    if [ -n "$BODY_FILE" ]; then
        [ -f "$BODY_FILE" ] || fail "PR 描述文件不存在：$BODY_FILE"
        echo "$BODY_FILE"
        return
    fi

    if [ -n "$BODY_TEXT" ]; then
        local tmp
        tmp="$(mktemp)"
        printf '%s\n' "$BODY_TEXT" >"$tmp"
        echo "$tmp"
        return
    fi

    local template=".github/PULL_REQUEST_TEMPLATE/pull_request_template.md"
    if [ -f "$template" ]; then
        echo "$template"
    fi
}

prompt_title() {
    if [ "$FILL" = true ] || [ "$WEB" = true ] || [ -n "$TITLE" ]; then
        return
    fi

    read -rp "$(echo -e "${GREEN}请输入 PR 标题: ${NC}")" TITLE
    [ -n "$TITLE" ] || fail "PR 标题不能为空"
}

build_gh_args() {
    local branch="$1"
    local base="$2"
    local body_path="$3"

    GH_ARGS=(pr create --base "$base" --head "$branch")

    if [ "$DRAFT" = true ]; then
        GH_ARGS+=(--draft)
    fi

    if [ "$WEB" = true ]; then
        GH_ARGS+=(--web)
        return
    fi

    if [ "$FILL" = true ]; then
        GH_ARGS+=(--fill)
        return
    fi

    GH_ARGS+=(--title "$TITLE")

    if [ -n "$body_path" ]; then
        GH_ARGS+=(--body-file "$body_path")
    else
        GH_ARGS+=(--body "")
    fi
}

print_summary() {
    local branch="$1"
    local base="$2"
    echo -e "${BLUE}PR 创建信息：${NC}"
    echo "  当前分支：$branch"
    echo "  目标分支：$base"
    echo "  远程仓库：$REMOTE"
    echo "  自动 push：$([ "$NO_PUSH" = true ] && echo "否" || echo "是")"
    echo "  Draft：$([ "$DRAFT" = true ] && echo "是" || echo "否")"
    echo "  Fill：$([ "$FILL" = true ] && echo "是" || echo "否")"
    echo "  Web：$([ "$WEB" = true ] && echo "是" || echo "否")"
    echo ""
}

while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help)
            usage
            exit 0
            ;;
        -t|--title)
            TITLE="${2:-}"
            [ -n "$TITLE" ] || fail "--title 需要参数"
            shift 2
            ;;
        -b|--base)
            BASE_BRANCH="${2:-}"
            [ -n "$BASE_BRANCH" ] || fail "--base 需要参数"
            shift 2
            ;;
        -r|--remote)
            REMOTE="${2:-}"
            [ -n "$REMOTE" ] || fail "--remote 需要参数"
            shift 2
            ;;
        --body)
            BODY_TEXT="${2:-}"
            shift 2
            ;;
        --body-file)
            BODY_FILE="${2:-}"
            [ -n "$BODY_FILE" ] || fail "--body-file 需要参数"
            shift 2
            ;;
        -f|--fill)
            FILL=true
            shift
            ;;
        -d|--draft)
            DRAFT=true
            shift
            ;;
        -w|--web)
            WEB=true
            shift
            ;;
        --no-push)
            NO_PUSH=true
            shift
            ;;
        --allow-dirty)
            ALLOW_DIRTY=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        *)
            fail "未知参数：$1。使用 --help 查看帮助"
            ;;
    esac
done

print_header
require_cmd git
require_cmd gh
git_root_check
ensure_remote
if [ "$DRY_RUN" != true ]; then
    ensure_gh_auth
fi

BRANCH="$(current_branch)"
BASE="$(detect_base_branch)"

ensure_branch_is_safe "$BRANCH" "$BASE"
ensure_clean_worktree
prompt_title

BODY_PATH="$(prepare_body_file)"
print_summary "$BRANCH" "$BASE"

ensure_branch_pushed "$BRANCH"

build_gh_args "$BRANCH" "$BASE" "${BODY_PATH:-}"

echo -e "${BLUE}创建 Pull Request...${NC}"
if [ "$DRY_RUN" = true ]; then
    printf 'gh'
    printf ' %q' "${GH_ARGS[@]}"
    printf '\n'
    exit 0
fi

gh "${GH_ARGS[@]}"
echo ""
echo -e "${GREEN}PR 创建流程完成。${NC}"
