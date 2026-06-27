# CampusOS ID 策略说明

> 状态：v0.3-dev-pre 稳定化基线
> 日期：2026-06-27

## 1. 当前结论

v0.3-dev-pre 阶段，数据库主键继续采用 `BIGINT`，应用侧通过 `pkg/idgen` 生成数字 ID，并以字符串形式暴露给前端和 API。

这样做的原因：

| 原因 | 说明 |
| --- | --- |
| 匹配现有 schema | 当前 `users`、`accounts`、`threads`、`posts`、`categories`、`plugins`、`api_keys` 等核心表均为 `BIGINT PRIMARY KEY` |
| 避免数据库序列漂移 | 不依赖 `plugins_id_seq`、`api_keys_id_seq` 等未定义序列 |
| 前端兼容大整数 | API 使用字符串承载 ID，避免 JavaScript number 精度丢失 |
| 方便后续统一 | Wasm/SDK/CLI 阶段可以基于同一套 ID 规范扩展 |

## 2. 当前已统一的对象

| 对象 | 生成位置 | 当前状态 |
| --- | --- | --- |
| 用户 `users.id` | `UserService.Register` | 应用侧数字 ID |
| 账号 `accounts.id` | `PgUserRepository.CreateAccount` | 应用侧数字 ID |
| 主题 `threads.id` | `ThreadService.CreateThread` | 应用侧数字 ID |
| 回复 `posts.id` | `PostService.CreatePost` | 应用侧数字 ID |
| 版块 `categories.id` | `CategoryService.Create` | 应用侧数字 ID |
| 插件 `plugins.id` | `PgPluginRepository.Save` | 应用侧数字 ID |
| API Key `api_keys.id` | `PgAPIKeyRepository.Create` | 应用侧数字 ID |
| 默认管理员/默认版块 | SQL seed + server seed | 固定数字 ID |

## 3. API 暴露规则

后端内部可以把 ID 当作字符串在领域对象中传递，但落库时必须能被 PostgreSQL `BIGINT` 接收。

前端和外部 API 应把 ID 当作字符串处理：

```json
{
  "id": "1782535717836817315",
  "author_id": "1782535717600090534",
  "category_id": "1000000000000000004"
}
```

不要在前端把这些 ID 转成 number。

## 4. 暂不采用 UUID 的范围

当前不建议在核心业务表中继续新增 UUID 主键，除非先完成 schema 改造。可以继续使用 UUID 的场景：

| 场景 | 说明 |
| --- | --- |
| trace id | 请求链路追踪，不进入 BIGINT 主键约束 |
| event id | 事件消息 ID，可保持字符串 |
| 外部系统 ID | OAuth、第三方平台、插件外部资源 ID |

## 5. 后续待办

v0.3-dev 之前仍建议完成：

| 待办 | 说明 |
| --- | --- |
| sessions/audit/notifications 写入路径检查 | 确认所有新增记录都由应用侧生成 BIGINT |
| migration 注释统一 | 确认所有表注释和文档都说明应用侧数字 ID |
| 插件 SDK 类型定义 | SDK 中 ID 类型使用 string，避免跨语言大整数精度问题 |
