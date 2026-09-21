# CampusOS PDF Viewer v4 源包

> 更新时间：2026-09-22（Asia/Shanghai）
> 状态：当前唯一的 PDF 预览外部插件源码；开发环境会构建并打包为校验后的 v4 release。

本包包含用户端 PDF.js 页面、管理员页面、Bridge 客户端、PDF Worker、Manifest 以及系统/用户配置 schema。运行时只加载
`plugins/.installed/campusos.pdf-viewer/<version>-<digest>/` 中校验通过的产物；源码、`node_modules`、用户配置与暂存目录不会
被静态网关服务。

## 静态资源约定

Vite 构建必须使用相对 `base: './'`。用户/管理员入口会被打入 `dist/user/`、`dist/admin/`，共享 JS、PDF.js Worker 等产物
写入 `dist/assets/`；v4 打包器将其分别发布为 `ui/user/`、`ui/admin/` 和 `ui/assets/`。因此 iframe 内的脚本和 Worker 始终
请求自身 release 路径，不会错误访问主站根目录 `/assets/`。

开发中修改本包前端、Vite 构建配置或 v4 打包器后，执行 `./scripts/docker-dev.ps1 rebuild`（Windows）或
`./scripts/docker-dev.sh rebuild`（Linux/WSL/Git Bash），以重新构建 `plugin-ui`、生成新的 release 并让 API 重新发现它。
用户不需要生成密钥、Token 或手动编辑 `.installed`。

权限、Bridge、三层授权和资源访问边界见
[插件平台与授权体系](../../docs/architecture/模块设计/插件平台与授权体系.md)。
