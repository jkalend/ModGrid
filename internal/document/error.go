// Package document owns strict local JSON contracts and canonical identities.
package document

const MaxDocumentBytes = 4 << 20

type Error struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Reasons []string `json:"reasons"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func Fail(code, message string) *Error {
	return &Error{Code: code, Message: message, Reasons: []string{}}
}
func failure(code, message string) *Error { return Fail(code, message) }
