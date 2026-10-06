package normalize

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strconv"
)

// op is a resolved operation together with the tag element that named it.
type op struct {
	name string
	fn   CheckFunc
}

// level holds what applies at one depth of a field: level 0 is the field
// itself and each dive adds the next level for the elements of a slice,
// array or map.
type level struct {
	ops      []op
	keyOps   []op // map keys at this level
	nilEmpty bool
	hasDef   bool
	defRaw   string
	def      reflect.Value // the parsed default, of the level's non-pointer type
}

func (l *level) empty() bool {
	return len(l.ops) == 0 && len(l.keyOps) == 0 && !l.nilEmpty && !l.hasDef
}

// fieldPlan is the parsed tag of one field.
type fieldPlan struct {
	index    int
	name     string
	levels   []level // nil for an untagged field
	traverse bool    // the field's type may contain something to normalize
	embedded bool
}

type planResult struct {
	fields   []fieldPlan
	hook     bool  // *T implements AfterNormalizer
	hookPath []int // embedded fields the hook is promoted through, if it is
	err      error
}

func (n *Normalizer) plan(t reflect.Type) *planResult {
	if r, ok := n.cache.Load(t); ok {
		return r.(*planResult)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	res, _ := n.buildLocked(t, newBuildState())
	return res
}

// buildState tracks one round of plan building. inProgress maps the struct
// types being built to their depth in the current chain. A type that refers
// back to one of them gets a provisional plan: an error found later in that
// type would change it, so it is kept for this round only and not cached.
type buildState struct {
	inProgress  map[reflect.Type]int
	provisional map[reflect.Type]*planResult
}

func newBuildState() *buildState {
	return &buildState{inProgress: map[reflect.Type]int{}, provisional: map[reflect.Type]*planResult{}}
}

// complete is the low mark of a plan that depends on no type in progress.
const complete = math.MaxInt

// buildLocked parses t and, recursively, every struct type reachable from its
// fields, so a malformed tag in a nested type is reported even when the nested
// value is nil. It returns the plan and the smallest depth of a type in
// progress the plan depends on.
func (n *Normalizer) buildLocked(t reflect.Type, st *buildState) (*planResult, int) {
	if r, ok := n.cache.Load(t); ok {
		return r.(*planResult), complete
	}
	if d, ok := st.inProgress[t]; ok {
		return &planResult{}, d
	}
	if r, ok := st.provisional[t]; ok {
		return r, 0
	}
	depth := len(st.inProgress)
	st.inProgress[t] = depth
	defer delete(st.inProgress, t)

	low := complete
	res := planResult{hook: reflect.PointerTo(t).Implements(afterNormalizerType)}
	if res.hook {
		res.hookPath = hookPath(t)
	}
	for i := range t.NumField() {
		sf := t.Field(i)
		embeddedStruct := sf.Anonymous && deref(sf.Type).Kind() == reflect.Struct
		if !sf.IsExported() && !embeddedStruct {
			continue
		}
		tag, hasTag, err := n.lookupTag(t, sf)
		if err != nil {
			res = planResult{err: err}
			break
		}
		if tag == skipTag {
			continue
		}
		fp, err := n.parseField(t, sf, tag, hasTag)
		if err != nil {
			res = planResult{err: err}
			break
		}
		l, err := n.checkNested(sf.Type, st, map[reflect.Type]bool{})
		low = min(low, l)
		if err != nil {
			res = planResult{err: err}
			break
		}
		if fp.traverse || fp.levels != nil {
			res.fields = append(res.fields, fp)
		}
	}
	if low >= depth {
		n.cache.Store(t, &res)
		return &res, complete
	}
	st.provisional[t] = &res
	return &res, low
}

// hookPath returns the field indexes, from t, of the embedded fields that
// the AfterNormalize method of *t is promoted through, following the Go rule
// that the shallowest embedding wins. It returns nil when t declares the
// method itself.
func hookPath(t reflect.Type) []int {
	if declaresHook(t) {
		return nil
	}
	type candidate struct {
		t    reflect.Type
		path []int
	}
	level := []candidate{{t: t}}
	seen := map[reflect.Type]bool{t: true}
	for len(level) > 0 {
		var found [][]int
		var next []candidate
		for _, c := range level {
			for i := range c.t.NumField() {
				sf := c.t.Field(i)
				if !sf.Anonymous {
					continue
				}
				path := append(slices.Clone(c.path), i)
				base := deref(sf.Type)
				switch {
				case sf.Type.Kind() == reflect.Interface:
					if sf.Type.Implements(afterNormalizerType) {
						found = append(found, path)
					}
				case base.Kind() != reflect.Struct:
				case declaresHook(base):
					found = append(found, path)
				case !seen[base]:
					seen[base] = true
					next = append(next, candidate{t: base, path: path})
				}
			}
		}
		if len(found) > 0 {
			if len(found) == 1 {
				return found[0]
			}
			return nil // ambiguous at this depth; not promoted
		}
		level = next
	}
	return nil
}

// declaresHook reports whether t or *t declares AfterNormalize itself rather
// than through an embedded field. reflect does not tell the two apart; a
// promoted method is a wrapper the compiler generates, and the runtime marks
// its position as "<autogenerated>".
func declaresHook(t reflect.Type) bool {
	return declaresMethod(t) || declaresMethod(reflect.PointerTo(t))
}

func declaresMethod(t reflect.Type) bool {
	m, ok := t.MethodByName("AfterNormalize")
	if !ok {
		return false
	}
	fn := runtime.FuncForPC(m.Func.Pointer())
	if fn == nil {
		return false
	}
	file, _ := fn.FileLine(fn.Entry())
	return file != "<autogenerated>"
}

// lookupTag reads the field's tag under the first of the Normalizer's keys
// that is present; more than one present key is an error.
func (n *Normalizer) lookupTag(owner reflect.Type, sf reflect.StructField) (tag string, found bool, err error) {
	var key string
	for _, name := range n.tagNames {
		v, ok := sf.Tag.Lookup(name)
		if !ok {
			continue
		}
		if found {
			return "", false, &TagError{Type: owner, Field: sf.Name, Tag: v,
				Err: fmt.Errorf("%w: both %q and %q keys given", ErrMalformedTag, key, name)}
		}
		tag, found, key = v, true, name
	}
	return tag, found, nil
}

func (n *Normalizer) parseField(owner reflect.Type, sf reflect.StructField, tag string, hasTag bool) (fieldPlan, error) {
	fp := fieldPlan{
		index:    sf.Index[0],
		name:     n.fieldNameOf(sf),
		embedded: sf.Anonymous && deref(sf.Type).Kind() == reflect.Struct,
	}
	tagErr := func(err error) error {
		return &TagError{Type: owner, Field: sf.Name, Tag: tag, Err: err}
	}
	if hasTag && tag != "" {
		levels, err := n.parseLevels(tag)
		if err != nil {
			return fp, tagErr(err)
		}
		if err := n.checkLevels(sf.Type, levels); err != nil {
			return fp, tagErr(err)
		}
		fp.levels = levels
	}
	fp.traverse = n.mayContain(sf.Type)
	return fp, nil
}

func (n *Normalizer) fieldNameOf(sf reflect.StructField) string {
	if n.fieldName != nil {
		if name := n.fieldName(sf); name != "" {
			return name
		}
	}
	return sf.Name
}

// parseLevels resolves the elements of tag into levels without looking at
// the field type.
func (n *Normalizer) parseLevels(tag string) ([]level, error) {
	els, err := splitTag(tag)
	if err != nil {
		return nil, err
	}
	levels := []level{{}}
	for _, el := range els {
		cur := &levels[len(levels)-1]
		switch el.name {
		case skipTag:
			return nil, fmt.Errorf("%w: %q must be the whole tag", ErrMalformedTag, skipTag)
		case diveTag:
			if el.hasArg || el.isGroup {
				return nil, fmt.Errorf("%w: %q takes no argument", ErrMalformedTag, diveTag)
			}
			levels = append(levels, level{})
		case keysTag:
			if !el.isGroup || len(el.group) == 0 {
				return nil, fmt.Errorf("%w: %q needs operations in parentheses, as in keys(trim)", ErrMalformedTag, keysTag)
			}
			if cur.keyOps != nil {
				return nil, fmt.Errorf("%w: %q given more than once at one level", ErrMalformedTag, keysTag)
			}
			for _, kel := range el.group {
				o, err := n.resolve(kel)
				if err != nil {
					return nil, err
				}
				cur.keyOps = append(cur.keyOps, o)
			}
		case nilEmptyTag:
			if el.hasArg || el.isGroup {
				return nil, fmt.Errorf("%w: %q takes no argument", ErrMalformedTag, nilEmptyTag)
			}
			if cur.nilEmpty {
				return nil, fmt.Errorf("%w: %q given more than once", ErrMalformedTag, nilEmptyTag)
			}
			cur.nilEmpty = true
		case defaultTag:
			if !el.hasArg {
				return nil, fmt.Errorf("%w: %q needs a value, as in default=x", ErrMalformedTag, defaultTag)
			}
			if cur.hasDef {
				return nil, fmt.Errorf("%w: %q given more than once", ErrMalformedTag, defaultTag)
			}
			cur.hasDef, cur.defRaw = true, el.arg
		default:
			o, err := n.resolve(el)
			if err != nil {
				return nil, err
			}
			cur.ops = append(cur.ops, o)
		}
		if cur := &levels[len(levels)-1]; cur.nilEmpty && cur.hasDef {
			return nil, fmt.Errorf("%w: %q and %q exclude each other", ErrMalformedTag, nilEmptyTag, defaultTag)
		}
	}
	return levels, nil
}

// resolve looks up the operation an element names.
func (n *Normalizer) resolve(el element) (op, error) {
	if el.isGroup {
		return op{}, fmt.Errorf("%w: only %q takes a group", ErrMalformedTag, keysTag)
	}
	if isReserved(el.name) {
		return op{}, fmt.Errorf("%w: %q is not allowed here", ErrMalformedTag, el.name)
	}
	plain, hasPlain := n.plain[el.name]
	param, hasParam := n.param[el.name]
	switch {
	case el.hasArg && hasParam:
		fn, err := param(el.arg)
		if err != nil {
			return op{}, fmt.Errorf("%w: %s: %w", ErrMalformedTag, el.raw, err)
		}
		if fn == nil {
			return op{}, fmt.Errorf("%w: %s: nil operation", ErrMalformedTag, el.raw)
		}
		return op{name: el.raw, fn: fn}, nil
	case el.hasArg && hasPlain:
		return op{}, fmt.Errorf("%w: %q takes no argument", ErrMalformedTag, el.name)
	case !el.hasArg && hasPlain:
		return op{name: el.raw, fn: plain}, nil
	case !el.hasArg && hasParam:
		return op{}, fmt.Errorf("%w: %q needs an argument, as in %s=value", ErrMalformedTag, el.name, el.name)
	}
	return op{}, fmt.Errorf("%w %q", ErrUnknownOperation, el.name)
}

// checkLevels matches levels against the field type t and parses defaults.
func (n *Normalizer) checkLevels(t reflect.Type, levels []level) error {
	for i := range levels {
		lv := &levels[i]
		last := i == len(levels)-1
		base := deref(t)
		if implementsUnwrapper(base) {
			// The wrapped type is only known at run time.
			if lv.nilEmpty || lv.hasDef || len(lv.keyOps) > 0 {
				return fmt.Errorf("%w: %s, %s and %s on wrapper type %s", ErrUnsupportedField, nilEmptyTag, defaultTag, keysTag, t)
			}
			return nil
		}
		if lv.nilEmpty {
			switch t.Kind() {
			case reflect.Pointer, reflect.Slice, reflect.Map:
			default:
				return fmt.Errorf("%w: %s on %s, which cannot be nil", ErrUnsupportedField, nilEmptyTag, t)
			}
		}
		if lv.hasDef {
			if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Pointer {
				return fmt.Errorf("%w: %s on %s", ErrUnsupportedField, defaultTag, t)
			}
			def, err := parseScalar(base, lv.defRaw)
			if err != nil {
				return err
			}
			lv.def = def
		}
		if len(lv.keyOps) > 0 && (base.Kind() != reflect.Map || base.Key().Kind() != reflect.String) {
			return fmt.Errorf("%w: %s on %s", ErrUnsupportedField, keysTag, t)
		}
		if len(lv.ops) > 0 && base.Kind() != reflect.String {
			if isContainer(base) {
				return fmt.Errorf("%w: operations before dive on %s", ErrUnsupportedField, t)
			}
			return fmt.Errorf("%w: %s", ErrUnsupportedField, t)
		}
		if last {
			if i > 0 && lv.empty() && base.Kind() != reflect.String && !n.mayContain(base) {
				return fmt.Errorf("%w: dive on %s with nothing to apply", ErrUnsupportedField, levelParent(t))
			}
			return nil
		}
		if !isContainer(base) {
			return fmt.Errorf("%w: dive on %s", ErrUnsupportedField, t)
		}
		t = base.Elem()
	}
	return nil
}

// levelParent names the type a final dive was applied to, for messages.
func levelParent(t reflect.Type) string { return "[]" + t.String() }

func isContainer(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return true
	}
	return false
}

// parseScalar parses a default value for a field of kind string, bool,
// integer or float.
func parseScalar(t reflect.Type, s string) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	bad := func(err error) (reflect.Value, error) {
		return reflect.Value{}, fmt.Errorf("%w: %s=%q for %s: %w", ErrMalformedTag, defaultTag, s, t, err)
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return bad(err)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(s, 0, t.Bits())
		if err != nil {
			return bad(err)
		}
		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u, err := strconv.ParseUint(s, 0, t.Bits())
		if err != nil {
			return bad(err)
		}
		v.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, t.Bits())
		if err != nil {
			return bad(err)
		}
		v.SetFloat(f)
	default:
		return reflect.Value{}, fmt.Errorf("%w: %s on %s", ErrUnsupportedField, defaultTag, t)
	}
	return v, nil
}

// checkNested parses every struct type a field of type t can hold, and
// returns the low mark of buildLocked.
func (n *Normalizer) checkNested(t reflect.Type, st *buildState, seen map[reflect.Type]bool) (int, error) {
	t = deref(t)
	if seen[t] {
		return complete, nil
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Struct:
		res, low := n.buildLocked(t, st)
		return low, res.err
	case reflect.Slice, reflect.Array, reflect.Map:
		return n.checkNested(t.Elem(), st, seen)
	}
	return complete, nil
}

// mayContain reports whether a value of type t can hold a struct or a
// wrapper, which must be walked even without a tag.
func (n *Normalizer) mayContain(t reflect.Type) bool {
	if r, ok := n.contains.Load(t); ok {
		return r.(bool)
	}
	r := mayContain(t, map[reflect.Type]bool{})
	n.contains.Store(t, r)
	return r
}

func mayContain(t reflect.Type, seen map[reflect.Type]bool) bool {
	t = deref(t)
	if seen[t] {
		return false
	}
	seen[t] = true
	if implementsUnwrapper(t) {
		return true
	}
	switch t.Kind() {
	case reflect.Struct:
		return true
	case reflect.Slice, reflect.Array, reflect.Map:
		return mayContain(t.Elem(), seen)
	}
	return false
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
