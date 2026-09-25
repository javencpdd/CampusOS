# 三层授权与资源访问

> 更新时间：2026-09-23
> 适用范围：`campusos.plugin/v4`、`campusos.ui/v3`、`campusos.bridge/v1`

CampusOS 不要求用户生成 API Key、Token 或插件密码来授权。外部插件运行在受宿主控制的隔离界面中；每一次敏感资源读取都由宿主重新判断，插件不会拿到用户 JWT、数据库凭据、真实文件路径或宿主目录权限。

## 决策路径

一项调用只有同时满足下列条件才会放行：

```text
Manifest 声明
∩ 管理员对当前版本的 Grant
∩ 用户对本人数据的 Consent（需要时）
∩ 当前插件版本、摘要与已启用状态
∩ self 范围、资源访问规则与系统策略
```

任一条件不满足即拒绝。版本、能力、用途或风险变化后，旧授权不会自动扩大；撤销后下一次调用即失效。

## 谁在什么位置授权

| 角色 | 入口 | 能做什么 |
| --- | --- | --- |
| 管理员 | 3001 管理端 → 外部插件 → 精细授权 | 审查当前发布版本声明的能力，按能力授予、拒绝或撤销；填写治理原因。 |
| 用户 | 3000 用户端 → 插件中心 → 我的插件/精细授权 | 添加已发布插件，并仅对自己的个人数据逐项同意或撤销。管理员未授予的能力不能由用户开启。 |
| 宿主 | API 与 Plugin UI Gateway | 验证发布版本、iframe 会话、调用范围及实际资源 ACL，再签发一次性的受限读取。 |

`PDF 文档预览`是可验证的例子：文章附件预览需要文章读者本来就有的阅读权；个人空间 PDF 仅允许文件所有者预览。插件只能通过 Bridge 读取一个已获授权资源的描述和最多 1 MiB 的 Range 片段，不能列目录或猜测其他文件。

## 隔离界面为何不需要 Token

用户界面加载自 Plugin UI Gateway（开发环境默认 3003），随后与父页面建立一次受校验的 `MessagePort`。宿主检查来源、插件 key/version、surface、受众和随机 challenge，再仅开放 Bridge 方法：

`config.read`、`config.update`、`resource.describe`、`resource.readRange`、`backend.invoke`、`ui.requestSurface`、`records.read`、`records.write`。

这不是把登录令牌交给 iframe。页面直接访问 API、存储 Cookie/JWT、拼接 `api` 容器名或读取宿主文件都会被拒绝或失败。

## Secret 与个人配置

系统 Secret 由宿主加密保存，后台仅显示掩码；同名保存会轮换旧值。用户配置不是 Secret 的替代品：添加插件时，宿主在用户空间创建受控的 `plugins/<key>/config/user.json`，并通过带版本的 Bridge 读写，不能由插件自行选择磁盘路径。

进一步阅读：[v4 Manifest 与配置](/plugins/manifest)、[隔离 UI 与 Bridge](/plugins/frontend-runtime)、[PDF Viewer 实战](/plugins/pdf-viewer-tutorial)。
