# CampusOS repository Skills

> 更新时间：2026-08-03

本目录将 Agent Skill 分为两类：

- [`sources/`](sources/)：可提交到 GitHub 的规范 Skill 源文件，每个子目录包含 `SKILL.md` 及可选的 `agents/`、`scripts/`、`references/`。
- [`guides/`](guides/)：面向开发者的调用、维护和验证说明，不参与 Skill 自动发现。

Codex 官方仓库发现入口是 [`.agents/skills/`](../.agents/skills/)。CampusOS 在那里提交轻量桥接文件，指向
`skills/sources/` 中的规范源文件。这样从 GitHub clone 后，只要在仓库根目录或其子目录启动 Codex，就能直接使用：

```text
$campusos-project-onboarding
$campusos-dev-nocommit
$campusos-docker-development
$campusos-webui-regression
```

不需要复制到用户系统目录。若当前会话早于 Skill 更新且列表尚未刷新，重启 Codex 一次即可。

新增、移动或修改 Skill 后执行：

```text
python skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --write
python skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --check
```

详细入口见 [Skill 使用说明索引](guides/README.md)。
