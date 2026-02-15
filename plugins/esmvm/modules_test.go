package esmvm


import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/sobek"
)

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create parent dir for %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func TestESMModuleLoaderRunEntrypointMJS(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "dep.mjs"), []byte(`export const staticValue = 42;`), 0644)
	if err != nil {
		t.Fatalf("failed to write dep.mjs: %v", err)
	}

	err = os.WriteFile(filepath.Join(dir, "dyn.mjs"), []byte(`export const dynamicValue = 99;`), 0644)
	if err != nil {
		t.Fatalf("failed to write dyn.mjs: %v", err)
	}

	mainPath := filepath.Join(dir, "main.mjs")
	mainSource := []byte(`
		import { staticValue } from "./dep.mjs";
		globalThis.__staticValue = staticValue;
		const mod = await import("./dyn.mjs");
		globalThis.__dynamicValue = mod.dynamicValue;
	`)

	err = os.WriteFile(mainPath, mainSource, 0644)
	if err != nil {
		t.Fatalf("failed to write main.mjs: %v", err)
	}

	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	loader := newESMModuleLoader(vm, loop, dir)
	loader.Setup()

	var entryResult sobek.Value

	err = loop.Start(func() error {
		result, runErr := loader.RunEntrypoint(mainPath, mainSource)
		entryResult = result
		return runErr
	})
	if err != nil {
		t.Fatalf("expected module entrypoint to run without error, got: %v", err)
	}

	if entryResult == nil {
		t.Fatal("expected module entrypoint to return a promise value")
	}

	staticValue := vm.Get("__staticValue")
	if staticValue == nil {
		t.Fatal("expected __staticValue to be set")
	}
	if got := staticValue.ToInteger(); got != 42 {
		t.Fatalf("expected __staticValue=42, got %d", got)
	}

	dynamicValue := vm.Get("__dynamicValue")
	if dynamicValue == nil {
		promise, ok := entryResult.Export().(*sobek.Promise)
		if !ok {
			t.Fatalf("expected entryResult to export to *sobek.Promise, got %T", entryResult.Export())
		}

		result := promise.Result()
		if result == nil {
			t.Fatalf("expected __dynamicValue to be set by dynamic import; promise state=%v result=<nil>", promise.State())
		}

		t.Fatalf("expected __dynamicValue to be set by dynamic import; promise state=%v result=%s", promise.State(), result.String())
	}
	if got := dynamicValue.ToInteger(); got != 99 {
		t.Fatalf("expected __dynamicValue=99, got %d", got)
	}
}

func TestESMModuleLoaderRunEntrypointScriptFallback(t *testing.T) {
	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	loader := newESMModuleLoader(vm, loop, t.TempDir())
	loader.Setup()

	err := loop.Start(func() error {
		_, runErr := loader.RunEntrypoint("hook.pb.js", []byte(`globalThis.__scriptFallback = 123;`))
		return runErr
	})
	if err != nil {
		t.Fatalf("expected script fallback to run without error, got: %v", err)
	}

	if got := vm.Get("__scriptFallback").ToInteger(); got != 123 {
		t.Fatalf("expected __scriptFallback=123, got %d", got)
	}
}

func TestESMModuleLoaderNestedRelativeResolutionWithFallbacks(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "shared", "value.mjs"), `export const value = 7;`)
	writeTestFile(t, filepath.Join(dir, "nested", "feature", "index.mjs"), `export const answer = 21;`)
	writeTestFile(t, filepath.Join(dir, "nested", "child.mjs"), `
		import { value } from "../shared/value";
		import { answer } from "./feature";
		const dyn = await import("./feature/index");
		export const total = value + answer + dyn.answer;
	`)

	mainPath := filepath.Join(dir, "main.mjs")
	mainSource := []byte(`
		import { total } from "./nested/child.mjs";
		globalThis.__total = total;
	`)
	writeTestFile(t, mainPath, string(mainSource))

	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	loader := newESMModuleLoader(vm, loop, dir)
	loader.Setup()

	err := loop.Start(func() error {
		_, runErr := loader.RunEntrypoint(mainPath, mainSource)
		return runErr
	})
	if err != nil {
		t.Fatalf("expected nested relative imports with fallback resolution to succeed, got: %v", err)
	}

	if got := vm.Get("__total").ToInteger(); got != 49 {
		t.Fatalf("expected __total=49, got %d", got)
	}
}

func TestESMModuleLoaderExtensionFallbackPrefersJSOverMJS(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "mod.js"), `export const source = "js";`)
	writeTestFile(t, filepath.Join(dir, "mod.mjs"), `export const source = "mjs";`)

	mainPath := filepath.Join(dir, "main.mjs")
	mainSource := []byte(`
		import { source } from "./mod";
		globalThis.__source = source;
	`)
	writeTestFile(t, mainPath, string(mainSource))

	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	loader := newESMModuleLoader(vm, loop, dir)
	loader.Setup()

	err := loop.Start(func() error {
		_, runErr := loader.RunEntrypoint(mainPath, mainSource)
		return runErr
	})
	if err != nil {
		t.Fatalf("expected extension fallback import to succeed, got: %v", err)
	}

	if got := vm.Get("__source").String(); got != "js" {
		t.Fatalf("expected extension fallback to prefer .js, got %q", got)
	}
}

func TestESMModuleLoaderBareSpecifierRejected(t *testing.T) {
	dir := t.TempDir()

	mainPath := filepath.Join(dir, "main.mjs")
	mainSource := []byte(`import "lodash";`)
	writeTestFile(t, mainPath, string(mainSource))

	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	loader := newESMModuleLoader(vm, loop, dir)
	loader.Setup()

	err := loop.Start(func() error {
		_, runErr := loader.RunEntrypoint(mainPath, mainSource)
		return runErr
	})
	if err == nil {
		t.Fatal("expected bare specifier import to fail")
	}

	if !strings.Contains(err.Error(), "unsupported bare ESM import specifier \"lodash\"") {
		t.Fatalf("expected bare specifier error, got: %v", err)
	}
}

func TestESMModuleLoaderDynamicImportMissingModuleReturnsEntrypointError(t *testing.T) {
	dir := t.TempDir()

	mainPath := filepath.Join(dir, "main.mjs")
	mainSource := []byte(`import("./missing.mjs");`)
	writeTestFile(t, mainPath, string(mainSource))

	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	loader := newESMModuleLoader(vm, loop, dir)
	loader.Setup()

	err := loop.Start(func() error {
		_, runErr := loader.RunEntrypoint(mainPath, mainSource)
		return runErr
	})
	if err == nil {
		t.Fatal("expected dynamic import of missing module to fail")
	}

	if !strings.Contains(err.Error(), "cannot resolve ESM import \"./missing.mjs\"") {
		t.Fatalf("expected missing import resolution error, got: %v", err)
	}
}

func TestESMModuleLoaderDynamicImportHandledRejectionSucceeds(t *testing.T) {
	dir := t.TempDir()

	mainPath := filepath.Join(dir, "main.mjs")
	mainSource := []byte(`
		try {
			await import("./missing.mjs");
			globalThis.__handled = false;
		} catch (err) {
			globalThis.__handled = true;
		}
	`)
	writeTestFile(t, mainPath, string(mainSource))

	vm := sobek.New()
	loop := NewEventLoop(vm, context.Background())
	loader := newESMModuleLoader(vm, loop, dir)
	loader.Setup()

	err := loop.Start(func() error {
		_, runErr := loader.RunEntrypoint(mainPath, mainSource)
		return runErr
	})
	if err != nil {
		t.Fatalf("expected handled dynamic import rejection to succeed, got: %v", err)
	}

	if got := vm.Get("__handled").ToBoolean(); !got {
		t.Fatal("expected dynamic import rejection to be catchable")
	}
}
