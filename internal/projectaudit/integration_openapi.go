package projectaudit

func integrationOpenAPISchemas() string {
	return `    IntegrationCreateRequest:
      type: object
      additionalProperties: false
      required: [name]
      properties:
        name: {type: string, minLength: 1, maxLength: 120}
    IntegrationUpdateRequest:
      type: object
      additionalProperties: false
      required: [name, status, version]
      properties:
        name: {type: string, minLength: 1, maxLength: 120}
        status: {type: string, enum: [active, suspended, revoked]}
        version: {type: string, pattern: '^[1-9][0-9]*$'}
    IntegrationVersionRequest:
      type: object
      additionalProperties: false
      required: [version]
      properties:
        version: {type: string, pattern: '^[1-9][0-9]*$'}
    IntegrationRule:
      type: object
      additionalProperties: false
      required: [id, channel, endpoint, action, resource_kind, resource_ids, status, not_before_ms, expires_at_ms]
      properties:
        id: {type: string, minLength: 1, maxLength: 128}
        channel: {type: string, enum: [integration_http, integration_mcp]}
        endpoint: {type: string, enum: [public_thread.read, public_thread_read]}
        action: {type: string, enum: [community.thread.read]}
        resource_kind: {type: string, enum: [community.thread]}
        resource_ids: {type: array, minItems: 1, maxItems: 32, uniqueItems: true, items: {type: string, minLength: 1, maxLength: 128, pattern: '^[A-Za-z0-9][A-Za-z0-9._:-]*$'}}
        status: {type: string, enum: [active, suspended, revoked]}
        not_before_ms: {type: integer, minimum: 0, maximum: 9007199254740991}
        expires_at_ms: {type: integer, minimum: 1, maximum: 9007199254740991}
    IntegrationIssueRequest:
      type: object
      additionalProperties: false
      required: [principal_version, not_before_ms, expires_at_ms, rules]
      properties:
        principal_version: {type: string, pattern: '^[1-9][0-9]*$'}
        not_before_ms: {type: integer, minimum: 0, maximum: 9007199254740991}
        expires_at_ms: {type: integer, minimum: 1, maximum: 9007199254740991, description: 'Must be future and no more than 90 days from issuance.'}
        rules: {type: array, maxItems: 64, items: {$ref: '#/components/schemas/IntegrationRule'}}
    IntegrationRotateRequest:
      type: object
      additionalProperties: false
      required: [principal_version, version, not_before_ms, expires_at_ms, rules]
      properties:
        principal_version: {type: string, pattern: '^[1-9][0-9]*$'}
        version: {type: string, pattern: '^[1-9][0-9]*$'}
        not_before_ms: {type: integer, minimum: 0, maximum: 9007199254740991}
        expires_at_ms: {type: integer, minimum: 1, maximum: 9007199254740991}
        rules: {type: array, maxItems: 64, items: {$ref: '#/components/schemas/IntegrationRule'}}
    IntegrationScopeRequest:
      type: object
      additionalProperties: false
      required: [version, rules]
      properties:
        version: {type: string, pattern: '^[1-9][0-9]*$'}
        rules: {type: array, maxItems: 64, items: {$ref: '#/components/schemas/IntegrationRule'}}
    IntegrationPrincipal:
      type: object
      additionalProperties: false
      required: [id, name, status, version, created_by_admin_id, created_at, updated_at]
      properties:
        id: {type: string}
        name: {type: string}
        status: {type: string, enum: [active, suspended, revoked]}
        version: {type: string}
        created_by_admin_id: {type: string}
        created_at: {type: string, format: date-time}
        updated_at: {type: string, format: date-time}
    IntegrationCredential:
      type: object
      additionalProperties: false
      required: [id, principal_id, prefix, purpose, status, rules, not_before_ms, expires_at_ms, version, created_by_admin_id, created_at, updated_at]
      properties:
        id: {type: string}
        principal_id: {type: string}
        prefix: {type: string, pattern: '^cint1_[a-f0-9]{16}$'}
        purpose: {type: string, enum: [integration_api]}
        status: {type: string, enum: [active, revoked]}
        rules: {type: array, items: {$ref: '#/components/schemas/IntegrationRule'}}
        not_before_ms: {type: integer}
        expires_at_ms: {type: integer}
        version: {type: string}
        created_by_admin_id: {type: string}
        created_at: {type: string, format: date-time}
        updated_at: {type: string, format: date-time}
    IntegrationInventory:
      type: object
      additionalProperties: false
      required: [principals, credentials, next_cursor]
      properties:
        principals: {type: array, items: {$ref: '#/components/schemas/IntegrationPrincipal'}}
        credentials: {type: array, items: {$ref: '#/components/schemas/IntegrationCredential'}}
        next_cursor: {type: string}
    IntegrationIssueResponse:
      type: object
      additionalProperties: false
      required: [credential, token]
      properties:
        credential: {$ref: '#/components/schemas/IntegrationCredential'}
        token: {type: string, readOnly: true, description: 'One-time response only. Never returned by inventory/self APIs or persisted server-side.'}
    IntegrationEndpointInventory:
      type: array
      items:
        type: object
        additionalProperties: false
        required: [channel, id, action, resource_kind, enabled, integration_allowed]
        properties:
          channel: {type: string}
          id: {type: string}
          action: {type: string}
          resource_kind: {type: string}
          enabled: {type: boolean}
          integration_allowed: {type: boolean}
    IntegrationCurrentSelf:
      type: object
      additionalProperties: false
      required: [principal, name, prefix, expires_at_ms]
      properties:
        principal:
          type: object
          additionalProperties: false
          required: [contract, actor, audience, authentication_strength, credential_id]
          properties:
            contract: {type: string, enum: [campusos.principal/v1]}
            actor:
              type: object
              additionalProperties: false
              required: [kind, id]
              properties:
                kind: {type: string, enum: [integration]}
                id: {type: string}
            audience: {type: string, enum: [integration_api]}
            authentication_strength: {type: string, enum: [credential]}
            credential_id: {type: string}
        name: {type: string}
        prefix: {type: string}
        expires_at_ms: {type: integer}
    IntegrationPublicThreadRequest:
      type: object
      additionalProperties: false
      required: [id]
      properties:
        id: {type: string, pattern: '^[A-Za-z0-9][A-Za-z0-9._:-]*$', maxLength: 128}
    IntegrationPublicThread:
      type: object
      additionalProperties: false
      required: [id, title, content, content_format, updated_at]
      properties:
        id: {type: string}
        title: {type: string}
        content: {type: string}
        content_format: {type: string}
        updated_at: {type: string, format: date-time}
`
}
