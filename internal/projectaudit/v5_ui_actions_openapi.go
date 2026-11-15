package projectaudit

func v5UIActionsOpenAPISchemas() string {
	return `    PluginV5UIActionRequest:
      type: object
      additionalProperties: false
      required: [proof, action]
      properties:
        client_id: {type: string, pattern: '^[a-f0-9]{32}$'}
        proof: {type: string, pattern: '^sha256:[a-f0-9]{64}$'}
        action: {type: string, enum: [web.read, editor.article.import, web.clients.status, web.clients.pair, web.clients.select, web.clients.disconnect]}
        url: {type: string, minLength: 1, maxLength: 2048}
        kind: {type: string, enum: [document, image]}
    PluginV5UIActionResult:
      type: object
      additionalProperties: false
      properties:
        scope: {type: string, pattern: '^sha256:[a-f0-9]{64}$'}
        local_available: {type: boolean}
        pairing_code: {type: string, pattern: '^[a-f0-9]{32}$'}
        expires_at: {type: integer, format: int64}
        clients:
          type: array
          maxItems: 64
          items:
            $ref: '#/components/schemas/WebCompanionClient'
        authorized: {type: boolean}
        bytes: {type: string, format: byte, maxLength: 1398104}
        mime: {type: string}
`
}
