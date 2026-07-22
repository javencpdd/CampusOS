package apperror

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var machineCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$`)

// Descriptor is the stable public error contract. LegacyCode preserves the
// numeric CampusOS response field while MachineCode is the client-facing ID.
type Descriptor struct {
	Owner       string `json:"owner"`
	MachineCode string `json:"machine_code"`
	LegacyCode  int    `json:"legacy_code"`
	HTTPStatus  int    `json:"http_status"`
	Message     string `json:"message"`
	Retryable   bool   `json:"retryable"`
}

func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.Owner) == "" {
		return errors.New("error descriptor owner is required")
	}
	if !machineCodePattern.MatchString(d.MachineCode) {
		return fmt.Errorf("invalid machine error code %q", d.MachineCode)
	}
	if d.LegacyCode <= 0 {
		return fmt.Errorf("error descriptor %s requires a positive legacy code", d.MachineCode)
	}
	if d.HTTPStatus < 400 || d.HTTPStatus > 599 {
		return fmt.Errorf("error descriptor %s has invalid HTTP status %d", d.MachineCode, d.HTTPStatus)
	}
	if strings.TrimSpace(d.Message) == "" {
		return fmt.Errorf("error descriptor %s requires a safe message", d.MachineCode)
	}
	return nil
}

// AppError carries a registered public descriptor and optional safe details.
// The cause remains server-side and is never serialized by this package.
type AppError struct {
	descriptor Descriptor
	details    any
	cause      error
	httpStatus int
}

func New(descriptor Descriptor, details any) *AppError {
	return &AppError{descriptor: descriptor, details: details}
}

func Wrap(cause error, descriptor Descriptor, details any) *AppError {
	return &AppError{descriptor: descriptor, details: details, cause: cause}
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	return e.descriptor.Message
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *AppError) Descriptor() Descriptor {
	if e == nil {
		return Descriptor{}
	}
	return e.descriptor
}

func (e *AppError) Details() any {
	if e == nil {
		return nil
	}
	return e.details
}

func (e *AppError) HTTPStatus() int {
	if e == nil {
		return 0
	}
	if e.httpStatus >= 400 && e.httpStatus <= 599 {
		return e.httpStatus
	}
	return e.descriptor.HTTPStatus
}

// WithHTTPStatus is a compatibility bridge for a stable machine code that
// historically appeared with more than one transport status. The catalog's
// canonical status remains unchanged.
func (e *AppError) WithHTTPStatus(status int) *AppError {
	if e != nil && status >= 400 && status <= 599 {
		e.httpStatus = status
	}
	return e
}

type Rule struct {
	Target     error
	Descriptor Descriptor
}

type Translator struct {
	module   string
	fallback Descriptor
	rules    []Rule
}

func NewTranslator(module string, fallback Descriptor, rules ...Rule) (*Translator, error) {
	module = strings.TrimSpace(module)
	if module == "" {
		return nil, errors.New("error translator module is required")
	}
	if err := validateRegisteredDescriptor(fallback); err != nil {
		return nil, fmt.Errorf("translator %s fallback: %w", module, err)
	}
	copyRules := append([]Rule(nil), rules...)
	for index, rule := range copyRules {
		if rule.Target == nil {
			return nil, fmt.Errorf("translator %s rule %d target is required", module, index)
		}
		if err := validateRegisteredDescriptor(rule.Descriptor); err != nil {
			return nil, fmt.Errorf("translator %s rule %d: %w", module, index, err)
		}
	}
	return &Translator{module: module, fallback: fallback, rules: copyRules}, nil
}

func MustTranslator(module string, fallback Descriptor, rules ...Rule) *Translator {
	translator, err := NewTranslator(module, fallback, rules...)
	if err != nil {
		panic(err)
	}
	return translator
}

// Translate keeps an existing AppError intact, maps known domain sentinels,
// and wraps every unknown cause in the translator's safe fallback.
func (t *Translator) Translate(err error) *AppError {
	if err == nil {
		return nil
	}
	var public *AppError
	if errors.As(err, &public) {
		return public
	}
	if t == nil {
		return Wrap(err, InternalError, nil)
	}
	for _, rule := range t.rules {
		if errors.Is(err, rule.Target) {
			return Wrap(err, rule.Descriptor, nil)
		}
	}
	return Wrap(err, t.fallback, nil)
}

func validateRegisteredDescriptor(descriptor Descriptor) error {
	if err := descriptor.Validate(); err != nil {
		return err
	}
	if !IsRegistered(descriptor) {
		return fmt.Errorf("unregistered public error %s", descriptor.MachineCode)
	}
	return nil
}
