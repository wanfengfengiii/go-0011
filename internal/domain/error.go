package domain

import "fmt"

// Error is the stable business-error boundary shared by transports and storage.
type Error struct {
	Code      string         `json:"code"`
	Category  string         `json:"category"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Category, e.Code) }

func NewError(code, category string, retryable bool) *Error {
	return &Error{Code: code, Category: category, Retryable: retryable}
}

var (
	ErrIdentityConflict  = NewError("IDENTITY_CONFLICT", "CONFLICT", false)
	ErrVersionConflict   = NewError("VERSION_CONFLICT", "CONFLICT", true)
	ErrLateEvent         = NewError("LATE_EVENT", "ORDERING", false)
	ErrIllegalTransition = NewError("ILLEGAL_TRANSITION", "STATE", false)
	ErrAbnormalLoadCurve = NewError("ABNORMAL_LOAD_CURVE", "DEVICE_DATA", false)
	ErrUnitOrDimension   = NewError("UNIT_OR_DIMENSION_ERROR", "VALIDATION", false)
)
