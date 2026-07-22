# Resource Package 资源包标准 v0.7

资源包只保存主题、首页包、个人主页风格、Skill、Prompt、Persona 或知识元数据，不是业务插件。

统一目录为 `data/resources/{themes,homepage-packs,space-style-packs,skills,prompts,personas,knowledge-metadata}`。每个包使用 `resource.json` 声明 `schema=campusos.resource/v1`、稳定 ID、类型、版本、兼容范围、根目录入口、checksum 和来源。

资源包不得包含 `plugin.yaml`、migration、服务端程序或启动脚本，不得获得数据库、JWT、内部 Service 和任意文件系统权限。v10 已将仓库内首页、个人主页和系统主题包统一迁入 `data/resources/{homepage-packs,space-style-packs,themes}`；`data/plugin_data` 只保留 External Plugin 运行数据。

导入顺序是：解压到临时目录、路径和大小检查、manifest 检查、入口检查、类型专用安全检查、checksum、写入仓库、应用偏好。任何一步失败都不能应用。

从 v0.13 起，Theme、Homepage Pack 和 Space Style Pack 还必须满足 `campusos.appearance-delivery/v1` 双端交付合同；旧包只能以 `legacy-readonly` 兼容读取，不能重新应用。请同时阅读 [v13 风格包双端交付标准](../系统设计相关/v13风格包双端交付标准.md)。

现有目录可以使用 CLI 纳管和检查：

```bash
go run ./cmd/campusosctl resource adopt data/resources/themes/my-theme --type theme
go run ./cmd/campusosctl resource inspect data/resources/themes/my-theme
```

从 v10 旧布局迁移时使用 `scripts/migrate-v10-module-plugin-layout.sh`；脚本遇到同名目标会停止，不覆盖文件，并可按状态记录逆序回滚。
安全但尚未满足 v0.13 双端合同的历史 Appearance 包会被迁为 `legacy-readonly`，仅可读取或导出；补齐合同后用普通
`resource adopt --force` 重新纳管。迁移脚本内部使用的 `resource adopt-legacy` 不是新包发布入口。
