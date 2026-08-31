package apperror

import (
	"fmt"
	"sort"
)

const CatalogVersion = "campusos.errors/v1"

var (
	RequestInvalid   = descriptor("platform", "request.invalid", 10001, 400, "invalid request", false)
	ResourceConflict = descriptor("platform", "resource.conflict", 10004, 409, "resource conflict", false)
	// Keep the cause server-side, but give every client a safe next action.
	InternalError    = descriptor("platform", "internal.error", 10006, 500, "系统暂时无法完成该操作，请稍后重试；如持续出现，请联系管理员。", true)
	AuthRequired     = descriptor("identity", "auth.required", 20001, 401, "unauthorized", false)
	AuthInvalidToken = descriptor("identity", "auth.invalid_token", 20002, 401, "invalid or expired token", false)
	PermissionDenied = descriptor("identity", "permission.denied", 20004, 403, "permission denied", false)
	ResourceNotFound = descriptor("platform", "resource.not_found", 30004, 404, "resource not found", false)

	IdentityChallengeUnavailable             = InternalError
	IdentityChallengeInvalid                 = descriptor("identity", "identity.registration_verification_invalid", 10009, 400, "registration verification is invalid or expired", false)
	IdentityChallengeRateLimited             = descriptor("identity", "identity.registration_verification_rate_limited", 10010, 429, "verification request is temporarily limited", true)
	IdentityRegistrationVerificationRequired = descriptor("identity", "identity.verification_required", 10008, 400, "email verification is required before registration", false)
	IdentityRegistrationConflict             = ResourceConflict
	IdentitySessionUnavailable               = InternalError
	IdentityAdminLoginUnavailable            = InternalError
	IdentityCredentialsInvalid               = AuthRequired
	IdentityAdminCredentialsInvalid          = AuthRequired
	IdentityCSRFInvalid                      = PermissionDenied
	IdentityRefreshInvalid                   = AuthInvalidToken
	IdentitySessionInactive                  = AuthInvalidToken
	IdentitySessionsUnavailable              = InternalError
	IdentitySessionNotFound                  = ResourceNotFound
	IdentityAdminAdmissionUnavailable        = InternalError
	IdentityAdminAdmissionInvalid            = descriptor("identity", "identity.admin_admission.invalid", 10001, 400, "administrator admission request is invalid", false)
	IdentityAdminAdmissionNotFound           = descriptor("identity", "identity.admin_admission.not_found", 30004, 404, "administrator admission record was not found", false)
	IdentityAdminAdmissionVersionConflict    = descriptor("identity", "identity.admin_admission.version_conflict", 10004, 409, "administrator admission version conflict", false)
	IdentityAdminAdmissionTransitionConflict = descriptor("identity", "identity.admin_admission.invalid_transition", 10004, 409, "administrator admission status transition is invalid", false)
	IdentityAdminAdmissionLastActive         = descriptor("identity", "identity.admin_admission.last_active", 10004, 409, "cannot suspend the last active administrator", false)
	IdentityMFAUnavailable                   = descriptor("identity", "identity.mfa.unavailable", 10020, 503, "multi-factor authentication is temporarily unavailable", true)
	IdentityMFAInvalid                       = descriptor("identity", "identity.mfa.invalid", 10011, 400, "multi-factor authentication request is invalid", false)
	IdentityMFATicketInvalid                 = descriptor("identity", "identity.mfa.ticket_invalid", 10012, 401, "multi-factor authentication ticket is invalid or expired", false)
	IdentityMFAFactorInvalid                 = descriptor("identity", "identity.mfa.factor_invalid", 10013, 400, "multi-factor authentication factor is invalid", false)
	IdentityMFAEnrollmentRequired            = descriptor("identity", "identity.mfa.enrollment_required", 10014, 403, "multi-factor authentication enrollment is required", false)
	IdentityMFAReplay                        = descriptor("identity", "identity.mfa.replay", 10015, 409, "multi-factor authentication code was already used", false)
	IdentityMFANotEnabled                    = descriptor("identity", "identity.mfa.not_enabled", 10016, 409, "multi-factor authentication is not enabled", false)
	IdentityMFAPolicyInvalid                 = descriptor("identity", "identity.mfa.policy_invalid", 10017, 400, "multi-factor authentication policy is invalid", false)
	IdentityMFAPolicySafety                  = descriptor("identity", "identity.mfa.policy_safety", 10018, 409, "multi-factor authentication policy safety requirements are not met", false)
	IdentityMFAStepUpRequired                = descriptor("identity", "identity.mfa.step_up_required", 10019, 403, "recent multi-factor authentication is required", false)

	AcademicTermInvalid             = descriptor("academic-term", "academic_term.invalid", 74001, 400, "学期请求无效", false)
	AcademicTermNotAvailable        = descriptor("academic-term", "academic_term.not_available", 74002, 404, "学期不存在或当前不可用", false)
	AcademicTermAlreadyExists       = descriptor("academic-term", "academic_term.already_exists", 74003, 409, "该年份和学期已存在", false)
	AcademicTermClosed              = descriptor("academic-term", "academic_term.closed", 74004, 409, "学期已关闭，不能执行该操作", false)
	AcademicTermDefaultUnavailable  = descriptor("academic-term", "academic_term.default_unavailable", 74005, 409, "当前没有可用的默认学期", false)
	AcademicTermVersionConflict     = descriptor("academic-term", "academic_term.version_conflict", 74006, 409, "学期已被其他管理员更新，请刷新后重试", false)
	AcademicTermHasSchedules        = descriptor("academic-term", "academic_term.has_schedules", 74007, 409, "该学期已有课表数据，不能删除", false)
	ScheduleObjectUnavailable       = descriptor("personal-schedule", "schedule.object_unavailable", 74008, 503, "课表数据暂时无法读取，请稍后重试", true)
	PersonalScheduleDisabled        = descriptor("personal-schedule", "schedule.feature_disabled", 74009, 503, "个人课表功能暂未启用", true)
	UserStorageQuotaExceeded        = descriptor("user-storage", "user_storage.quota_exceeded", 76001, 409, "个人空间不足，无法保存文件", false)
	PersonalDocumentDisabled        = descriptor("personal-documents", "personal_document.feature_disabled", 77001, 503, "我的文档功能暂未启用", true)
	PersonalDocumentInvalid         = descriptor("personal-documents", "personal_document.invalid", 77002, 400, "文档请求无效", false)
	PersonalDocumentNotFound        = descriptor("personal-documents", "personal_document.not_found", 77003, 404, "文档不存在或无权访问", false)
	PersonalDocumentTooLarge        = descriptor("personal-documents", "personal_document.too_large", 77004, 400, "文件超过该类型允许的大小", false)
	PersonalDocumentVersionConflict = descriptor("personal-documents", "personal_document.version_conflict", 77005, 409, "文档已被更新，请刷新后重试", false)
	PersonalDocumentNotEditable     = descriptor("personal-documents", "personal_document.not_editable", 77006, 409, "当前文档格式不支持在线编辑", false)

	MutualAidFeatureDisabled   = descriptor("mutual-aid", "mutual_aid.feature_disabled", 50301, 503, "mutual aid feature is disabled", true)
	MutualAidNotFound          = descriptor("mutual-aid", "mutual_aid.not_found", 40003, 404, "mutual aid thread not found", false)
	MutualAidForbidden         = descriptor("mutual-aid", "mutual_aid.forbidden", 20003, 403, "mutual aid thread belongs to another user", false)
	MutualAidInvalidInput      = descriptor("mutual-aid", "mutual_aid.invalid_input", 10001, 400, "mutual aid request is invalid", false)
	MutualAidInvalidTransition = descriptor("mutual-aid", "mutual_aid.invalid_transition", 10001, 400, "mutual aid status transition is invalid", false)
	MutualAidVersionConflict   = descriptor("mutual-aid", "mutual_aid.version_conflict", 40009, 409, "mutual aid detail version conflict", false)
	MutualAidNotEditable       = descriptor("mutual-aid", "mutual_aid.not_editable", 40009, 409, "mutual aid thread is no longer editable", false)

	SecondhandFeatureDisabled   = descriptor("secondhand", "secondhand.feature_disabled", 50301, 503, "secondhand feature is disabled", true)
	SecondhandNotFound          = descriptor("secondhand", "secondhand.not_found", 40003, 404, "secondhand thread not found", false)
	SecondhandForbidden         = descriptor("secondhand", "secondhand.forbidden", 20003, 403, "secondhand thread belongs to another user", false)
	SecondhandInvalidInput      = descriptor("secondhand", "secondhand.invalid_input", 10001, 400, "secondhand request is invalid", false)
	SecondhandInvalidTransition = descriptor("secondhand", "secondhand.invalid_transition", 10001, 400, "secondhand status transition is invalid", false)
	SecondhandVersionConflict   = descriptor("secondhand", "secondhand.version_conflict", 40009, 409, "secondhand detail version conflict", false)
	SecondhandNotEditable       = descriptor("secondhand", "secondhand.not_editable", 40009, 409, "secondhand thread is no longer editable", false)

	ReliabilitySummaryUnavailable       = descriptor("reliability", "reliability.summary.unavailable", 89101, 500, "reliability summary is unavailable", true)
	ReliabilityEventsUnavailable        = descriptor("reliability", "reliability.events.unavailable", 89102, 500, "reliable events are unavailable", true)
	ReliabilityAttemptsUnavailable      = descriptor("reliability", "reliability.attempts.unavailable", 89111, 500, "delivery attempts are unavailable", true)
	ReliabilityReplayUnavailable        = descriptor("reliability", "reliability.replay.unavailable", 89103, 500, "reliable event replay failed", true)
	ReliabilityEventNotFound            = descriptor("reliability", "reliability.event.not_found", 89103, 404, "reliable event was not found", false)
	ReliabilityEventNotReplayable       = descriptor("reliability", "reliability.event.not_replayable", 89103, 409, "reliable event is not in the dead-letter queue", false)
	ReliabilityReplayKeyRequired        = descriptor("reliability", "reliability.replay.idempotency_key_required", 89103, 409, "Idempotency-Key is required for dead-letter replay", false)
	ReliabilityReplayAlreadyRequested   = descriptor("reliability", "reliability.replay.already_requested", 89103, 409, "dead-letter replay was already requested", false)
	ReliabilityReplayActorRequired      = descriptor("reliability", "reliability.replay.actor_required", 89103, 401, "replay actor is required", false)
	ReliabilityWorkersUnavailable       = descriptor("reliability", "reliability.workers.unavailable", 89107, 500, "reliability workers are unavailable", true)
	ReliabilityOperationsUnavailable    = descriptor("reliability", "reliability.operations.unavailable", 89108, 500, "reliable operations are unavailable", true)
	ReliabilityAuditsUnavailable        = descriptor("reliability", "reliability.audits.unavailable", 89112, 500, "reliability command audits are unavailable", true)
	ReliabilityCompatibilityUnavailable = descriptor("reliability", "reliability.compatibility.unavailable", 89105, 500, "compatibility usage is unavailable", true)
	ReliabilityRetentionInvalid         = descriptor("reliability", "reliability.retention.invalid", 89106, 400, "retention preview request is invalid", false)
	ReliabilityRetentionStartInvalid    = descriptor("reliability", "reliability.retention.start_invalid", 89109, 400, "retention preview could not be started", false)
	ReliabilityRetentionRunsUnavailable = descriptor("reliability", "reliability.retention.runs_unavailable", 89110, 500, "retention runs are unavailable", true)
	ReliabilityRateLimited              = descriptor("reliability", "reliability.query.rate_limited", 89113, 429, "reliability query rate limit exceeded", true)
)

var catalog = []Descriptor{
	RequestInvalid, ResourceConflict, InternalError, AuthRequired, AuthInvalidToken, PermissionDenied, ResourceNotFound,
	IdentityChallengeInvalid, IdentityChallengeRateLimited, IdentityRegistrationVerificationRequired,
	IdentityAdminAdmissionInvalid, IdentityAdminAdmissionNotFound, IdentityAdminAdmissionVersionConflict,
	IdentityAdminAdmissionTransitionConflict, IdentityAdminAdmissionLastActive,
	IdentityMFAInvalid, IdentityMFATicketInvalid, IdentityMFAFactorInvalid, IdentityMFAEnrollmentRequired,
	IdentityMFAUnavailable, IdentityMFAReplay, IdentityMFANotEnabled, IdentityMFAPolicyInvalid, IdentityMFAPolicySafety,
	IdentityMFAStepUpRequired,
	AcademicTermInvalid, AcademicTermNotAvailable, AcademicTermAlreadyExists, AcademicTermClosed,
	AcademicTermDefaultUnavailable, AcademicTermVersionConflict, AcademicTermHasSchedules,
	ScheduleObjectUnavailable, PersonalScheduleDisabled,
	UserStorageQuotaExceeded, PersonalDocumentDisabled, PersonalDocumentInvalid, PersonalDocumentNotFound,
	PersonalDocumentTooLarge, PersonalDocumentVersionConflict, PersonalDocumentNotEditable,
	MutualAidFeatureDisabled, MutualAidNotFound, MutualAidForbidden, MutualAidInvalidInput,
	MutualAidInvalidTransition, MutualAidVersionConflict, MutualAidNotEditable,
	SecondhandFeatureDisabled, SecondhandNotFound, SecondhandForbidden, SecondhandInvalidInput,
	SecondhandInvalidTransition, SecondhandVersionConflict, SecondhandNotEditable,
	ReliabilitySummaryUnavailable, ReliabilityEventsUnavailable, ReliabilityAttemptsUnavailable,
	ReliabilityReplayUnavailable, ReliabilityEventNotFound, ReliabilityEventNotReplayable,
	ReliabilityReplayKeyRequired, ReliabilityReplayAlreadyRequested, ReliabilityReplayActorRequired,
	ReliabilityWorkersUnavailable, ReliabilityOperationsUnavailable, ReliabilityAuditsUnavailable,
	ReliabilityCompatibilityUnavailable, ReliabilityRetentionInvalid, ReliabilityRetentionStartInvalid,
	ReliabilityRetentionRunsUnavailable, ReliabilityRateLimited,
}

var catalogIndex = mustCatalogIndex(catalog)

func Catalog() []Descriptor {
	result := append([]Descriptor(nil), catalog...)
	sort.Slice(result, func(i, j int) bool { return result[i].MachineCode < result[j].MachineCode })
	return result
}

func ValidateCatalog(descriptors []Descriptor) error {
	seen := make(map[string]struct{}, len(descriptors))
	for _, item := range descriptors {
		if err := item.Validate(); err != nil {
			return err
		}
		if _, exists := seen[item.MachineCode]; exists {
			return fmt.Errorf("duplicate machine error code %s", item.MachineCode)
		}
		seen[item.MachineCode] = struct{}{}
	}
	return nil
}

func IsRegistered(descriptor Descriptor) bool {
	registered, ok := catalogIndex[descriptor.MachineCode]
	return ok && registered == descriptor
}

func descriptor(owner, machineCode string, legacyCode, httpStatus int, message string, retryable bool) Descriptor {
	return Descriptor{Owner: owner, MachineCode: machineCode, LegacyCode: legacyCode, HTTPStatus: httpStatus, Message: message, Retryable: retryable}
}

func mustCatalogIndex(descriptors []Descriptor) map[string]Descriptor {
	if err := ValidateCatalog(descriptors); err != nil {
		panic(err)
	}
	index := make(map[string]Descriptor, len(descriptors))
	for _, item := range descriptors {
		index[item.MachineCode] = item
	}
	return index
}
