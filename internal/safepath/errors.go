package safepath

import (
	"errors"
	"fmt"
)

// ErrorCode identifies deterministic fail-closed Grabtaculous diagnostic categories.
type ErrorCode string

const (
	ErrPreflightFailed ErrorCode = "GRABTAC-E101"
	ErrPathConfinement ErrorCode = "GRABTAC-E201"
	ErrCatalogInvalid  ErrorCode = "GRABTAC-E301"
	ErrSecurityPolicy  ErrorCode = "GRABTAC-E401"
	ErrControlPlane    ErrorCode = "GRABTAC-E501"
	ErrRolloutFailed   ErrorCode = "GRABTAC-E601"
	ErrSnapshotFailed  ErrorCode = "GRABTAC-E701"
)

// DiagError represents a structured, plain-English fail-closed error with actionable remediation.
type DiagError struct {
	Code        ErrorCode `json:"code"`
	Message     string    `json:"message"`
	Remediation string    `json:"remediation,omitempty"`
	Cause       error     `json:"-"`
}

func (e *DiagError) Error() string {
	if e == nil {
		return ""
	}
	if e.Remediation != "" {
		return fmt.Sprintf("%s: %s (Fix: %s)", e.Code, e.Message, e.Remediation)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *DiagError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewError creates a new structured DiagError.
func NewError(code ErrorCode, message, remediation string) *DiagError {
	return &DiagError{
		Code:        code,
		Message:     message,
		Remediation: remediation,
	}
}

// WrapError wraps an underlying error with a structured ErrorCode and remediation hint.
func WrapError(code ErrorCode, message, remediation string, cause error) *DiagError {
	return &DiagError{
		Code:        code,
		Message:     message,
		Remediation: remediation,
		Cause:       cause,
	}
}

// ExtractCode returns the ErrorCode if err wraps a *DiagError, otherwise empty.
func ExtractCode(err error) ErrorCode {
	var de *DiagError
	if errors.As(err, &de) {
		return de.Code
	}
	return ""
}
