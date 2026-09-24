# v4 插件打包、安装与更新

> 更新时间：2026-09-23
> 当前示例：[`plugins/campusos.pdf-viewer`](https://github.com/javencpdd/CampusOS/tree/main/plugins/campusos.pdf-viewer)

v4 源码目录不是运行目录。开发者在 `plugins/<key>/` 编写 Manifest、前端和配置 Schema；宿主只从校验后的不可变发布目录 `plugins/.installed/<key>/<version>/` 发现并运行插件。`.staging/` 只用于短暂预检，不能成为执行来源。

## 包的边界

发布包根目录必须包含 `plugin.yaml` 以及 Manifest 所声明的 UI 产物、静态资源和文档。不要包含：

- `.git/`、`node_modules/`、日志、临时文件；
- 用户文件、数据库数据、个人配置或 Secret；
- 未构建的前端源替代 `dist/` 产物。

`campusos.pdf-viewer` 的用户端与管理端均是 Vite 产物；Vite `base` 必须是 `./`，以便 Gateway 在固定 `/plugins/<key>/<version>/...` 路径下托管资源。

## 本地闭环

```bash
go run ./cmd/campusos-plugin-v4-check -root plugins
go run ./cmd/campusosctl plugin v4 validate plugins/campusos.pdf-viewer
go run ./cmd/campusosctl plugin v4 stage plugins/campusos.pdf-viewer --out .tmp/pdf-viewer.stage
go run ./cmd/campusosctl plugin v4 pack plugins/campusos.pdf-viewer --out .tmp/pdf-viewer.campusos-plugin.tar.gz
go run ./cmd/campusosctl plugin v4 sign .tmp/pdf-viewer.campusos-plugin.tar.gz --key-id organization-key --key-file /secure/key --out .tmp/pdf-viewer.signed.tar.gz
go run ./cmd/campusosctl plugin v4 install .tmp/pdf-viewer.signed.tar.gz --root plugins
```

命令参数以仓库中 `campusosctl plugin v4 --help` 为准；开发 Docker 也可通过 `CAMPUSOS_PLUGIN_V4_DEV_SOURCE=true` 把已校验的开发源码复制成实际发布版本，但 API 仍只扫描 `.installed/`。

## 管理流程

1. 管理员预检 Manifest、包路径、摘要、签名、能力与版本变化。
2. 校验通过后，宿主把发布包写入新版本目录，再原子切换活动版本；失败不污染当前版本。
3. 管理员决定是否发布到用户目录，并对当前版本逐项 Grant。
4. 用户添加已发布插件并完成本人数据的 Consent；这会创建受控用户配置目录。
5. 禁用、撤销、升级或卸载会使后续 Bridge 调用重新判定。升级后的新增能力必须重新治理。

可信市场“批准申请”不等于下载、安装或执行代码；它只让管理员进入下一步的预检和导入流程。

通用的回滚、跨版本数据迁移和任意 Wasm/container Runtime 仍是后续范围，不应假定已经具备生产承诺。参见[可信市场与目录](/plugins/market-managed-data)。
