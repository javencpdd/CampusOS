# CampusOS 自包含插件源码

> 更新时间：2026-09-21（Asia/Shanghai）
> 状态：R11-01、R11-03 已完成；PDF 用户端隔离 UI、Host Bridge、静态网关和实际迁出已完成。Admin 动态 Host、用户安装生命周期、升级/卸载和完整浏览器/恢复验收仍待后续 R11 任务实施。

这里是受版本控制的插件**源码**，每个一级非点号目录是一份 `campusos.plugin/v4` 源包。
已安装包必须位于本目录的 `.installed/`，安装暂存位于 `.staging/`；源码和暂存目录均不作为生产自动执行入口，运行时只发现通过校验的 `.installed/`。

```text
plugins/<plugin-key>/
  plugin.yaml
  config/{system,user}.{schema,defaults}.json
  frontend/{user,admin}/
  backend/                 # 可选
  storage/sqlite/          # 可选
  tests/
```

当前可执行的源码合同检查：

```powershell
go run ./cmd/campusos-plugin-v4-check -root plugins
```

`make plugin-v4-check` 与 `make contracts-check` 会执行同一检查。该检查不会构建、安装、运行或授予插件权限。详情见
[v1.1 插件自包含与动态加载重构计划](../docs/项目计划书v1/项目计划v1.1/02-v1.1插件自包含与动态加载重构计划书.md)。
