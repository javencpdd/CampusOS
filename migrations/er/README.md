# CampusOS 数据库 ER 产物

> 更新时间：2026-09-12

`current/` 是由 `migrations/tools/generate_er.py` 自动生成的当前 Schema 投影，包含同一份模型生成的 PNG、SVG 和中文关系说明。
文档门户和 `make database-er-check` 都只引用、检查该目录。

可按需要保留日期命名的审查快照，例如 `20260912/`；它们不是 CI 事实源，不能替代 `current/`。重新生成当前产物：

```bash
python migrations/tools/generate_er.py
python migrations/tools/generate_er.py --check
```
