package projectaudit

import (
	"strings"

	"github.com/campusos/CampusOS/pkg/config"
)

func runtimeConfigProfile(operation string) (operationProfile, bool) {
	p := operationProfile{NoBody: true, ResponseSchema: "RuntimeConfigSnapshot", AdditionalErrors: []string{"400", "401", "403", "409", "429", "503"}}
	switch operation {
	case "http.platform.runtime_config.read":
	case "http.platform.runtime_config.document":
		p.NoBody, p.RequestSchema, p.ResponseSchema = false, "RuntimeConfigDocumentConfirmation", "RuntimeConfigDocument"
	case "http.platform.runtime_config.update":
		p.NoBody, p.RequestSchema = false, "RuntimeConfigUpdate"
	default:
		return operationProfile{}, false
	}
	return p, true
}

func runtimeConfigOpenAPISchemas() string {
	var changes strings.Builder
	for _, f := range config.RuntimeFields() {
		changes.WriteString("            " + f.Key + ": {type: string, maxLength: 4096")
		if f.Kind == "secret" {
			changes.WriteString(", writeOnly: true")
		}
		changes.WriteString("}\n")
	}
	return `    RuntimeConfigUpdate:
      type: object
      additionalProperties: false
      required: [revision]
      properties:
        revision: {type: string, pattern: '^[0-9a-f]{64}$'}
        restart: {type: boolean, default: false}
        verification_method:
          type: string
          enum: [password, mfa]
          description: Password confirmation is scoped to this operation; MFA uses the current recently verified session. Omission preserves the existing session policy.
        current_password:
          type: string
          minLength: 1
          maxLength: 72
          writeOnly: true
          description: Required only with verification_method password. Never stored, returned, or used to upgrade session MFA strength.
        document:
          type: string
          maxLength: 262144
          writeOnly: true
          description: Complete literal dotenv document for the fixed deployment file. Mutually exclusive with nonempty changes. Noncatalog changes require deployment tooling.
        changes:
          type: object
          additionalProperties: false
          description: Changed catalog fields only. Omit unchanged credentials; an explicit empty string clears a credential.
          properties:
` + changes.String() + `    RuntimeConfigDocumentConfirmation:
      type: object
      additionalProperties: false
      required: [revision, verification_method]
      properties:
        revision: {type: string, pattern: '^[0-9a-f]{64}$'}
        verification_method: {type: string, enum: [password, mfa]}
        current_password: {type: string, minLength: 1, maxLength: 72, writeOnly: true}
    RuntimeConfigDocument:
      type: object
      additionalProperties: false
      required: [content, revision]
      properties:
        content: {type: string, description: 'Sensitive complete dotenv. No-store. Never log or persist in browser storage.'}
        revision: {type: string}
    RuntimeConfigSnapshot:
      type: object
      additionalProperties: false
      required: [available, revision, generation, pending, restarting, fields, deployment_keys]
      properties:
        available: {type: boolean}
        revision: {type: string}
        generation: {type: string, pattern: '^[0-9a-f]{32}$'}
        pending: {type: boolean}
        restarting: {type: boolean}
        deployment_keys: {type: array, items: {type: string}, description: 'Changed noncatalog keys requiring deployment tools after this save.'}
        fields:
          type: array
          items: {$ref: '#/components/schemas/RuntimeConfigField'}
    RuntimeConfigField:
      type: object
      additionalProperties: false
      required: [key, label, group, kind, help, value, active_value, configured, pending]
      properties:
        key: {type: string}
        label: {type: string}
        group: {type: string}
        kind: {type: string, enum: [boolean, text, secret, select, number]}
        help: {type: string}
        options: {type: array, items: {type: string}}
        value: {type: string, description: 'Always empty for a secret field; existing credentials are never returned.'}
        active_value: {type: string, description: 'Active runtime value; always empty for secret fields.'}
        configured: {type: boolean}
        pending: {type: boolean}
`
}
