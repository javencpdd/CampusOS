# AI / Knowledge 双向 SDK 示例

> 更新时间：2026-10-25（Asia/Shanghai）

独立 Container 作者程序，仅依赖公共 Go SDK 和标准库。文本贡献按用户显式 JSON 意图调用单独授权的本机/远程模型或公开集合问答；来源贡献按宿主公开集合绑定分页调用检索与当前引用。消费能力与贡献点分别声明，管理员批准和用户同意分别处理。

运行 `bash scripts/build-v5-ai-knowledge.sh --out <新独立目录>`，将 package-template 的零摘要替换为本机实际 RepoDigest，将 `collection_to_select` 替换为管理员选择的确切公开集合，再使用校园CLI打包、签名和验证。安装、审批、同意、启用分步操作；安装器不执行构建脚本。

文本调用意图为 `mode: "model"` 或 `"answer"`，其余字段见[接入合同](../../docs/api/v1.2插件AI与Knowledge双向接入合同.md)。Profile修订、Binding修订与RunID由可信宿主交互提供，不能设置URL、owner或Token。来源贡献仅重开已授权的公开来源，不读取私人来源、数据库、网络、原始Secret或宿主目录。回显包 `campusos.chat-echo` 仍可独立使用。

更新时间：2026-10-25（Asia/Shanghai）。模型示例明确请求30秒资源期限，须管理员独立批准相同模型能力及资源；只有公开读取批准时部署仍至多5秒，不能借此调用模型。实际期限取更严格限制，Provider每页仍至多5秒；模型Profile自身期限独立执行。
