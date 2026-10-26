package projectaudit

func ragProfile(code string) (operationProfile, bool) {
	p := operationProfile{AdditionalErrors: []string{"400", "401", "403", "404", "409", "503"}}
	switch code {
	case "http.knowledge.rag.status":
		p.NoBody = true
		p.ResponseSchema = "KnowledgeRAGStatus"
	case "http.knowledge.rag.public":
		p.RequestSchema = "KnowledgeRAGPublicRequest"
		p.ResponseSchema = "KnowledgeRAGPublicResult"
	case "http.knowledge.private.rag.prepare":
		p.RequestSchema = "KnowledgeRAGPrivatePrepare"
		p.ResponseSchema = "KnowledgeRAGReview"
	case "http.knowledge.private.rag.answer":
		p.RequestSchema = "KnowledgeRAGPrivateAnswer"
		p.ResponseSchema = "KnowledgeRAGPrivateResult"
	case "http.knowledge.private.rag.revoke":
		p.RequestSchema = "KnowledgeRAGRevokeRequest"
		p.ResponseSchema = "KnowledgeRAGRevoked"
	default:
		return operationProfile{}, false
	}
	return p, true
}
func ragSchemas() string {
	return `    KnowledgeRAGPublicRequest:
      type: object
      additionalProperties: false
      required:
      - collection_id
      - question
      - run_id
      - binding_revision
      - step
      properties:
        collection_id:
          type: string
          pattern: ^kc_[0-9a-f]{32}$
        question: &id001
          type: string
          minLength: 1
          maxLength: 1024
        run_id: &id002
          type: string
          pattern: ^ar_[0-9a-f]{32}$
        binding_revision: &id003
          type: string
          pattern: ^[1-9][0-9]*$
        step: &id004
          type: string
          enum:
          - chat
    KnowledgeRAGPrivatePrepare:
      type: object
      additionalProperties: false
      required:
      - selection_id
      - revision
      - question
      - run_id
      - binding_revision
      - step
      properties:
        selection_id: &id005
          type: string
          pattern: ^kp_[0-9a-f]{32}$
        revision: &id006
          type: string
          pattern: ^[0-9a-f]{64}$
        question: *id001
        run_id: *id002
        binding_revision: *id003
        step: *id004
    KnowledgeRAGPrivateAnswer:
      type: object
      additionalProperties: false
      required:
      - selection_id
      - revision
      - question
      - run_id
      - binding_revision
      - step
      - approval_id
      - review_digest
      - consented
      properties:
        selection_id: *id005
        revision: *id006
        question: *id001
        run_id: *id002
        binding_revision: *id003
        step: *id004
        approval_id:
          type: string
          pattern: ^kr_[0-9a-f]{32}$
        review_digest: *id006
        consented:
          type: boolean
          enum:
          - true
    KnowledgeRAGRevokeRequest:
      type: object
      additionalProperties: false
      required:
      - approval_id
      properties:
        approval_id:
          type: string
          pattern: ^kr_[0-9a-f]{32}$
    KnowledgeRAGRevoked:
      type: object
      additionalProperties: false
      required:
      - revoked
      properties:
        revoked:
          type: boolean
          enum:
          - true
    KnowledgeRAGModel:
      type: object
      additionalProperties: false
      required:
      - profile_id
      - profile_revision
      - binding_revision
      - definition
      - model
      - endpoint
      - system_prompt
      - max_tokens
      properties:
        profile_id:
          type: string
          enum:
          - local-chat
          - remote-chat
        profile_revision: *id003
        binding_revision: *id003
        definition:
          type: string
          enum:
          - builtin.local-openai.text
          - builtin.openai-compatible.text
        model: &id007
          type: string
        endpoint:
          type: string
          maxLength: 2048
        system_prompt: *id007
        max_tokens:
          type: integer
          minimum: 1
          maximum: 65536
    KnowledgeRAGStatus:
      type: object
      additionalProperties: false
      required:
      - model
      - supported_steps
      - unsupported_steps
      properties:
        model:
          $ref: '#/components/schemas/KnowledgeRAGModel'
        supported_steps:
          type: array
          items:
            type: string
            enum:
            - chat
          minItems: 1
          maxItems: 1
        unsupported_steps:
          type: array
          items:
            type: string
            enum:
            - embedding
            - rerank
            - parser
            - query_rewrite
            - telemetry
          minItems: 5
          maxItems: 5
    KnowledgeRAGPrivateEvidence:
      type: object
      additionalProperties: false
      required:
      - id
      - selection_id
      - kind
      - source_id
      - revision
      - format
      - title
      - excerpt
      - index_consent_version
      properties:
        id:
          type: string
          enum:
          - E1
        selection_id: *id005
        kind:
          type: string
          enum:
          - personal_document
          - community_thread
        source_id: *id007
        revision: *id006
        format:
          type: string
          enum:
          - text
          - markdown
          - campusdoc
        title:
          type: string
          maxLength: 1024
        excerpt:
          type: string
          maxLength: 240
        index_consent_version: *id003
    KnowledgeRAGPublicEvidence:
      type: object
      additionalProperties: false
      required:
      - id
      - title
      - excerpt
      - citation
      properties:
        id:
          type: string
          enum:
          - E1
          - E2
          - E3
        title: *id007
        excerpt:
          type: string
          maxLength: 240
        citation:
          $ref: '#/components/schemas/KnowledgeSearchCitation'
    KnowledgeRAGPrivateQuestion:
      type: object
      additionalProperties: false
      required:
      - selection_id
      - revision
      - question
      - run_id
      - binding_revision
      properties:
        selection_id: *id005
        revision: *id006
        question: *id001
        run_id: *id002
        binding_revision: *id003
    KnowledgeRAGReview:
      type: object
      additionalProperties: false
      required:
      - contract
      - status
      - request
      - step
      - model
      - content
      - evidence
      properties:
        contract:
          type: string
          enum:
          - campusos.knowledge-private-rag/v1
        status:
          type: string
          enum:
          - ready
          - refused
        reason:
          type: string
          enum:
          - no_evidence
        request:
          $ref: '#/components/schemas/KnowledgeRAGPrivateQuestion'
        step:
          type: string
          enum:
          - chat
        model:
          $ref: '#/components/schemas/KnowledgeRAGModel'
        content:
          type: string
          maxLength: 16384
        evidence:
          type: array
          maxItems: 1
          items:
            $ref: '#/components/schemas/KnowledgeRAGPrivateEvidence'
        digest: *id006
        approval_id:
          type: string
          pattern: ^kr_[0-9a-f]{32}$
        expires_at_ms:
          type: integer
          format: int64
    KnowledgeRAGPublicResult:
      type: object
      additionalProperties: false
      required:
      - contract
      - status
      - answer
      - citations
      properties:
        contract:
          type: string
          enum:
          - campusos.knowledge-public-rag/v1
        status:
          type: string
          enum:
          - answered
          - refused
        reason:
          type: string
          enum:
          - no_evidence
          - model_no_evidence
        answer:
          type: string
          maxLength: 4096
        run_id: *id002
        citations:
          type: array
          maxItems: 3
          items:
            $ref: '#/components/schemas/KnowledgeRAGPublicEvidence'
    KnowledgeRAGPrivateResult:
      type: object
      additionalProperties: false
      required:
      - contract
      - status
      - answer
      - citations
      properties:
        contract:
          type: string
          enum:
          - campusos.knowledge-private-rag/v1
        status:
          type: string
          enum:
          - answered
          - refused
        reason:
          type: string
          enum:
          - no_evidence
          - model_no_evidence
        answer:
          type: string
          maxLength: 4096
        run_id: *id002
        citations:
          type: array
          maxItems: 1
          items:
            $ref: '#/components/schemas/KnowledgeRAGPrivateEvidence'
`
}
