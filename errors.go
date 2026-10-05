package normalize

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

var (
	// ErrInvalidArgument is returned when the value passed to Struct is not a
	// non-nil pointer to a struct, or the value passed to Var or [Rule.Var]
	// is not a non-nil pointer. It is also returned by the methods of a
	// Normalizer or [Rule] that was not created with [New] or [Compile].
	ErrInvalidArgument = errors.New("normalize: argument must be a non-nil pointer (to a struct, for Struct)")

	// ErrUnknownOperation is wrapped by a [TagError] naming an operation that
	// is neither built in nor registered.
	ErrUnknownOperation = errors.New("unknown operation")

	// ErrUnsupportedField is wrapped by a [TagError] whose operations cannot
	// apply to the field's type, for example trim on an int or dive on a
	// string. It is also wrapped by a [FieldError] for a map key that cannot
	// be stored back, such as a NaN float key.
	ErrUnsupportedField = errors.New("operations not supported on this field type")

	// ErrMalformedTag is wrapped by a [TagError] whose tag value cannot be
	// parsed, for example an empty element, an unterminated quote or an
	// invalid argument.
	ErrMalformedTag = errors.New("malformed tag")

	// ErrInvalidTarget is wrapped by a [FieldError] when an [Unwrapper]'s
	// NormalizeTarget returns something other than nil or a non-nil pointer.
	ErrInvalidTarget = errors.New("NormalizeTarget must return nil or a non-nil pointer")

	// ErrKeyCollision is wrapped by a [FieldError] when normalizing the keys
	// of a map would turn two keys into one. The keys are left unchanged; the
	// values are still normalized.
	ErrKeyCollision = errors.New("normalized map keys collide")

	// ErrTooDeep is wrapped by a [FieldError] when a value is nested more
	// deeply than the limit set with [WithMaxDepth]. Values nested deeper are
	// not normalized.
	ErrTooDeep = errors.New("value nested too deeply")

	// ErrTooManyErrors is wrapped by the last error of a call that had more
	// errors than the limit set with [WithMaxErrors]; the message tells how
	// many were not reported.
	ErrTooManyErrors = errors.New("too many errors")

	// ErrConflict is wrapped by the error of [Normalizer.Use] when a plugin
	// adds an operation that was registered or added by another plugin, or
	// when another plugin value is used under a name already in use.
	ErrConflict = errors.New("plugin conflict")
)

// TagError reports a struct tag that cannot be applied. It wraps one of
// [ErrUnknownOperation], [ErrUnsupportedField] or [ErrMalformedTag], so
// callers can use [errors.Is].
type TagError struct {
	Type  reflect.Type // the struct declaring the field, or the value type for Var, Apply and Rule
	Field string       // the Go field name; empty for Var, Apply and Rule
	Tag   string       // the full tag value
	Err   error
}

func (e *TagError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("normalize: %s: tag %q: %v", e.Type, e.Tag, e.Err)
	}
	return fmt.Sprintf("normalize: %s.%s: tag %q: %v", e.Type, e.Field, e.Tag, e.Err)
}

func (e *TagError) Unwrap() error { return e.Err }

// FieldError reports a value that could not be normalized. Err is the error
// of an operation that rejected the value, of a failed AfterNormalize
// method, [ErrKeyCollision], [ErrInvalidTarget], [ErrTooDeep],
// [ErrUnsupportedField] for a map key that cannot be stored back, or a
// [TagError] of a type reached through an [Unwrapper]. The value is left as
// it was.
type FieldError struct {
	// Path locates the value from the root struct, as in Users[2].Email,
	// Labels["en"] or Counts[7]; it is empty for the root value itself.
	Path string
	// Op is the tag element that failed, as in "truncate=64", "keys" or
	// "AfterNormalize"; it is empty when no operation was running.
	Op string
	// Err is the cause.
	Err error
}

func (e *FieldError) Error() string {
	cause := strings.TrimPrefix(fmt.Sprint(e.Err), "normalize: ")
	switch {
	case e.Path == "" && e.Op == "":
		return "normalize: " + cause
	case e.Path == "":
		return fmt.Sprintf("normalize: %s: %s", e.Op, cause)
	case e.Op == "":
		return fmt.Sprintf("normalize: %s: %s", e.Path, cause)
	}
	return fmt.Sprintf("normalize: %s: %s: %s", e.Path, e.Op, cause)
}

func (e *FieldError) Unwrap() error { return e.Err }
