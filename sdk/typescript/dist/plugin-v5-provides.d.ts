export type PluginV5DataMode = "authorized_resource" | "public_only" | "no_host_private_data";
export interface PluginV5SchemaFileRef {
    path: string;
    digest: string;
}
export interface PluginV5Provide {
    id: string;
    point: "preview.renderer" | "ai.chat" | "knowledge.source";
    contract_version: "v1";
    input_schema: string;
    output_schema: string;
    capabilities: string[];
    data_mode: PluginV5DataMode;
    config_schema?: PluginV5SchemaFileRef;
}
export interface PluginV5ExtensionPointDescriptor {
    point: PluginV5Provide["point"];
    contract_version: "v1";
    input_schema: string;
    output_schema: string;
    capabilities: string[];
    data_modes: PluginV5DataMode[];
}
export interface PluginV5ExtensionCatalog {
    contract: "campusos.extension-catalog/v1";
    host_contract: string;
    points: PluginV5ExtensionPointDescriptor[];
}
export type PluginV5ProvidesErrorCode = "plugin.provides_invalid" | "plugin.extension_catalog_unavailable" | "plugin.extension_point_unsupported" | "plugin.extension_schema_mismatch" | "plugin.extension_capability_unsupported" | "plugin.extension_data_mode_unsupported" | "plugin.contribution_shape_mismatch" | "plugin.duplicate_contribution";
export interface PluginV5ExtensionCallBase {
    request_id: string;
    contribution_id: string;
    instance_generation: number;
    deadline_ms: number;
}
export interface PluginV5ExtensionResultBase {
    request_id: string;
    contribution_id: string;
    instance_generation: number;
}
export interface PluginV5PreviewRequest extends PluginV5ExtensionCallBase {
    resource_ref: string;
    mime_type: "application/pdf";
}
export interface PluginV5PreviewResponse extends PluginV5ExtensionResultBase {
    ui_slot_id: string;
    resource_revision: string;
}
export interface PluginV5ChatRequest extends PluginV5ExtensionCallBase {
    prompt: string;
    context_class: "public" | "none";
}
export interface PluginV5ChatResponse extends PluginV5ExtensionResultBase {
    text: string;
    input_tokens: number;
    output_tokens: number;
    finish_reason: "completed" | "length";
}
export interface PluginV5KnowledgeSourceRequest extends PluginV5ExtensionCallBase {
    binding_id: string;
    cursor?: string;
    limit: number;
}
export interface PluginV5KnowledgeSourceItem {
    external_id: string;
    revision: string;
    title: string;
    text: string;
    source_url?: string;
}
export interface PluginV5KnowledgeSourceResponse extends PluginV5ExtensionResultBase {
    items: PluginV5KnowledgeSourceItem[];
    next_cursor?: string;
}
