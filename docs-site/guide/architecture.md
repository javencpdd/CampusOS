# 系统架构

> 更新时间：2026-10-12

浏览器不直接访问 PostgreSQL、Redis、NATS、个人空间文件或插件发布目录。Web/Admin 请求先经 API 的认证、稳定权限、对象归属与业务状态校验，再进入领域服务和存储层。

```text
浏览器 → API middleware → 认证/权限/归属 → 领域服务 → PostgreSQL/文件/事件
                                           ↘ Plugin UI Gateway → 隔离插件 iframe
```

## 主要模块

| 代码区域 | 职责 |
| --- | --- |
| `internal/modules/core/identity` | 独立 User/Admin 账号、凭据、会话与角色绑定，以及 MFA/恢复。 |
| `internal/modules/core/community` / `moderation` | 版块、帖子、回复、标签、内容状态和治理。 |
| `internal/modules/core/userstorage` | 用户文件路径、配额、原子写入和文件归属。 |
| `internal/modules/core/contenteditor` | 文本/CampusDoc 校验和安全渲染。 |
| `internal/modules/core/academicterm` | 学期开放、默认学期和版本控制。 |
| `internal/modules/features/richtext` / `personaldocuments` | 图文发布、文章附件、个人文档和文件入口。 |
| `internal/plugin` | v4 发布发现、管理状态、三层授权、用户配置和 Bridge。 |
| `web/` / `admin/` | 用户与管理端 Vue 应用；核心页面不内嵌插件实现。 |
| `plugins/` | 外部插件独立源码与发布包边界。 |

## 权限与插件

社区和空间操作按“认证 → 稳定权限 → 服务端计算对象范围 → 业务约束 → 审计”处理。插件则额外要求 Manifest 声明、管理员 Grant、用户 Consent（涉及本人数据时）、当前发布版本及资源 ACL 全部同时成立。

PDF Viewer 的 iframe 无法自行读取文件。它通过 Gateway 建立受校验的 Bridge，再请求有限的资源描述或 Range；API 复核文章可读性或个人文件所有权。

进一步阅读：[模块、插件与资源包](/guide/module-plugin-resource-boundaries)、[数据目录](/reference/data-layout)、[三层授权](/plugins/authorization-v3)。


## v1.2 当前授权接入范围

02a 策略与委托内核、02d 独立管理身份域已完成阶段验收；02b 正在逐条业务链接入。公开帖子详情已采用服务端事实构造、纯公开策略和返回前版本复核。增加浏览量与取得正文在同一条件操作内完成，撤私密、下架或回收后不能沿用旧的公开决定读取新正文。

当前 Schema 为 `000001`–`000016`，100 张业务表与 2 张迁移系统表。`000016` 的隐藏帖子事实版本由存储端维护，内容、owner、版块或可见性变化都会推进，包括先改变再恢复。升级须先应用迁移再启动依赖版本列的读取服务；回退要求见[数据库迁移治理](/operations/database-migration-hygiene)。

公开列表、总数与分页也已接入：查询先过滤公开集合，在同一快照形成总数和页面；策略判定后返回前整页复核，成员、顺序、总数或内容版本变化会整页重试，持续竞争返回 409。空页保留并复核公开总数。`/threads/:id/me` 作者详情已接入独立 owner Policy：当前用户 Session 构造完整 Principal，按 owner/版本取得同行原始事实与正文，前后重核会话，竞争重新授权，私有读取不增加浏览量；公开分支遇撤权不会回退作者例外。其他 Feature 私有详情、作者列表、治理/MFA 主体接入、批量授权与有效权限预览仍待后续 02b 切片。公开谓词不会代替插件或集成入口的 Scope、Grant/Consent 与运行状态检查；本页不表示 v1.2 整版完成。
