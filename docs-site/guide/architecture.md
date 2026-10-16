# 系统架构

> 更新时间：2026-10-16 20:34（Asia/Shanghai）

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
| `internal/plugin` | 当前v5 Manifest/包字节/签名与独立包索引；旧管理/配置数据保留，持久安装由03b接入，v4自动发现已退役。 |
| `web/` / `admin/` | 用户与管理端 Vue 应用；核心页面不内嵌插件实现。 |
| `plugins/` | 外部插件独立源码与发布包边界。 |

## 权限与插件

社区和空间操作按“认证 → 稳定权限 → 服务端计算对象范围 → 业务约束 → 审计”处理。插件则额外要求 Manifest 声明、管理员 Grant、用户 Consent（涉及本人数据时）、当前发布版本及资源 ACL 全部同时成立。

PDF Viewer 的 iframe 无法自行读取文件。它通过 Gateway 建立受校验的 Bridge，再请求有限的资源描述或 Range；API 复核文章可读性或个人文件所有权。

进一步阅读：[模块、插件与资源包](/guide/module-plugin-resource-boundaries)、[数据目录](/reference/data-layout)、[三层授权](/plugins/authorization-v3)。


## v1.2 当前授权接入范围

02a策略/委托内核、02d独立管理域和02b限定资源范围均通过阶段验收。User/Admin业务分别使用当前完整主体及实际资源，公开/本人/Feature/作者/治理、字节和Space、独立Admin管理元数据/有界预览及当前SPA身份已完成；元数据或预览不能转为私人原文或执行凭据。

Admin刷新使用独立HttpOnly Cookie与CSRF，认证器配置/确认/升阶在后台本人会话中完成。登录/恢复/受保护导航读取当前身份；API仍逐次检查业务权限与近期MFA，客户端丢弃身份变化后的旧结果。

V12-02b 限定资源范围三态完成：144条当前资源/身份入口、八组责任链；63份历史切片的539个断言与21项新增/修改断言，在当前源码下560项必需顶层测试全部PASS，无FAIL/SKIP/race。13个隔离PG16数据库按互不兼容夹具分库；000016–000024九组迁移的四阶段原字段/主体/非复用高水位保留；17项实际cmd/server+当前Admin/Web的Chrome浏览器断言、Web89/Admin50组件及共同门禁通过。02b退出时current24/next25；完整源码、日志、构建物和目标证据摘要由退出报告核对。

当前25条迁移、102业务表与2执行器表。所有原始切片记录保留在仓库 `docs/进度/v1.2-dev/`；阶段出口为 `v1.2.110-dev.md`，当前责任清单及带摘要机器报告与其关联。02c已通过完整阶段三态验收，v5/Dispatcher与Knowledge来源ACL分别属于03c/09a等后续责任；不是v1.2整版发布完成声明。

V12-02c完整阶段三态通过：Identity独立PG/Memory摘要凭据、Admin当前权限/MFA/CSRF管理、一次性签发/原子轮换/两级撤销、独立Integration认证和精确通道/端点/资源白名单均已接入。显式v2复用首片v1并增加Community公开帖子，实际调用原模块公开读Port；机器与来源事实行锁保持到必需typed审计/Outbox提交，再返回投影。私有、待审、下架与删除不借白名单放行。12条真实路由、Admin一次性秘密界面、Schema/SDK、HTTP/PG/并发/故障路径完成；30项原生顶层、384份Go/oracle决策、18项实际后端Chrome断言及11项共同门禁通过。000025新增两张独立表，当前25/next26、102业务表+2执行器表；100张旧表原始行、主体和分配高水位保留，非空身份/权限引用拒绝回滚。03a现已完成完整阶段验收，详见下文；知识集合09a、完整公共知识MCP11和v5Dispatcher03c仍保留原责任。 仓库进度为 `docs/进度/v1.2-dev/v1.2.112-dev.md`，对应责任清单与完整退出报告。
V12-03a完整阶段三态通过：完整v5 Manifest/共享SDK、当前宿主目录、真实包字节快照与确定性归档、Ed25519当前信任/撤销和不可变发布身份、主CLI及两个重建示例均已验收。旧v4/源码自动发现、适配及旧包入口已退役；冻结G1清单叠加03a覆盖，历史证据不回写。11项原生顶层99个PASS事件、64声明并发/16发布竞争，23份Go/oracle决策、43个实际CLI进程、实际cmd/server＋隔离PG16迁移1–25的5项检查及13项共同门禁通过。Host能力目录当前为空，扩展DTO不等于可调用Provider；内存发布索引不等于持久安装/恢复，03b负责持久目录/安装/Operation，03c/04/05/06负责Dispatcher/UI/Runtime。当前25/next26，未新增迁移。仓库证据为`docs/进度/v1.2-dev/v1.2.114-dev.md`与`docs/项目计划书v1/项目计划v1.2/evidence/v12-03a-exit-linux.json`；当前操作见[v5包说明](/plugins/manifest)。
