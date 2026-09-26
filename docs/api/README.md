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
