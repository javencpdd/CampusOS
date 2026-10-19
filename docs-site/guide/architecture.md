# 系统架构

> 更新时间：2026-10-19（Asia/Shanghai）

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
| `internal/plugin` | 当前v5 Manifest/包字节/签名与独立包索引；旧管理/配置数据保留，持久安装/Operation已由03b接入，v4自动发现已退役。 |
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

当前28条迁移、112业务表与2执行器表。所有原始切片记录保留在仓库 `docs/进度/v1.2-dev/`；阶段出口为 `v1.2.110-dev.md`，当前责任清单及带摘要机器报告与其关联。02c已通过完整阶段三态验收，v5/Dispatcher与Knowledge来源ACL分别属于03c/09a等后续责任；不是v1.2整版发布完成声明。

V12-02c完整阶段三态通过：Identity独立PG/Memory摘要凭据、Admin当前权限/MFA/CSRF管理、一次性签发/原子轮换/两级撤销、独立Integration认证和精确通道/端点/资源白名单均已接入。显式v2复用首片v1并增加Community公开帖子，实际调用原模块公开读Port；机器与来源事实行锁保持到必需typed审计/Outbox提交，再返回投影。私有、待审、下架与删除不借白名单放行。12条真实路由、Admin一次性秘密界面、Schema/SDK、HTTP/PG/并发/故障路径完成；30项原生顶层、384份Go/oracle决策、18项实际后端Chrome断言及11项共同门禁通过。000025新增两张独立表，02c退出时25/next26、102业务表+2执行器表；100张旧表原始行、主体和分配高水位保留，非空身份/权限引用拒绝回滚。03a现已完成完整阶段验收，详见下文；知识集合09a、完整公共知识MCP11和v5Dispatcher03c仍保留原责任。 仓库进度为 `docs/进度/v1.2-dev/v1.2.112-dev.md`，对应责任清单与完整退出报告。
V12-03a完整阶段三态通过：完整v5 Manifest/共享SDK、当前宿主目录、真实包字节快照与确定性归档、Ed25519当前信任/撤销和不可变发布身份、主CLI及两个重建示例均已验收。旧v4/源码自动发现、适配及旧包入口已退役；冻结G1清单叠加03a覆盖，历史证据不回写。11项原生顶层99个PASS事件、64声明并发/16发布竞争，23份Go/oracle决策、43个实际CLI进程、实际cmd/server＋隔离PG16迁移1–25的5项检查及13项共同门禁通过。Host能力目录当前为空，扩展DTO不等于可调用Provider；内存发布索引不等于持久安装/恢复，03b负责持久目录/安装/Operation，03c/04/05/06负责Dispatcher/UI/Runtime。03a退出时25/next26，彼时未新增迁移。仓库证据为`docs/进度/v1.2-dev/v1.2.114-dev.md`与`docs/项目计划书v1/项目计划v1.2/evidence/v12-03a-exit-linux.json`；当前操作见[v5包说明](/plugins/manifest)。

历史.115首个发布身份持久化切片三态通过：`VerifyAndRecordContext`在当前包/签名/信任复核后提交PostgreSQL永久摘要坐标，服务器不回退Memory允许。000026新增`plugin_package_identities`，独立于旧Manager与User/Admin；数据库拒绝更新/删除/清空，非空拒绝回滚。首片完成时26/next27，103张业务表+2执行器；账本不保存包字节、安装、Grant或运行状态。17原生顶层/110个PASS事件、PG16.14重启前后6个独立验证进程、16目录竞争/真实提交故障、102旧表及旧序列保留与13项共同门禁通过。该历史首片之后的全部03b工作现由.116完成。仓库合同为`docs/api/v1.2插件v5发布身份持久化合同.md`，进度与报告为`v1.2.115-dev.md`和`v12-03b-package-identities-linux.json`。

### V12-03b 完整阶段

更新时间：2026-10-18 05:06（Asia/Shanghai）。V12-03b完整阶段已通过实现、自动化和Linux目标验收。000027增加包族/发布目录、全局/用户安装、独立v5 Grant/Consent/配置和Operation共8表，当前27/next28、111业务表+2执行器。当前会话/权限/MFA、CAS/幂等、审计/Outbox事务、租约/启动对账、保留模式升级、owner清理与包GC均接入生产；旧Manager控制/市场/HostAPI启动退役，旧HTTP返回410，新9个v5接口提供安全投影。原生29顶层/117 PASS事件；含PG/恢复/装配/实际HTTP共33顶层/126事件，103旧表/旧序列保留和13共同门禁通过。启用在生产保持pending/runtime_unavailable，有界宿主观察桩不代表真实Runner或贡献发布。03c/04/05/06继续负责Dispatcher/资源ACL/贡献发布、UI与Runner。在.116退出时尚未启动03c；当前首片见下文，也不是v1.2整版发布。仓库证据：`docs/进度/v1.2-dev/v1.2.116-dev.md`及`docs/项目计划书v1/项目计划v1.2/evidence/v12-03b-exit-linux.json`；当前操作见[v5包说明](/plugins/manifest)。


## 历史 V12-03c 类型化贡献目录首片

更新时间：2026-10-18 11:49（Asia/Shanghai）。`plugin.v5.contributions`已装配宿主内部三类贡献查询/选择。每次从持久03b事实复核当前包、信任、安装、版本Grant/所需Consent、owner和ready/config/代次；同查询多匹配报冲突，明确坐标不回退，旧绑定重核，扫描预算耗尽失败关闭。启动对账失败时目录保持不可用，无注册缓存或假Provider。实现/定向race及Linux原生+独占PG16目标通过，8项共同门禁通过，进度为`docs/进度/v1.2-dev/v1.2.117-dev.md`，报告为`v12-03c-directory-linux.json`。没有新增迁移，仍27/next28、111业务表+2执行器。03c仍进行中：Dispatcher、Feature/资源ACL实际调用、运行发布、取消/排空及事件投递留待后续；生产启用仍pending/runtime_unavailable。


## 历史 V12-03c prompt-only Chat Dispatcher切片

更新时间：2026-10-18 12:32（Asia/Shanghai）。宿主`plugin.v5.chat-dispatcher`实现只转发当前User明确prompt的`ai.chat`调用，限定context_class=none；严格DTO、owner/当前会话/消费者Feature/绑定的调用前后复核、字节/期限/进程内并发门禁已定向race通过，Linux原生/独占PG目标通过，8项共同门禁通过。Consumer Feature与Transport由编译宿主显式绑定，生产两者均未绑定，不伪造AI Gateway Feature或Runner，内置AI/RAG保持独立。证据为`docs/进度/v1.2-dev/v1.2.118-dev.md`及`v12-03c-chat-dispatch-linux.json`。03c仍缺完整双向调用/实例凭据、Preview/Knowledge资源ACL、运行贡献发布、取消排空与事件；无新迁移，仍27/next28、111业务表+2执行器。

## V12-03c 受管宿主调用与事件

更新时间：2026-10-18（Asia/Shanghai）。03c完整宿主合同三态已通过，证据见仓库`docs/进度/v1.2-dev/v1.2.119-dev.md`与`v12-03c-exit-linux.json`。三类贡献严格双向envelope、受管发布/短期实例凭据、当前身份/Feature/资源修订复核、字节/期限/进程并发与取消排空均已实现。Host目录仅公开帖子读取和三类元数据订阅；公开资源调用真实Core Port，知识源生产授权未绑定时关闭投递。内置AI/RAG不依赖插件。

000028增加独立元数据队列表，current28/next29、112业务表+2执行器。已提交Core Outbox证明、有限引用/状态/修订、独立事务扇出及独立插件工作器，带单赢家租约、旧ACK拒绝、5次重试和死信；不复制正文、Prompt、Secret或原始Outbox。非空down拒绝并保留证据。短期凭据和发布不跨重启，旧ready事实不恢复执行权。

03c退出时依计划使用有界编译契约适配器，当时生产无真实Runner。04负责UI Host，05/06负责Wasm/Container与硬隔离，09负责真实知识绑定。前两节为当时的历史范围，旧证据不回写。

当前源码59项必需顶层/265 PASS事件、13项共同门禁通过；111旧业务表和11旧序列完整保留。依赖已满足的最前未完成阶段转为04，本轮未启动。

## V12-05 Linux Wasm Runtime

更新时间：2026-10-19（Asia/Shanghai）。原生 API 显式绑定固定宿主 worker 镜像，执行签名不可变 Wasm；事务外真实编译/初始化、实际 cgroup CPU/RSS/PID 校验成功，才提交 Observe 并发布受管贡献。逐次复核当前安装、信任、Grant/Consent、配置/代次/Feature/Core ACL。有限 `campusos.host_call` 的正向路径为公开线程读，元数据和普通配置使用有限 ABI；没有 WASI/目录/网络/DB/宿主凭据。

单写宿主预留总预算并持有 PG 独占 guard，失锁/到期/撤销/退出先撤下并回收。强杀重启先清理相同部署命名空间的旧 worker，再发布新代次。配置见[Wasm 执行器](/deployment/configuration#v12-05-wasm-执行器)，完整实现/自动化/Linux 目标证据见仓库 `.123` 及 `v12-05-runtime-exit-linux.json`。当前28/next29，无新迁移；Container/Windows 和内建 AI/RAG 绑定仍属06/14c。


## V12-06 Container Runtime（Linux）

更新时间：2026-10-19（Asia/Shanghai）。原生宿主显式开启[Container执行器](/deployment/configuration#v12-06-container-执行器)，精确本地签名镜像在network none、非root/只读根/cgroup v2与独立限额临时目录内执行。HTTP/gRPC经私有标准输入/输出管道，无监听端口/host mount/socket/数据库或运行凭据暴露；普通配置、元数据ACK和当前typed Host Broker由受管层复核。

Wasm/Container共用PG guard与总预算；只有真实readiness才Observe/发布，取消/OOM/退出与撤权/到期关闭当前代次，重启先围栏同部署孤儿。Linux证据为仓库`.125`与`v12-06-runtime-linux.json`；Windows原生Docker Desktop实际目标仍未验收，06保持进行中。前一节05的阶段边界是历史记录。
