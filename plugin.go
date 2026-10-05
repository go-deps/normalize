package normalize

import (
	"fmt"
	"slices"
	"strings"
)

// Plugin adds a set of operations to a Normalizer, so that extensions with
// their own dependencies can live in separate modules and the core stays
// small:
//
//	normalize.Use(normtext.Plugin())
//
// Name identifies the plugin, by convention its module path. Register adds
// the plugin's operations to r.
//
// The interface will not gain methods, so implementations keep compiling
// across versions of this module; new capabilities, if any, come as methods
// of [Registry] or as separate optional interfaces.
type Plugin interface {
	Name() string
	Register(r *Registry) error
}

// Registry collects the operations of a [Plugin] before they are added to a
// [Normalizer]. Names follow the rules of [Normalizer.Register]; unlike
// there, registering the same name and form twice is an error. The zero
// value is ready to use, so plugins can be tested on their own.
type Registry struct {
	plain map[string]CheckFunc
	param map[string]ParamFunc
}

// Register adds an operation used without an argument.
func (r *Registry) Register(name string, fn Func) error {
	if fn == nil {
		return fmt.Errorf("normalize: Register %q: nil Func", name)
	}
	return r.RegisterCheck(name, wrap(fn))
}

// RegisterCheck adds an operation, used without an argument, that can reject
// its input.
func (r *Registry) RegisterCheck(name string, fn CheckFunc) error {
	if fn == nil {
		return fmt.Errorf("normalize: Register %q: nil CheckFunc", name)
	}
	if err := checkName(name); err != nil {
		return err
	}
	if _, dup := r.plain[name]; dup {
		return fmt.Errorf("normalize: Register %q: registered twice", name)
	}
	if r.plain == nil {
		r.plain = map[string]CheckFunc{}
	}
	r.plain[name] = fn
	return nil
}

// RegisterParam adds the form of an operation that takes an argument.
func (r *Registry) RegisterParam(name string, fn ParamFunc) error {
	if fn == nil {
		return fmt.Errorf("normalize: Register %q: nil ParamFunc", name)
	}
	if err := checkName(name); err != nil {
		return err
	}
	if _, dup := r.param[name]; dup {
		return fmt.Errorf("normalize: Register %q=: registered twice", name)
	}
	if r.param == nil {
		r.param = map[string]ParamFunc{}
	}
	r.param[name] = fn
	return nil
}

// registeredOwner marks an operation form added with Register,
// RegisterCheck or RegisterParam in [Normalizer.owners].
const registeredOwner = "Register"

// Use adds the operations of each plugin to n.
//
// A plugin may replace a built-in operation, so that adding an operation to
// this module never breaks a plugin that already has one of that name. An
// operation form added with Register or by another plugin is a conflict:
// Use then returns an error wrapping [ErrConflict] and changes nothing. To
// replace an operation on purpose, call Register after Use.
//
// Using a plugin again with an equal value does nothing. Using another value
// under a name already in use, for example the same plugin with different
// settings, is a conflict.
func (n *Normalizer) Use(plugins ...Plugin) error {
	if n.plain == nil {
		return ErrInvalidArgument
	}
	type pending struct {
		p   Plugin
		reg *Registry
	}
	var all []pending
	for _, p := range plugins {
		if p == nil {
			return fmt.Errorf("normalize: Use: nil Plugin")
		}
		reg := &Registry{}
		if err := p.Register(reg); err != nil {
			return fmt.Errorf("normalize: plugin %s: %w", p.Name(), err)
		}
		all = append(all, pending{p: p, reg: reg})
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	owner := map[string]string{} // operation form -> plugin, within this call
	var conflicts []string
	for i, pd := range all {
		name := pd.p.Name()
		prev, used := n.plugins[name]
		if j := slices.IndexFunc(all[:i], func(o pending) bool { return o.p.Name() == name }); j >= 0 {
			prev, used = all[j].p, true
		}
		if used {
			if !samePlugin(prev, pd.p) {
				conflicts = append(conflicts, fmt.Sprintf("plugin %s already in use with another value", name))
			}
			all[i].reg = nil
			continue
		}
		check := func(form string) {
			if o, taken := n.owners[form]; taken {
				conflicts = append(conflicts, fmt.Sprintf("%s (plugin %s, already added by %s)", form, name, o))
			} else if other, dup := owner[form]; dup {
				conflicts = append(conflicts, fmt.Sprintf("%s (plugins %s and %s)", form, other, name))
			}
			owner[form] = name
		}
		for op := range pd.reg.plain {
			check(op)
		}
		for op := range pd.reg.param {
			check(op + "=")
		}
	}
	if len(conflicts) > 0 {
		slices.Sort(conflicts)
		return fmt.Errorf("normalize: Use: %w: %s", ErrConflict, strings.Join(conflicts, ", "))
	}
	for _, pd := range all {
		if pd.reg == nil {
			continue
		}
		name := pd.p.Name()
		for op, fn := range pd.reg.plain {
			n.plain[op] = fn
			n.owners[op] = name
		}
		for op, fn := range pd.reg.param {
			n.param[op] = fn
			n.owners[op+"="] = name
		}
		n.plugins[name] = pd.p
	}
	n.clearCaches()
	return nil
}

// samePlugin reports whether a and b are equal values. Values of types that
// cannot be compared are never equal.
func samePlugin(a, b Plugin) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}

// Use adds the operations of each plugin to the Normalizer behind the
// package-level Struct, Var, Apply and Compile. Call it once at start-up,
// from package main: that Normalizer is shared by the whole program, so a
// library should create its own with [New] and [WithPlugins] instead.
func Use(plugins ...Plugin) error { return std.Use(plugins...) }

// WithPlugins is an Option that adds the operations of each plugin, as
// [Normalizer.Use] does. New panics if they conflict, as a conflict is a
// programming error; call Use to get the error instead.
func WithPlugins(plugins ...Plugin) Option {
	return func(n *Normalizer) {
		if err := n.Use(plugins...); err != nil {
			panic(err)
		}
	}
}
