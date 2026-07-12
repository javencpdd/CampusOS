# Resource Package 资源包标准 v0.7

资源包只保存主题、首页包、个人主页风格、Skill、Prompt、Persona 或知识元数据，不是业务插件。

统一目录为 `data/resources/{themes,homepage-packs,space-style-packs,skills,prompts,personas,knowledge-metadata}`。每个包使用 `resource.json` 声明 `schema=campusos.resource/v1`、稳定 ID、类型、版本、兼容范围、根目录入口、checksum 和来源。

资源包不得包含 `plugin.yaml`、migration、服务端程序或启动脚本，不得获得数据库、JWT、内部 Service 和任意文件系统权限。现有 `data/plugin_data/homepage-customizer/style-packs`、`personal-space/style-packs`、`web-theme/style-packs` 继续由 Legacy Resource Source 读取，不立即移动或删除。

导入顺序是：解压到临时目录、路径和大小检查、manifest 检查、入口检查、类型专用安全检查、checksum、写入仓库、应用偏好。任何一步失败都不能应用。
