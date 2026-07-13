# 打包、导入与更新

## 标准插件包

文件后缀：

```text
.campusos-plugin.tar.gz
```

归档根目录必须直接包含 `plugin.yaml`：

```text
plugin.yaml
README.md
plugin.wasm
src/
```

不要再套一层插件名目录，否则导入器无法从包根部读取 manifest。

## CLI 打包

```bash
go run ./cmd/campusosctl plugin pack data/plugins/hello-wasm
```

指定输出：

```bash
go run ./cmd/campusosctl plugin pack \
  data/plugins/hello-wasm \
  --out /tmp/hello-wasm-0.1.0.campusos-plugin.tar.gz
```

## 导入预检

Admin 导入前会检查：

- `plugin.yaml` 是否存在并可解析。
- 插件名、版本和 Runtime。
- 归档路径是否安全。
- Wasm 模块是否存在。
- 包大小和 checksum。
- 权限声明和风险等级。
- 签名状态、受信任签名密钥和 Manifest 是否要求验签。
- 同名插件及版本变化。
- 新增/移除的系统权限与用户权限、是否需要重新授权、数据 Schema 版本变化。

绝对路径、`../` 路径逃逸、符号链接、硬链接和缺少 manifest 的包会被拒绝。

## Admin 导入

```http
POST /api/v1/plugin-packages/precheck
POST /api/v1/plugin-packages/import
```

导入使用 `multipart/form-data`，文件字段名为 `file`。同名插件默认拒绝覆盖；管理员明确选择覆盖后才执行替换。

## CLI 安装

```bash
go run ./cmd/campusosctl plugin install \
  /tmp/hello-wasm-0.1.0.campusos-plugin.tar.gz \
  --dir data/plugins
```

覆盖：

```bash
go run ./cmd/campusosctl plugin install \
  /tmp/hello-wasm-0.1.0.campusos-plugin.tar.gz \
  --dir data/plugins \
  --replace
```

## 导出

Admin：

```http
GET /api/v1/plugins/:name/export
```

标准导出只包含插件运行需要的代码和静态文件，不包含数据库状态或运行数据。

## 不应打包的内容

| 内容 | 原因 |
| --- | --- |
| `.git/` | 版本库元数据。 |
| `node_modules/` | 体积大且不可移植。 |
| `data/` | 运行数据不属于代码包。 |
| `*.log`、`*.tmp` | 日志和临时文件。 |
| API Key 和 token | 敏感凭据。 |

## 更新与回滚

覆盖更新前记录：

1. 当前版本和 checksum。
2. 新包来源、权限变化和 config schema 变化。
3. `plugin_data` 是否需要迁移。
4. 失败时恢复旧包和数据的方法。

替换已有插件时会先创建代码快照；管理员可以从快照恢复旧包。受管数据不会随代码回滚而倒退，数据迁移仍需插件作者提供向后兼容策略。

新增用户权限会标记“需要重新授权”。Grant 绑定精确插件版本，升级后不会把旧同意自动扩大到新权限。

## 签名

签名不是把私钥随包导出。使用离线 Ed25519 私钥对包内文件的内容摘要签名：

```bash
go run ./cmd/campusosctl plugin sign <plugin-dir> --key-id organization-key --key-file /secure/private-key.txt
go run ./cmd/campusosctl plugin pack <plugin-dir>
```

实例只保存受信任公钥，位于 `data/config/plugin-trust-keys.json`。如果 Manifest 设置 `release.signature_required: true`，未签名、签名损坏或未受信任的包会被拒绝。详见 [插件中心、受管数据与签名](/plugins/market-managed-data)。
