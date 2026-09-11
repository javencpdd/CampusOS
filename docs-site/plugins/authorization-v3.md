# Manifest v3 与三层授权

> 更新时间：2026-09-11
> 状态：v1.0 Release Candidate 功能说明；目标环境发布证据待收集

CampusOS 的 External Plugin 不需要用户自行创建长期 API Key，也不能读取用户 JWT、数据库连接或宿主目录。敏感调用的有效权限是以下条件的交集：

```text
Manifest 声明 ∩ 管理员 Grant ∩ 用户 Consent（需要时）
∩ 插件版本 ∩ self/system Scope ∩ 运行状态 ∩ 系统策略
```

未知方法、未知 Capability、版本变化、范围越界和任一授权缺失都会默认拒绝，并返回稳定的 `DENY_*` 原因码。授权成功结果不做跨请求缓存，所以撤销后的下一次敏感调用立即重新判定。

## 管理员与用户操作

- 管理员在 3001 管理端“插件管理 → 授权”审查当前版本的逐项能力、用途和风险，填写原因后授予、拒绝或撤销。
- 涉及个人数据时，用户在 3000 用户端“插件中心 → 精细授权”逐项同意；管理员未授予的能力不能由用户自行开启。
- 管理员可在授权对话框管理系统 Secret；用户可在精细授权对话框管理个人 Secret。保存同名项会轮换旧值，页面只显示掩码。
- 用户可生成一次显示、默认 15 分钟的后台 Delegation。它只包含当前已授权能力，最长 24 小时，可立即撤销。

## Manifest v3 示例

```yaml
api_version: campusos.plugin/v3
host_api_version: v3
name: schedule-reminder
display_name: 课表提醒
version: 1.0.0
runtime: process
scope: user
capability_declarations:
  - code: schedule.self.read
    required: true
    purpose: 读取当前用户课表并生成提醒
    scope: self
config:
  command: ./plugin
  process_contract: campusos.process/v1
  health_url: http://127.0.0.1:39090/health
  extension_url: http://127.0.0.1:39090/events
storage: { type: none }
```

同一语义版本的包摘要和能力指纹不可变化。修改代码、用途或范围后必须提升版本；启动时若发现冲突，CampusOS 会隔离问题插件、保留可诊断错误并继续启动 API。

## Secret 与后台任务

部署端必须设置 32 字节 `CAMPUSOS_PLUGIN_SECRET_KEY` 和 `CAMPUSOS_PLUGIN_SECRET_KEY_VERSION`。数据库保存 AES-256-GCM 密文、nonce 与 Key 版本，不保存或回显明文。

后台任务只能携带宿主签发的短期 Delegation。数据库只保存 Token 摘要；过期、撤销、版本变化、插件停用或 Scope 不匹配都会拒绝调用。

## 开发验证

```bash
go run ./cmd/campusosctl plugin doctor ./my-plugin
go run ./cmd/campusosctl plugin conformance ./my-plugin --json
make contracts-check
```

Capability 机器合同位于 `docs/api/plugin-capabilities-v1.json`。三个完整示例位于 `examples/plugins/v1-schedule-reminder`、`v1-mail-watcher` 和 `v1-content-enhancer`。

进一步阅读：[Manifest 与配置](/plugins/manifest)、[Host API 与权限](/plugins/host-api)、[插件生命周期](/plugins/lifecycle)。
