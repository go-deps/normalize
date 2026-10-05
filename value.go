package normalize

import (
	"reflect"
)

type valueKey struct {
	typ reflect.Type
	tag string
}

type valuePlan struct {
	levels []level
	err    error
}

// Var normalizes the value ptr points to, in place, as if it were a struct
// field of that type with tag:
//
//	err := n.Var(&email, "trim,email")
//	err := n.Var(&tags, "nilempty,dive,trim,lower")
//	err := n.Var(&limit, "default=20") // limit is a *int
//
// ptr must be a non-nil pointer, or ErrInvalidArgument is returned. Errors
// are reported as by Struct: a *TagError, whose Field is empty, for a tag
// that does not fit the type, and *FieldError values with paths relative to
// the value otherwise.
//
// Parsed tags are cached by type and text, so tag should be a constant
// rather than built from input. To parse a tag once and check it at start-up,
// use a Rule.
func (n *Normalizer) Var(ptr any, tag string) error {
	v := reflect.ValueOf(ptr)
	if n.plain == nil || v.Kind() != reflect.Pointer || v.IsNil() {
		return ErrInvalidArgument
	}
	vp := n.valuePlan(v.Elem().Type(), tag)
	if vp.err != nil {
		return vp.err
	}
	w := walker{n: n}
	w.value(v.Elem(), vp.levels)
	return w.result()
}

func (n *Normalizer) valuePlan(t reflect.Type, tag string) valuePlan {
	key := valueKey{typ: t, tag: tag}
	if vp, ok := n.values.Load(key); ok {
		return vp.(valuePlan)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var vp valuePlan
	var err error
	if tag != "" {
		vp.levels, err = n.parseLevels(tag)
		if err == nil {
			err = n.checkLevels(t, vp.levels)
		}
		if err != nil {
			err = &TagError{Type: t, Tag: tag, Err: err}
		}
	}
	if err == nil {
		_, err = n.checkNested(t, newBuildState(), map[reflect.Type]bool{})
	}
	if err != nil {
		vp = valuePlan{err: err}
	}
	n.values.Store(key, vp)
	return vp
}

// Apply normalizes v with tag, using the built-in operations and the plugins
// added with [Use], and returns the result:
//
//	email, err := normalize.Apply(in.Email, "trim,email")
//
// It works for any type Var accepts, including named string types. Slices and
// maps are changed in place, as they share memory with v. On error the
// result holds every part that could be normalized; a rejected string is
// returned unchanged.
func Apply[T any](v T, tag string) (T, error) { return ApplyWith(std, v, tag) }

// ApplyWith is Apply with the operations and options of n.
func ApplyWith[T any](n *Normalizer, v T, tag string) (T, error) {
	err := n.Var(&v, tag)
	return v, err
}

// Rule is a tag parsed and checked once for values of type T, and is safe
// for concurrent use. The operations of its own tag are fixed when it is
// compiled, even if the [Normalizer] is changed later; the tags of structs
// inside T are read by the Normalizer, as for [Normalizer.Struct].
type Rule[T any] struct {
	n      *Normalizer
	levels []level
}

// Compile parses tag for values of type T with the built-in operations and
// the plugins added with [Use].
func Compile[T any](tag string) (*Rule[T], error) { return CompileWith[T](std, tag) }

// CompileWith parses tag for values of type T with the operations of n.
func CompileWith[T any](n *Normalizer, tag string) (*Rule[T], error) {
	if n == nil || n.plain == nil {
		return nil, ErrInvalidArgument
	}
	vp := n.valuePlan(reflect.TypeFor[T](), tag)
	if vp.err != nil {
		return nil, vp.err
	}
	return &Rule[T]{n: n, levels: vp.levels}, nil
}

// MustCompile is like [Compile] but panics with the error, a *[TagError], if
// the tag cannot be applied. It is meant for package-level variables:
//
//	var cleanEmail = normalize.MustCompile[string]("trim,email")
func MustCompile[T any](tag string) *Rule[T] { return MustCompileWith[T](std, tag) }

// MustCompileWith is like [CompileWith] but panics with the error if the tag
// cannot be applied.
func MustCompileWith[T any](n *Normalizer, tag string) *Rule[T] {
	r, err := CompileWith[T](n, tag)
	if err != nil {
		panic(err)
	}
	return r
}

// Apply normalizes v and returns the result, as the package-level Apply.
func (r *Rule[T]) Apply(v T) (T, error) {
	err := r.Var(&v)
	return v, err
}

// Var normalizes the value ptr points to, in place. A nil ptr, or a Rule not
// made by Compile, returns [ErrInvalidArgument].
func (r *Rule[T]) Var(ptr *T) error {
	if ptr == nil || r == nil || r.n == nil {
		return ErrInvalidArgument
	}
	w := walker{n: r.n}
	w.value(reflect.ValueOf(ptr).Elem(), r.levels)
	return w.result()
}
