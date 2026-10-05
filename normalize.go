package normalize

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// DefaultTagName is the main struct tag key. A Normalizer created without
// WithTagName reads it and its short form "norm".
const DefaultTagName = "normalize"

var defaultTagNames = []string{DefaultTagName, "norm"}

const (
	skipTag     = "-"
	diveTag     = "dive"
	keysTag     = "keys"
	nilEmptyTag = "nilempty"
	defaultTag  = "default"
)

func isReserved(name string) bool {
	switch name {
	case skipTag, diveTag, keysTag, nilEmptyTag, defaultTag:
		return true
	}
	return false
}

// Func is a string operation that can be named in a tag. It should be
// idempotent (f(f(s)) == f(s)): a string shared by several fields is passed
// through each field's operations.
type Func func(string) string

// CheckFunc is an operation that can reject its input. When it returns an
// error the field keeps its previous value and the error is reported in a
// *FieldError. Like Func, it should be idempotent.
type CheckFunc func(string) (string, error)

// ParamFunc builds an operation from the argument written after "=" in a tag,
// as in truncate=64. It is called when the tag is parsed, before any value is
// changed, so an invalid argument is reported as a *[TagError]. Tags are
// parsed again after the operations change, so it should not have side
// effects, and it must not call the Normalizer it is registered with.
type ParamFunc func(arg string) (CheckFunc, error)

// Unwrapper is implemented by types that wrap a value, such as an optional
// field, so that the field's operations reach the wrapped value.
//
// NormalizeTarget returns a pointer to the wrapped value, or nil when there is
// nothing to normalize (for example an absent optional). It is looked up on a
// pointer to the field, so a pointer receiver works:
//
//	type Optional[T any] struct {
//		Value T
//		Set   bool
//	}
//
//	func (o *Optional[T]) NormalizeTarget() any {
//		if !o.Set {
//			return nil
//		}
//		return &o.Value
//	}
//
// The wrapped value is handled like a field of its own type with the same
// tag. Because its type is only known at run time, an operation that does not
// fit the wrapped type is reported when a value is present, not when the
// enclosing struct type is first parsed.
type Unwrapper interface {
	NormalizeTarget() any
}

// AfterNormalizer is implemented by struct types that need a final step tags
// cannot express. AfterNormalize is called on a pointer to the struct after
// all of its fields, including nested structs, have been normalized. An error
// it returns is reported in a *FieldError.
//
// As with any method, a struct that embeds an AfterNormalizer is itself one:
// only the outer method is called, and an outer type that declares its own
// AfterNormalize calls the embedded one if it needs to.
type AfterNormalizer interface {
	AfterNormalize() error
}

var (
	unwrapperType       = reflect.TypeFor[Unwrapper]()
	afterNormalizerType = reflect.TypeFor[AfterNormalizer]()
)

// Option configures a Normalizer.
type Option func(*Normalizer)

// WithTagName sets the struct tag keys the Normalizer reads instead of
// "normalize" and "norm", for example to add the shortest form:
//
//	normalize.New(normalize.WithTagName("normalize", "norm", "n"))
//
// A field that carries more than one of the keys is a *[TagError]. Empty and
// repeated names are ignored; calling it without names keeps the defaults.
func WithTagName(names ...string) Option {
	return func(n *Normalizer) {
		var keep []string
		for _, name := range names {
			if name != "" && !slices.Contains(keep, name) {
				keep = append(keep, name)
			}
		}
		if len(keep) > 0 {
			n.tagNames = keep
		}
	}
}

// WithFieldName sets how fields are named in [FieldError] paths, for example
// after their JSON names. A function returning "" keeps the Go field name.
func WithFieldName(fn func(reflect.StructField) string) Option {
	return func(n *Normalizer) { n.fieldName = fn }
}

const (
	defaultMaxDepth  = 10000
	maxMaxDepth      = 100000
	defaultMaxErrors = 100
)

// WithMaxDepth sets how deeply nested a value may be, counting each struct,
// pointer, slice, array and map on the way, 10000 by default. A value nested
// more deeply is not normalized past the limit, and a [FieldError] wrapping
// [ErrTooDeep] is reported. The limit protects the stack from values built
// without a nesting limit; encoding/json stops at the same depth. A limit
// below 1 keeps the default, and one above 100000 is lowered to it, as a
// deeper walk could exhaust the goroutine stack.
func WithMaxDepth(depth int) Option {
	return func(n *Normalizer) {
		if depth > 0 {
			n.maxDepth = min(depth, maxMaxDepth)
		}
	}
}

// WithMaxErrors sets how many errors one call reports, 100 by default.
// Normalization goes on past the limit, and the number of errors left out is
// reported in a final error wrapping [ErrTooManyErrors], so that a large
// input that fails everywhere does not build a huge error. A limit below 1
// keeps the default.
func WithMaxErrors(count int) Option {
	return func(n *Normalizer) {
		if count > 0 {
			n.maxErrors = count
		}
	}
}

// Normalizer holds a set of operations and a per-type cache of parsed tags.
// It is safe for concurrent use. The zero value is not usable: its methods
// return [ErrInvalidArgument]. Create one with New.
type Normalizer struct {
	tagNames  []string
	fieldName func(reflect.StructField) string
	maxDepth  int
	maxErrors int

	mu       sync.Mutex // guards plain, param, owners, plugins and plan building
	plain    map[string]CheckFunc
	param    map[string]ParamFunc
	owners   map[string]string // operation form ("name" or "name=") -> who added it; built-ins have none
	plugins  map[string]Plugin
	cache    sync.Map // reflect.Type -> *planResult
	values   sync.Map // valueKey -> valuePlan, for Var, Apply and Rule
	contains sync.Map // reflect.Type -> bool, see mayContain
}

// New returns a Normalizer with the built-in operations registered.
func New(opts ...Option) *Normalizer {
	n := &Normalizer{
		tagNames:  defaultTagNames,
		maxDepth:  defaultMaxDepth,
		maxErrors: defaultMaxErrors,
		plain:     make(map[string]CheckFunc, len(builtinPlain)),
		param:     make(map[string]ParamFunc, len(builtinParam)),
		owners:    map[string]string{},
		plugins:   map[string]Plugin{},
	}
	for name, fn := range builtinPlain {
		n.plain[name] = wrap(fn)
	}
	for name, fn := range builtinParam {
		n.param[name] = fn
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

var std = New()

// Struct normalizes ptr, which must be a non-nil pointer to a struct, with the
// built-in operations, the plugins added with Use and the default tag keys.
func Struct(ptr any) error { return std.Struct(ptr) }

// Var normalizes the value ptr points to with tag, using the built-in
// operations and the plugins added with [Use]. See [Normalizer.Var].
func Var(ptr any, tag string) error { return std.Var(ptr, tag) }

func wrap(fn Func) CheckFunc {
	return func(s string) (string, error) { return fn(s), nil }
}

// Register adds or replaces the operation called name, used without an
// argument. The name must be non-empty, must not contain white space or any
// of the characters , = ( ) ', and must not be one of the reserved names
// "-", "dive", "keys", "nilempty" and "default". Registering discards the
// type cache.
//
// A name can have both a plain form (Register, RegisterCheck) and a form with
// an argument (RegisterParam); the tag picks one by the presence of "=".
func (n *Normalizer) Register(name string, fn Func) error {
	if fn == nil {
		return fmt.Errorf("normalize: Register %q: nil Func", name)
	}
	return n.RegisterCheck(name, wrap(fn))
}

// RegisterCheck is like Register for an operation that can reject its input.
func (n *Normalizer) RegisterCheck(name string, fn CheckFunc) error {
	if fn == nil {
		return fmt.Errorf("normalize: Register %q: nil CheckFunc", name)
	}
	if err := checkName(name); err != nil {
		return err
	}
	if n.plain == nil {
		return ErrInvalidArgument
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.plain[name] = fn
	n.owners[name] = registeredOwner
	n.clearCaches()
	return nil
}

// RegisterParam adds or replaces the form of the operation called name that
// takes an argument, as in name=arg. Names follow the rules of Register.
func (n *Normalizer) RegisterParam(name string, fn ParamFunc) error {
	if fn == nil {
		return fmt.Errorf("normalize: Register %q: nil ParamFunc", name)
	}
	if err := checkName(name); err != nil {
		return err
	}
	if n.plain == nil {
		return ErrInvalidArgument
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.param[name] = fn
	n.owners[name+"="] = registeredOwner
	n.clearCaches()
	return nil
}

func checkName(name string) error {
	switch {
	case name == "" || isReserved(name):
		return fmt.Errorf("normalize: Register %q: reserved or empty name", name)
	case strings.ContainsFunc(name, func(r rune) bool { return strings.ContainsRune(",=()'", r) || isSpace(r) }):
		return fmt.Errorf("normalize: Register %q: name must not contain white space or any of , = ( ) '", name)
	}
	return nil
}

func (n *Normalizer) clearCaches() {
	n.cache.Clear()
	n.values.Clear()
}

// Struct normalizes ptr, which must be a non-nil pointer to a struct, in
// place.
//
// It returns [ErrInvalidArgument] for any other argument and a *[TagError]
// for a malformed tag anywhere in the type; in both cases nothing is
// changed. Otherwise every field is processed, and the errors of operations
// that rejected their input, of AfterNormalize methods, of misbehaving
// Unwrappers, key collisions and the depth limit are returned together as
// *[FieldError] values joined with [errors.Join], followed by an error
// wrapping [ErrTooManyErrors] when the error limit is reached.
func (n *Normalizer) Struct(ptr any) error {
	v := reflect.ValueOf(ptr)
	if n.plain == nil || v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return ErrInvalidArgument
	}
	if res := n.plan(v.Elem().Type()); res.err != nil {
		return res.err
	}
	w := walker{n: n}
	w.walkStruct(v.Elem())
	return w.result()
}
