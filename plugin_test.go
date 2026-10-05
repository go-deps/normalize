package normalize_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-deps/normalize"
)

type testPlugin struct {
	name  string
	plain map[string]normalize.Func
	param map[string]normalize.ParamFunc
	err   error
	calls *int
}

func (p testPlugin) Name() string { return p.name }

func (p testPlugin) Register(r *normalize.Registry) error {
	if p.calls != nil {
		*p.calls++
	}
	if p.err != nil {
		return p.err
	}
	for name, fn := range p.plain {
		if err := r.Register(name, fn); err != nil {
			return err
		}
	}
	for name, fn := range p.param {
		if err := r.RegisterParam(name, fn); err != nil {
			return err
		}
	}
	return nil
}

func repeat(arg string) (normalize.CheckFunc, error) {
	return func(s string) (string, error) { return s + arg, nil }, nil
}

func TestUse(t *testing.T) {
	n := normalize.New()
	calls := 0
	shout := &testPlugin{name: "example.com/shout", plain: map[string]normalize.Func{"shout": strings.ToUpper}, calls: &calls}
	if err := n.Use(shout); err != nil {
		t.Fatal(err)
	}
	if got, err := normalize.ApplyWith(n, "hi", "shout"); err != nil || got != "HI" {
		t.Errorf("plugin operation: %q %v", got, err)
	}
	if err := n.Use(shout, shout); err != nil || calls != 3 {
		t.Errorf("using a plugin again must succeed and change nothing: %v, %d calls", err, calls)
	}
	other := &testPlugin{name: "example.com/shout", plain: map[string]normalize.Func{"shout": strings.ToLower}}
	if err := n.Use(other); !errors.Is(err, normalize.ErrConflict) {
		t.Errorf("another value under a name in use: got %v, want ErrConflict", err)
	}
	if err := normalize.New().Use(other, shout); !errors.Is(err, normalize.ErrConflict) {
		t.Errorf("two values under one name in one call: got %v, want ErrConflict", err)
	}
	if got, _ := normalize.ApplyWith(n, "hi", "shout"); got != "HI" {
		t.Errorf("a failed Use must keep the first plugin: %q", got)
	}

	lowerParam := testPlugin{name: "example.com/lowerparam", param: map[string]normalize.ParamFunc{"lower": repeat}}
	if err := n.Use(lowerParam); err != nil {
		t.Fatalf("a param form next to a built-in plain form: %v", err)
	}
	if got, _ := normalize.ApplyWith(n, "A", "lower"); got != "a" {
		t.Errorf("built-in plain form kept: %q", got)
	}
	if got, _ := normalize.ApplyWith(n, "A", "lower=!"); got != "A!" {
		t.Errorf("plugin param form: %q", got)
	}
}

func TestUseConflicts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		plugins []normalize.Plugin
		want    string
	}{
		{"two plugins over a built-in", []normalize.Plugin{
			testPlugin{name: "a", plain: map[string]normalize.Func{"trim": strings.ToUpper}},
			testPlugin{name: "b", plain: map[string]normalize.Func{"trim": strings.ToLower}},
		}, "trim (plugins a and b)"},
		{"two plugins", []normalize.Plugin{
			testPlugin{name: "a", param: map[string]normalize.ParamFunc{"pad": repeat}},
			testPlugin{name: "b", param: map[string]normalize.ParamFunc{"pad": repeat}},
		}, "pad= (plugins a and b)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n := normalize.New()
			err := n.Use(tt.plugins...)
			if !errors.Is(err, normalize.ErrConflict) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want a conflict on %s", err, tt.want)
			}
			if _, err := normalize.ApplyWith(n, "x", "pad=1"); !errors.Is(err, normalize.ErrMalformedTag) && !errors.Is(err, normalize.ErrUnknownOperation) {
				t.Errorf("a failed Use must add nothing: %v", err)
			}
		})
	}

	n := normalize.New()
	if err := n.Register("mine", strings.TrimSpace); err != nil {
		t.Fatal(err)
	}
	if err := n.Use(testPlugin{name: "c", plain: map[string]normalize.Func{"mine": strings.ToUpper}}); !errors.Is(err, normalize.ErrConflict) {
		t.Error("an operation registered by the user is a conflict")
	}
	if err := n.Use(testPlugin{name: "d", plain: map[string]normalize.Func{"shout": strings.ToUpper}}); err != nil {
		t.Fatal(err)
	}
	if err := n.Use(testPlugin{name: "e", plain: map[string]normalize.Func{"shout": strings.ToLower}}); !errors.Is(err, normalize.ErrConflict) || !strings.Contains(err.Error(), "already added by d") {
		t.Errorf("an operation added by an earlier plugin is a conflict: %v", err)
	}

	atomic := normalize.New()
	if err := atomic.Use(testPlugin{name: "first", plain: map[string]normalize.Func{"taken": strings.ToUpper}}); err != nil {
		t.Fatal(err)
	}
	err := atomic.Use(
		testPlugin{name: "ok", plain: map[string]normalize.Func{"fine": strings.ToUpper}},
		testPlugin{name: "bad", plain: map[string]normalize.Func{"taken": strings.ToLower}},
	)
	if err == nil {
		t.Fatal("want conflict")
	}
	if _, err := normalize.ApplyWith(atomic, "x", "fine"); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Errorf("no plugin of a failed call may be added: %v", err)
	}
	if err := atomic.Use(testPlugin{name: "ok", plain: map[string]normalize.Func{"fine": strings.ToUpper}}); err != nil {
		t.Errorf("a plugin from a failed call is not marked as used: %v", err)
	}
}

func TestUseErrors(t *testing.T) {
	n := normalize.New()
	errBroken := errors.New("broken")
	if err := n.Use(testPlugin{name: "x", err: errBroken}); !errors.Is(err, errBroken) || !strings.Contains(err.Error(), "plugin x") {
		t.Errorf("plugin error: %v", err)
	}
	if err := n.Use(nil); err == nil {
		t.Error("nil plugin: want error")
	}
	bad := testPlugin{name: "y", plain: map[string]normalize.Func{"dive": strings.ToUpper}}
	if err := n.Use(bad); err == nil {
		t.Error("reserved name: want error")
	}
	var zero normalize.Normalizer
	if err := zero.Use(testPlugin{name: "z"}); !errors.Is(err, normalize.ErrInvalidArgument) {
		t.Errorf("zero Normalizer: %v", err)
	}
}

func TestPluginOverridesBuiltin(t *testing.T) {
	n := normalize.New()
	if err := n.Use(testPlugin{name: "a", plain: map[string]normalize.Func{"trim": strings.ToUpper}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := normalize.ApplyWith(n, " a ", "trim"); got != " A " {
		t.Errorf("plugin form replaces the built-in: %q", got)
	}
	if got, _ := normalize.Apply(" a ", "trim"); got != "a" {
		t.Errorf("other Normalizers keep the built-in: %q", got)
	}
	if err := n.Register("trim", strings.TrimSpace); err != nil {
		t.Fatal(err)
	}
	if got, _ := normalize.ApplyWith(n, " a ", "trim"); got != "a" {
		t.Errorf("Register after Use replaces the plugin form: %q", got)
	}
}

func TestZeroRegistry(t *testing.T) {
	var r normalize.Registry
	if err := r.Register("a", strings.ToUpper); err != nil {
		t.Error(err)
	}
	if err := r.RegisterCheck("b", func(s string) (string, error) { return s, nil }); err != nil {
		t.Error(err)
	}
	if err := r.RegisterParam("c", repeat); err != nil {
		t.Error(err)
	}
}

type dupPlugin struct{}

func (dupPlugin) Name() string { return "dup" }

func (dupPlugin) Register(r *normalize.Registry) error {
	if err := r.Register("a", strings.ToUpper); err != nil {
		return err
	}
	return r.Register("a", strings.ToLower)
}

type nilFuncPlugin struct{ kind int }

func (nilFuncPlugin) Name() string { return "nil" }

func (p nilFuncPlugin) Register(r *normalize.Registry) error {
	switch p.kind {
	case 0:
		return r.Register("a", nil)
	case 1:
		return r.RegisterCheck("a", nil)
	default:
		return r.RegisterParam("a", nil)
	}
}

type dupParamPlugin struct{}

func (dupParamPlugin) Name() string { return "dupparam" }

func (dupParamPlugin) Register(r *normalize.Registry) error {
	if err := r.RegisterParam("a", repeat); err != nil {
		return err
	}
	return r.RegisterParam("a", repeat)
}

func TestRegistryRejects(t *testing.T) {
	for _, p := range []normalize.Plugin{dupPlugin{}, dupParamPlugin{}, nilFuncPlugin{0}, nilFuncPlugin{1}, nilFuncPlugin{2}} {
		if err := normalize.New().Use(p); err == nil {
			t.Errorf("%T: want error", p)
		}
	}
}

func TestWithPlugins(t *testing.T) {
	n := normalize.New(normalize.WithPlugins(testPlugin{name: "s", plain: map[string]normalize.Func{"shout": strings.ToUpper}}))
	if got, _ := normalize.ApplyWith(n, "a", "shout"); got != "A" {
		t.Errorf("got %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("WithPlugins with a conflict must panic")
		}
	}()
	normalize.New(normalize.WithPlugins(
		testPlugin{name: "t", plain: map[string]normalize.Func{"trim": strings.ToUpper}},
		testPlugin{name: "u", plain: map[string]normalize.Func{"trim": strings.ToLower}},
	))
}

func TestPackageUse(t *testing.T) {
	p := testPlugin{name: "example.com/pkguse", plain: map[string]normalize.Func{"pkguse_rev": func(s string) string {
		r := []rune(s)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		return string(r)
	}}}
	if err := normalize.Use(p); err != nil {
		t.Fatal(err)
	}
	if got, err := normalize.Apply("ab", "pkguse_rev"); err != nil || got != "ba" {
		t.Errorf("got %q %v", got, err)
	}
}

func TestTagNames(t *testing.T) {
	type short struct {
		A string `norm:"trim"`
		B string `n:"trim"`
	}
	in := short{A: " a ", B: " b "}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.A != "a" || in.B != " b " {
		t.Errorf("default keys are normalize and norm: %+v", in)
	}

	in = short{A: " a ", B: " b "}
	if err := normalize.New(normalize.WithTagName("normalize", "norm", "n")).Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.A != "a" || in.B != "b" {
		t.Errorf("with n: %+v", in)
	}

	type both struct {
		S string `normalize:"trim" norm:"lower"`
	}
	err := normalize.Struct(&both{})
	var te *normalize.TagError
	if !errors.Is(err, normalize.ErrMalformedTag) || !errors.As(err, &te) || te.Field != "S" {
		t.Errorf("two keys on one field: %v", err)
	}

	in = short{A: " a "}
	if err := normalize.New(normalize.WithTagName()).Struct(&in); err != nil || in.A != "a" {
		t.Errorf("WithTagName without names keeps the defaults: %+v %v", in, err)
	}
	in = short{A: " a "}
	if err := normalize.New(normalize.WithTagName("", "")).Struct(&in); err != nil || in.A != "a" {
		t.Errorf("WithTagName with only empty names keeps the defaults: %+v %v", in, err)
	}
	in = short{B: " b "}
	if err := normalize.New(normalize.WithTagName("n", "", "n")).Struct(&in); err != nil || in.B != "b" {
		t.Errorf("a repeated name is not a field with two keys: %+v %v", in, err)
	}
}
