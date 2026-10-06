package normalize

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
	"unsafe"
)

// walker carries the state of one Struct or Var call: the visit record that
// keeps a cyclic or shared value from being walked twice, the path to the
// current value for error messages, and the errors collected so far. The
// first visits and path segments fit in inline arrays, and larger records
// are reused across calls, so a typical call does not allocate.
type walker struct {
	n       *Normalizer
	inline  [16]visit
	nInline int
	visited map[visit]struct{}

	pathBuf  [8]seg
	pathMore []seg // segments past len(pathBuf)
	depth    int

	nest    int // nesting of value calls, bounded by maxDepth
	changes int // values set so far; a map element is stored back only if it grew

	lastType reflect.Type // the struct type walked last, and its plan
	lastPlan *planResult

	errs     []error
	dropped  int  // errors past maxErrors
	skipHook bool // the next struct is embedded in one whose hook covers it
	skipDef  bool // the next value is behind a non-nil pointer
}

// visit identifies a container walked with a given plan. The same container
// reached with another plan, such as a slice shared by two fields with
// different tags, is walked again.
type visit struct {
	addr uintptr
	typ  reflect.Type
	len  int
	plan *level
}

type segKind uint8

const (
	segField segKind = iota
	segIndex
	segKey
)

type seg struct {
	kind  segKind
	name  string
	index int
	key   reflect.Value
}

// scratch holds the visit record and path segments of a finished call for
// reuse by the next one.
type scratch struct {
	visited  map[visit]struct{}
	pathMore []seg
}

var scratchPool sync.Pool

// maxPooledVisits bounds the visit records kept for reuse, so one huge call
// does not pin its memory.
const maxPooledVisits = 1 << 16

func (w *walker) takeScratch() {
	sc, ok := scratchPool.Get().(*scratch)
	if !ok {
		return
	}
	if w.visited == nil {
		w.visited = sc.visited
	}
	if w.pathMore == nil {
		w.pathMore = sc.pathMore
	}
}

func (w *walker) releaseScratch() {
	if w.visited == nil && w.pathMore == nil {
		return
	}
	if len(w.visited) <= maxPooledVisits && cap(w.pathMore) <= maxPooledVisits {
		clear(w.visited)
		clear(w.pathMore[:cap(w.pathMore)])
		scratchPool.Put(&scratch{visited: w.visited, pathMore: w.pathMore[:0]})
	}
	w.visited, w.pathMore = nil, nil
}

func (w *walker) push(s seg) {
	if w.depth < len(w.pathBuf) {
		w.pathBuf[w.depth] = s
	} else {
		if w.pathMore == nil {
			w.takeScratch()
		}
		w.pathMore = append(w.pathMore[:w.depth-len(w.pathBuf)], s)
	}
	w.depth++
}

func (w *walker) pop() { w.depth-- }

func (w *walker) seg(i int) *seg {
	if i < len(w.pathBuf) {
		return &w.pathBuf[i]
	}
	return &w.pathMore[i-len(w.pathBuf)]
}

// Map keys come from input, so error paths keep only their start, and a path
// as a whole is shortened in the middle; one error stays small whatever the
// input.
const (
	maxPathKey = 32
	maxPath    = 512
)

func (w *walker) pathString() string {
	var b strings.Builder
	for i := range w.depth {
		s := w.seg(i)
		switch s.kind {
		case segField:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.name)
		case segIndex:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.index))
			b.WriteByte(']')
		case segKey:
			b.WriteByte('[')
			if s.key.Kind() == reflect.String {
				k, cut := cutAt(s.key.String(), maxPathKey)
				q := strconv.Quote(k)
				if cut {
					q = q[:len(q)-1] + `…"`
				}
				b.WriteString(q)
			} else {
				k, cut := cutAt(fmt.Sprint(s.key), maxPathKey)
				b.WriteString(k)
				if cut {
					b.WriteString("…")
				}
			}
			b.WriteByte(']')
		}
	}
	p := b.String()
	if len(p) > maxPath {
		head, _ := cutAt(p, maxPath/2)
		tail := p[len(p)-maxPath/2:]
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
		p = head + "…" + tail
	}
	return p
}

// cutAt returns the longest prefix of s of at most n bytes that ends on a
// rune boundary, and whether s was cut.
func cutAt(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

func (w *walker) fail(op string, err error) {
	if len(w.errs) >= w.n.maxErrors {
		w.dropped++
		return
	}
	w.errs = append(w.errs, &FieldError{Path: w.pathString(), Op: op, Err: err})
}

func (w *walker) unsupported(t reflect.Type) {
	w.fail("", fmt.Errorf("%w: %s", ErrUnsupportedField, t))
}

func (w *walker) result() error {
	w.releaseScratch()
	errs := w.errs
	if w.dropped > 0 {
		errs = append(errs, fmt.Errorf("normalize: %w: %d more not reported", ErrTooManyErrors, w.dropped))
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	}
	return errors.Join(errs...)
}

// markVisited records v, a non-nil pointer or a non-empty slice or map, as
// walked with plan, and reports whether it was seen before with the same
// plan. Values of zero size are not recorded: they all share one address
// and cannot form a cycle.
func (w *walker) markVisited(v reflect.Value, plan *level) (seen bool) {
	if v.Type().Elem().Size() == 0 {
		return false
	}
	key := visit{addr: v.Pointer(), typ: v.Type(), plan: plan}
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
		w.takeScratch()
		if w.visited == nil {
			w.visited = make(map[visit]struct{})
		}
	}
	w.visited[key] = struct{}{}
	return false
}

func firstLevel(lv []level) *level {
	if len(lv) == 0 {
		return nil
	}
	return &lv[0]
}

func (w *walker) walkStruct(v reflect.Value) {
	skipHook := w.skipHook
	w.skipHook = false
	t := v.Type()
	res := w.lastPlan
	if t != w.lastType {
		res = w.n.plan(t)
		w.lastType, w.lastPlan = t, res
	}
	if res.err != nil {
		// Types checked in advance never get here; this is a struct met
		// behind an Unwrapper, or one whose tags a later Register broke
		// for a Rule compiled before it.
		w.fail("", res.err)
		return
	}
	for i := range res.fields {
		fp := &res.fields[i]
		if !fp.embedded {
			w.push(seg{kind: segField, name: fp.name})
		}
		w.skipHook = fp.embedded && res.hook
		w.value(v.Field(fp.index), fp.levels)
		w.skipHook = false
		if !fp.embedded {
			w.pop()
		}
	}
	if res.hook && !skipHook {
		w.callHook(v, res.hookPath)
	}
}

// callHook calls AfterNormalize on v. It is not called when the method is
// promoted through a nil embedded pointer or interface on path, which would
// make it run with a nil receiver, or when v was reached through an
// unexported embedded field, as such a value cannot be used as an interface.
func (w *walker) callHook(v reflect.Value, path []int) {
	if !v.CanAddr() {
		return
	}
	f := v
	for _, i := range path {
		if f.Kind() == reflect.Pointer {
			f = f.Elem()
		}
		f = f.Field(i)
		if k := f.Kind(); (k == reflect.Pointer || k == reflect.Interface) && f.IsNil() {
			return
		}
	}
	a := v.Addr()
	if !a.CanInterface() {
		return
	}
	w.changes++ // the method may change the struct
	if err := a.Interface().(AfterNormalizer).AfterNormalize(); err != nil {
		w.fail("AfterNormalize", err)
	}
}

// value applies lv to v: lv[0] to v itself and the rest, after dive, to its
// elements. v is addressable: it is always reached through a pointer or a
// temporary copy.
func (w *walker) value(v reflect.Value, lv []level) {
	if w.nest >= w.n.maxDepth {
		w.fail("", fmt.Errorf("%w: more than %d levels", ErrTooDeep, w.n.maxDepth))
		return
	}
	w.nest++
	w.valueAt(v, lv)
	w.nest--
}

func (w *walker) valueAt(v reflect.Value, lv []level) {
	cur := firstLevel(lv)
	if v.Kind() == reflect.Pointer {
		w.pointer(v, lv)
		return
	}
	skipDef := w.skipDef
	w.skipDef = false
	if v.CanAddr() {
		if a := v.Addr(); a.CanInterface() {
			if u, ok := a.Interface().(Unwrapper); ok {
				w.skipHook = false
				w.unwrap(u, lv)
				return
			}
		}
	}
	switch v.Kind() {
	case reflect.String:
		if len(lv) > 1 {
			w.unsupported(v.Type())
			return
		}
		if cur != nil {
			w.str(v, cur, skipDef)
		}
	case reflect.Struct:
		w.walkStruct(v)
	case reflect.Slice:
		if cur != nil && cur.nilEmpty && v.Len() == 0 {
			if !v.IsNil() && v.CanSet() {
				v.SetZero()
				w.changes++
			}
			return
		}
		w.elems(v, lv)
	case reflect.Array:
		w.elems(v, lv)
	case reflect.Map:
		w.mapValue(v, lv)
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		if cur == nil {
			return
		}
		if len(cur.ops) > 0 || len(lv) > 1 {
			w.unsupported(v.Type())
			return
		}
		if cur.hasDef && !skipDef && v.IsZero() && !cur.def.IsZero() && v.CanSet() {
			v.Set(cur.def)
			w.changes++
		}
	default:
		if cur != nil && (len(cur.ops) > 0 || len(lv) > 1) {
			w.unsupported(v.Type())
		}
	}
}

func (w *walker) pointer(v reflect.Value, lv []level) {
	cur := firstLevel(lv)
	if v.IsNil() {
		if cur != nil && cur.hasDef && v.CanSet() {
			p := reflect.New(v.Type().Elem())
			p.Elem().Set(cur.def)
			v.Set(p)
			w.changes++
		}
		return
	}
	if k := v.Elem().Kind(); (k == reflect.Struct || k == reflect.Array) && w.markVisited(v, cur) {
		return
	}
	w.skipDef = cur != nil && cur.hasDef // a present pointer keeps its value
	w.value(v.Elem(), lv)
	w.skipDef = false
	if cur != nil && cur.nilEmpty && v.CanSet() && isEmpty(v.Elem()) {
		v.SetZero()
		w.changes++
	}
}

func (w *walker) str(v reflect.Value, cur *level, skipDef bool) {
	useDef := cur.hasDef && !skipDef
	if len(cur.ops) == 0 && !useDef {
		return
	}
	orig := v.String()
	s := orig
	for i := range cur.ops {
		o := &cur.ops[i]
		r, err := o.fn(s)
		if err != nil {
			w.fail(o.name, err)
			return
		}
		s = r
	}
	if s == "" && useDef {
		s = cur.def.String()
	}
	if s != orig {
		v.SetString(s)
		w.changes++
	}
}

func (w *walker) elems(v reflect.Value, lv []level) {
	var next []level
	if len(lv) > 1 {
		next = lv[1:]
	}
	if len(lv) > 0 && len(lv[0].ops) > 0 {
		w.unsupported(v.Type())
		return
	}
	if next == nil && !w.n.mayContain(v.Type().Elem()) {
		return
	}
	if v.Kind() == reflect.Slice && v.Len() > 0 && w.markVisited(v, firstLevel(next)) {
		return
	}
	for i := range v.Len() {
		w.push(seg{kind: segIndex, index: i})
		w.value(v.Index(i), next)
		w.pop()
	}
}

func (w *walker) mapValue(v reflect.Value, lv []level) {
	cur := firstLevel(lv)
	if cur != nil && len(cur.ops) > 0 {
		w.unsupported(v.Type())
		return
	}
	if v.Len() == 0 {
		if cur != nil && cur.nilEmpty && !v.IsNil() && v.CanSet() {
			v.SetZero()
			w.changes++
		}
		return
	}
	if cur != nil && len(cur.keyOps) > 0 {
		w.mapKeys(v, cur.keyOps)
	}
	var next []level
	if len(lv) > 1 {
		next = lv[1:]
	}
	et := v.Type().Elem()
	if next == nil && !w.n.mayContain(et) {
		return
	}
	if w.markVisited(v, firstLevel(next)) {
		return
	}
	key := reflect.New(v.Type().Key()).Elem()
	tmp := reflect.New(et).Elem()
	// A hook counts as a change whether or not it wrote anything, so for
	// elements that may hold one, the copy is compared with a snapshot.
	var orig reflect.Value
	if hasValueHook(et) {
		orig = reflect.New(et).Elem()
	}
	var iter reflect.MapIter
	iter.Reset(v)
	for iter.Next() {
		key.SetIterKey(&iter)
		tmp.SetIterValue(&iter)
		var origPtr unsafe.Pointer
		if et.Kind() == reflect.Pointer {
			origPtr = tmp.UnsafePointer()
		}
		if orig.IsValid() {
			orig.Set(tmp)
		}
		before := w.changes
		w.push(seg{kind: segKey, key: key})
		w.value(tmp, next)
		switch {
		case w.changes == before:
			// Unchanged: no write, so values shared between goroutines
			// are only read.
		case et.Kind() == reflect.Pointer && tmp.UnsafePointer() == origPtr:
			// Changed through the pointer; the element itself is the same.
		case orig.IsValid() && sameBits(tmp, orig):
			// Only a hook ran, and it changed nothing.
		case !key.Equal(key): //nolint:gocritic // true only for NaN keys
			w.fail("", fmt.Errorf("%w: the key is not equal to itself (NaN), so the element cannot be stored back", ErrUnsupportedField))
		default:
			v.SetMapIndex(key, tmp)
		}
		w.pop()
	}
}

// sameBits reports whether a and b, addressable values of one type, hold the
// same bytes.
func sameBits(a, b reflect.Value) bool {
	n := int(a.Type().Size())
	return bytes.Equal(
		unsafe.Slice((*byte)(a.Addr().UnsafePointer()), n),
		unsafe.Slice((*byte)(b.Addr().UnsafePointer()), n))
}

var valueHooks sync.Map // reflect.Type → bool

// hasValueHook reports whether a value of type t holds, directly or in
// struct fields and array elements, a struct with an AfterNormalize method.
// Hooks behind pointers, slices and maps do not count: they change shared
// memory, not the value itself.
func hasValueHook(t reflect.Type) bool {
	if r, ok := valueHooks.Load(t); ok {
		return r.(bool)
	}
	var has bool
	switch t.Kind() {
	case reflect.Struct:
		has = reflect.PointerTo(t).Implements(afterNormalizerType)
		for i := 0; i < t.NumField() && !has; i++ {
			has = hasValueHook(t.Field(i).Type)
		}
	case reflect.Array:
		has = hasValueHook(t.Elem())
	}
	valueHooks.Store(t, has)
	return has
}

// mapKeys applies ops to every key of v, a map with string keys. On an error
// or a collision the keys are left unchanged.
func (w *walker) mapKeys(v reflect.Value, ops []op) {
	type change struct {
		old, new string
	}
	var changes []change
	kt := v.Type().Key()
	k := reflect.New(kt).Elem()
	var iter reflect.MapIter
	iter.Reset(v)
	for iter.Next() {
		k.SetIterKey(&iter)
		s := k.String()
		ns := s
		for i := range ops {
			r, err := ops[i].fn(ns)
			if err != nil {
				w.push(seg{kind: segKey, key: k})
				w.fail(ops[i].name, err)
				w.pop()
				return
			}
			ns = r
		}
		if ns != s {
			changes = append(changes, change{old: s, new: ns})
		}
	}
	if len(changes) == 0 {
		return
	}
	moved := make(map[string]bool, len(changes))
	for _, c := range changes {
		moved[c.old] = true
	}
	targets := make(map[string]bool, len(changes))
	for _, c := range changes {
		exists := v.MapIndex(reflect.ValueOf(c.new).Convert(kt)).IsValid()
		if targets[c.new] || (exists && !moved[c.new]) {
			w.fail(keysTag, fmt.Errorf("%w: %q", ErrKeyCollision, c.new))
			return
		}
		targets[c.new] = true
	}
	vals := make([]reflect.Value, len(changes))
	for i, c := range changes {
		vals[i] = v.MapIndex(reflect.ValueOf(c.old).Convert(kt))
	}
	for _, c := range changes {
		v.SetMapIndex(reflect.ValueOf(c.old).Convert(kt), reflect.Value{})
	}
	for i, c := range changes {
		v.SetMapIndex(reflect.ValueOf(c.new).Convert(kt), vals[i])
	}
	w.changes++
}

func (w *walker) unwrap(u Unwrapper, lv []level) {
	target := u.NormalizeTarget()
	if target == nil {
		return
	}
	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Pointer {
		w.fail("", fmt.Errorf("%w: %T returned %T", ErrInvalidTarget, u, target))
		return
	}
	if tv.IsNil() {
		return
	}
	if t, ok := target.(Unwrapper); ok && t == u {
		w.fail("", fmt.Errorf("%w: %T returns a pointer to itself", ErrInvalidTarget, u))
		return
	}
	elem := tv.Elem()
	if k := elem.Kind(); (k == reflect.Struct || k == reflect.Array) && w.markVisited(tv, firstLevel(lv)) {
		return
	}
	w.value(elem, lv)
}

// isEmpty reports whether v, the target of a pointer, counts as empty for
// nilempty: an empty slice or map, or the zero value of any other type.
func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Slice, reflect.Map:
		return v.Len() == 0
	}
	return v.IsZero()
}
