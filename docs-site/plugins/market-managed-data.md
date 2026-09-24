# 可信插件市场、目录与用户数据

> 更新时间：2026-09-23

“插件中心”和“外部插件运行管理”是两个相邻但不同的治理面：前者面向用户可添加的已发布插件和可信市场申请；后者面向管理员的包校验、安装、运行状态、发布及逐项授权。二者不应各自维护一份插件实现或数据副本。

## 可信市场申请

1. 管理员配置市场 ID、HTTPS 目录地址和 Ed25519 公钥，并启用该来源。
2. 用户只能从已启用来源选择市场返回的插件 ID 后提交申请；不能填写任意 URL、下载地址或包地址。
3. 平台验证目录签名并保存来源、插件 ID、版本、发布者和链接快照。
4. 管理员批准申请仅代表允许继续审核；不会下载、安装或执行代码。
5. 管理员仍要在外部插件运行管理中预检、验签、安装并发布，用户才会在插件中心看到该插件。

没有白名单来源时，用户无法提交外部市场申请。这一流程避免把“申请”误解成用户端远程代码执行。

## 目录与数据边界

```text
plugins/<key>/                                      # 开发源码：Manifest、前端、Schema、文档
plugins/.installed/<key>/<version>/                 # 宿主校验后的不可变发布包
plugins/.staging/                                   # 短暂预检，不执行
data/personal-space/<user-id>/plugins/<key>/
  config/user.json                                  # 用户配置，受宿主版本控制
  config/meta.json
  .pending/  .snapshots/                            # 宿主维护的写入恢复材料
data/plugins/、data/plugin_data/                    # v1-v3 历史兼容数据，不是 v4 源码入口
```

用户点击“添加”后，宿主才按数字用户 ID 与插件 key 创建上述目录。插件 iframe 不可枚举个人空间、指定替代路径或直接读取这些文件；它只能通过 Bridge 的 `config.*` 和获得授权的 `resource.*` 方法访问内容。

## PDF Viewer 示例

`campusos.pdf-viewer` 不创建插件专用平台数据库表。它以 `runtime: none` 的隔离 UI 形式读取宿主已授权的 PDF Range，最近阅读位置等可选个人状态走用户配置/受控记录能力。文章附件使用文章阅读 ACL；个人文件只能由所有者预览。

进一步阅读：[三层授权](/plugins/authorization-v3)、[打包与安装](/plugins/package-import)、[PDF Viewer 教程](/plugins/pdf-viewer-tutorial)。
