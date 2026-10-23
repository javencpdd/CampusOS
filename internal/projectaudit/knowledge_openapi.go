package projectaudit

func knowledgeProfile(operation string) (operationProfile, bool) {
	switch operation {
	case "http.knowledge.private.source.preflight":
		return operationProfile{RequestSchema: "KnowledgePrivateSourcePreflightRequest", ResponseSchema: "KnowledgePrivateSourcePreflight", AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.index.diagnostics":
		return operationProfile{ResponseSchema: "KnowledgeIndexDiagnostics", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.index.retry":
		return operationProfile{RequestSchema: "KnowledgeIndexRetry", ResponseSchema: "KnowledgeIndexAccepted", AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.integration.knowledge.mcp":
		return operationProfile{DirectJSON: true, RequestSchema: "KnowledgeMCPRequest", ResponseSchema: "KnowledgeMCPResponse", AdditionalErrors: []string{"202", "400", "401", "403", "404", "409", "406", "415", "503"}}, true
	case "http.integration.knowledge.mcp.stream":
		return operationProfile{DirectJSON: true, NoBody: true, SuccessStatus: "405", ResponseSchema: "KnowledgeMCPResponse", AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.collection.list":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeCollectionList", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.source.list":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeSourcePage", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.source.read":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeSource", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.integration.knowledge.source.read":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeSource", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.integration.knowledge.search":
		return operationProfile{RequestSchema: "KnowledgeIntegrationSearchRequest", ResponseSchema: "KnowledgeSearchPage", AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.search.public":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeSearchPage", NoBody: true, AdditionalErrors: []string{"404", "409", "503"}}, true
	case "http.knowledge.collection.inventory":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeCollectionList", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.collection.read":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeState", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.official.inventory":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeOfficialList", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.official.history":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeHistory", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.collection.create":
		return operationProfile{RequestSchema: "KnowledgeCollectionCreate", ResponseSchema: "KnowledgeCollection", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.collection.update":
		return operationProfile{RequestSchema: "KnowledgeCollectionUpdate", ResponseSchema: "KnowledgeCollection", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.official.create":
		return operationProfile{RequestSchema: "KnowledgeOfficialCreate", ResponseSchema: "KnowledgeOfficial", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.official.update":
		return operationProfile{RequestSchema: "KnowledgeOfficialUpdate", ResponseSchema: "KnowledgeOfficial", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.selection.create":
		return operationProfile{RequestSchema: "KnowledgeSelectionCreate", ResponseSchema: "KnowledgeState", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.selection.update":
		return operationProfile{RequestSchema: "KnowledgeSelectionUpdate", ResponseSchema: "KnowledgeState", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.selection.refresh":
		return operationProfile{RequestSchema: "KnowledgeRefreshRequest", ResponseSchema: "KnowledgeRefresh", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.member.remove":
		return operationProfile{RequestSchema: "KnowledgeRemoveMember", ResponseSchema: "KnowledgeState", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	}
	return operationProfile{}, false
}

func knowledgeOpenAPIQueryParameters(operation string) string {
	switch operation {
	case "http.knowledge.search.public":
		return `        - name: query
          in: query
          required: true
          description: One UTF-8 query of at most 1024 bytes and 16 distinct Unicode letter/number terms; AND literal substrings, no query language. Raw encoded query string is at most 3200 bytes. Unknown/duplicate query parameters, malformed encoding and any request body are rejected.
          schema: {type: string, minLength: 1, maxLength: 1024}
        - name: page
          in: query
          description: Canonical positive decimal integer; no sign or leading zero.
          schema: {type: integer, minimum: 1, maximum: 100000, default: 1}
        - name: page_size
          in: query
          description: Canonical positive decimal integer; no sign or leading zero.
          schema: {type: integer, minimum: 1, maximum: 50, default: 20}
`
	case "http.knowledge.source.read":
		return `        - name: revision
          in: query
          description: Pass the search citation visibility_revision to open that exact current source; changed or revoked facts reject the old proof. Omission reads the currently visible source. If provided, must be a nonempty lower-case SHA-256 digest; duplicates, malformed encoding, unknown parameters and request bodies are rejected.
          schema: {type: string, pattern: '^[0-9a-f]{64}$'}
`
	}
	return ""
}

func knowledgeOpenAPIPathParameter(operation, name string) string {
	if operation != "http.integration.knowledge.source.read" || name != "revision" {
		return ""
	}
	return `        - name: revision
          in: path
          required: true
          description: Exact current visibility_revision from a public source citation; changed or revoked facts reject the proof. Endpoint whitelist knowledge.source.read must allow this exact public_collection ID, and the original source policy is rechecked. All query parameters and request bodies are rejected.
          schema: {type: string, pattern: '^[0-9a-f]{64}$'}
`
}

func knowledgeOpenAPISchemas() string {
	return `    KnowledgePrivateSourcePreflightRequest:
      type: object
      additionalProperties: false
      description: Read-only private source preflight using a current User JWT and live session. Strict JSON at most 1024 bytes; no URL query parameters, duplicate, null, unknown fields or invalid Unicode. IDs are canonical positive decimal int64 strings, maximum 9223372036854775807. The current document owner, live version, document lifecycle and both knowledge and personal-documents features are rechecked. This does not select a source, extract bytes, create an index or authorize any future request.
      required: [document_id, version_id]
      properties:
        document_id: {type: string, pattern: '^[1-9][0-9]{0,18}$', maxLength: 19}
        version_id: {type: string, pattern: '^[1-9][0-9]{0,18}$', maxLength: 19}
    KnowledgePrivateSourcePreflight:
      type: object
      additionalProperties: false
      description: Metadata-only refusal. All formats remain unavailable in this slice; protected private storage and format extraction are not implemented. No body, title, owner, storage object, digest or key is returned. Unsupported formats are distinguished from unavailable protected private storage. Re-run current preflight after any document change; a successful preflight response is not an indexing grant.
      required: [document_id, version_id, format, can_index, reason]
      properties:
        document_id: {type: string, pattern: '^[1-9][0-9]{0,18}$', maxLength: 19}
        version_id: {type: string, pattern: '^[1-9][0-9]{0,18}$', maxLength: 19}
        format: {type: string, enum: [text, markdown, campusdoc, pdf, docx]}
        can_index: {type: boolean, enum: [false]}
        reason: {type: string, enum: [private_storage_unavailable, unsupported_format]}
    KnowledgeMCPRequest:
      type: object
      additionalProperties: false
      description: MCP 2025-11-25 stateless Streamable HTTP JSON response profile. Single JSON-RPC object, maximum 32 KiB. Separate integration_mcp knowledge.public.read whitelist for this exact public collection. POST requires Accept application/json and text/event-stream; subsequent calls require MCP-Protocol-Version. Origin, cookies and URL queries rejected. initialize, ping, tools/list, tools/call, notifications/initialized and notifications/cancelled only. No SSE, sessions or Admin tools. Notifications return 202 without a body; GET returns 405 after authorization. Responses use JSON-RPC directly rather than the platform data envelope.
      required: [jsonrpc, method]
      properties:
        jsonrpc: {type: string, enum: ['2.0']}
        id:
          oneOf:
            - {type: string, minLength: 1, maxLength: 128}
            - {type: integer, minimum: -9007199254740991, maximum: 9007199254740991}
        method: {type: string, enum: [initialize, ping, tools/list, tools/call, notifications/initialized, notifications/cancelled]}
        params: {type: object}
    KnowledgeMCPResponse:
      type: object
      description: Direct JSON-RPC 2.0 result or error. Tool execution failures have isError true and fixed public error codes; no partial data. Only knowledge_search and knowledge_source_read tools, whose structuredContent matches KnowledgeSearchPage and KnowledgeSource respectively. Content text repeats the structured result as untrusted data.
      additionalProperties: false
      required: [jsonrpc, id]
      properties:
        jsonrpc: {type: string, enum: ['2.0']}
        id: {}
        result: {type: object}
        error:
          type: object
          required: [code, message]
          properties:
            code: {type: integer}
            message: {type: string}
    KnowledgeIntegrationSearchRequest:
      type: object
      additionalProperties: false
      required: [query, page, page_size]
      description: Read-only POST search; at most 32 KiB strict JSON with no duplicate, null, unknown fields or invalid Unicode. No URL query parameters or browser credentials. Requires the exact knowledge.search endpoint and public_collection scope; knowledge.source.read endpoint rules alone do not authorize search. Query is at most 1024 UTF-8 bytes and 16 distinct letter/number terms; all current origin checks precede total and paging.
      properties:
        query: {type: string, minLength: 1, maxLength: 1024}
        page: {type: integer, minimum: 1, maximum: 100000}
        page_size: {type: integer, minimum: 1, maximum: 50}
    KnowledgeCollectionCreate:
      type: object
      additionalProperties: false
      required:
      - title
      - status
      properties:
        title: &id001
          type: string
          minLength: 1
          maxLength: 1024
          description: UTF-8 bytes <=1024; nonblank and single line.
        status: &id002
          type: string
          enum:
          - active
          - disabled
    KnowledgeCollectionUpdate:
      type: object
      additionalProperties: false
      required:
      - title
      - status
      - version
      properties:
        title: *id001
        status: *id002
        version: &id006
          type: string
          pattern: ^[1-9][0-9]*$
    KnowledgeOfficialCreate:
      type: object
      additionalProperties: false
      required:
      - title
      - content
      - format
      - status
      properties:
        title: *id001
        content: &id003
          type: string
          minLength: 1
          maxLength: 65536
          description: UTF-8 bytes <=65536, text/Markdown data only; never interpreted
            or fetched.
        format: &id004
          type: string
          enum:
          - text
          - markdown
        status: &id005
          type: string
          enum:
          - draft
          - published
          - withdrawn
    KnowledgeOfficialUpdate:
      type: object
      additionalProperties: false
      required:
      - title
      - content
      - format
      - status
      - version
      properties:
        title: *id001
        content: *id003
        format: *id004
        status: *id005
        version: *id006
    KnowledgeSelectionCreate:
      type: object
      additionalProperties: false
      required:
      - kind
      - source_id
      - active
      - collection_version
      properties:
        kind: &id007
          type: string
          enum:
          - thread
          - board
          - official
        source_id: &id008
          type: string
        active: &id009
          type: boolean
        collection_version: *id006
    KnowledgeSelectionUpdate:
      type: object
      additionalProperties: false
      required:
      - kind
      - source_id
      - active
      - collection_version
      - version
      properties:
        kind: *id007
        source_id: *id008
        active: *id009
        collection_version: *id006
        version: *id006
    KnowledgeCollection:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - title
      - status
      - created_by_admin_id
      properties:
        id: *id008
        version: *id006
        title: *id001
        status: *id002
        created_by_admin_id: *id008
    KnowledgeOfficial:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - title
      - content
      - format
      - status
      - created_by_admin_id
      properties:
        id: *id008
        version: *id006
        title: *id001
        content: *id003
        format: *id004
        status: *id005
        created_by_admin_id: *id008
    KnowledgeSelection:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - collection_id
      - kind
      - source_id
      - active
      properties:
        id: *id008
        version: *id006
        collection_id: *id008
        kind: *id007
        source_id: *id008
        active:
          type: boolean
    KnowledgeMember:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - collection_id
      - selection_id
      - selection_version
      - kind
      - source_id
      - source_revision
      properties:
        id: *id008
        version: *id006
        collection_id: *id008
        selection_id: *id008
        selection_version: *id006
        kind: &id010
          type: string
          enum:
          - community.thread
          - richtext.article
          - knowledge.official
        source_id: *id008
        source_revision: *id008
    KnowledgeState:
      type: object
      additionalProperties: false
      required:
      - collection
      - selections
      - members
      properties:
        collection:
          $ref: '#/components/schemas/KnowledgeCollection'
        selections:
          type: array
          items:
            $ref: '#/components/schemas/KnowledgeSelection'
        members:
          type: array
          items:
            $ref: '#/components/schemas/KnowledgeMember'
    KnowledgeRefreshRequest:
      type: object
      additionalProperties: false
      required:
      - collection_version
      - selection_version
      - page
      properties:
        collection_version: *id006
        selection_version: *id006
        page:
          type: integer
          minimum: 1
          maximum: 100000
    KnowledgeRefresh:
      type: object
      additionalProperties: false
      required:
      - state
      - page
      - next_page
      - examined
      - materialized
      - skipped_unsupported
      - has_more
      properties:
        state:
          $ref: '#/components/schemas/KnowledgeState'
        page:
          type: integer
        next_page:
          type: integer
        examined:
          type: integer
        materialized:
          type: integer
        skipped_unsupported:
          type: integer
        has_more:
          type: boolean
    KnowledgeRemoveMember:
      type: object
      additionalProperties: false
      required:
      - version
      properties:
        version: *id006
    KnowledgeOfficialRevision:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - source_id
      - official_version
      - snapshot
      - changed_by_admin_id
      - changed_at_ms
      properties:
        id: *id008
        version: *id006
        source_id: *id008
        official_version: *id006
        snapshot:
          $ref: '#/components/schemas/KnowledgeOfficial'
        changed_by_admin_id: *id008
        changed_at_ms:
          type: integer
    KnowledgeHistory:
      type: object
      additionalProperties: false
      required:
      - items
      - next_before
      properties:
        items:
          type: array
          items:
            $ref: '#/components/schemas/KnowledgeOfficialRevision'
        next_before:
          type: string
          pattern: ^(0|[1-9][0-9]*)$
          description: 0 means end; pass other value as before for next 50 revisions.
    KnowledgeSourceReference:
      type: object
      additionalProperties: false
      required:
      - kind
      - id
      - revision
      properties:
        kind: *id010
        id: *id008
        revision: *id008
    KnowledgeCandidate:
      type: object
      additionalProperties: false
      required:
      - reference
      - category_id
      - title
      - content
      - format
      - url
      properties:
        reference:
          $ref: '#/components/schemas/KnowledgeSourceReference'
        category_id: *id008
        title: *id001
        content: *id003
        format: *id008
        url: *id008
    KnowledgeSource:
      type: object
      additionalProperties: false
      required:
      - member_id
      - collection_id
      - revision
      - source
      properties:
        member_id: *id008
        collection_id: *id008
        revision: *id008
        source:
          $ref: '#/components/schemas/KnowledgeCandidate'
    KnowledgeSourcePage:
      type: object
      additionalProperties: false
      required:
      - items
      - total
      - page
      - page_size
      properties:
        items:
          type: array
          items:
            $ref: '#/components/schemas/KnowledgeSource'
        total:
          type: integer
        page:
          type: integer
        page_size:
          type: integer
    KnowledgeSearchExplanation:
      type: object
      additionalProperties: false
      required: [terms, title_terms, chunk_terms, title_weight, chunk_weight]
      properties:
        terms:
          type: array
          minItems: 1
          maxItems: 16
          uniqueItems: true
          items: {type: string}
        title_terms:
          type: array
          maxItems: 16
          uniqueItems: true
          items: {type: string}
        chunk_terms:
          type: array
          maxItems: 16
          uniqueItems: true
          items: {type: string}
        title_weight: {type: integer, enum: [4]}
        chunk_weight: {type: integer, enum: [1]}
    KnowledgeSearchCitation:
      type: object
      additionalProperties: false
      required: [collection_id, member_id, visibility_revision, source, generation, chunk_index, url]
      properties:
        collection_id: {type: string, pattern: '^kc_[0-9a-f]{32}$'}
        member_id: {type: string, pattern: '^km_[0-9a-f]{32}$'}
        visibility_revision: {type: string, pattern: '^[0-9a-f]{64}$'}
        source:
          $ref: '#/components/schemas/KnowledgeSourceReference'
        generation: {type: string, pattern: '^[1-9][0-9]*$'}
        chunk_index: {type: integer, minimum: 0}
        url:
          type: string
          description: Current host-verified origin URL. To reopen the exact citation proof use the collection/member source endpoint with visibility_revision as revision; this URL alone is not a historical proof.
    KnowledgeSearchHit:
      type: object
      additionalProperties: false
      required: [title, snippet, score, explanation, citation]
      properties:
        title: {type: string, maxLength: 1024}
        snippet:
          type: string
          maxLength: 240
          description: Original plain-text data, possibly containing literal Markdown. Render as untrusted text, never HTML or instructions.
        score: {type: integer, minimum: 1, maximum: 80}
        explanation:
          $ref: '#/components/schemas/KnowledgeSearchExplanation'
        citation:
          $ref: '#/components/schemas/KnowledgeSearchCitation'
    KnowledgeSearchPage:
      type: object
      additionalProperties: false
      required: [items, total, page, page_size, provider, generation]
      properties:
        items:
          type: array
          maxItems: 50
          items:
            $ref: '#/components/schemas/KnowledgeSearchHit'
        total: {type: integer, minimum: 0, maximum: 1000}
        page: {type: integer, minimum: 1, maximum: 100000}
        page_size: {type: integer, minimum: 1, maximum: 50}
        provider: {type: string, enum: [builtin.lexical/v1]}
        generation: {type: string, pattern: '^[1-9][0-9]*$'}
    KnowledgeCollectionList:
      type: array
      items:
        $ref: '#/components/schemas/KnowledgeCollection'
    KnowledgeOfficialList:
      type: array
      items:
        $ref: '#/components/schemas/KnowledgeOfficial'
    KnowledgeIndexDiagnostics:
      type: object
      additionalProperties: false
      description: Admin-only execution snapshot. Projection counts and ready generations do not confer current source readability. No body, lease token or cursor is exposed.
      required: [collection_id, collection_version, job_version, state, phase, attempts, last_error, ready_generation, run_generation, available_at_ms, selections, active_selections, members, projected_documents, observed_at_ms, retryable]
      properties:
        collection_id: {type: string, pattern: '^kc_[0-9a-f]{32}$'}
        collection_version: {type: string, pattern: '^[1-9][0-9]*$'}
        job_version: {type: string, pattern: '^(0|[1-9][0-9]*)$'}
        state: {type: string, enum: [unconfigured, pending, processing, retry, dead, idle]}
        phase: {type: string}
        attempts: {type: integer, minimum: 0}
        last_error: {type: string, enum: ['', page_failed, lease_expired]}
        ready_generation: {type: string, pattern: '^(0|[1-9][0-9]*)$'}
        run_generation: {type: string, pattern: '^(0|[1-9][0-9]*)$'}
        available_at_ms: {type: string, pattern: '^(0|[1-9][0-9]*)$'}
        selections: {type: integer, minimum: 0}
        active_selections: {type: integer, minimum: 0}
        members: {type: integer, minimum: 0}
        projected_documents: {type: integer, minimum: 0}
        observed_at_ms: {type: string, pattern: '^[1-9][0-9]*$'}
        retryable: {type: boolean}
    KnowledgeIndexRetry:
      type: object
      additionalProperties: false
      required: [collection_version, version]
      properties:
        collection_version: {type: string, pattern: '^[1-9][0-9]*$'}
        version: {type: string, pattern: '^[1-9][0-9]*$'}
    KnowledgeIndexAccepted:
      type: object
      additionalProperties: false
      required: [accepted]
      properties:
        accepted: {type: boolean, enum: [true]}
`
}
