package fault

import "crypto/rand"
import "errors"
import "encoding/hex"

type Error struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	Retryable     bool   `json:"retryable"`
	CorrelationID string `json:"correlation_id"`
	UpstreamCode  string `json:"upstream_code,omitempty"`
	RequestID     string `json:"upstream_request_id,omitempty"`
	Status        int    `json:"upstream_status,omitempty"`
	RetryAfter    string `json:"retry_after,omitempty"`
	Data          any    `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func New(code, message string) *Error {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return &Error{Code: code, Message: message, CorrelationID: hex.EncodeToString(b)}
}
func Wrap(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return New("INTERNAL_ERROR", "local operation failed")
}
