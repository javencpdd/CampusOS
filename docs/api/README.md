# CampusOS API 与机器合同

`docs/api/` 同时保存开发者说明和由当前路由、权限目录或 Schema 生成的机器合同。第一次接入时从
[API 索引](API索引.md) 开始；自动化客户端应以 OpenAPI、JSON Schema 和权限目录为准。

## 阅读顺序

1. [官方接口约定](../../docs-site/api/overview.md)：认证、响应包络、错误处理和兼容规则。
2. [API 索引](API索引.md)：按身份、社区、内容、插件和运维业务域查找接口。
3. [OpenAPI](openapi-v0.6-current.yaml)：请求、响应和字段级机器合同。
4. [HTTP 路由与授权矩阵](HTTP路由与授权矩阵-v0.6.md)：路由所有者、认证、权限和审计要求。
5. 对应专项合同：Session、管理员准入、MFA、可靠任务、Host API 或插件 Manifest。

`openapi-v0.6-current.yaml`、`http-routes-v0.6.json` 等文件名为兼容已有链接保留，不能根据文件名中的
`v0.6` 判断当前产品版本。内容版本和漂移检查由当前代码生成。

## 内容分类

| 类型 | 文件示例 | 使用规则 |
| --- | --- | --- |
| 当前机器合同 | `openapi-v0.6-current.yaml`、`http-routes-v0.6.json`、`error-catalog-v0.13.json` | 客户端生成和 CI 漂移检查的优先依据 |
| 权限与插件合同 | `Host-API-v2受管数据合同.md`、`plugin-manifest-v2.schema.json`、`plugin-permissions-v2.json` | External Plugin 只能使用声明并获批的能力 |
| 业务专项说明 | `v0.12会话与Token安全流程.md`、`v0.13管理员准入API.md`、`v0.13多因素认证API.md` | 与 OpenAPI 一起阅读，不单独推断隐藏字段 |
| v1.2 G1 目标合同（尚未接入运行时） | [18 条退出矩阵](v1.2-G1合同退出矩阵.json)、[主体与资源 Policy](v1.2主体上下文合同.md)、[其他 Scope/委托](v1.2其他Scope与委托边界合同.md)、[独立身份域](v1.2独立身份域合同.md)、[完整 v5 Manifest](v1.2插件v5完整Manifest目标合同.md)、[双向调用/事件/生命周期](v1.2插件双向调用事件与生命周期合同.md)、[旧机制替换清单](v1.2旧机制删除与替换清单.md) | G1 目标合同及 Linux 离线验收已完成；当前 HTTP/DB/插件运行时仍以现行生成合同为准 |
| 历史兼容合同 | `openapi-v0.3-pre.yaml`、Host API v1 | 只用于旧客户端迁移和行为比较 |

## 变更规则

1. API 变更优先采用兼容新增；破坏性变化必须有版本、迁移和回滚说明。
2. 新路由必须声明模块所有者、认证、Permission Code、作用域和审计策略。
3. 修改路由、错误目录、权限或 Schema 后运行 `make contracts-check` 和 `make architecture-check`。
4. 不在手写教程中复制长期易漂移的完整路由、表或权限清单。
5. UI 隐藏不是授权；服务端合同和权限校验始终是最终边界。

V12-09a公开集合、官方资料、选择规则与显式成员见[知识集合与来源管理合同](v1.2知识集合与来源管理合同.md)；当前09b完整索引退出见[.142](../进度/v1.2-dev/v1.2.142-dev.md)，10公开检索完整质量/容量退出见[.145](../进度/v1.2-dev/v1.2.145-dev.md)及[评测规范](v1.2公开知识检索评测规范.md)。

09b常驻消费、持久请求与索引执行合同见[索引任务请求合同](v1.2知识索引任务请求合同.md)；原首片与完整阶段验收由进度和证据分别标记。

V12-11 首片的独立机器入口、精确集合授权和当前引用失败边界见[公开知识 Integration 引用读取合同](v1.2公开知识Integration引用读取合同.md)；11完整Linux退出见[.148](../进度/v1.2-dev/v1.2.148-dev.md)，Windows延期。

V12-11 单集合机器POST检索、独立端点/集合授权、返回前当前来源与凭据复核见[Integration检索合同](v1.2公开知识Integration检索合同.md)。

- [v1.2公开知识MCP与HostSDK合同](v1.2公开知识MCP与HostSDK合同.md)：固定2025-11-25只读MCP、受管公开集合Host及Go/TypeScript作者调用。

- [Web 公开知识检索操作说明](../../docs-site/guide/public-knowledge-search.md)：V12-12 的公开集合搜索、分页和当前引用；独立 Admin 来源管理、索引诊断指标与重试操作。

- [v1.2知识管理诊断与重试合同](v1.2知识管理诊断与重试合同.md)：当前 Admin 只读执行快照指标、MFA/CSRF/CAS 和审计事务重试。

- [v1.2私人知识索引与撤回合同](v1.2私人知识索引与撤回合同.md)：当前 User 明确逐项选入本人文档/讨论帖、独立密钥加密、持久任务、私人检索和撤回/备份恢复。保护不足时禁止索引。
- [我的私人知识操作说明](../../docs-site/guide/private-knowledge.md)：选入、检索、重试、撤回与 Linux 保护目录准备。
- [v1.2私人知识来源预检合同](v1.2私人知识来源预检合同.md)：.150 首片历史快照；当前条件 readiness 见上方合同，预检不记录同意或创建任务。
