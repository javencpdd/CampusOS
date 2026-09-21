# CampusOS PDF Viewer v4 源包

> 更新时间：2026-09-21（Asia/Shanghai）
> 状态：迁出准备骨架；当前生产/开发运行的 PDF 页面仍在旧 Web 模块，不能把本目录视为已安装或可运行插件。

本包预留用户预览页面、管理员配置页面、配置 schema/defaults、测试和未来 PDF.js Worker 构建产物。
R11-02 的跨 Origin iframe、Worker 和 Range 原型通过前，不复制或删除旧页面；R11-09 才迁移实际实现并移除宿主静态 import。

包不含密码、Token、平台路径、数据库连接或可执行安装脚本。用户实际配置未来由宿主创建在
`data/personal-space/<user-id>/plugins/campusos.pdf-viewer/config/`，不由本包直接访问。
