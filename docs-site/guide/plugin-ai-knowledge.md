# 插件的 AI 与公开知识能力

> 更新时间：2026-10-25（Asia/Shanghai）。V12-14c 进度和原始验收见仓库 `docs/进度/v1.2-dev/v1.2.158-dev.md`。

插件可以消费受管模型、检索和公开问答，也可以把文本或公开来源贡献给内置功能。两个方向各自声明和批准；安装一个 Provider 不会授予模型或数据读取权限。内置[受管模型](./managed-ai.md)和[有据问答](./cited-rag.md)在没有插件时继续可用。

管理员在插件管理页分别审批本机模型 `ai.chat.local.invoke`、远程模型 `ai.chat.remote.invoke` 和公开集合 `knowledge.public.answer`。本机批准不能替代远程批准，公开问答还须取得对应模型许可。集合只允许所选公开 ID，来源下架或变为私密后立即拒绝。重新审批会停用全局安装，核对后须显式启用。

模型能力还需要用户在插件中心独立同意当前版本和用途。管理员批准、用户安装、用户同意和启用是不同操作；审批变化后须重新同意。私人索引同意、私人问答核对单均不授权插件读取私人来源。撤销会话、同意、Grant或Feature会取消在途调用并丢弃答案，配额和超时继续受宿主限制。

作者示例位于仓库 `plugins/campusos.ai-knowledge/`。先在独立目录运行 `scripts/build-v5-ai-knowledge.sh`，按真实镜像RepoDigest和选定公开集合构造签名包，再分步安装和审批。脚本只依赖公共Go SDK与标准库，不在安装期间构建或修改宿主。

Go SDK提供 `ModelChat`、`KnowledgeAnswer` 和可处理文本/来源两种贡献的 `ServeV5ContainerExtensionsWithHost`；TypeScript提供模型/公开问答DTO和helper。当前修订可由可信宿主的模型核对状态提供，插件不接收User/Admin Token，也不能设置提供方URL、Secret、owner或私人来源。详细字段与最小代码示例见仓库 `docs/api/v1.2插件AI与Knowledge双向接入合同.md`。

公开来源 Provider 本版本只返回宿主当前公开集合的来源引用，宿主逐项核验真实标题、正文和修订，使用自己的URL；Provider不能把任意外部正文声明为已授权公开来源。外部网站采集与WeKnora适配仍列在后续P2范围。模型结果按纯文本处理，不执行工具或模型URL。
