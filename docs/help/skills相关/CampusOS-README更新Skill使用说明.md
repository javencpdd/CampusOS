# CampusOS README 更新 Skill 使用说明

> 日期：2026-07-02
> Skill 名称：`campusos-readme-update`
> 仓库内位置：`/home/jack/bbs/bbs01/CampusOS/skills/campusos-readme-update`
> 用途：后续维护和同步 `README.md`

## 1. 用途

`campusos-readme-update` 用于在项目功能、版本计划、进度文档、数据库配置、Docker 服务、CI/CD、插件或 skill 发生变化后，系统化更新 `README.md`。

它会提醒执行者先读取真实来源，再更新 README：

| 来源 | 作用 |
| --- | --- |
| `docker-compose.yml`、`.env.example`、`Makefile`、`scripts/` | 确认启动方式、端口、命令 |
| `migrations/` | 确认当前数据库迁移列表 |
| `docs/进度/` | 确认已完成内容 |
| `docs/项目计划v*/` | 确认规划和未完成内容 |
| `docs/help/` | 同步帮助文档入口 |
| `internal/`、`web/`、`admin/`、`sdk/`、`examples/`、`skills/` | 确认代码中真实存在的能力 |

## 2. 推荐触发方式

```text
使用 campusos-readme-update 回顾当前项目状态并更新 README。
```

或：

```text
Use $campusos-readme-update to refresh CampusOS README based on current docs, code, migrations, and validation results.
```

## 3. Skill 结构

```text
skills/campusos-readme-update/
├── SKILL.md
├── agents/
│   └── openai.yaml
├── references/
│   └── readme-update-checklist.md
└── scripts/
    └── check_readme_links.py
```

## 4. 链接检查工具

README 更新后建议执行：

```bash
python3 skills/campusos-readme-update/scripts/check_readme_links.py README.md
```

该脚本会检查 README 中的本地 Markdown 链接是否指向真实文件，忽略 `http://`、`https://`、`mailto:` 和页内锚点。

## 5. 推荐验证命令

最小验证：

```bash
git diff --check -- README.md
python3 skills/campusos-readme-update/scripts/check_readme_links.py README.md
```

如果 README 更新涉及后端、数据库、Docker 或前端构建说明，应增加：

```bash
GOCACHE=/tmp/campusos-go-cache go test ./... -count=1
docker compose config -q
make migrate-status
cd web && npm run build
cd admin && npm run build
```

## 6. 同步到 Codex 用户级目录

仓库内副本用于版本管理。若要让后续新会话自动发现该 skill，可以同步到用户级目录：

```bash
mkdir -p /home/jack/.codex/skills
rsync -a skills/campusos-readme-update/ /home/jack/.codex/skills/campusos-readme-update/
```

同步后即可用：

```text
使用 campusos-readme-update 更新 README。
```

## 7. 注意事项

| 项目 | 要求 |
| --- | --- |
| 已完成能力 | 必须由代码、迁移、进度文档或验证结果支撑 |
| 计划能力 | 明确写成“计划中”“未完成”或“部分完成” |
| 数据库说明 | 当前推荐 Docker PostgreSQL，开发机使用 `127.0.0.1:5433` |
| Windows 说明 | 没有真实实机验证前，不写成已完整支持 |
| 文档链接 | 更新后必须检查本地链接 |
| 提交 | 如果用户要求提交，只提交 README 和直接相关文档/脚本 |
