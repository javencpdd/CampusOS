# CampusOS WebUI 回归 Skill 使用说明

> 更新时间：2026-08-03
> Skill：`campusos-webui-regression`
> 实现目录：`skills/sources/campusos-webui-regression/`

## 适用场景

用于用户端或后台出现空列表、字段缺失、表单/价格异常、通知不触发、头像/空间行为不符、首次点击像刷新、日志
不实时等问题。Skill 会先重放页面使用的确切 HTTP 请求，再沿 Vue → API wrapper → Handler → Service/Port →
Repository → PostgreSQL/数据逐层定位，避免把后端 500 误判成“暂无数据”。

推荐调用：

```text
使用 $campusos-webui-regression 复现并修复这个 CampusOS WebUI 问题，增加回归测试并验证真实 API。
```

## 验证原则

- 最低故障层必须有确定性测试，导航/渲染参与时再补前端测试。
- 有运行栈时验证准确 HTTP 请求；有浏览器时再完成交互验收。
- 没有浏览器不能宣称视觉通过，但可以明确报告 API/运行态证据。
- 不为通过 UI 测试降低权限、公开投影、内容清洗、配额、事务或 migration 不可变性。

具体症状、代码归属和现有测试入口见 `references/regression-matrix.md`。
