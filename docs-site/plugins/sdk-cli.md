# v4 校验、打包与本地验证

> 更新时间：2026-09-23

v4 没有“在 `data/plugins` 里创建一个可执行目录后由核心自动加载”的开发模式。新插件从根目录 `plugins/<key>/` 开始，以现有 `campusos.pdf-viewer` 为参考实现。

## 最小验证路径

```bash
# 全仓库 v4 Manifest 和发布约束
go run ./cmd/campusos-plugin-v4-check -root plugins

# 单插件 Manifest/产物校验
go run ./cmd/campusosctl plugin v4 validate plugins/campusos.pdf-viewer

# Go 侧单元测试（按修改范围选择）
go test ./internal/plugin/...
go test ./internal/server/...
```

插件前端通过其自身目录的 `package.json` 构建。以 PDF Viewer 为例，分别构建 `frontend/user` 与 `frontend/admin`，再确认 Manifest 指向的 `dist/` 入口及静态资源都在发布包中。

## Docker 开发验证

开发栈启动后：用户端为 3000、管理端为 3001、文档站为 3002、API 为 8080、Plugin UI Gateway 为 3003。插件前端、Vite 配置、Manifest、打包/安装逻辑或 Gateway 配置变更后执行：

```powershell
.\scripts\docker-dev.ps1 rebuild
```

仅修改普通 Web/Admin 源码时按 Docker 开发文档选择热加载或 `up`。不要在浏览器中尝试访问 `http://api:...`：该名称只存在 Docker 网络，iframe 必须经宿主 Bridge 访问资源。

## 推荐验收用例

1. 管理员安装并发布新版本，再逐项授予 Manifest 中的能力。
2. 用户添加插件、完成个人 Consent，确认创建用户配置目录。
3. 用 PDF Viewer 预览自己的个人 PDF 和可阅读文章的附件；撤销后重新尝试应被拒绝。
4. 确认 iframe 没有拿到 JWT、真实文件路径或跨用户资源。

CLI 的精确子命令和参数以 `go run ./cmd/campusosctl plugin v4 --help` 为准。历史 v1-v3 的 `plugin init`、`plugin dev`、Wasm/进程示例不构成 v4 新插件模板。
