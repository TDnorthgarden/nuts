package libdslgo

import (
	"fmt"
)

// ErrorType represents the category of error
type ErrorType int

const (
	ErrorTypeParse      ErrorType = iota // Parsing errors
	ErrorTypeEval                        // Evaluation errors
	ErrorTypeValidation                  // Validation errors
	ErrorTypeRuntime                     // Runtime errors
	ErrorTypeSecurity                    // Security errors
)

// ErrorCode represents specific error codes
type ErrorCode string

const (
	// Parse errors
	ErrCodeInvalidSyntax      ErrorCode = "PARSE_INVALID_SYNTAX"
	ErrCodeUnexpectedToken    ErrorCode = "PARSE_UNEXPECTED_TOKEN"
	ErrCodeUnterminatedString ErrorCode = "PARSE_UNTERMINATED_STRING"
	ErrCodeInvalidNumber      ErrorCode = "PARSE_INVALID_NUMBER"
	ErrCodeInvalidOperator    ErrorCode = "PARSE_INVALID_OPERATOR"
	ErrCodeMissingOperand     ErrorCode = "PARSE_MISSING_OPERAND"
	ErrCodeUnmatchedParen     ErrorCode = "PARSE_UNMATCHED_PAREN"

	// Evaluation errors
	ErrCodeFieldNotFound     ErrorCode = "EVAL_FIELD_NOT_FOUND"
	ErrCodeInvalidComparison ErrorCode = "EVAL_INVALID_COMPARISON"
	ErrCodeInvalidOperation  ErrorCode = "EVAL_INVALID_OPERATION"
	ErrCodeTypeMismatch      ErrorCode = "EVAL_TYPE_MISMATCH"
	ErrCodeDepthExceeded     ErrorCode = "EVAL_DEPTH_EXCEEDED"
	ErrCodeTimeout           ErrorCode = "EVAL_TIMEOUT"
	ErrCodeCancelled         ErrorCode = "EVAL_CANCELLED"

	// Validation errors
	ErrCodeInvalidRuleName  ErrorCode = "VALID_INVALID_RULE_NAME"
	ErrCodeInvalidMacroName ErrorCode = "VALID_INVALID_MACRO_NAME"
	ErrCodeInvalidListName  ErrorCode = "VALID_INVALID_LIST_NAME"
	ErrCodeMissingField     ErrorCode = "VALID_MISSING_FIELD"
	ErrCodeInvalidValue     ErrorCode = "VALID_INVALID_VALUE"

	// Security errors
	ErrCodeRegexTooComplex  ErrorCode = "SECURITY_REGEX_TOO_COMPLEX"
	ErrCodeDangerousPattern ErrorCode = "SECURITY_DANGEROUS_PATTERN"
	ErrCodeResourceLimit    ErrorCode = "SECURITY_RESOURCE_LIMIT"
	ErrCodeInvalidInput     ErrorCode = "SECURITY_INVALID_INPUT"

	// Runtime errors
	ErrCodeMacroNotFound ErrorCode = "RUNTIME_MACRO_NOT_FOUND"
	ErrCodeListNotFound  ErrorCode = "RUNTIME_LIST_NOT_FOUND"
	ErrCodeRuleNotFound  ErrorCode = "RUNTIME_RULE_NOT_FOUND"
	ErrCodeEngineState   ErrorCode = "RUNTIME_ENGINE_STATE"
)

// DslError represents a custom DSL error with type, code, and position information
type DslError struct {
	Type    ErrorType
	Code    ErrorCode
	Message string
	Pos     *Position
	Cause   error
}

func (e *DslError) Error() string {
	if e.Pos != nil {
		return fmt.Sprintf("[%s] %s (line %d, column %d): %v", e.Code, e.Message, e.Pos.Line, e.Pos.Column, e.Cause)
	}
	return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
}

func (e *DslError) Unwrap() error {
	return e.Cause
}

// IsCode checks if the error matches the given error code
func (e *DslError) IsCode(code ErrorCode) bool {
	return e.Code == code
}

// NewParseError creates a new parse error
func NewParseError(code ErrorCode, message string, pos *Position, cause error) *DslError {
	return &DslError{
		Type:    ErrorTypeParse,
		Code:    code,
		Message: message,
		Pos:     pos,
		Cause:   cause,
	}
}

// NewEvalError creates a new evaluation error
func NewEvalError(code ErrorCode, message string, cause error) *DslError {
	return &DslError{
		Type:    ErrorTypeEval,
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// NewValidationError creates a new validation error
func NewValidationError(code ErrorCode, message string, cause error) *DslError {
	return &DslError{
		Type:    ErrorTypeValidation,
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// NewSecurityError creates a new security error
func NewSecurityError(code ErrorCode, message string, cause error) *DslError {
	return &DslError{
		Type:    ErrorTypeSecurity,
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// NewRuntimeError creates a new runtime error
func NewRuntimeError(code ErrorCode, message string, cause error) *DslError {
	return &DslError{
		Type:    ErrorTypeRuntime,
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}
