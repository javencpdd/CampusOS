# 插件中心、受管数据与签名

> 更新时间：2026-09-21 21:22（Asia/Shanghai）

当前本地插件市场不是任意代码下载器。管理员先安装和审核 External Plugin，再决定是否发布到用户目录；用户只能查看、添加、授权、撤销和管理自己的数据。

## 可信市场申请（v1.1）

“已安装插件目录”与“可信插件市场”是两条不同流程：前者只列出当前已经被平台校验、安装并发布的外部插件；后者只提供尚未安装插件的候选元数据。

1. 管理员在 3001 的“用户目录与授权”配置市场 ID、HTTPS 目录 URL 与 Ed25519 公钥，并启用该来源。
2. 用户在 3000 的插件中心只能从已启用来源检索，按市场返回的插件 ID 提交申请；不能填写任意市场地址、下载 URL 或包地址。
3. 平台验证目录响应签名，固定来源 Host，并在申请中保存来源、插件 ID、版本、发布者与链接快照。
4. 管理员据快照批准或拒绝。批准不下载、不安装、不执行代码；仍必须在“外部插件运行管理”重新预检查、验签并导入包。
5. 导入后的插件需要发布到用户目录；用户再点击“添加”并按声明完成个人授权。没有任何可信来源时，用户无法提交外部市场申请。

可信市场来源是平台治理数据而非插件私有表。已经有申请记录的来源会保留审计证据，管理员应停用而非删除。

## 两层权限

- `permissions.api`：插件进程请求 Host API 的系统权限，由管理员审核。
- `permissions.user`：个人记录和文件的用户授权，必须写明用途、风险和是否可撤销。

用户授权不会把数据库、CampusOS Token 或宿主机目录交给插件。用户归属集合只能通过用户 JWT 调用受管 REST API；扩展进程的 Host API v2 只可访问声明为 `owner: system` 的集合。

## V2 Manifest

```yaml
api_version: campusos.plugin/v2
host_api_version: v2
type: external
managed_data:
  collections:
    - name: notes
      owner: user
      fields:
        - name: title
          type: string
          required: true
permissions:
  user:
    - resource: managed_data
      actions: [read, write, delete]
      purpose: 保存个人笔记
      risk: low
      revocable: true
    - resource: plugin_search
      actions: [read]
      purpose: 允许 CampusOS 检索已声明的笔记字段
      risk: medium
      revocable: true
```

字段、搜索、过滤、数量和大小限制都必须先写进 Manifest。更新与删除使用乐观锁版本号，避免多端覆盖。

搜索需要 `managed_data:read` 和单独的 `plugin_search:read` 用户同意。管理员未发布目录、用户撤销 Grant 或插件版本变化时，普通数据调用都会被拒绝；插件下架后，用户仍可导出或删除自己的保留数据。

## 数据位置

```text
data/plugins/<plugin-id>/                         # 实现
data/plugin_data/<plugin-id>/                     # 插件私有运行数据、快照
data/personal-space/<user-id>/plugins/<plugin-id>/ # 用户附件
data/resources/                                   # 主题、Skills、Prompt、Persona 等资源包
```

## 包签名

签名使用 Ed25519，签的是包内文件的确定性内容摘要，而不是把私钥放入插件包：

```bash
go run ./cmd/campusosctl plugin sign <plugin-dir> --key-id organization-key --key-file /secure/private-key.txt
go run ./cmd/campusosctl plugin pack <plugin-dir>
```

CampusOS 从 `data/config/plugin-trust-keys.json` 读取受信任公钥。设置 `release.signature_required: true` 的插件必须验签通过才能导入。

导入 v2 外部插件后，Catalog 与发布记录会即时同步。管理表单不能手工写入 `verified`，该状态只来源于宿主实际验签。用户中心提供个人记录/文件占用和推荐申请，管理端提供 Runtime 健康、权限摘要与市场审计。

更多字段和方法请看 [Host API 与权限](/plugins/host-api) 与仓库中的 `docs/api/Host-API-v2受管数据合同.md`。
