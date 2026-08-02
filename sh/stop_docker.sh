#!/bin/bash
# 用途：停止当前 Docker Engine 上所有正在运行的容器，不仅限于 CampusOS。
# 适用平台：安装了 Docker CLI 和 xargs 的 Linux、macOS、WSL2 或 Git Bash。
# 基本用法：./sh/stop_docker.sh（Docker 需要提权时使用 sudo ./sh/stop_docker.sh）。
# 注意：只想停止 CampusOS 开发栈时，请优先使用 ./scripts/docker-dev.sh down。

# 停止所有运行中的容器（忽略空列表错误）
docker ps -q | xargs -r docker stop 2>/dev/null

# 验证结果
if [ $? -eq 0 ]; then
  echo -e "\e[32m✓ 所有 Docker 容器已安全停止\e[0m"
else
  echo -e "\e[33m⚠ 无运行中的容器\e[0m"
fi
