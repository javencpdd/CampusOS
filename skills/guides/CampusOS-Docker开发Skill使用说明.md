# CampusOS Docker 开发 Skill 使用说明

> 更新时间：2026-08-03
> Skill：`campusos-docker-development`
> 实现目录：`skills/sources/campusos-docker-development/`

## 适用场景

用于 Windows PowerShell 与 Linux/WSL2/Git Bash 的 Docker 开发首配、`.env.dev.local`、`up/rebuild`、源码热
更新、Docker Hub 代理、3000–3002 LAN 暴露、平台日志、健康检查和安全停止。它不替代生产部署认证，也不会
把当前机器成功写成全平台认证。

推荐调用：

```text
使用 $campusos-docker-development 检查这个 CampusOS Docker 开发问题，并同步 Windows/Linux 行为和文档。
```

## 核心判断

| 改动 | 动作 |
| --- | --- |
| 普通 Go/Vue/VitePress 源码 | 依赖热更新，无需命令 |
| `.env.dev.local`、端口、运行环境、挂载 | `docker-dev.* up` |
| Dockerfile、package/lockfile、Compose build、`deploy/docker/dev-*` | `docker-dev.* rebuild` |
| Docker Desktop 自身代理/DNS/引擎 | 修改后重启 Docker Desktop |

Skill 强制保持 API、PostgreSQL、Redis、NATS loopback-only；LAN 模式只允许显式开放三个 UI 端口。完整合同在
`references/docker-development-contract.md`，验证复用仓库已有 Docker、LAN、换行和文档检查器。
