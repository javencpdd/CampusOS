package projectaudit

func v5UIActionsOpenAPISchemas() string {
	return `    PluginV5UIActionRequest:
      type: object
      additionalProperties: false
      required: [proof, action]
      properties:
        proof: {type: string, pattern: '^sha256:[a-f0-9]{64}$'}
        action: {type: string, enum: [web.read, editor.article.import]}
        url: {type: string, minLength: 1, maxLength: 2048}
        kind: {type: string, enum: [document, image]}
    PluginV5UIActionResult:
      type: object
      additionalProperties: false
      properties:
        authorized: {type: boolean}
        bytes: {type: string, format: byte, maxLength: 1398104}
        mime: {type: string}
`
}
