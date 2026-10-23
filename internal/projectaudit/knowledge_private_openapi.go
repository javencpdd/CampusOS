package projectaudit

func privateIndexProfile(code string) (operationProfile, bool) {
	base := operationProfile{AdditionalErrors: []string{"400", "401", "403", "404", "409", "503"}}
	switch code {
	case "http.knowledge.private.status":
		base.NoBody = true
		base.ResponseSchema = "KnowledgePrivateStatus"
	case "http.knowledge.private.preview":
		base.RequestSchema = "KnowledgePrivatePreviewRequest"
		base.ResponseSchema = "KnowledgePrivatePreview"
	case "http.knowledge.private.list":
		base.NoBody = true
		base.ResponseSchema = "KnowledgePrivateSelections"
	case "http.knowledge.private.select":
		base.RequestSchema = "KnowledgePrivateSelectRequest"
		base.ResponseSchema = "KnowledgePrivateSelection"
	case "http.knowledge.private.withdraw":
		base.NoBody = true
		base.ResponseSchema = "KnowledgePrivateWithdrawn"
	case "http.knowledge.private.retry":
		base.NoBody = true
		base.ResponseSchema = "KnowledgeIndexRetryAccepted"
	case "http.knowledge.private.search":
		base.RequestSchema = "KnowledgePrivateSearchRequest"
		base.ResponseSchema = "KnowledgePrivateSearchPage"
	case "http.knowledge.private.source":
		base.RequestSchema = "KnowledgePrivateSourceRequest"
		base.ResponseSchema = "KnowledgePrivateSource"
	default:
		return operationProfile{}, false
	}
	return base, true
}
func privateIndexSchemas() string {
	return `    KnowledgePrivateStatus:
      type: object
      additionalProperties: false
      required: [can_index, reason, default_inclusion, supported_formats, max_sources, max_source_bytes, outbound_authorized]
      properties:
        can_index: {type: boolean}
        reason: {type: string, enum: [ready, private_storage_unavailable]}
        default_inclusion: {type: boolean, enum: [false]}
        supported_formats: {type: array, items: {type: string, enum: [text, markdown, campusdoc]}}
        max_sources: {type: integer, enum: [100]}
        max_source_bytes: {type: integer, enum: [65536]}
        outbound_authorized: {type: boolean, enum: [false]}
    KnowledgePrivatePreviewRequest:
      type: object
      additionalProperties: false
      required: [kind, source_id]
      properties:
        kind: {type: string, enum: [personal_document, community_thread]}
        source_id: {type: string, pattern: '^[1-9][0-9]{0,18}$', maxLength: 19}
    KnowledgePrivateSelectRequest:
      type: object
      additionalProperties: false
      description: Explicit current owner consent for one exact current source revision. Encryption and external key custody are mandatory; no AI or outbound consent is implied. 409 on duplicate or stale source; no automatic replay.
      required: [kind, source_id, revision, consent]
      properties:
        kind: {type: string, enum: [personal_document, community_thread]}
        source_id: {type: string, pattern: '^[1-9][0-9]{0,18}$', maxLength: 19}
        revision: {type: string, pattern: '^[0-9a-f]{64}$'}
        consent: {type: boolean, enum: [true]}
    KnowledgePrivateSource:
      type: object
      additionalProperties: false
      required: [kind, source_id, revision, format]
      properties:
        kind: {type: string, enum: [personal_document, community_thread]}
        source_id: {type: string}
        revision: {type: string, pattern: '^[0-9a-f]{64}$'}
        format: {type: string, enum: [text, markdown, campusdoc, pdf, docx]}
        title: {type: string, maxLength: 1024}
        text: {type: string, maxLength: 65536}
    KnowledgePrivatePreview:
      type: object
      additionalProperties: false
      required: [source, can_index, reason]
      properties:
        source: {$ref: '#/components/schemas/KnowledgePrivateSource'}
        can_index: {type: boolean}
        reason: {type: string, enum: [ready, unsupported_format]}
    KnowledgePrivateSelection:
      type: object
      additionalProperties: false
      required: [id, kind, source_id, revision, format, version, state, task_state, attempts, error_code]
      properties:
        title: {type: string, maxLength: 1024}

        id: {type: string, pattern: '^kp_[0-9a-f]{32}$'}
        kind: {type: string, enum: [personal_document, community_thread]}
        source_id: {type: string}
        revision: {type: string, pattern: '^[0-9a-f]{64}$'}
        format: {type: string, enum: [text, markdown, campusdoc]}
        version: {type: string, pattern: '^[1-9][0-9]*$'}
        state: {type: string, enum: [pending, ready, unavailable]}
        task_state: {type: string, enum: ['', pending, processing, retry, idle, dead]}
        attempts: {type: integer, minimum: 0, maximum: 5}
        error_code: {type: string, enum: ['', source_unavailable, key_unavailable, storage_unavailable]}
    KnowledgePrivateSelections:
      type: object
      additionalProperties: false
      required: [items]
      properties:
        items: {type: array, maxItems: 100, items: {$ref: '#/components/schemas/KnowledgePrivateSelection'}}
    KnowledgePrivateWithdrawn:
      type: object
      additionalProperties: false
      required: [withdrawn]
      properties:
        withdrawn: {type: boolean, enum: [true]}
    KnowledgePrivateSearchRequest:
      type: object
      additionalProperties: false
      description: POST body only, no URL query or persisted query. At most 1024 UTF-8 bytes and 16 terms. All owner candidates rechecked before totals. Five second deadline and 4 MiB corpus limit fail closed.
      required: [query]
      properties:
        query: {type: string, minLength: 1, maxLength: 1024}
    KnowledgePrivateSourceRequest:
      type: object
      additionalProperties: false
      required: [revision]
      properties:
        revision: {type: string, pattern: '^[0-9a-f]{64}$'}
    KnowledgePrivateSearchHit:
      type: object
      additionalProperties: false
      required: [id, kind, source_id, revision, title, snippet, score]
      properties:
        id: {type: string, pattern: '^kp_[0-9a-f]{32}$'}
        kind: {type: string, enum: [personal_document, community_thread]}
        source_id: {type: string}
        revision: {type: string, pattern: '^[0-9a-f]{64}$'}
        title: {type: string}
        snippet: {type: string, maxLength: 240}
        score: {type: integer}
    KnowledgePrivateSearchPage:
      type: object
      additionalProperties: false
      required: [items, total]
      properties:
        items: {type: array, maxItems: 100, items: {$ref: '#/components/schemas/KnowledgePrivateSearchHit'}}
        total: {type: integer, minimum: 0, maximum: 100}
`
}
