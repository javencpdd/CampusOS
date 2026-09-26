export interface PluginV5ConfigField {
    type: "string" | "integer" | "number" | "boolean";
    title: string;
    enum?: Array<string | number | boolean>;
    minimum?: number;
    maximum?: number;
    minLength?: number;
    maxLength?: number;
}
export interface PluginV5ConfigDefinition {
    contract: "campusos.plugin-config/v1";
    version: string;
    schema: {
        type: "object";
        additionalProperties: false;
        properties: Record<string, PluginV5ConfigField>;
        required: string[];
    };
    defaults: Record<string, unknown>;
    selectors: Array<{
        field: string;
        kind: "profile" | "public_collection";
    }>;
    secret_names: string[];
}
export interface PluginV5ConfigUpdate {
    expected_revision: number;
    values: Record<string, unknown>;
    secret_refs: Record<string, string>;
}
export interface PluginV5ConfigDecisionInput {
    definition: PluginV5ConfigDefinition;
    update: PluginV5ConfigUpdate;
    current_revision: number;
    available_profiles: string[];
    available_public_collections: string[];
}
export type PluginV5ConfigErrorCode = "plugin.config_invalid" | "plugin.config_schema_unsupported" | "plugin.config_defaults_invalid" | "plugin.config_revision_conflict" | "plugin.config_selector_forbidden" | "plugin.secret_ref_invalid";
