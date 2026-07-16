# 以课表为例编写 CampusOS 插件

本教程用“课表”解释 CampusOS 最容易混淆的两类扩展：现有 **个人课表 Built-in Feature** 和可独立安装的 **课表助手 External Plugin**。配套可运行示例位于：

```text
examples/plugins/schedule-helper/
```

## 1. 先选择正确的扩展类型

| 需求 | 应选择的类型 | 原因 |
| --- | --- | --- |
| 修改内置课表的学期、日历或 Excel 解析 | Built-in Feature | 需要修改 `internal/modules/features/schedule` 并随 CampusOS 编译发布 |
| 增加可独立安装的课程提醒、课程评价或导出适配 | External Plugin | 可单独安装、升级、停用和卸载 |
| 只改变课表页面颜色、字体、背景和间距 | Resource Package | 没有业务 Runtime，不应伪装成插件 |

不要把第三方 Go 代码声明为 `runtime: builtin` 来获得内部访问。Built-in 不是动态加载任意进程内代码的机制。

## 2. 读懂现有个人课表

现有课表实现由以下部分组成：

```text
internal/modules/features/schedule/            业务、导入、日历和 HTTP Handler
modules/features/personal-schedule/             campusos.module/v1 描述符与说明
data/personal-space/<user>/file/schedule/       每个用户自己的学期 JSON
```

模块 ID 是 `feature.personal-schedule`，显式依赖 Identity、User Storage 和 Feature Registry。Admin 的 `/features` 管理功能状态和公共配置；用户在 `/schedule` 管理自己的私有课表。

如果你的需求必须改变这些既有语义，应修改 Built-in Feature、补 Go/Web 测试并重启 API。它不是一个可在 Admin 中卸载的 External Plugin。

## 3. 教程插件要解决什么

`schedule-helper` 只演示独立插件边界：

- 用户主动创建 `terms` 和 `courses` 记录。
- CampusOS 按插件、用户和集合隔离数据。
- 用户明确授权后，浏览器通过受管 REST API 读写自己的记录。
- 插件进程只提供健康和 Extension 端点，不接收用户 ID、JWT、数据库连接或物理目录。
- 它不会自动同步 `internal/modules/features/schedule` 的课表；当前没有向 External Plugin 发布稳定的个人课表 Host API。

最后一点是安全边界，不是遗漏。未来若开放 `ScheduleQuery`，应增加正式权限、裁剪后的 DTO、用户同意和负向测试，而不是让插件读取 `data/personal-space`。

## 4. 查看示例目录

```text
examples/plugins/schedule-helper/
├── plugin.yaml
├── README.md
├── go.mod
├── main.go
└── main_test.go
```

需要在 `data/plugins` 中进行本地安装式开发时，可以复制示例：

```bash
cp -R examples/plugins/schedule-helper data/plugins/schedule-helper
```

插件实现放在 `data/plugins/schedule-helper/`；运行数据由宿主保存，不要在代码目录创建用户数据库或课表 JSON。

## 5. Manifest v2 的关键部分

完整文件见 `examples/plugins/schedule-helper/plugin.yaml`。

### 身份和 Runtime

```yaml
api_version: campusos.plugin/v2
host_api_version: v2
name: schedule-helper
version: 0.1.0
runtime: grpc
scope: user
type: external
```

当前 `runtime: grpc` 是历史兼容名称：CampusOS 管理一个外部进程，并通过已校验的 loopback HTTP Extension 端点通信。它不是标准 protobuf gRPC。

### 用户授权

```yaml
permissions:
  api: []
  user:
    - resource: managed_data
      actions: [read, write, delete]
      purpose: 保存和管理当前用户主动录入的学期与课程。
      risk: low
      revocable: true
    - resource: plugin_search
      actions: [read]
      purpose: 只在当前用户自己的课程名称、教师和地点中检索。
      risk: medium
      revocable: true
```

`permissions.api` 是管理员审核的插件进程 Host API 权限；`permissions.user` 是普通用户的明确同意。示例进程不需要系统数据，因此 `api` 保持为空。

### 受管集合

```yaml
managed_data:
  collections:
    - name: terms
      owner: user
      fields:
        - name: term_key
          type: string
          required: true
        - name: first_week_start
          type: string
          required: true
      filterable: [term_key]
    - name: courses
      owner: user
      fields:
        - name: term_key
          type: string
          required: true
        - name: name
          type: string
          required: true
        - name: weekday
          type: number
          required: true
        - name: weeks
          type: array
          required: true
      searchable: [name]
      filterable: [term_key, name, weekday]
```

字段、搜索、过滤、记录数量和单条大小都必须先声明。未声明字段不能被偷偷写入或作为筛选条件。

## 6. 编写受管进程

`main.go` 只监听本机地址：

```go
address := "127.0.0.1:19092"
http.ListenAndServe(address, newServer())
```

Manifest 与进程必须使用同一个 Extension 地址：

```yaml
config:
  command: ./plugin
  extension_url: http://127.0.0.1:19092/extension
```

不要监听公网地址，也不要让插件自行接收 CampusOS 用户 token。Extension Gateway 会处理登录主体、Trace ID、超时、请求大小和审计。

## 7. 本地测试、构建和预检

在仓库根目录执行：

```bash
GOCACHE=/tmp/campusos-go-cache go test ./examples/plugins/schedule-helper/... -count=1
go run ./cmd/campusosctl plugin inspect examples/plugins/schedule-helper
go run ./cmd/campusosctl plugin dev examples/plugins/schedule-helper
```

`plugin dev` 依次运行插件测试、构建 `plugin` 可执行文件并验证 Manifest、权限、Runtime 产物和路径安全。

机器可读预检：

```bash
go run ./cmd/campusosctl plugin verify examples/plugins/schedule-helper --json
```

## 8. 打包和导入

```bash
go run ./cmd/campusosctl plugin pack \
  examples/plugins/schedule-helper \
  --out /tmp/schedule-helper-0.1.0.campusos-plugin.tar.gz
```

在 Admin 中完成：

1. 打开“扩展与集成 -> 外部插件”。
2. 选择插件包并执行安全预检。
3. 核对 Runtime、权限、版本、checksum 和签名状态。
4. 确认导入并启动插件。
5. 打开“插件中心”，将本地 Catalog 条目设为 `published`。

导入不会自动把第三方插件发布给所有用户；发布也不会绕过每个用户的授权。

## 9. 用户授权和写入课表记录

用户在 `http://localhost:3000/plugins` 查看用途和风险，勾选权限后授权。也可以用 API 验证，以下 `<user-token>` 必须是普通用户自己的 Access Token：

```bash
curl -X POST http://localhost:8080/api/v1/plugin-market/schedule-helper/enable \
  -H 'Authorization: Bearer <user-token>' \
  -H 'Content-Type: application/json' \
  -d '{"permissions":["managed_data:read","managed_data:write","managed_data:delete","plugin_search:read"]}'
```

创建 2026 秋季学期：

```bash
curl -X POST http://localhost:8080/api/v1/plugin-market/schedule-helper/records/terms \
  -H 'Authorization: Bearer <user-token>' \
  -H 'Content-Type: application/json' \
  -d '{
    "record_key":"term-2026-fall",
    "data":{
      "term_key":"2026-fall",
      "title":"2026 年秋季学期",
      "year":2026,
      "semester":"fall",
      "first_week_start":"2026-09-07"
    }
  }'
```

创建课程：

```bash
curl -X POST http://localhost:8080/api/v1/plugin-market/schedule-helper/records/courses \
  -H 'Authorization: Bearer <user-token>' \
  -H 'Content-Type: application/json' \
  -d '{
    "record_key":"course-distributed-systems",
    "data":{
      "term_key":"2026-fall",
      "name":"分布式系统",
      "teacher":"张老师",
      "weekday":3,
      "start_period":3,
      "end_period":4,
      "weeks":[1,2,3,4,5,6,7,8],
      "location":"教学楼 A-302"
    }
  }'
```

按学期读取：

```bash
curl 'http://localhost:8080/api/v1/plugin-market/schedule-helper/records/courses?filter.term_key=2026-fall&page=1&page_size=20' \
  -H 'Authorization: Bearer <user-token>'
```

检索课程：

```bash
curl 'http://localhost:8080/api/v1/plugin-market/search?plugin=schedule-helper&collection=courses&q=分布式' \
  -H 'Authorization: Bearer <user-token>'
```

更新和删除必须携带服务端返回的 `version`，版本冲突返回 `409`，防止两个页面相互覆盖。

## 10. 数据与停用语义

| 操作 | 代码 | 用户记录 | 用户授权 |
| --- | --- | --- | --- |
| 停止 Runtime | 保留 | 保留 | 保留 |
| 从 Catalog 下架 | 保留 | 保留，可导出/删除 | 新用户不能授权 |
| 用户撤销授权 | 保留 | 保留，可导出/删除 | 立即失效 |
| 卸载插件 | 按管理员操作处理 | 默认不自动删除 | 不应扩大或迁移到其他插件 |

用户数据由 PostgreSQL 的受管记录和宿主命名空间保存，不进入插件代码包。若以后增加附件，必须声明 `files`，文件才会进入：

```text
data/personal-space/<user-id>/plugins/schedule-helper/
```

## 11. 增加前端界面的正确方式

第三方插件优先使用 `campusos.ui/v1` 声明式 Surface、Route、Navigation 和 Action。它只能组合 Campus UI 白名单组件；不能加载任意同源 Vue 模块或脚本。

课表周视图属于复杂交互。如果需要完整编辑器，有两种安全路线：

1. 先用声明式表单和受管记录 API完成课程增删改查。
2. 将通用课表 Renderer 作为经过 Core 审核的 Provider/受信任模块纳入后续 CampusOS 版本，再由插件声明使用。

不要在插件包里注入任意 HTML/JavaScript 来获取主页面 DOM、LocalStorage 或 JWT。

## 12. 测试清单

- Manifest v2 可以被 `inspect`、`verify` 和 `pack` 解析。
- 进程只监听 loopback，`GET /health` 和 `POST /extension` 有测试。
- 未发布 Catalog 时普通用户看不到插件。
- 未授权、撤销授权和版本变化后，用户记录 API 默认拒绝。
- 用户 A 不能读取用户 B 的课程。
- 未声明字段、筛选项、超限记录和错误 MIME 被拒绝。
- 更新/删除缺少 `version` 或版本过期时返回冲突。
- 停用或撤销授权不删除数据，导出与显式删除仍可用。
- 插件不导入 CampusOS `internal/*`，不连接数据库，不读取 JWT 私钥或个人空间物理路径。

完成插件改动后至少运行：

```bash
go run ./cmd/campusosctl plugin dev examples/plugins/schedule-helper
GOCACHE=/tmp/campusos-go-cache go test ./internal/plugin/... -count=1
make architecture-check
make docs-links
```

## 13. 常见错误

**把 `personal-schedule` 当作普通外部插件导入**  
它是 Built-in Feature，`module.yaml` 是当前权威描述符，不能通过插件导入流程安装。

**插件直接读取 `data/personal-space/<user>/file/schedule`**  
这会绕过用户授权、路径安全和 Storage Provider，属于禁止行为。

**把 `runtime: grpc` 理解为标准 gRPC**  
当前实现是受管进程加 loopback HTTP；标准 protobuf 协议仍是后续版本工作。

**认为 `scope: user` 表示用户可以自行安装代码**  
安装和发布仍由管理员治理；`scope` 描述管理级别，不替代生命周期和权限。

**把主题 CSS 或课表皮肤放进插件实现目录**  
纯视觉内容应使用 Resource Package，保存到 `data/resources` 或兼容 `data/plugin_data` 资源目录。

下一步可阅读 [Manifest 与配置](/plugins/manifest)、[Host API 与权限](/plugins/host-api) 和 [打包、导入与更新](/plugins/package-import)。
