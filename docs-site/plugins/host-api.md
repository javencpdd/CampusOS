# Host API：历史接口边界

> 更新时间：2026-09-23
> 状态：v1-v3 兼容资料；不适用于 v4 新插件。

旧 Host API 曾向受管 Wasm/进程提供短期令牌和固定能力。v4 前端插件不接收 Host Token，也不直接调用内部地址、数据库或个人空间路径；它通过宿主校验的 Bridge 请求资源描述、Range 内容、配置或允许的记录能力。

新插件请以[三层授权](/plugins/authorization-v3)、[隔离 UI 与 Bridge](/plugins/frontend-runtime)和[PDF Viewer 教程](/plugins/pdf-viewer-tutorial)为准。历史插件的具体合同以对应版本的仓库 API 文档为准。
