#!/bin/bash
# ============================================================================
# 脚本名称: proxy.sh
# 功能描述: 代理开关脚本 - 支持开启/关闭系统代理，并测试连接
# 使用方法: source proxy.sh [on|off|status|test|help]
# 作者:    Jack
# 创建日期: 2024

## SSH (Socket) 代理实现方式

# 通过修改 `~/.ssh/config` 中的 `ProxyCommand` 实现 SSH 流量走代理：

# - SOCKS5 代理：使用 `nc -X 5 -x` 或 `ncat` 或 `connect-proxy -S`
# - HTTP 代理：使用 `connect-proxy -H` 或 `nc -X connect -x`
# - 脚本自动标记管理的配置行，关闭时精确清除不留残留

# ============================================================================

# ========================= 代理配置（按需修改） ==============================
PROXY_IP="127.0.0.1"          # 代理服务器 IP 地址
PROXY_PORT="7897"              # 代理服务器端口
PROXY_TYPE="http"              # 代理协议类型: http / socks5
# ============================================================================

# 生成代理地址（无需修改）
PROXY_URL="${PROXY_TYPE}://${PROXY_IP}:${PROXY_PORT}"
SOCKS_PROXY_URL="socks5://${PROXY_IP}:${PROXY_PORT}"

# 测试目标地址（使用 ping 测试）
TEST_TARGET="youtube.com"

# ================================ 颜色定义 ===================================
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # 恢复默认颜色

# ================================ 辅助函数 ===================================

# 打印带颜色的提示信息
_info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
_success() { echo -e "${GREEN}[OK]${NC}   $*"; }
_warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
_error()   { echo -e "${RED}[ERROR]${NC} $*"; }

# 显示帮助文档
_show_help() {
    cat << 'EOF'
╔══════════════════════════════════════════════════════════════╗
║                     代理管理脚本 (proxy.sh)                 ║
╠══════════════════════════════════════════════════════════════╣
║                                                              ║
║  用法:  source proxy.sh <命令>                               ║
║                                                              ║
║  命令:                                                       ║
║    on       开启代理 (设置环境变量)                          ║
║    off      关闭代理 (清除环境变量)                          ║
║    status   查看当前代理状态                                 ║
║    test     测试代理连接 (ping YouTube)                      ║
║    help     显示此帮助信息                                   ║
║                                                              ║
║  配置说明:                                                   ║
║    修改脚本开头的 PROXY_IP、PROXY_PORT、PROXY_TYPE 变量     ║
║    以适配你的代理服务器                                      ║
║                                                              ║
║  示例:                                                       ║
║    source proxy.sh on          # 开启代理                    ║
║    source proxy.sh off         # 关闭代理                    ║
║    source proxy.sh test        # 测试连接                    ║
║                                                              ║
║  注意:                                                       ║
║    - 必须使用 source 执行，否则环境变量无法生效              ║
║    - 也支持 . proxy.sh on 的写法                            ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝
EOF
}

# ================================ 核心功能 ===================================

# 开启代理
_proxy_on() {
    _info "正在开启代理..."
    _info "代理地址: ${CYAN}${PROXY_IP}:${PROXY_PORT}${NC}  协议: ${CYAN}${PROXY_TYPE}${NC}"

    # --- 设置环境变量 ---
    # 小写变量: 供大多数 Linux 工具使用 (curl, wget, apt 等)
    export http_proxy="${PROXY_URL}"
    export https_proxy="${PROXY_URL}"
    export all_proxy="${PROXY_URL}"
    export ftp_proxy="${PROXY_URL}"
    export no_proxy="localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

    # 大写变量: 供部分应用程序使用
    export HTTP_PROXY="${PROXY_URL}"
    export HTTPS_PROXY="${PROXY_URL}"
    export ALL_PROXY="${PROXY_URL}"
    export FTP_PROXY="${PROXY_URL}"
    export NO_PROXY="${no_proxy}"

    _success "代理环境变量已设置"

    # --- 可选: 配置 Git 代理 ---
    if command -v git &> /dev/null; then
        git config --global http.proxy "${PROXY_URL}"
        git config --global https.proxy "${PROXY_URL}"
        _success "Git 代理已配置"
    fi

    # --- 可选: 配置 SSH 代理 (Socket 代理) ---
    _configure_ssh_proxy "on"

    # --- 自动测试连接 ---
    _info "正在测试代理连接..."
    _test_connection
}

# 配置/清除 SSH 代理 (Socket 代理)
# 通过修改 ~/.ssh/config 中的 ProxyCommand 实现 SSH 流量走代理
_configure_ssh_proxy() {
    local action="$1"
    local ssh_config="$HOME/.ssh/config"
    local proxy_marker="# proxy.sh-managed"  # 用于识别脚本管理的配置行

    # 确保 ~/.ssh 目录存在
    if [ ! -d "$HOME/.ssh" ]; then
        mkdir -p "$HOME/.ssh"
        chmod 700 "$HOME/.ssh"
    fi

    if [ "${action}" = "on" ]; then
        # 根据代理类型选择 ProxyCommand
        # - nc (netcat):     支持 SOCKS5 代理
        # - connect-proxy:   支持 HTTP/HTTPS 代理 (需安装 connect-proxy)
        # - ssh -W:          SSH 内置转发 (SOCKS5)
        local proxy_cmd=""

        if [ "${PROXY_TYPE}" = "socks5" ]; then
            # 使用 nc (netcat) 进行 SOCKS5 代理转发
            if command -v nc &> /dev/null; then
                proxy_cmd="ProxyCommand nc -X 5 -x ${PROXY_IP}:${PROXY_PORT} %h %p ${proxy_marker}"
            elif command -v ncat &> /dev/null; then
                proxy_cmd="ProxyCommand ncat --proxy-type socks5 --proxy ${PROXY_IP}:${PROXY_PORT} %h %p ${proxy_marker}"
            elif command -v connect-proxy &> /dev/null; then
                proxy_cmd="ProxyCommand connect-proxy -S ${PROXY_IP}:${PROXY_PORT} %h %p ${proxy_marker}"
            else
                _warn "SSH 代理配置跳过: 未找到 nc/ncat/connect-proxy，请安装其中之一"
                return
            fi
        else
            # HTTP 代理: 优先使用 connect-proxy，回退到 nc
            if command -v connect-proxy &> /dev/null; then
                proxy_cmd="ProxyCommand connect-proxy -H ${PROXY_IP}:${PROXY_PORT} %h %p ${proxy_marker}"
            elif command -v nc &> /dev/null; then
                proxy_cmd="ProxyCommand nc -X connect -x ${PROXY_IP}:${PROXY_PORT} %h %p ${proxy_marker}"
            else
                _warn "SSH 代理配置跳过: 未找到 connect-proxy/nc，请安装其中之一"
                return
            fi
        fi

        # 清除旧的代理配置（如果有）
        if [ -f "${ssh_config}" ]; then
            sed -i "/${proxy_marker}/d" "${ssh_config}"
        fi

        # 写入 SSH 代理配置（全局生效，匹配所有主机）
        {
            echo ""
            echo "# ===== 代理配置 (由 proxy.sh 自动管理) ===== ${proxy_marker}"
            echo "Host * ${proxy_marker}"
            echo "    ${proxy_cmd}"
            echo "# ===== 代理配置结束 ${proxy_marker}"
        } >> "${ssh_config}"

        _success "SSH (Socket) 代理已配置 -> ${ssh_config}"

    elif [ "${action}" = "off" ]; then
        # 清除脚本管理的 SSH 代理配置
        if [ -f "${ssh_config}" ]; then
            if grep -q "${proxy_marker}" "${ssh_config}" 2>/dev/null; then
                sed -i "/${proxy_marker}/d" "${ssh_config}"
                # 清除可能留下的空行（连续多个空行压缩为一个）
                sed -i '/^$/N;/^\n$/d' "${ssh_config}"
                _success "SSH (Socket) 代理已清除"
            else
                _info "SSH 配置中未发现代理设置，跳过"
            fi
        fi
    fi
}

# 关闭代理
_proxy_off() {
    _info "正在关闭代理..."

    # --- 清除环境变量 ---
    unset http_proxy https_proxy all_proxy ftp_proxy no_proxy
    unset HTTP_PROXY HTTPS_PROXY ALL_PROXY FTP_PROXY NO_PROXY

    _success "代理环境变量已清除"

    # --- 可选: 清除 Git 代理 ---
    if command -v git &> /dev/null; then
        git config --global --unset http.proxy  2>/dev/null
        git config --global --unset https.proxy 2>/dev/null
        _success "Git 代理已清除"
    fi

    # --- 可选: 清除 SSH 代理 (Socket 代理) ---
    _configure_ssh_proxy "off"

    _success "代理已关闭"
}

# 查看代理状态
_proxy_status() {
    echo ""
    echo -e "${CYAN}╔══════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║          当前代理状态                ║${NC}"
    echo -e "${CYAN}╠══════════════════════════════════════╣${NC}"

    if [ -n "${http_proxy}" ] || [ -n "${HTTP_PROXY}" ]; then
        echo -e "${CYAN}║${NC} 状态:     ${GREEN}● 已开启${NC}"
        echo -e "${CYAN}║${NC} http:     ${http_proxy:-未设置}"
        echo -e "${CYAN}║${NC} https:    ${https_proxy:-未设置}"
        echo -e "${CYAN}║${NC} all:      ${all_proxy:-未设置}"
        echo -e "${CYAN}║${NC} no_proxy: ${no_proxy:-未设置}"

        # 显示 Git 代理
        if command -v git &> /dev/null; then
            local git_proxy
            git_proxy=$(git config --global --get http.proxy 2>/dev/null)
            echo -e "${CYAN}║${NC} git:      ${git_proxy:-未设置}"
        fi

        # 显示 SSH 代理状态
        local ssh_config="$HOME/.ssh/config"
        local ssh_status="未配置"
        if [ -f "${ssh_config}" ] && grep -q "proxy.sh-managed" "${ssh_config}" 2>/dev/null; then
            ssh_status="${GREEN}已配置${NC}"
        fi
        echo -e "${CYAN}║${NC} ssh:      ${ssh_status}"
    else
        echo -e "${CYAN}║${NC} 状态:     ${RED}○ 已关闭${NC}"
    fi

    echo -e "${CYAN}╚══════════════════════════════════════╝${NC}"
    echo ""
}

# 测试代理连接（ping YouTube）
_test_connection() {
    echo ""
    _info "正在测试与 ${TEST_TARGET} 的连接..."
    echo -e "  ${CYAN}────────────────────────────────────${NC}"

    # 方法1: 使用 ping 测试基本连通性
    if command -v ping &> /dev/null; then
        if ping -c 3 -W 5 "${TEST_TARGET}" &> /dev/null; then
            _success "Ping ${TEST_TARGET}: 连接成功 ✓"
        else
            _warn "Ping ${TEST_TARGET}: 无法到达（可能被 ICMP 限制）"
        fi
    fi

    # 方法2: 使用 curl 通过代理测试 HTTP 连接（更可靠的测试方式）
    if command -v curl &> /dev/null; then
        local curl_result
        curl_result=$(curl -s -o /dev/null -w "%{http_code}" \
            --connect-timeout 10 \
            --max-time 15 \
            -x "${PROXY_URL}" \
            "https://www.youtube.com" 2>/dev/null)

        if [ "${curl_result}" = "200" ] || [ "${curl_result}" = "301" ] || [ "${curl_result}" = "302" ]; then
            _success "Curl ${TEST_TARGET} (HTTP ${curl_result}): 代理连接成功 ✓"
        else
            _warn "Curl ${TEST_TARGET}: HTTP 状态码 ${curl_result:-超时}"
        fi
    fi

    # 方法3: 使用 curl 测试 Google（作为备用测试）
    if command -v curl &> /dev/null; then
        local google_result
        google_result=$(curl -s -o /dev/null -w "%{http_code}" \
            --connect-timeout 10 \
            --max-time 15 \
            -x "${PROXY_URL}" \
            "https://www.google.com" 2>/dev/null)

        if [ "${google_result}" = "200" ] || [ "${google_result}" = "301" ] || [ "${google_result}" = "302" ]; then
            _success "Curl google.com (HTTP ${google_result}): 代理连接成功 ✓"
        else
            _warn "Curl google.com: HTTP 状态码 ${google_result:-超时}"
        fi
    fi

    echo -e "  ${CYAN}────────────────────────────────────${NC}"
    echo ""
}

# ================================ 主逻辑 =====================================

# 检查是否通过 source 执行
# 如果直接执行脚本（而非 source），环境变量将无法传递到当前 shell
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    _error "请使用 source 执行此脚本，否则代理设置不会生效！"
    _info  "正确用法: source proxy.sh [on|off|status|test|help]"
    _info  "    或:   . proxy.sh [on|off|status|test|help]"
    exit 1
fi

# 解析命令参数
case "${1:-help}" in
    on|enable|start)
        _proxy_on
        ;;
    off|disable|stop)
        _proxy_off
        ;;
    status|info|show)
        _proxy_status
        ;;
    test|ping|check)
        _test_connection
        ;;
    help|--help|-h)
        _show_help
        ;;
    *)
        _error "未知命令: ${1}"
        _info  "使用 'source proxy.sh help' 查看帮助"
        ;;
esac