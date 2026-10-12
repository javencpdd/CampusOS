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

公开列表、总数与分页也已接入：查询先过滤公开集合，在同一快照形成总数和页面；策略判定后返回前整页复核，成员、顺序、总数或内容版本变化会整页重试，持续竞争返回 409。空页保留并复核公开总数。`/threads/:id/me` 作者详情已接入独立 owner Policy：当前用户 Session 构造完整 Principal，按 owner/版本取得同行原始事实与正文，前后重核会话，竞争重新授权，私有读取不增加浏览量；公开分支遇撤权不会回退作者例外。`/spaces/me/contents` 本人 active 列表也已接入当前 Session 与独立 owner Policy：分页前固定 owner/raw 状态，同一仓储快照取得页/总数，返回前复核整页与完整身份；空页/关闭同步也复核，投影后再次检查，缺新 Port 失败关闭。其他 Feature 私有详情、其他作者/Admin/回收列表、治理/MFA 主体接入、批量授权与有效权限预览仍待后续 02b 切片。公开谓词不会代替插件或集成入口的 Scope、Grant/Consent 与运行状态检查；本页不表示 v1.2 整版完成。

> 2026-10-12 个人文档读取后继切片：实际 user Session + active `personal.document.read`，Document/immutable version/Object 与返回前身份复核、回收元数据脱敏，9 项顶层/3 真实 PG/5 格式/30 组合/9 提交竞争通过；详见仓库 `docs/api/v1.2个人文档读取策略接入合同.md`。02b 仍 partial，治理/其他 Feature/批量/预览继续实施。

> 2026-10-12 RichText正文/附件/UserAsset/Invocation受检查读取通过：原帖Policy与Feature/Object/完整Session返回前复核，80必需测试（3真实PG）与共同门禁通过，详见仓库 `docs/api/v1.2RichText读取策略接入合同.md`、`docs/进度/v1.2-dev/v1.2.51-dev.md`。旧内联图片和其他责任待验收，02b仍partial。

> 2026-10-12 互助/二手详情/list投影通过：93必需（6真实PG），原帖Policy+Detail/creator/version+当前User Session，整页/count再验。仓库合同 `docs/api/v1.2互助与二手读取策略接入合同.md` 与 `.52`记录证据；旧内联图片和02b其他责任仍pending。

> 2026-10-12 内联图片切片已实现父资源Policy、真实图片引用、当前会话及字节快照复核，Web使用认证Blob展示；20必需目标（5真实PG）和Web36组件测试通过，实现/自动化/Linux目标与共同门禁通过。合同 `docs/api/v1.2内联图片授权读取合同.md`，证据 `.53`；个人文件及02b写入/批量/预览仍待完成。

2026-10-12 .54个人空间文件读取：公开头像仅当前active资料或public未停用Space引用，历史仅当前本人；库存/容量/资料与完整Session返回前复核。新增窄CurrentQuotaReader加载持久化配额，不以启动缓存替代当前事实。原文仍由Documents/UserAsset/附件受检查入口负责，无通用Storage Object HTTP下载。补强后21必需/2真实PG/108检查/7竞争及全部共同门禁通过，作者/治理/Admin/批量/预览继续。

2026-10-12 .55 Community discussion作者编辑三态通过：实际edit Policy与完整User Session、事务内账号/会话/资源锁、facts_version+revision CAS、提交前身份及结果授权重验；内容修订/平台审计/outbox原子，计数保留，正文安全规范化，typed Feature独立命令边界。28必需/3真实PG含6提交变化与NOWAIT凭据锁、实际JWT HTTP、共同门禁通过。其余作者生命周期/Feature/治理/MFA/Admin/批量/预览继续，02b未退出。

2026-10-12 .56 本人trash/restore/submit-review三态通过：独立owner lifecycle纯Policy绑定真实raw状态/完整User Session/非复用事实版本，不用read权限写入或假造active；凭据/资源锁与CAS、必需审计/outbox原子，提交前复核。35必需/5真实PG、432纯策略/324服务组合、新增3事务审计回滚/3提交ABA与实际JWT HTTP、共同门禁通过。创建/Feature/治理/Admin/批量/预览继续，02b未退出。

2026-10-12 .57 discussion创建三态通过：当前user/audience与实际raw版块/group/type配置、账号/会话/版块锁、身份与authority前后复核、真实ThreadID/审计/outbox原子；不给未创建资源虚构edit事实。41必需/7真实PG、4合法提交竞争/审计回滚及JWT HTTP与共同门禁通过。Feature/治理/Admin/批量/预览继续，02b未退出。
