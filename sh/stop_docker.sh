#!/bin/bash
# stop_docker.sh - 一键安全停止所有 Docker 容器
# 用法: sudo ./stop_docker.sh

# 停止所有运行中的容器（忽略空列表错误）
docker ps -q | xargs -r docker stop 2>/dev/null

# 验证结果
if [ $? -eq 0 ]; then
  echo -e "\e[32m✓ 所有 Docker 容器已安全停止\e[0m"
else
  echo -e "\e[33m⚠ 无运行中的容器\e[0m"
fi