# 系统架构

> 更新时间：2026-10-15 18:12（Asia/Shanghai）

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

当前 Schema 为 `000001`–`000022`，100 张业务表与 2 张迁移系统表。`000016` 的隐藏帖子事实版本由存储端维护，内容、owner、版块或可见性变化都会推进，包括先改变再恢复。升级须先应用迁移再启动依赖版本列的读取服务；回退要求见[数据库迁移治理](/operations/database-migration-hygiene)。

公开列表、总数与分页也已接入：查询先过滤公开集合，在同一快照形成总数和页面；策略判定后返回前整页复核，成员、顺序、总数或内容版本变化会整页重试，持续竞争返回 409。空页保留并复核公开总数。`/threads/:id/me` 作者详情已接入独立 owner Policy：当前用户 Session 构造完整 Principal，按 owner/版本取得同行原始事实与正文，前后重核会话，竞争重新授权，私有读取不增加浏览量；公开分支遇撤权不会回退作者例外。`/spaces/me/contents` 本人 active 列表也已接入当前 Session 与独立 owner Policy：分页前固定 owner/raw 状态，同一仓储快照取得页/总数，返回前复核整页与完整身份；空页/关闭同步也复核，投影后再次检查，缺新 Port 失败关闭。其他 Feature 私有详情、其他作者/Admin/回收列表、治理/MFA 主体接入、批量授权与有效权限预览仍待后续 02b 切片。公开谓词不会代替插件或集成入口的 Scope、Grant/Consent 与运行状态检查；本页不表示 v1.2 整版完成。

> 2026-10-12 个人文档读取后继切片：实际 user Session + active `personal.document.read`，Document/immutable version/Object 与返回前身份复核、回收元数据脱敏，9 项顶层/3 真实 PG/5 格式/30 组合/9 提交竞争通过；详见仓库 `docs/api/v1.2个人文档读取策略接入合同.md`。02b 仍 partial，治理/其他 Feature/批量/预览继续实施。

> 2026-10-12 RichText正文/附件/UserAsset/Invocation受检查读取通过：原帖Policy与Feature/Object/完整Session返回前复核，80必需测试（3真实PG）与共同门禁通过，详见仓库 `docs/api/v1.2RichText读取策略接入合同.md`、`docs/进度/v1.2-dev/v1.2.51-dev.md`。旧内联图片和其他责任待验收，02b仍partial。

> 2026-10-12 互助/二手详情/list投影通过：93必需（6真实PG），原帖Policy+Detail/creator/version+当前User Session，整页/count再验。仓库合同 `docs/api/v1.2互助与二手读取策略接入合同.md` 与 `.52`记录证据；旧内联图片和02b其他责任仍pending。

> 2026-10-12 内联图片切片已实现父资源Policy、真实图片引用、当前会话及字节快照复核，Web使用认证Blob展示；20必需目标（5真实PG）和Web36组件测试通过，实现/自动化/Linux目标与共同门禁通过。合同 `docs/api/v1.2内联图片授权读取合同.md`，证据 `.53`；个人文件及02b写入/批量/预览仍待完成。

2026-10-12 .54个人空间文件读取：公开头像仅当前active资料或public未停用Space引用，历史仅当前本人；库存/容量/资料与完整Session返回前复核。新增窄CurrentQuotaReader加载持久化配额，不以启动缓存替代当前事实。原文仍由Documents/UserAsset/附件受检查入口负责，无通用Storage Object HTTP下载。补强后21必需/2真实PG/108检查/7竞争及全部共同门禁通过，作者/治理/Admin/批量/预览继续。

2026-10-12 .55 Community discussion作者编辑三态通过：实际edit Policy与完整User Session、事务内账号/会话/资源锁、facts_version+revision CAS、提交前身份及结果授权重验；内容修订/平台审计/outbox原子，计数保留，正文安全规范化，typed Feature独立命令边界。28必需/3真实PG含6提交变化与NOWAIT凭据锁、实际JWT HTTP、共同门禁通过。其余作者生命周期/Feature/治理/MFA/Admin/批量/预览继续，02b未退出。

2026-10-12 .56 本人trash/restore/submit-review三态通过：独立owner lifecycle纯Policy绑定真实raw状态/完整User Session/非复用事实版本，不用read权限写入或假造active；凭据/资源锁与CAS、必需审计/outbox原子，提交前复核。35必需/5真实PG、432纯策略/324服务组合、新增3事务审计回滚/3提交ABA与实际JWT HTTP、共同门禁通过。创建/Feature/治理/Admin/批量/预览继续，02b未退出。

2026-10-12 .57 discussion创建三态通过：当前user/audience与实际raw版块/group/type配置、账号/会话/版块锁、身份与authority前后复核、真实ThreadID/审计/outbox原子；不给未创建资源虚构edit事实。41必需/7真实PG、4合法提交竞争/审计回滚及JWT HTTP与共同门禁通过。Feature/治理/Admin/批量/预览继续，02b未退出。

2026-10-12 22:54 .58 互助/二手作者创建三态通过：窄AuthenticatedThreadCreator与实际User Session，raw版块/group/type事实、凭据/版块/类型配置行锁、typed Detail完整绑定与最终重读、实际Core插入版本复核，Thread/Detail/修订/必需审计/outbox原子。64必需/11真实PG（13新增/51回归）、两模块10提交竞争与明细/审计回滚、第二连接NOWAIT验证及JWT HTTP/共同门禁通过。discussion创建新增类型锁也纳入回归；RichText创建及其他Feature写入、治理/Admin/MFA、批量/预览/退出仍待完成。无新迁移，不声称浏览器/Windows/完整服务器验收。

2026-10-12 23:22 .59 RichText作者创建三态通过：checked Community creator/current User及raw board/group/type锁与实际插入版本复核；Feature自行重读完整Article/creator/body/JSON，不丢大整数精度，必需审计/outbox原子。69必需/13真实PG、新5提交变化/文章及审计回滚/NOWAIT行锁和JWT HTTP、共同门禁通过；其他Feature写入/治理/AdminMFA/批量/预览/退出继续。

2026-10-12 23:49 .60互助/二手编辑/业务状态三态通过：Core协调当前User+原帖命名Policy+事实版本锁/CAS，Feature拥有Detail锁/完整快照/版本CAS；四写路径84必需/17真实PG、20提交变化/4审计回滚/NOWAIT真实锁和计数保留，JWT HTTP及共同门禁通过。图文/私有写入、治理/AdminMFA、批量/预览/退出继续。

2026-10-13 00:04 .61图文编辑/发布/离线/回收三态通过：Core命名edit/owner lifecycle、实际Article完整快照与事务锁、Core非复用事实/修订CAS和必需审计/outbox原子；91必需/19真实PG、16提交竞争/4审计回滚/NOWAIT锁及计数、JWT HTTP和共同门禁通过。私有写入/治理/AdminMFA/批量/预览/退出继续。

2026-10-13 00:43 .62个人文档六条作者写入三态通过：当前User/owner-state-version，实际文档/当前与历史版本/Object锁和完整快照，文档/preview/必需审计/outbox原子；107必需/22真实PG、26提交变化/6审计回滚、实际JWT HTTP及共同门禁通过。取消后补偿不取消，已变化receipt保留并显式待清理。附件/资产/空间/回复、治理/AdminMFA/批量/预览/退出继续。

2026-10-13 01:07 .63当前Admin主体/MFA前置三态通过：签名subject/session与actual独立账号/会话绑定，完整authority双读及深拷贝、实际事务凭据锁，真实MFA时间15分钟边界降为password；120必需/24真实PG、8已提交变化、JWT域HTTP与共同门禁通过。治理grant/业务路由与actor存储仍待接入，02b仍partial。

2026-10-13 01:45（Asia/Shanghai） .64管理域执行权限事实前置三态通过：000017非复用版本、完整实际角色/绑定/权限及事务锁、up/down/up原数据/序列保留；123必需/26真实PG、Admin静态构建和共同门禁通过。17迁移/100业务表+2系统表/116外键，治理授权/actor存储及其余02b责任继续。

2026-10-13 08:53（Asia/Shanghai） .65版块治理执行事实源三态通过：当前User单条完整委派；独立Admin实际角色/绑定/非复用权限与15分钟MFA；完整私有snapshot前后重验、真实事务凭据及见证锁。130必需/28真实PG、14提交变化和共同门禁通过；冻结wire不变，业务资源命令/HTTP与actor存储仍继续，02b仍partial。

2026-10-13 09:04（Asia/Shanghai） .66业务actor域存储000018三态通过：八表九组明确域/opaque ID，原User列/FK保留，旧文本/NULL/system未知域不推断，FK置空保留已知历史；有损down拒绝，Thread actor ABA推进事实版本。132必需/30真实PG、原字段up/down/up摘要一致、Admin构建/共同门禁通过。18迁移/100业务表+2系统表/116外键，资源命令与typed actor适配继续，02b仍partial。

2026-10-13 09:44（Asia/Shanghai） .67当前User/Admin原资源下架路径三态通过：actual当前主体/单条执行见证/冻结Policy与Thread CAS，凭据/授权/资源锁和typed修订/案例/动作/必需审计/outbox原子。140必需/32真实PG、12提交变化/六次必需失败回滚、实际JWT HTTP/Module装配与共同门禁通过。操作结果和事件仅ID/下架状态，Space以最小事件清除投影。其他治理/作者/列表/批量/预览与退出继续，02b仍partial。

2026-10-13 10:12（Asia/Shanghai） .68回复事实存储000019三态通过：Post非复用版本、actual事务锁/content与delete CAS、typed删除actor和有损down拒绝；Memory去别名/隐藏快照/不可回滚高水位。144必需/34真实PG，旧live/deleted行摘要与序列高水位up/down/up保留、Admin构建和共同门禁通过。19迁移/100业务表+2系统表/116外键，回复实际业务/列表Policy及其他02b责任继续，阶段仍partial。

2026-10-13 10:43（Asia/Shanghai） .69回复作者三路径三态通过：当前User独立owner规则、actual父帖/board/group/type与引用/Post事实及事务锁/CAS，计数/通知/必需审计/outbox原子、完整最终证明；事件仅ID。150必需/36真实PG、324纯规则/18提交变化/三次审计回滚、实际JWT HTTP十八模式和共同门禁通过。回复治理/整页及其余02b责任继续，阶段仍partial。

2026-10-13 13:26（Asia/Shanghai） .70回复治理删除三态通过：实际Parent/Post组合版本、当前User单条版块执行grant或独立Admin权限与15分钟MFA，真实凭据/授权/资源锁与CAS，typed删除actor、计数/通知/模块及平台必需审计/outbox原子；结果和事件仅ID。158必需/39真实PG、12提交变化、三平台审计及真实模块审计失败回滚、实际JWT HTTP与共同门禁通过。回复整页及其余02b责任继续，阶段仍partial。

2026-10-13 13:55（Asia/Shanghai） .71回复父帖/详情/列表/count/page读取三态通过：实际active父帖命名策略、公开或当前User本人，live/published Post在单SQL形成total/page，整页/空页的父帖、当前完整主体、成员/顺序/total/完整Post及nonce最终复核。165必需/41真实PG、288纯规则/144实际父帖模式、17提交变化拒绝、counter更新允许、父帖/回复触发器失效拒绝、实际JWT HTTP十三模式及共同门禁通过。其余02b责任继续，阶段仍partial。

2026-10-13 19:39（Asia/Shanghai） .72独立Admin命名执行事实前置与事件元数据路径三态通过：完整当前账号/会话/角色/绑定/权限见证，精确动作及scope，写动作当前MFA；事件仅标准元数据，同谓词先过滤再页/count与整页/空页最终复核。172必需/43真实PG、81命名权限组合、九种提交变化及scope重放拒绝、实际JWT HTTP十五模式和七种变化、Admin组件和模拟API浏览器检查及共同门禁通过。其余02b责任继续，阶段仍partial。

2026-10-13 20:10（Asia/Shanghai） .73管理员置顶/取消置顶/锁定/解锁三态通过：当前独立Admin精确pin/lock权限与MFA、实际active Thread命名Policy和版块/nonce，真实凭据/权限/资源锁、双CAS及typed历史/必需审计/outbox原子，最终复核和三字段安全回执。179必需/45真实PG、1440纯策略/56实际JWT HTTP、十提交变化拒绝/失效trigger拒绝/计数保留、两种Admin ID的八成功命令和十六必需失败回滚及共同门禁通过。其余02b责任继续，阶段仍partial。

2026-10-13 20:51（Asia/Shanghai） .74管理员审核通过/驳回三态通过：当前独立Admin精确review权限/MFA和实际active/pending Thread命名Policy，凭据/权限/Thread/Case真实行锁、双CAS、typed案例开立/解析及修订/动作/必需审计/outbox原子、完整案例/主体/资源最终复核与两字段回执。187必需/47真实PG、720纯策略/32实际JWT HTTP、十提交变化拒绝/失效trigger拒绝/计数保留、两种Admin ID八成功/二十必需失败回滚及共同门禁通过。其余02b责任继续，阶段仍partial。

2026-10-13 21:30（Asia/Shanghai） .75管理员回收/回收恢复/直接恢复三态通过：当前独立Admin精确权限/MFA、实际Thread命名状态规则和凭据/权限/Thread/Case锁、双CAS、typed历史/Case解析/必需审计/outbox及已装配作者通知原子，完整最终证明与安全回执。194必需/49真实PG、1080纯策略/45实际JWT HTTP、十提交变化及失效trigger拒绝/计数保留、两种Admin ID十二成功/三十必需失败回滚（含实际通知SQL）及共同门禁通过。其余02b责任继续，阶段仍partial。

2026-10-13 22:16（Asia/Shanghai） .76管理员清除三态通过：实际trashed/unpurged Thread命名Policy与当前独立Admin精确purge权限/MFA，实际凭据/权限/Thread/Case锁、nonce/修订双CAS、隐藏deleted_at完整tombstone最终证明及typed历史/必需审计/outbox原子、安全204。202必需/51真实PG、360纯策略/15实际JWT HTTP、十一提交变化（含日期ABA）/失效guard拒绝及计数保留、四成功命令/八必需失败回滚、Memory去别名/回滚高水位与共同门禁通过。其余02b责任继续，阶段仍partial。

2026-10-14 02:52（Asia/Shanghai） .77图文授权事实存储000020三态通过：完整Article正文/精确JSON/元数据/创建者/typed updater/日期的非复用版本，actual事务行锁和CAS、失效guard拒绝、隐藏快照与回滚高水位；205必需/53真实PG、五种ABA、opaque Admin typed更新及20迁移up/down/up原数据/分配保留，旧202回归和共同门禁通过。图文治理和其余02b责任继续，阶段仍partial。

2026-10-14 04:10（Asia/Shanghai） .78图文管理员下架/直接恢复/回收三态通过：实际Core命名Policy与当前独立Admin精确权限/MFA，完整Article20非复用快照、实际凭据/权限/Thread/Article锁及双资源CAS、typed updater/history和必需审计/outbox原子、完整最终证明及两字段/204回执；213必需/55真实PG、六成功/24必需失败回滚、十提交变化/失效guard拒绝及计数保留、57实际JWT HTTP和生产Module装配通过。其余02b责任继续，阶段仍partial。

2026-10-14 04:45（Asia/Shanghai） .79用户作用域执行事实PG存储000021三态通过：四张既有授权表共享非复用完整版本、scope先过滤/完整JSON去别名、实际角色/assignment/link/definition行锁、四受管触发器及序列前后守卫；215必需/57真实PG、四表ABA/重插和回滚高水位、失效守卫拒绝、active/deleted/弃用原字段up/down/up摘要与序列保留，旧213及共同门禁通过。User执行源/业务和其余02b责任继续，阶段仍partial。

2026-10-14 05:56（Asia/Shanghai） .80用户作用域Memory事实存储三态通过：actual显式assignment/scope先限定、完整四类JSON去别名，实际权限/scope/custom role变更的私有非复用epoch、读写对象别名保护、rollback current proof与不可回退高水位及MaxInt64变更前关闭；217必需/57真实PG、两个新增Memory顶层/race，旧215及21迁移数据保留和共同门禁通过。User执行源/业务和其余02b责任继续，阶段仍partial。

2026-10-14 06:48（Asia/Shanghai） .81 User版主pin/unpin/lock/unlock三态通过：当前完整User/Session、scope先限定的实际角色关系及非复用版本、旧User admin准入限制/实际行锁、独立User命名Policy与Thread nonce/revision CAS、typed和原User历史、必需平台审计/outbox及模块审计/当前config原子；230必需/62真实PG、1440策略/64实际JWT HTTP、24必需SQL回滚、Core十一提交变化拒绝/计数允许与Identity十三提交变化拒绝，旧217、21迁移数据/高水位及共同门禁通过。无新增DDL，迁移仍21/下一22；文件/空间写入、Admin列表、批量/预览及阶段退出继续。

2026-10-14 07:37（Asia/Shanghai） .82 当前User版主有效权限预览/有界固定类型批量三态通过：actual受管Thread/Post、当前完整User/作用域角色关系及单条post.delete执行grant、与执行相同命名Policy/config、最多50不同实际Post IDs、条目加载后完整authority/parent/config及整批/empty最终复核、安全booleans和无副作用；235必需/64真实PG、十八实际JWT HTTP、十提交变化拒绝/计数允许、Web九组件和format/build、旧230及21原数据/高水位和共同门禁通过。仅当前User治理批量/预览，文件空间、Admin列表/其他批量预览/当前前端身份及阶段退出继续。迁移仍21/下一22，无DDL。

2026-10-14 11:04（Asia/Shanghai） .83 RichText资产/附件PG事实存储000022三态通过：两张既有表完整私有JSON与非复用版本、actual Asset/Binding行锁及空库存admission lock，metadata/旧生命周期version/关系/名称/排序/日期ABA及重插可见；238必需/66真实PG、八种实际ABA/重插、回滚高水位与三失效守卫关闭，22四状态原字段/旧version/对象及绑定引用up/down/up摘要保留，旧235及共同门禁通过。仅存储前置，Memory事实和实际文件/空间命令、Admin列表/批量/预览/当前前端身份及阶段退出继续。当前迁移22/下一23，100业务表/2系统表、116物理FK。

2026-10-14 11:16（Asia/Shanghai） .84 RichText资产/附件Memory事实存储三态通过：actual同Article绑定/Asset完整私有JSON、每记录非复用nonce与共享高水位、raw/date/嵌套Article去别名、rollback恢复内容/审计/当前proof但不回退分配；38影响范围必需/10真实PG、新两Native及四错误重排/五耗尽原子关闭、原36受影响回归与全量Go/race/database/合同/架构/文档共同门禁通过。仅内部Memory存储数据前置，实际读取nonce与文件/空间命令、Admin列表/批量/预览/当前前端身份和阶段退出继续。无DDL，当前迁移22/下一23。

2026-10-14 11:43（Asia/Shanghai） .85 RichText资产/绑定读取完整22事实三态通过：actual字段与nonce同源解码、整批/empty/currentUser及I/O后完整复核，九种实际metadata/关系/owner/状态/重插ABA关闭held reader，counter-only允许；45影响范围必需/15真实PG、新四顶层、原41回归及全量Go/race/database/合同/架构/文档共同门禁通过。仅后继受检查读取，实际文件/空间命令、Admin列表/批量/预览/当前前端身份和阶段退出继续。无DDL，当前迁移22/下一23。

2026-10-14 12:22（Asia/Shanghai） .86 附件四元数据作者命令三态通过：当前完整User/同一Core作者Policy，实际Core/Article20/Asset-Binding22及bind Object事务锁、空admission、精确结果与完整最终证明，必需audit/outbox原子和safe IDs；50影响范围必需/17真实PG、32Native/六actual extra mutations/24signedJWT HTTP、四PG成功/八必需SQL失败回滚、六committed变化拒绝与counter允许，原45及共同门禁通过。仅四个附件元数据命令，上传/私有文档复制/其他文件/空间命令、Admin列表/批量/预览/当前前端身份和阶段退出继续。无DDL，当前迁移22/下一23。

2026-10-14 13:01（Asia/Shanghai） .87 Object 当前配额写入前置三态通过：reserve/commit 在实际账本事务内读取持久化 quota，缺 override 使用配置默认值，来源不可用关闭；实际 quota 行锁及 owner admission 串行化受支持 updater；失败保留原 receipt 并释放容量。54 必需/19 真实 PG、当前升降额/默认值/实际锁与外层事务回滚、五种 byte I/O 失败和原 50 回归及共同门禁通过。容量检查不授予业务权限，附件上传/复制、资产与空间、Admin 列表/批量/预览/当前前端身份及退出继续。无 DDL，current22/next23。

2026-10-14 13:27（Asia/Shanghai） .88 真实附件上传三态通过：同一 Core 作者 edit preflight/execute Policy、当前完整 User 与完整 Core/Article20/Asset-Binding22 在 I/O 前后重验；当前 quota、精确 staged Object 与实际命令锁、预期 Asset+Binding/必需 audit/outbox 原子；五秒取消独立的完整 receipt 补偿、变化回执不误删和 pending 显式报告、strict multipart/safe IDs。63 必需/21 真实 PG、四 SQL 失败与完整回滚、十 staged 提交变化/撤权/守卫/cancel/receipt/cleanup 和 counter 允许、真实 JWT HTTP/格式库存预算及原54回归与共同门禁通过。可信源文档复制、资产与旧图片/空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current22/next23，无 DDL。

2026-10-14 13:56（Asia/Shanghai） .89 可信私有文档附件复制三态通过：当前完整 User、同一 Core 作者 Policy 和独立 source read Policy；目标 whole 快照先验证，来源模块持有 detached Document/currentVersion/Object 及实际源行锁，I/O 前后与实际命令最终复核；精确 size/SHA/MIME、独立 copied Object、必需 audit/outbox rollback 和 receipt 补偿、strict DTO/safe IDs。75 必需/27 真实 PG、新9顶层/3 PG、原63与既有个人文档作者3 PG回归、支持的源 version ABA/凭据/config/源对象/截短流关闭、源行锁/三 SQL 失败/实际事务内源变化完整回滚、PDF/DOCX 独立 bytes/实际 JWT/生产可信桥及共同门禁通过。资产与旧图片/空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current22/next23，无 DDL。

2026-10-14 17:30（Asia/Shanghai） .90 资产作者回收/恢复三态通过：当前完整 User、独立固定 owner lifecycle Policy、完整 Asset22/非复用 nonce、两个真实引用 FK/约束触发器守卫及 Asset 行锁的零引用 admission；恢复实际 owned ready Object、精确预期结果及当前 proof/config 最终复核；Asset/platform audit/outbox 原子、安全 IDs/status。81 必需/29 真实 PG、160 纯策略/20 业务/四事务内额外变化及12实际 JWT HTTP、两动作成功/六必需 SQL 失败完整回滚、实际 User/Asset/restore Object 锁与两 FK 阻塞、八提交变化/守卫及 counter 允许、原75回归和共同门禁通过。资产与旧图片/空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current22/next23，无 DDL。

2026-10-14 17:58（Asia/Shanghai） .91 独立 Admin 资产隔离/恢复三态通过：当前完整账号/会话/角色/绑定/权限17与15分钟 MFA、实际资产 scope 的 personal_space.asset.manage 和独立命名 Policy；完整 Asset22/引用守卫与 restore Object 实际锁、精确预期结果/最终 authority/config；迁移18 typed Admin NULL User 历史、必需 Asset/platform audit/outbox 原子和 strict DTO/safe IDs。92 必需/33 真实 PG、640纯规则/60业务/五实际额外变化/36 signed JWT 与四生产模块检查，四 PG 成功/12 必需 SQL 失败及四实际事务内 metadata 变化完整回滚、八提交变化/守卫和 counter 允许，原81及四 Identity execution 回归/共同门禁通过。资产与旧图片/空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current22/next23，无 DDL。

2026-10-14 18:26（Asia/Shanghai） .92 独立 Admin 资产汇总/清除预览三态通过：当前 read_audit 固定 aggregate scope 与 manage/实际 Asset scope/MFA、固定 summary 与共享 purge 元数据 Policy；固定时间两次完整汇总及空结果最终 authority/config，预览完整 Asset22/nonce/引用与守卫/最终证明；no-body/safe DTO，无私有文件信息或写副作用。99 必需/36 真实 PG、320+16纯策略/99精确权限组合、26当前与状态场景/19实际读取后反例/16 signed JWT、14当前PG/两最终权限变化及第三真实空DB的三 current/revoke/ABA、原92回归与共同门禁通过。资产与旧图片/空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current22/next23，无 DDL。

2026-10-14 18:47（Asia/Shanghai） .93 Object 删除生命周期前置三态通过：存储持有完整私有 owned receipt/去别名与JSON空对象、实际 prepare/restore/finalize 全回执与行锁、pending/终态安全重试和一次容量释放；取消独立5秒 finalize、restore/finalize 失败明确 pending，拒绝外层业务事务物理IO及终态恢复/过期回执。105 必需/38 真实 PG、四Native/八并发恰一成功、真实 ledger SQL 拒绝/回滚/重试与另一对象保留、七 metadata/private key/alias/outer txn/prepare后变化/restore SQL 场景和actual行锁、原99回归及共同门禁通过。资产与旧图片/空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current22/next23，无 DDL。

2026-10-14 19:44（Asia/Shanghai） .94 实际独立 Admin Asset physical purge 三态通过：同一92纯Policy/当前manage/MFA与完整Asset22/引用/93opaque Object receipt；prepare与final元数据必需typed Asset/platform audit/outbox，字节guard实际锁管理域/Asset/FK admission贯穿I/O而Object独立提交；最终terminal/完整事实复核，postprepare失败诚实pending且当前重试不重复terminal bytes，strict确认/safe IDs。112必需/40真实PG、四Native/14早拒绝/三个阶段审计原子或pending及重试/四postbyte场景与18 signed JWT；18 PG成功或16真实phase SQL拒绝、typedNULLUseractor、metadataObject锁/byte期间权限Asset锁及两FKdeadline阻塞/Object无guard锁和当前terminal重试，原105回归及共同门禁通过。资产与旧图片/空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current22/next23，无 DDL。

2026-10-14 23:00（Asia/Shanghai） .95 旧内联图片三入口作者上传三态通过：当前完整User与固定本人库存Policy/实际Core作者edit及完整Article20，current quota账本/实际locks与必需audit/outbox；新文件私有inode/size/mtime/digest回执、精确失败清理或pending，strict multipart/兼容URL与编辑器metadata。123必需/42真实PG、11新增含实际JWT三入口与真实路由/OpenAPI检查，十四PG执行（3成功/11真实必需SQL拒绝）及十二current/ABA/实际额外SQL变化，原112回归与共同门禁通过。空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current22/next23，无 DDL。

2026-10-14 23:44（Asia/Shanghai） .96 空间/样式快照事实存储三态通过：前向23完整私有行与非复用nonce、actual row/owner admission及空库存guard、context executor/完整外层事务回滚，Memory嵌套去别名/rollback highwater/耗尽原子；原metadata/typed actor18/快照与高水位up/down/up保留。141必需/47真实PG、5新顶层（3PG）与136当前图片/空间读取回归、current23/four owned DB、Admin build/schema/ER及共同门禁通过。空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current23/next24，原表/FK/主体域保留。

2026-10-15 00:15（Asia/Shanghai） .97 当前User空间资料创建/更新与本人读取三态通过：固定ownerPolicy/完整当前会话、actualSpace23/默认无行、实际User/Session/owner admission/Space锁，规范化请求和独立预期模型/精确结果/完整最终proof；必需audit/outbox原子/safe IDs，Web等待ACK回读。149必需/49真实PG、8新顶层含64规则/实际JWT/模块绑定/六实际额外写入/只读最终会话、6PG执行（2成功4必需P0001回滚）/8currentABA与额外SQL变化；原141回归/两个Web组件与共同门禁通过。空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current23/next24，原表/FK/主体域保留。

2026-10-15 16:25（Asia/Shanghai） .98 历史头像选择三态通过：固定owner/avatar_select规则、完整当前User/空间23或默认无行、全部保留库存及所选有界字节/inode/大小/mtime，写前后与最终proof/config复核；必需audit/outbox原子/safe IDs，无文件与容量变化，Web ACK后回读资料/存储/历史。155必需/51真实PG、6新顶层/96规则、八实际写后变化/五held-proof与实际JWT，6PG执行（2成功4必需P0001回滚）/七实际变化；原149回归/四Web组件及共同门禁通过。空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current23/next24，原表/FK/主体域保留。

2026-10-15 16:44（Asia/Shanghai） .99 头像上传字节/元数据三态通过：固定owner/avatar_upload、完整当前User/Space23/default，actual User/Session/Space/admission/quota/account锁；完整优化新字节分配后固定namespace写入，私有精确回执/最终proof，必需audit/outbox原子、精确补偿或pending，旧头像不提前裁剪。163必需/53真实PG、8新顶层/128规则/实际JWT/模块与Core回执、八PG执行（2成功6必需SQL P0001）/七真实变化；原155回归/七Web组件及共同门禁通过。历史裁剪仍需下一持久化生命周期，超过上限明确cleanup_pending。空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current23/next24，原表/FK/主体域保留。

2026-10-15 17:15（Asia/Shanghai） .100 头像保留清理三态通过：固定owner/currentUser/Space23/库存/config，私有稳定identity回执与owned任务实际行锁，持久prepare先于bytes，取消独立5秒final将一次负账本/终态/必需audit-outbox原子；变化inode不删、失败pending/保守容量、新适配器重启当前重试，保护User与Space正在使用的头像。173必需/55真实PG、10新增/160规则、十一PG场景（2当前/保护、9真实阶段SQL拒绝）/八currentABA-inode-extraSQL-cancel-scope，原163回归/十Web组件/共同门禁与Windows仅编译通过。实际上传提交后裁剪，满额可独立重试清理。空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current23/next24，原表/FK/主体域保留。

2026-10-15 17:45（Asia/Shanghai） .101 实际Appearance空间样式三命令三态通过：生产Module/App完整resolver转发、固定owner当前User/Space23/default与全部Snapshot23及真实锁，精确旧样式快照新增/样式结果/最终完整事实，必需audit-outbox原子/safe IDs，Web ACK后当前回读。先复现并补齐三cloner的Capabilities别名。181必需/57真实PG、8新增/256规则、28PG执行（6成功22真实P0001）/八currentABA或额外SQL、原173回归/十六Web组件及共同门禁通过；HTML/ZIP/source与样式读取仍继续。空间命令、Admin 列表/批量/预览/当前前端身份与退出继续。current23/next24，原表/FK/主体域保留。

2026-10-15 18:12（Asia/Shanghai） .102 空间样式HTML/ZIP/源码包三变体三态通过：实际Appearance完整resolver转发，固定owner/完整User/Space23/全部Snapshot23，先授权后有界独立构建；源码实际root及完整strict Package/隐藏RawFiles前后复核，沿用精确样式/历史/必需audit-outbox原子。strict JSON/有界单文件multipart关闭后清理，实际JWT/六生产入口及22Web组件通过。189必需/59真实PG，8新增/352规则、30PG执行（6成功24实际P0001）/十实际变化，原181回归与共同门禁通过；样式读取、独立Admin及整阶段退出继续。current23/next24，原表/FK/主体域保留。
