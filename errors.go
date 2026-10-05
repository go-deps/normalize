package normalize

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	// ErrInvalidArgument is returned when the value passed to Struct is not a
	// non-nil pointer to a struct.
	ErrInvalidArgument = errors.New("normalize: argument must be a non-nil pointer to a struct")

	// ErrUnknownOperation is wrapped by a TagError naming an operation that is
	// neither built in nor registered.
	ErrUnknownOperation = errors.New("unknown operation")

	// ErrUnsupportedField is wrapped by a TagError whose operations cannot apply
	// to the field's type, for example trim on an int or dive on a string.
	ErrUnsupportedField = errors.New("operations not supported on this field type")

	// ErrMalformedTag is wrapped by a TagError whose tag value cannot be parsed,
	// for example an empty element or a repeated dive.
	ErrMalformedTag = errors.New("malformed tag")

	// ErrInvalidTarget is returned when an Unwrapper's NormalizeTarget returns
	// something other than nil or a non-nil pointer.
	ErrInvalidTarget = errors.New("normalize: NormalizeTarget must return nil or a non-nil pointer")
)

// TagError reports a struct tag that cannot be applied. It wraps one of
// ErrUnknownOperation, ErrUnsupportedField or ErrMalformedTag, so callers can
// use errors.Is.
type TagError struct {
	Type  reflect.Type // the struct type declaring the field
	Field string       // the Go field name
	Tag   string       // the full tag value
	Err   error
}

func (e *TagError) Error() string {
	return fmt.Sprintf("normalize: %s.%s: tag %q: %v", e.Type, e.Field, e.Tag, e.Err)
}

func (e *TagError) Unwrap() error { return e.Err }
