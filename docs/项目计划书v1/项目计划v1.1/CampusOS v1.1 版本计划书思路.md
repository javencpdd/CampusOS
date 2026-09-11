
1. **v1.0 已完成能力**
   - 插件身份体系；
   - Capability Catalog；
   - AuthorizationService；
   - Admin Grant；
   - User Consent；
   - Managed Data；
   - Process Runtime；
   - SDK/CLI/Harness。

2. **v1.0 架构约束**
   - 插件权限采用 Declaration + Admin Grant + User Consent 三层交集；
   - 不允许插件自行声明用户身份；
   - 资源访问必须经过宿主授权；
   - 不修改冻结 migration baseline。

3. **当前 v1.1 原计划核心**
   - 增强 Personal Space；
   - 引入 Resource Management；
   - 支持 PDF/MP4/MP3 在线预览；
   - 扩展 Plugin Resource Access。

---

经过项目审查，我认为原计划总体方向正确，但存在几个需要优化的问题：

## 一、项目计划审查结论

### 1. 最大调整：降低“资源系统”的侵入性

原计划：

> 新增 Resource Management Layer

存在风险：

因为 CampusOS 已经有：

- Managed Data；
- 用户空间；
- 插件数据管理。

如果直接新增 Resource 体系，容易出现：

```
Managed Data

+

Resource

+

File Permission

```

三个数据体系并存。

长期会产生：

- 权限重复；
- 数据归属混乱；
- 插件访问入口不统一。


优化：

定义：

> Resource 是 Personal Space 中一种 Managed User Asset。

而不是独立数据体系。


调整后：

```
Personal Space

    |

Managed Data

    |

User Asset

    |

File / Media Resource

```


---

### 2. 文件权限不能独立设计

原计划：

新增：

```
resource_permission
```

风险：

与：

```
AuthorizationService
```

冲突。


优化：

资源权限分两层：

|层级|职责|
|-|-|
|AuthorizationService|决定插件是否可以访问|
|Resource ACL|决定资源拥有者是否允许共享|

即：

```
Plugin Access

↓

Authorization

↓

Resource ACL

↓

Storage
```


---

### 3. Preview Service 应优先于 Sharing

原因：

用户当前最直接需求：

上传 → 查看。


分享属于协作能力。

因此优先级调整：

```
上传
 ↓
存储
 ↓
预览
 ↓
权限
 ↓
分享
```

---

# CampusOS v1.1 优化执行计划书

---

# 一、版本目标

## 版本名称

CampusOS v1.1

## Personal Space Resource Capability Enhancement


## 核心目标

基于已有个人空间和插件平台能力，完善用户资源生命周期管理，实现：

```
用户

↓

个人空间

↓

资源管理

↓

在线访问

↓

插件调用
```


---

# 二、版本范围

## 包含

### P0

- 用户资源模型；
- 文件上传；
- 文件存储；
- PDF预览；
- MP4播放；
- MP3播放；
- 权限接入。


### P1

- 文件共享；
- 文件生命周期；
- 插件访问资源。


### P2

- 对象存储迁移；
- 大文件优化；
- CDN；
- 媒体转码。


---

# 三、任务拆解（WBS）


# EPIC-1：Resource基础能力建设

## R1.1 数据模型设计

优先级：

P0


目标：

建立统一资源模型。


任务：

- 分析现有 Managed Data；
- 确认 Resource 边界；
- 设计 migration。


交付：

```
000006_resource.sql
```


完成标准：

- migration 可升级；
- rollback 可执行；
- 不影响旧表。


负责人：

后端负责人。


---

## R1.2 Resource Service


优先级：

P0


目标：

提供资源生命周期管理。


功能：

- 创建；
- 查询；
- 删除；
- 状态管理。


接口：

```
POST /resources

GET /resources

GET /resources/{id}

DELETE /resources/{id}

```


完成标准：

- API测试通过；
- 权限校验存在。


---

# EPIC-2：Storage能力


## R2.1 存储抽象层


优先级：

P0


目标：

业务与存储解耦。


设计：

```
Resource Service

       |

Storage Interface

       |

Local Storage

```


完成标准：

未来可替换 MinIO/S3。


风险：

中。


控制：

禁止业务直接访问文件路径。


---

# EPIC-3：在线预览系统


## R3.1 Preview Gateway


优先级：

P0


目标：

统一资源访问入口。


接口：

```
GET /resources/{id}/preview
```


完成标准：

- 权限检查；
- Range支持；
- Token控制。


---

## R3.2 PDF Preview


优先级：

P0


技术：

PDF.js。


交付：

前端Viewer。


完成标准：

支持：

- 打开；
- 翻页；
- 缩放。


---

## R3.3 Video Preview


优先级：

P0


范围：

MP4。


完成：

HTTP Range Streaming。


暂不做：

- FFmpeg；
- HLS。


原因：

控制开发范围。


---

## R3.4 Audio Preview


优先级：

P0


范围：

MP3。


完成：

HTML5 Audio。


---

# EPIC-4：权限体系融合


## R4.1 Resource访问授权


优先级：

P0


目标：

复用：

AuthorizationService。


新增 Capability：

```
resource.read

resource.preview

resource.download

```


完成标准：

插件访问必须经过：

```
Capability

+

Consent

+

Resource Ownership
```


---

# EPIC-5：资源共享


## R5.1 用户共享


优先级：

P1


功能：

用户A共享资源给用户B。


支持：

- 查看；
- 预览；
- 下载。


---

# EPIC-6：插件资源访问


## R6.1 Demo Plugin


优先级：

P1


目标：

验证：

```
Plugin

↓

Authorization

↓

Resource

```


交付：

示例插件。


---

# 四、优先级排序


## P0（必须完成）


原因：

形成最小闭环。


顺序：

```
数据库

↓

Resource Service

↓

Storage

↓

Preview

↓

Authorization

```


包含：

|任务|原因|
|-|-|
|Resource模型|基础|
|Storage|依赖|
|Preview|核心需求|
|权限接入|安全要求|


---

## P1（增强能力）


包含：

- 分享；
- 插件访问；
- Demo插件。


原因：

依赖P0稳定。


---

## P2（长期优化）


包含：

- 对象存储；
- 视频转码；
- CDN；
- 大文件优化。


---

# 五、风险分析与安全边界


# 风险1：破坏已有Managed Data体系

等级：

★★★★★


影响：

严重。


原因：

新增Resource可能重复已有数据模型。


控制：

必须先完成：

```
Managed Data

与

Resource

关系设计评审
```


止损条件：

如果无法统一：

停止Resource独立建模。


---

# 风险2：权限体系分裂


等级：

★★★★★


禁止：

新增：

```
file_permission
```

替代：

```
AuthorizationService

+

Resource ACL
```


---

# 风险3：大文件传输性能


等级：

★★★


措施：

v1.1：

- Range Streaming。


延期：

- CDN；
- HLS。


---

# 风险4：存储不可替换


等级：

★★★


措施：

必须：

Storage Adapter。


禁止：

业务代码：

```go
os.Open(path)
```


---

# 六、时间计划


假设：

2~3人开发团队。


周期：

8周。


---

# Phase 0：架构确认

时间：

第1周


任务：

- 数据模型评审；
- API设计；
- 权限设计。


产出：

设计文档。


负责人：

架构负责人。


---

# Phase 1：Resource基础能力

时间：

第2-3周


完成：

- Migration；
- Service；
- API。


里程碑：

Resource MVP。


---

# Phase 2：Preview能力

时间：

第4-5周


完成：

- PDF；
- MP4；
- MP3。


里程碑：

用户可以在线查看资源。


---

# Phase 3：权限融合

时间：

第6周


完成：

- Capability；
- Consent；
- Audit。


里程碑：

安全闭环。


---

# Phase 4：扩展能力

时间：

第7-8周


完成：

- 分享；
- 插件Demo；
- 测试。


里程碑：

v1.1 RC。


---

# 七、资源配置


## 后端工程师

负责：

- Migration；
- Service；
- API；
- Storage。


---

## 前端工程师

负责：

- 文件列表；
- Preview页面；
- 播放组件。


---

## 测试/DevOps

负责：

- 权限测试；
- 性能测试；
- 发布验证。


---

# 八、最终版本交付标准


v1.1 Final必须满足：

## 用户侧

✅ 可以上传文件

✅ 可以管理文件

✅ 可以在线查看PDF

✅ 可以播放MP4

✅ 可以播放MP3


## 系统侧

✅ 不破坏v1.0数据

✅ 权限统一

✅ Audit完整

✅ API稳定


## 插件侧

✅ 插件可以经过授权访问资源


---

# 九、最终项目判断

经过调整，CampusOS v1.1 最合理定位：

不是：

> 新增文件系统。


而是：

> 将已有 Personal Space 从“用户数据容器”升级为“用户资源管理空间”。

核心路线：

```
v1.0

插件平台


↓

v1.1

资源能力


↓

v1.2

校园业务


↓

v2.0

生态平台
```


该规划能够最大化复用 v1.0 已完成架构，同时避免引入新的权限体系和数据体系，符合长期可维护性要求。
:::

---

这版相比前面的计划，主要进行了三个项目经理视角的修正：

1. **删除 AI 方向，避免版本目标漂移。**
2. **降低 Resource 独立化风险，避免和 Managed Data 冲突。**
3. **把 Preview 作为用户价值核心，而不是把分享、插件生态提前。**

这更接近一个真实软件项目 v1.1 迭代计划。