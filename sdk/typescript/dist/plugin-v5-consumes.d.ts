export type PluginV5ConsumeScope = {
    kind: "public";
} | {
    kind: "self";
} | {
    kind: "collection";
    collection_ids: string[];
} | {
    kind: "system_config";
    keys: string[];
};
export interface PluginV5Consume {
    capability: string;
    scope: PluginV5ConsumeScope;
    purpose: string;
    required: boolean;
}
export type PluginV5Consumes = PluginV5Consume[];
export type PluginV5HostCapability = {
    code: string;
} & ({
    scope_kind: "public";
    data_classification: "public";
    consent_required: false;
    available_public_collection_ids?: never;
    available_config_keys?: never;
} | {
    scope_kind: "self";
    data_classification: "public" | "internal" | "sensitive" | "restricted";
    consent_required: true;
    available_public_collection_ids?: never;
    available_config_keys?: never;
} | {
    scope_kind: "collection";
    data_classification: "public";
    consent_required: false;
    available_public_collection_ids: string[];
    available_config_keys?: never;
} | {
    scope_kind: "system_config";
    data_classification: "internal";
    consent_required: false;
    available_config_keys: string[];
    available_public_collection_ids?: never;
});
export interface PluginV5HostCatalog {
    contract: "campusos.host-capabilities/v1";
    host_contract: string;
    items: PluginV5HostCapability[];
}
export type PluginV5ConsumesErrorCode = "plugin.consumes_invalid" | "plugin.host_contract_unsupported" | "plugin.catalog_unavailable" | "plugin.capability_unpublished" | "plugin.scope_unsupported" | "plugin.scope_target_unavailable" | "plugin.duplicate_capability";
