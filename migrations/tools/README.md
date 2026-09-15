# CampusOS migration ER 图生成工具

> 更新时间：2026-09-12

该工具递归扫描 `migrations/**/*.up.sql`，静态解析 PostgreSQL 建表、主键、全局唯一约束和外键，生成同源的：

- `CampusOS数据库ER图.png`：适合直接预览；
- `CampusOS数据库ER图.svg`：适合缩放、评审和嵌入文档；
- `CampusOS数据库实体关系说明.md`：中文实体字段、1:1、1:N、逻辑 M:N、可空性及引用动作说明。

工具不会连接数据库、执行 SQL 或读取 `.env`。默认只读 UP migration，避免把回滚脚本误判为当前 Schema。

## 1. 环境要求

- Python 3.10 或更高版本；
- Pillow，用于 PNG 渲染；
- SVG 由工具直接生成，不依赖 Graphviz。

安装 PNG 渲染依赖：

```bash
python -m pip install -r migrations/tools/requirements.txt
```

## 2. 生成

在仓库根目录执行：

```bash
python migrations/tools/generate_er.py
```

也可以使用 Make 入口：`make database-er`；CI/评审只检查漂移时使用 `make database-er-check`。

Windows PowerShell：

```powershell
.\migrations\tools\generate_er_windows.ps1
```

如果本机 PowerShell 禁止执行脚本，可仅对当前命令使用 Bypass，不修改系统级策略：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\migrations\tools\generate_er_windows.ps1
```

Linux / WSL2 / Git Bash：

```bash
bash migrations/tools/generate_er.sh
```

Git Bash 会跳过 WindowsApps 中不可执行的 `python3` 别名，依次尝试可用的 `python3`、`python` 和 `py -3`；
也可通过 `PYTHON_BIN` 显式指定解释器。

默认输出目录为 `migrations/er/current/`。目录下的 `current/` 是文档入口与 CI 检查的唯一权威投影；如需保留某次
审查快照，请显式指定日期目录，不要改写 `current/` 的输出位置：

```bash
python migrations/tools/generate_er.py --migrations migrations --out migrations/er/20260912
```

## 3. 漂移检查

```bash
python migrations/tools/generate_er.py --check
```

检查同时验证：

1. Markdown 内的表数量、外键数量和 Schema SHA-256；
2. SVG `<metadata>` 中的 Schema SHA-256；
3. PNG metadata 中的 Schema SHA-256。

任一产物缺失或 migration 内容发生变化，命令都会返回非零；先重新生成，再审查变更。

## 4. 关系判定

- 物理关系只来自显式 `FOREIGN KEY`，不会根据 `*_id` 名称虚构外键。
- 解析器按 migration 顺序应用 `ALTER TABLE ... DROP CONSTRAINT`，并支持同名外键删除后重建，输出的是当前最终结构而不是历史约束并集。
- 外键列集合同时构成主键或非部分唯一约束时，判定为一对一/可选一对一；否则为一对多。
- 外键可空性决定子记录是“必须引用一个父记录”还是“可以不引用父记录”。
- `ON DELETE`、`ON UPDATE` 未声明时按 PostgreSQL `NO ACTION` 说明。
- 逻辑 M:N 保留两条真实物理外键；命名推断项会明确标注“非唯一性保证”。
- 部分唯一索引不用于推断全局 1:1，防止夸大约束。

PNG/SVG 为跨域总览，卡片优先展示 PK/FK/UQ 字段；所有字段、默认值和外键动作以中文说明文档为完整来源。

## 5. 测试

```bash
python -m unittest migrations.tools.test_generate_er
python migrations/tools/generate_er.py --check
```

新增或调整 migration 后，应重新生成三份产物，并同时运行架构同步检查。
