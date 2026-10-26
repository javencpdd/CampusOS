package projectaudit

func aiProfile(operation string) (operationProfile, bool) {
	p := operationProfile{NoBody: true, AdditionalErrors: []string{"400", "401", "403", "409", "500"}}
	switch operation {
	case "http.ai.profiles":
		p.ResponseSchema = "AIManagedInventory"
	case "http.ai.binding.change":
		p.NoBody, p.RequestSchema, p.ResponseSchema = false, "AIManagedBind", "AIManagedUpdated"
	case "http.ai.profile.state":
		p.NoBody, p.RequestSchema, p.ResponseSchema = false, "AIManagedState", "AIManagedUpdated"
	default:
		return operationProfile{}, false
	}
	return p, true
}

func aiOpenAPISchemas() string {
	return `    AIManagedInventory:
      type: object
      additionalProperties: false
      required: [profiles, bindings, budgets]
      properties:
        profiles: {type: array, maxItems: 2, items: {$ref: '#/components/schemas/AIManagedProfile'}}
        bindings: {type: array, maxItems: 1, items: {$ref: '#/components/schemas/AIManagedBinding'}}
        budgets: {type: array, maxItems: 2, items: {$ref: '#/components/schemas/AIManagedBudget'}}
    AIManagedProfile:
      type: object
      additionalProperties: false
      required: [id, revision, definition, model, enabled, timeout_ms, max_tokens, max_concurrent]
      properties:
        id: {type: string, enum: [remote-chat, local-chat]}
        revision: {type: string, pattern: '^[1-9][0-9]*$'}
        definition: {type: string, enum: [builtin.openai-compatible.text, builtin.local-openai.text]}
        model: {type: string, maxLength: 256}
        enabled: {type: boolean}
        timeout_ms: {type: integer, minimum: 100, maximum: 60000}
        max_tokens: {type: integer, minimum: 1, maximum: 65536}
        max_concurrent: {type: integer, minimum: 1, maximum: 32}
    AIManagedBinding:
      type: object
      additionalProperties: false
      required: [name, profile_id, revision]
      properties:
        name: {type: string, enum: [chat]}
        profile_id: {type: string, enum: [remote-chat, local-chat]}
        revision: {type: string, pattern: '^[1-9][0-9]*$'}
    AIManagedBudget:
      type: object
      additionalProperties: false
      required: [profile_id, quota, charged, reserved]
      description: Durable request units; one charged per dispatched invocation including uncertain outcomes. Token usage is descriptive, not currency pricing.
      properties:
        profile_id: {type: string, enum: [remote-chat, local-chat]}
        quota: {type: integer, minimum: 1, maximum: 1000000000}
        charged: {type: integer, minimum: 0}
        reserved: {type: integer, minimum: 0}
    AIManagedBind:
      type: object
      additionalProperties: false
      required: [profile_id, binding_version, profile_version]
      properties:
        profile_id: {type: string, enum: [remote-chat, local-chat]}
        binding_version: {type: string, pattern: '^[1-9][0-9]*$'}
        profile_version: {type: string, pattern: '^[1-9][0-9]*$'}
    AIManagedState:
      type: object
      additionalProperties: false
      required: [version, enabled]
      properties:
        version: {type: string, pattern: '^[1-9][0-9]*$'}
        enabled: {type: boolean}
    AIManagedUpdated:
      type: object
      additionalProperties: false
      required: [updated]
      properties:
        updated: {type: boolean, enum: [true]}
`
}
