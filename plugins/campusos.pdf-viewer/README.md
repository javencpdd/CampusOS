# v5 PDF UI 包示例

> 更新时间：2026-10-16（Asia/Shanghai）

源码入口已从v4的`plugin.yaml`重建为完整v5 `plugin.json`，发布身份为`campusos/pdf-viewer@3.0.0-dev.1`。用户与管理员两份Vite UI均明确声明入口及SHA-256；无后端、无consumes、无宿主资源、无Secret或安装脚本。原配置文件和UI源码仅为后续04接入参考，不构成当前授权。

从仓库根目录运行`bash scripts/build-v5-examples.sh`，再用`campusosctl plugin pack`打包。发布树只包含`plugin.json`、`dist`、`schemas`及可选README/LICENSE，源码、依赖、旧配置和本地数据不会随包发送。签名外封独立于包，操作者须显式选择私钥及发布者信任文件。

03a只验证包字节与合同。本示例尚未通过03b安装状态机和04 UI Host/Bridge挂载；不自动恢复旧v4发现、权限适配或预览注册。平台原资源下载独立于插件。完整操作见[包与签名指南](../../docs/help/插件v5包与签名操作说明.md)。
