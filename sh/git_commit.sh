#!/bin/bash
# ============================================
# CampusOS Git 快捷提交脚本
# 用法：
#   ./sh/git_commit.sh                    # 交互模式（显示变更，提示输入提交信息）
#   ./sh/git_commit.sh "提交信息"           # 快速模式（直接提交所有变更）
#   ./sh/git_commit.sh -m "提交信息"        # 同上
#   ./sh/git_commit.sh -p                  # 仅 push，不提交
#   ./sh/git_commit.sh -s                  # 仅查看状态，不提交
#   ./sh/git_commit.sh -l [n]             # 查看最近 n 条提交记录（默认 10）
#   ./sh/git_commit.sh -d                  # 查看变更详情（diff）
# ============================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 确保在项目根目录运行
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

# ---- 辅助函数 ----
print_header() {
    echo ""
    echo -e "${CYAN}════════════════════════════════════════${NC}"
    echo -e "${CYAN}  CampusOS Git 工具${NC}"
    echo -e "${CYAN}════════════════════════════════════════${NC}"
    echo ""
}

show_status() {
    echo -e "${BLUE}📋 工作区状态：${NC}"
    echo "────────────────────────────────────────"
    git status -s
    echo ""
}

show_diff_summary() {
    echo -e "${BLUE}📊 变更统计：${NC}"
    echo "────────────────────────────────────────"
    git diff --stat 2>/dev/null || true
    git diff --cached --stat 2>/dev/null || true
    echo ""
}

do_commit() {
    local msg="$1"

    # 检查是否有变更
    if git diff --quiet HEAD 2>/dev/null && [ -z "$(git ls-files --others --exclude-standard)" ]; then
        echo -e "${YELLOW}⚠️  没有检测到任何变更，无需提交。${NC}"
        return 0
    fi

    # 添加所有变更
    echo -e "${BLUE}📦 暂存所有变更...${NC}"
    git add -A

    # 显示将要提交的内容
    echo -e "${BLUE}📋 将要提交的文件：${NC}"
    git diff --cached --name-status
    echo ""

    # 提交
    echo -e "${GREEN}✅ 提交中...${NC}"
    git commit -m "$msg"
    echo ""
    echo -e "${GREEN}✅ 提交成功！${NC}"
    echo -e "   提交信息: ${YELLOW}$msg${NC}"
    echo -e "   提交哈希: $(git rev-parse --short HEAD)"
    echo ""
}

do_push() {
    local branch
    branch=$(git rev-parse --abbrev-ref HEAD)
    echo -e "${BLUE}🚀 推送到远程 (${branch})...${NC}"
    if git push origin "$branch"; then
        echo -e "${GREEN}✅ 推送成功！${NC}"
    else
        echo -e "${RED}❌ 推送失败，请检查网络或远程仓库配置。${NC}"
        return 1
    fi
}

ask_push() {
    echo ""
    read -rp "$(echo -e "${YELLOW}是否推送到远程仓库？[Y/n]: ${NC}")" push_choice
    case "$push_choice" in
        [nN]|[nN][oO])
            echo -e "${YELLOW}⏭️  跳过推送。${NC}"
            ;;
        *)
            do_push
            ;;
    esac
}

show_log() {
    local count="${1:-10}"
    echo -e "${BLUE}📜 最近 ${count} 条提交记录：${NC}"
    echo "────────────────────────────────────────"
    git log --oneline --graph -n "$count" --format="%C(yellow)%h%C(reset) %C(green)%ar%C(reset) %s"
    echo ""
}

# ---- 参数解析 ----
print_header

case "${1:-}" in
    -h|--help)
        echo "用法："
        echo "  ./sh/git_commit.sh              交互模式"
        echo "  ./sh/git_commit.sh \"提交信息\"     快速提交所有变更"
        echo "  ./sh/git_commit.sh -m \"提交信息\"  同上"
        echo "  ./sh/git_commit.sh -p            仅推送"
        echo "  ./sh/git_commit.sh -s            仅查看状态"
        echo "  ./sh/git_commit.sh -l [n]        查看提交记录（默认10条）"
        echo "  ./sh/git_commit.sh -d            查看变更详情"
        echo "  ./sh/git_commit.sh -h            显示帮助"
        echo ""
        exit 0
        ;;
    -s)
        show_status
        show_diff_summary
        exit 0
        ;;
    -d)
        show_status
        echo -e "${BLUE}📝 变更详情：${NC}"
        echo "────────────────────────────────────────"
        git diff 2>/dev/null || true
        git diff --cached 2>/dev/null || true
        echo ""
        exit 0
        ;;
    -l)
        show_log "${2:-10}"
        exit 0
        ;;
    -p)
        show_status
        do_push
        exit 0
        ;;
    -m)
        if [ -z "${2:-}" ]; then
            echo -e "${RED}❌ 错误：请提供提交信息。用法: ./sh/git_commit.sh -m \"提交信息\"${NC}"
            exit 1
        fi
        show_status
        do_commit "$2"
        ask_push
        exit 0
        ;;
    "")
        # 交互模式
        show_status
        show_diff_summary

        # 检查是否有变更
        if git diff --quiet HEAD 2>/dev/null && [ -z "$(git ls-files --others --exclude-standard)" ]; then
            echo -e "${YELLOW}⚠️  没有检测到任何变更，无需提交。${NC}"
            echo ""
            show_log 5
            exit 0
        fi

        # 提交信息模板
        echo -e "${BLUE}💡 提交类型参考：${NC}"
        echo "  feat:     新功能"
        echo "  fix:      修复 bug"
        echo "  docs:     文档更新"
        echo "  style:    代码格式（不影响功能）"
        echo "  refactor: 重构"
        echo "  test:     测试相关"
        echo "  chore:    构建/工具变更"
        echo "  update:   更新"
        echo ""

        read -rp "$(echo -e "${GREEN}请输入提交信息 (留空使用默认值): ${NC}")" commit_msg

        if [ -z "$commit_msg" ]; then
            commit_msg="更新参考CampusOS/docs/进度"
            echo -e "${YELLOW}📝 使用默认提交信息: ${commit_msg}${NC}"
        fi

        do_commit "$commit_msg"
        ask_push
        ;;
    *)
        # 第一个参数作为提交信息
        show_status
        do_commit "$1"
        ask_push
        ;;
esac

echo ""
echo -e "${CYAN}════════════════════════════════════════${NC}"
echo -e "${CYAN}  完成！${NC}"
echo -e "${CYAN}════════════════════════════════════════${NC}"