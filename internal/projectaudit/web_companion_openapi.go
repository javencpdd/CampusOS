package projectaudit

func webCompanionOpenAPISchemas() string {
	return `    WebCompanionClient:
      type: object
      additionalProperties: false
      required: [id, name, online, expires_at, proxy_mode]
      properties:
        id: {type: string, pattern: '^[a-f0-9]{32}$'}
        name: {type: string, minLength: 1, maxLength: 80}
        online: {type: boolean}
        expires_at: {type: integer, format: int64}
        proxy_mode: {type: string, enum: [direct, http, https, socks5]}
    WebCompanionConnectRequest:
      type: object
      additionalProperties: false
      required: [pairing_code, name, proxy_mode]
      properties:
        pairing_code: {type: string, pattern: '^[a-f0-9]{32}$'}
        name: {type: string, minLength: 1, maxLength: 80}
        proxy_mode: {type: string, enum: [direct, http, https, socks5]}
    WebCompanionConnectResult:
      type: object
      additionalProperties: false
      required: [token, client]
      properties:
        token: {type: string, pattern: '^[a-f0-9]{64}$'}
        client:
          $ref: '#/components/schemas/WebCompanionClient'
    WebCompanionJob:
      type: object
      additionalProperties: false
      required: [id, url, max_bytes]
      properties:
        id: {type: string, pattern: '^[a-f0-9]{32}$'}
        url: {type: string, minLength: 1, maxLength: 2048}
        max_bytes: {type: integer, minimum: 1, maximum: 1048576}
    WebCompanionPollResult:
      type: object
      additionalProperties: false
      required: [job]
      properties:
        job:
          nullable: true
          allOf:
            - $ref: '#/components/schemas/WebCompanionJob'
    WebCompanionResultRequest:
      type: object
      additionalProperties: false
      required: [job_id, bytes, failed]
      properties:
        job_id: {type: string, pattern: '^[a-f0-9]{32}$'}
        bytes: {type: string, format: byte, maxLength: 1398104}
        failed: {type: boolean}
    WebCompanionReceived:
      type: object
      additionalProperties: false
      required: [received]
      properties:
        received: {type: boolean, enum: [true]}
`
}
