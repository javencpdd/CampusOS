# CampusOS 数据库 ER 产物

> 更新时间：2026-09-12

`current/` 是由 `migrations/tools/generate_er.py` 自动生成的当前 Schema 投影，包含同一份模型生成的 PNG、SVG 和中文关系说明。
文档门户和 `make database-er-check` 都只引用、检查该目录。

可按需要保留日期命名的审查快照，例如 `20260912/`；它们不是 CI 事实源，不能替代 `current/`。重新生成当前产物：

```bash
python migrations/tools/generate_er.py
python migrations/tools/generate_er.py --check
```


## Admin Chen 视图

管理端 `/architecture` 的“ER逻辑关系图”采用Chen记法，实体、属性和联系分别用矩形、椭圆与菱形，使用无箭头直线及参与/基数标记。它使用 `scripts/generate-architecture-schema.py` 生成的公开Schema事实和审阅过的逻辑引用；字段/标识符/外键来自当前SQL和迁移执行器，不读取实时数据。弱实体须有单独模型声明，不能把每个外键表自动标成弱实体。

`make database-er`同时生成本目录物理表结构资料及Admin Chen事实；`make database-er-check`和`make architecture-check`检查两部分。只刷新/检查Admin事实：

```bash
python3 scripts/generate-architecture-schema.py
python3 scripts/generate-architecture-schema.py --check
```
