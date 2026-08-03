# CampusOS 仓库 Skill 映射与直用说明

> 更新时间：2026-08-03

## 结论

CampusOS 的 Skill 不再依赖开发者手工复制到个人系统目录。规范源文件保存在 `skills/sources/`，仓库根目录
`.agents/skills/` 保存可提交的轻量发现桥接。开发者从 GitHub clone 后，在仓库内启动 Codex 即可直接调用。

## 为什么不用仓库符号链接

Codex 支持符号链接 Skill，但 Windows 的 Git 符号链接检出依赖开发者模式、权限和 `core.symlinks`。为了让
Windows 与 Linux clone 后行为一致，本仓库提交普通目录和桥接 `SKILL.md`；桥接只负责发现，实际说明仍从
`skills/sources/<skill-name>/` 读取，避免维护两份完整源文件。

## 目录

| 路径 | 作用 | 是否手工编辑 |
| --- | --- | --- |
| `skills/sources/<skill-name>/` | Skill 唯一规范源文件 | 是 |
| `skills/guides/` | 开发者使用说明 | 是 |
| `.agents/skills/<skill-name>/` | Codex 仓库级发现桥接 | 由同步脚本生成 |

## Clone 后使用

在仓库根目录或任意子目录启动 Codex，然后输入：

```text
$campusos-project-onboarding
```

也可以使用 `$campusos-dev-nocommit`、`$campusos-docker-development`、`$campusos-webui-regression` 等。
无需修改 `%USERPROFILE%\.codex\skills`、`%USERPROFILE%\.agents\skills`、`~/.codex/skills` 或
`~/.agents/skills`。如果一个已经打开的 Codex 会话没有刷新列表，重启一次 Codex。

## 维护

修改规范源文件后执行：

```powershell
python .\skills\sources\campusos-skill-repository-sync\scripts\sync_skill_bridges.py --root . --write
python .\skills\sources\campusos-skill-repository-sync\scripts\sync_skill_bridges.py --root . --check
```

Linux：

```bash
python3 skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --write
python3 skills/sources/campusos-skill-repository-sync/scripts/sync_skill_bridges.py --root . --check
```

同步脚本不会自动删除意外出现的桥接目录；它会报错并要求维护者先检查，避免误删其他项目级 Skill。
