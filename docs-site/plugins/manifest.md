# v5 Manifest 与包校验

> 更新时间：2026-10-16（Asia/Shanghai）
> 当前wire：`campusos.plugin/v5`；旧v4生产入口已退役。安装、Dispatcher、动态UI与Runtime按后续阶段接入。

每个当前源码包以`plugin.json`为完整声明入口。publisher/key/version组成发布身份；包摘要与签名放在独立外封，避免自摘要循环。声明本身不授予文件、网络、数据库或执行权限。

## 形态与字段

| 字段 | 当前合同 |
| --- | --- |
| `api_version` / `host_contracts` | 精确v5版本及可接受的宿主合同，未知版本拒绝。 |
| `runtime` / `backend` | none、wasm或container；Wasm/Container可无UI，但须有具名贡献和对应产物。 |
| `ui` | user/admin入口及SHA-256；UI-only不能携带后端或宿主资源。 |
| `consumes` | 能力、public/self/collection/system_config类型化Scope、用途和required。未知能力或目标即拒绝。 |
| `provides` | 已发布扩展点、版本、输入/输出Schema、能力和数据模式。 |
| `configuration` | 封闭有界JSON Schema、默认值、Selector和Secret名称分流。 |
| `host_resources` / `data` | 请求预算、配置归属、私有数据配额和升级恢复声明；声明不等于批准。 |

完整机器字段见[Manifest Schema](../../docs/api/plugin-v5-manifest-v1.schema.json)与仓库`docs/api/v1.2插件v5完整Manifest目标合同.md`。真实源码示例为[PDF双UI包](../../plugins/campusos.pdf-viewer/plugin.json)及[无UI Echo/WASI包](../../plugins/campusos.chat-echo/plugin.json)。

## 开发与检查

```bash
bash scripts/build-v5-examples.sh
go run ./cmd/campusos-plugin-v5-check -root plugins
go run ./cmd/campusosctl plugin pack plugins/campusos.chat-echo --out /tmp/chat-echo-v5.tar.gz
```

源码检查使用[当前宿主目录](../../docs/api/plugin-v5-host-catalog-current.json)和[扩展DTO目录](../../docs/api/plugin-v5-extension-catalog-current.json)，不将G1合成夹具发布为生产能力。当前可调用的v5 Host能力列表为空；扩展DTO目录不是已安装Provider。

包只包含`plugin.json`、`dist`、`schemas`及可选README/LICENSE。捕获、路径、链接、压缩大小、摘要和签名的边界见仓库`docs/api/v1.2插件v5包字节与签名合同.md`。签名须使用操作者显式提供的Ed25519私钥；verify再按发布者/key_id和当前公钥状态核对。命令不会执行插件，成功也返回`installed=false`。

## 安装与旧协议边界

旧YAML、v4目录与开发源开关不再自动加载；旧pack/build/dev/install流水线已关闭。原v4类型和校验仅保留给旧记录/历史测试，不构成当前入口。HTTP包导入、预检、导出、回退暂不可用，待03b接入持久操作；动态UI与Bridge属于04。原资源下载与插件解耦。

当前操作使用仓库`docs/help/插件v5包与签名操作说明.md`，阶段证据见仓库`docs/进度/v1.2-dev/v1.2.114-dev.md`。历史[v3授权说明](/plugins/authorization-v3)不能作为v5安装许可。
