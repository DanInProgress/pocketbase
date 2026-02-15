package esmvm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/grafana/sobek"
)

// esmModuleLoader manages Sobek ESM module resolution and dynamic import wiring
// for a single runtime.
type esmModuleLoader struct {
	rt        *sobek.Runtime
	eventLoop *EventLoop
	baseDir   string

	mux         sync.RWMutex
	cache       map[string]sobek.ModuleRecord
	modulePaths map[sobek.ModuleRecord]string
}

func newESMModuleLoader(rt *sobek.Runtime, eventLoop *EventLoop, baseDir string) *esmModuleLoader {
	return &esmModuleLoader{
		rt:          rt,
		eventLoop:   eventLoop,
		baseDir:     filepath.Clean(baseDir),
		cache:       make(map[string]sobek.ModuleRecord),
		modulePaths: make(map[sobek.ModuleRecord]string),
	}
}

func (l *esmModuleLoader) Setup() {
	l.rt.SetImportModuleDynamically(l.importDynamically)
}

func (l *esmModuleLoader) RunEntrypoint(path string, source []byte) (sobek.Value, error) {
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(l.baseDir, absPath)
	}
	absPath = filepath.Clean(absPath)

	if filepath.Ext(absPath) != ".mjs" {
		return l.rt.RunScript(defaultScriptPath, string(source))
	}

	module, err := sobek.ParseModule(absPath, string(source), l.resolveImportedModule)
	if err != nil {
		return nil, fmt.Errorf("failed to parse module %q: %w", absPath, err)
	}

	l.mux.Lock()
	l.modulePaths[module] = absPath
	l.mux.Unlock()

	if err := module.Link(); err != nil {
		l.mux.Lock()
		delete(l.modulePaths, module)
		l.mux.Unlock()
		return nil, fmt.Errorf("failed to link module %q: %w", absPath, err)
	}

	promise := module.Evaluate(l.rt)
	if promise == nil {
		return sobek.Undefined(), nil
	}

	if promise.State() != sobek.PromiseStateRejected {
		return l.rt.ToValue(promise), nil
	}

	result := promise.Result()
	if result == nil || sobek.IsNull(result) || sobek.IsUndefined(result) {
		return nil, errors.New("module evaluation rejected")
	}

	if promiseErr, ok := result.Export().(error); ok {
		return nil, normalizeException(promiseErr)
	}

	return nil, fmt.Errorf("module evaluation rejected: %s", result.String())
}

func (l *esmModuleLoader) importDynamically(referrer interface{}, specifier sobek.Value, promiseCapability interface{}) {
	if specifier == nil || sobek.IsNull(specifier) || sobek.IsUndefined(specifier) {
		err := fmt.Errorf("dynamic import requires a non-empty specifier")
		l.rt.FinishLoadingImportModule(referrer, specifier, promiseCapability, nil, err)
		return
	}

	module, err := l.resolveImportedModule(referrer, specifier.String())
	l.rt.FinishLoadingImportModule(referrer, specifier, promiseCapability, module, err)
}

func (l *esmModuleLoader) resolveImportedModule(referrer interface{}, specifier string) (sobek.ModuleRecord, error) {
	if specifier == "" {
		return nil, fmt.Errorf("empty module specifier")
	}

	resolvedPath, err := l.resolvePath(referrer, specifier)
	if err != nil {
		return nil, err
	}

	return l.loadModule(resolvedPath)
}

func (l *esmModuleLoader) resolvePath(referrer interface{}, specifier string) (string, error) {
	if filepath.IsAbs(specifier) {
		return l.resolveFilePath(filepath.Clean(specifier), specifier)
	}

	if !strings.HasPrefix(specifier, "./") && !strings.HasPrefix(specifier, "../") {
		return "", fmt.Errorf("unsupported bare ESM import specifier %q", specifier)
	}

	baseDir := l.baseDir
	if refPath := l.resolveReferrerPath(referrer); refPath != "" {
		baseDir = filepath.Dir(refPath)
	}

	resolved := filepath.Clean(filepath.Join(baseDir, specifier))
	return l.resolveFilePath(resolved, specifier)
}

func (l *esmModuleLoader) resolveReferrerPath(referrer interface{}) string {
	switch v := referrer.(type) {
	case sobek.ModuleRecord:
		l.mux.RLock()
		path := l.modulePaths[v]
		l.mux.RUnlock()
		return path
	case string:
		if v == "" {
			return ""
		}
		if filepath.IsAbs(v) {
			return filepath.Clean(v)
		}
		return filepath.Clean(filepath.Join(l.baseDir, v))
	default:
		return ""
	}
}

func (l *esmModuleLoader) resolveFilePath(basePath string, originalSpecifier string) (string, error) {
	candidates := []string{basePath}

	if ext := filepath.Ext(basePath); ext == "" {
		candidates = append(candidates,
			basePath+".js",
			basePath+".mjs",
			basePath+".cjs",
		)
	}

	candidates = append(candidates,
		filepath.Join(basePath, "index.js"),
		filepath.Join(basePath, "index.mjs"),
		filepath.Join(basePath, "index.cjs"),
	)

	seen := map[string]struct{}{}

	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}

		return candidate, nil
	}

	return "", fmt.Errorf("cannot resolve ESM import %q", originalSpecifier)
}

func (l *esmModuleLoader) loadModule(path string) (sobek.ModuleRecord, error) {
	l.mux.RLock()
	cached := l.cache[path]
	l.mux.RUnlock()
	if cached != nil {
		return cached, nil
	}

	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read module %q: %w", path, err)
	}

	module, err := sobek.ParseModule(path, string(source), l.resolveImportedModule)
	if err != nil {
		return nil, fmt.Errorf("failed to parse module %q: %w", path, err)
	}

	l.mux.Lock()
	if cached := l.cache[path]; cached != nil {
		l.mux.Unlock()
		return cached, nil
	}
	l.cache[path] = module
	l.modulePaths[module] = path
	l.mux.Unlock()

	if err := module.Link(); err != nil {
		l.mux.Lock()
		delete(l.cache, path)
		delete(l.modulePaths, module)
		l.mux.Unlock()
		return nil, fmt.Errorf("failed to link module %q: %w", path, err)
	}

	return module, nil
}
