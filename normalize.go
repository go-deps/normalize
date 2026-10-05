package normalize

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// DefaultTagName is the struct tag key read by a Normalizer created without
// WithTagName.
const DefaultTagName = "normalize"

const (
	skipTag = "-"
	diveTag = "dive"
)

// Func is a string operation that can be named in a tag. It should be
// idempotent (f(f(s)) == f(s)): a string shared by several fields is passed
// through each field's operations.
type Func func(string) string

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

var unwrapperType = reflect.TypeFor[Unwrapper]()

// Option configures a Normalizer.
type Option func(*Normalizer)

// WithTagName sets the struct tag key the Normalizer reads instead of
// DefaultTagName.
func WithTagName(name string) Option {
	return func(n *Normalizer) { n.tagName = name }
}

// Normalizer holds a set of operations and a per-type cache of parsed tags.
// The zero value is not usable; create one with New.
type Normalizer struct {
	tagName string

	mu    sync.Mutex // guards funcs and plan building
	funcs map[string]Func
	cache sync.Map // reflect.Type -> planResult
}

// New returns a Normalizer with the built-in operations registered.
func New(opts ...Option) *Normalizer {
	n := &Normalizer{
		tagName: DefaultTagName,
		funcs: map[string]Func{
			"trim":     strings.TrimSpace,
			"lower":    strings.ToLower,
			"upper":    strings.ToUpper,
			"collapse": collapse,
		},
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

var std = New()

// Struct normalizes ptr, which must be a non-nil pointer to a struct, with the
// built-in operations and the DefaultTagName tag key.
func Struct(ptr any) error { return std.Struct(ptr) }

// Register adds or replaces the operation called name. The name must be
// non-empty, must not contain a comma or white space, and must not be one of
// the reserved names "-" and "dive". Registering discards the type cache.
func (n *Normalizer) Register(name string, fn Func) error {
	switch {
	case fn == nil:
		return fmt.Errorf("normalize: Register %q: nil Func", name)
	case name == "" || name == skipTag || name == diveTag:
		return fmt.Errorf("normalize: Register %q: reserved or empty name", name)
	case strings.ContainsFunc(name, func(r rune) bool { return r == ',' || isSpace(r) }):
		return fmt.Errorf("normalize: Register %q: name must not contain a comma or white space", name)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.funcs[name] = fn
	n.cache.Clear()
	return nil
}

// Struct normalizes ptr, which must be a non-nil pointer to a struct, in
// place. It returns ErrInvalidArgument for any other argument, a *TagError
// for a malformed tag anywhere in the type, and ErrInvalidTarget for a
// misbehaving Unwrapper.
func (n *Normalizer) Struct(ptr any) error {
	v := reflect.ValueOf(ptr)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return ErrInvalidArgument
	}
	w := walker{n: n}
	return w.walkStruct(v.Elem())
}

// fieldPlan is the parsed tag of one field.
type fieldPlan struct {
	index    int
	name     string
	ops      []Func // applied to the field itself
	elemOps  []Func // applied to each element, after dive
	dive     bool
	traverse bool // the field's type may contain something to normalize
}

type planResult struct {
	fields []fieldPlan
	err    error
}

func (n *Normalizer) plan(t reflect.Type) planResult {
	if r, ok := n.cache.Load(t); ok {
		return r.(planResult)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.buildLocked(t, map[reflect.Type]bool{})
}

// buildLocked parses t and, recursively, every struct type reachable from its
// fields, so a malformed tag in a nested type is reported even when the nested
// value is nil. inProgress breaks cycles in recursive types.
func (n *Normalizer) buildLocked(t reflect.Type, inProgress map[reflect.Type]bool) planResult {
	if r, ok := n.cache.Load(t); ok {
		return r.(planResult)
	}
	if inProgress[t] {
		return planResult{}
	}
	inProgress[t] = true

	var res planResult
	for i := range t.NumField() {
		sf := t.Field(i)
		embeddedStruct := sf.Anonymous && sf.Type.Kind() == reflect.Struct
		if !sf.IsExported() && !embeddedStruct {
			continue
		}
		tag, hasTag := sf.Tag.Lookup(n.tagName)
		if tag == skipTag {
			continue
		}
		fp, err := n.parseField(t, sf, tag, hasTag)
		if err != nil {
			res = planResult{err: err}
			break
		}
		if err := n.checkNested(sf.Type, inProgress); err != nil {
			res = planResult{err: err}
			break
		}
		if fp.traverse || len(fp.ops) > 0 || fp.dive {
			res.fields = append(res.fields, fp)
		}
	}
	n.cache.Store(t, res)
	return res
}

func (n *Normalizer) parseField(owner reflect.Type, sf reflect.StructField, tag string, hasTag bool) (fieldPlan, error) {
	fp := fieldPlan{index: sf.Index[0], name: sf.Name}
	tagErr := func(err error) error {
		return &TagError{Type: owner, Field: sf.Name, Tag: tag, Err: err}
	}
	if hasTag && tag != "" {
		for part := range strings.SplitSeq(tag, ",") {
			switch part {
			case "":
				return fp, tagErr(fmt.Errorf("%w: empty element", ErrMalformedTag))
			case diveTag:
				if fp.dive {
					return fp, tagErr(fmt.Errorf("%w: dive given more than once", ErrMalformedTag))
				}
				fp.dive = true
			case skipTag:
				return fp, tagErr(fmt.Errorf("%w: %q must be the whole tag", ErrMalformedTag, skipTag))
			default:
				fn, ok := n.funcs[part]
				if !ok {
					return fp, tagErr(fmt.Errorf("%w %q", ErrUnknownOperation, part))
				}
				if fp.dive {
					fp.elemOps = append(fp.elemOps, fn)
				} else {
					fp.ops = append(fp.ops, fn)
				}
			}
		}
	}

	ft := deref(sf.Type)
	switch {
	case implementsUnwrapper(ft):
		// The wrapped type is only known at run time.
		fp.traverse = true
	case ft.Kind() == reflect.String:
		if fp.dive {
			return fp, tagErr(fmt.Errorf("%w: dive on %s", ErrUnsupportedField, sf.Type))
		}
	case ft.Kind() == reflect.Struct:
		if len(fp.ops) > 0 || fp.dive {
			return fp, tagErr(fmt.Errorf("%w: %s", ErrUnsupportedField, sf.Type))
		}
		fp.traverse = true
	case ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array:
		if len(fp.ops) > 0 {
			return fp, tagErr(fmt.Errorf("%w: operations before dive on %s", ErrUnsupportedField, sf.Type))
		}
		et := deref(ft.Elem())
		switch {
		case implementsUnwrapper(et):
			fp.traverse = true
		case et.Kind() == reflect.String:
		case et.Kind() == reflect.Struct:
			if len(fp.elemOps) > 0 {
				return fp, tagErr(fmt.Errorf("%w: operations after dive on %s", ErrUnsupportedField, sf.Type))
			}
			fp.traverse = true
		default:
			if fp.dive {
				return fp, tagErr(fmt.Errorf("%w: dive on %s", ErrUnsupportedField, sf.Type))
			}
		}
	default:
		if len(fp.ops) > 0 || fp.dive {
			return fp, tagErr(fmt.Errorf("%w: %s", ErrUnsupportedField, sf.Type))
		}
	}
	return fp, nil
}

// checkNested parses the struct types a field of type t can hold.
func (n *Normalizer) checkNested(t reflect.Type, inProgress map[reflect.Type]bool) error {
	t = deref(t)
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = deref(t.Elem())
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return n.buildLocked(t, inProgress).err
}

// walker carries the state of one Struct call. It records every struct and
// slice entered through a pointer or a slice header, so a cyclic or shared
// value is walked once. The first visits fit in an inline array; the map is
// created only for larger inputs.
type walker struct {
	n       *Normalizer
	inline  [16]visit
	nInline int
	visited map[visit]struct{}
}

type visit struct {
	addr uintptr
	typ  reflect.Type
	len  int
}

// markVisited records v, a non-nil pointer to a struct or a non-empty slice,
// and reports whether it was seen before. Only containers are recorded: a
// struct holds no operations of its own, and a slice is recorded only when no
// element operations apply, so skipping a repeat visit never skips work that a
// different tag would have done.
func (w *walker) markVisited(v reflect.Value) (seen bool) {
	key := visit{addr: v.Pointer(), typ: v.Type()}
	if v.Kind() == reflect.Slice {
		key.len = v.Len()
	}
	for _, k := range w.inline[:w.nInline] {
		if k == key {
			return true
		}
	}
	if _, ok := w.visited[key]; ok {
		return true
	}
	if w.nInline < len(w.inline) {
		w.inline[w.nInline] = key
		w.nInline++
		return false
	}
	if w.visited == nil {
		w.visited = make(map[visit]struct{})
	}
	w.visited[key] = struct{}{}
	return false
}

func (w *walker) walkStruct(v reflect.Value) error {
	res := w.n.plan(v.Type())
	if res.err != nil {
		return res.err
	}
	for _, fp := range res.fields {
		if err := w.normalizeValue(v.Field(fp.index), fp.ops, fp.elemOps, fp.dive); err != nil {
			return err
		}
	}
	return nil
}

// normalizeValue applies ops to v, or to each element of v after dive. v is
// addressable: it is always reached through a pointer.
func (w *walker) normalizeValue(v reflect.Value, ops, elemOps []Func, dive bool) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		if v.Elem().Kind() == reflect.Struct && w.markVisited(v) {
			return nil
		}
		return w.normalizeValue(v.Elem(), ops, elemOps, dive)
	}
	if v.CanAddr() && v.Addr().CanInterface() {
		if u, ok := v.Addr().Interface().(Unwrapper); ok {
			return w.unwrap(u, ops, elemOps, dive)
		}
	}
	switch v.Kind() {
	case reflect.String:
		if len(ops) == 0 {
			return nil
		}
		s := v.String()
		for _, fn := range ops {
			s = fn(s)
		}
		v.SetString(s)
	case reflect.Struct:
		return w.walkStruct(v)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Len() > 0 && len(elemOps) == 0 && w.markVisited(v) {
			return nil
		}
		for i := range v.Len() {
			if err := w.normalizeValue(v.Index(i), elemOps, nil, false); err != nil {
				return err
			}
		}
	default:
		if len(ops) > 0 || dive {
			return fmt.Errorf("normalize: %w: %s", ErrUnsupportedField, v.Type())
		}
	}
	return nil
}

func (w *walker) unwrap(u Unwrapper, ops, elemOps []Func, dive bool) error {
	target := u.NormalizeTarget()
	if target == nil {
		return nil
	}
	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer {
		return ErrInvalidTarget
	}
	if tv.IsNil() {
		return nil
	}
	elem := tv.Elem()
	if t, ok := target.(Unwrapper); ok && t == u {
		return fmt.Errorf("%w: %T returns a pointer to itself", ErrInvalidTarget, u)
	}
	if elem.Kind() == reflect.Struct && w.markVisited(tv) {
		return nil
	}
	return w.normalizeValue(elem, ops, elemOps, dive)
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func implementsUnwrapper(t reflect.Type) bool {
	return t.Kind() != reflect.Interface && reflect.PointerTo(t).Implements(unwrapperType)
}
