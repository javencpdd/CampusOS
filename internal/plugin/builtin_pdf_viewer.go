package plugin

// PDFViewerPluginName is a stable first-party Plugin Manager identity. The
// renderer itself remains a compiled trusted Web module; this manifest only
// declares lifecycle, UI and least-privilege authorization facts.
const PDFViewerPluginName = "builtin.pdf-viewer"

func NewPDFViewerBuiltinManifest() *Manifest {
	return &Manifest{
		APIVersion:     ManifestAPIVersionV3,
		HostAPIVersion: HostAPIVersionV3,
		Name:           PDFViewerPluginName,
		DisplayName:    "PDF 文档预览",
		// Capability declarations are part of the immutable authorization
		// identity. Bump this version whenever they change so a persisted
		// development database receives a fresh, reviewable declaration set
		// instead of retaining the previous version's empty/stale rows.
		Version:     "1.1.3-dev",
		Description: "CampusOS 随 Web 客户端交付的受信任 PDF 阅读器；只经宿主认证读取当前允许预览的 PDF，并可在用户同意后保存最近阅读页。",
		Author:      "CampusOS",
		Runtime:     "builtin",
		Scope:       ScopeSystem,
		Type:        PluginTypeBuiltin,
		CapabilityDeclarations: []CapabilityRequest{
			{Code: "article_attachment.self.preview", Required: true, Purpose: "在当前登录用户明确点击时预览其有权阅读的图文文章 PDF 附件。", Scope: "self"},
			{Code: "personal_space_file.self.read", Required: true, Purpose: "在当前登录用户明确点击时预览其个人空间中的 PDF 文件。", Scope: "self"},
			{Code: "plugin_ui.surface.open", Required: true, Purpose: "仅由当前用户点击在受宿主控制的弹窗、全屏或同源新标签页中打开 PDF 阅读界面。", Scope: "self"},
			{Code: "plugin_record.self.read", Required: false, Purpose: "在当前用户同意后读取其本人 PDF 的最近阅读页；不读取文件内容或其他用户记录。", Scope: "self"},
			{Code: "plugin_record.self.write", Required: false, Purpose: "在当前用户同意后保存其本人 PDF 的最近阅读页；不写入文件内容或其他用户记录。", Scope: "self"},
		},
		ManagedData: ManagedDataConfig{DefaultQuotaByte: 64 * 1024, Collections: []DataCollection{{
			Name: "reading_positions", Owner: OwnerUser, MaxRecords: 200, MaxRecordByte: 256,
			Fields: []DataField{{Name: "page", Type: "number", Required: true}},
		}}},
		Lifecycle: LifecycleConfig{Backend: BackendLifecycleConfig{ActivationMode: ActivationHot}, Frontend: FrontendLifecycleConfig{ActivationMode: ActivationHot}},
		UI: UIContribution{ContractVersion: CurrentUIContract, Responsive: ResponsiveUI{SupportedViewports: []string{"mobile", "tablet", "desktop"}, MobileBehavior: "responsive", OverflowPolicy: "internal-only"},
			Surfaces: []UISurface{{ID: "builtin.pdf-viewer.preview", Version: "v1", Type: "document-preview", LayoutRole: "overlay", Renderer: "trusted-module", ModuleID: "core.pdf-viewer", Presentations: []string{"modal", "drawer", "fullscreen", "new-tab"}, Regions: []string{"toolbar", "document"}}},
			Routes:   []UIRoute{{ID: "builtin.pdf-viewer.route.preview", Path: "/extensions/builtin.pdf-viewer/preview", SurfaceID: "builtin.pdf-viewer.preview", Title: "PDF 预览", RequiresAuth: true}},
		},
		Experience: ExperienceConfig{UseCases: []string{"预览自己个人空间中的 PDF", "预览自己个人文档中的 PDF", "预览有权阅读的文章 PDF 附件"}, DataUse: "不接触服务器路径、Object ID 或长期下载链接；每次读取均由 CampusOS API 重新验证。用户额外同意后，仅在现有 plugin_records 中保存自己的最近阅读页。", RiskSummary: "可在插件中心撤销个人授权；管理员可停用插件或撤销能力。", DisabledBehavior: "预览入口会安全降级为原有认证下载，不会公开文件。", Maintainer: "CampusOS"},
	}
}
