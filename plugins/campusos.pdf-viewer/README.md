# v5 PDF UI 包示例

> 更新时间：2026-10-20（Asia/Shanghai）

当前发布身份为`campusos/pdf-viewer@3.0.0-dev.3`。README 属于不可变发布字节，因此本次教程修订使用新版本，保留 dev.2 已封存的包和证据。用户与管理员两份 Vite UI 声明入口及 SHA-256；运行时为 `none`，没有后端、宿主计算资源、Secret 或安装脚本。

从仓库根目录运行`bash scripts/build-v5-examples.sh`，再用`campusosctl plugin pack`打包。发布树只包含`plugin.json`、`dist`、`schemas`及可选README/LICENSE，源码、依赖、旧配置和本地数据不会随包发送。签名外封独立于包，操作者须显式选择私钥及发布者信任文件。

构建和发布由作者显式执行，安装器不运行构建命令：

```bash
bash scripts/build-v5-examples.sh
campusosctl plugin validate plugins/campusos.pdf-viewer
campusosctl plugin pack plugins/campusos.pdf-viewer --out /tmp/pdf-viewer.tar.gz
campusosctl plugin sign /tmp/pdf-viewer.tar.gz --key-id YOUR_KEY_ID --key-file /path/to/private-key --out /tmp/pdf-viewer.release.json
campusosctl plugin verify /tmp/pdf-viewer.tar.gz --release /tmp/pdf-viewer.release.json --trust /path/to/trust.json
```

在管理端导入包和分离签名，再单独安装、批准当前版本声明的 `ui.resource.selected.read` 与 `ui.surface.open` 用途并启用。用户在插件中心安装、同意用途并启用自己的实例。导入、签名或安装均不会自动授予用途。管理员界面只读取宿主校验后的普通配置；用户通过文章附件、个人资产或文档的预览入口选定 PDF，支持弹窗、抽屉、全屏和新标签页。

Bridge 只接收宿主为当前主体和选定资源签发的短期不透明引用，每次读取重新核对来源权限、用途及当前代次。插件不能读取任意路径或 URL。撤权、到期、配置代次变化或卸载会使旧租约失效，平台下载继续使用原有 Core 授权路径。UI-only 安装保持 `observed=stopped`、`active_generation=0`；资源展示成功不声称后端 Runner 就绪。

04 和 08a 的 Linux 原生只读宿主验收已覆盖生产 Web/Admin、真实浏览器、当前授权与 Core 下载回归；Windows 按用户要求延期。完整操作、独立 headless Provider 作者流程和验收边界见[包与签名指南](../../docs/help/插件v5包与签名操作说明.md)。
