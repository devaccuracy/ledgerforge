/*
Copyright 2026 LedgerForge Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/devaccuracy/ledgerforge"

// The MCP server is a transport adapter that consumes the public facade.
// This explicit package-to-import exception covers its transport tools; new domain packages inherit the rules.
var legacyExceptions = map[string]map[string]bool{
	"internal/mcpserver/": {module: true},
}

func forbiddenImport(file, dependency string) bool {
	if legacyExceptions[filepath.ToSlash(filepath.Dir(file))+"/"][dependency] {
		return false
	}
	business := strings.HasPrefix(file, "internal/") || strings.HasPrefix(file, "model/") || strings.HasPrefix(file, "database/") || strings.HasPrefix(file, "config/") || file == "ledgerforge.go"
	if business && (dependency == "github.com/gin-gonic/gin" || strings.HasPrefix(dependency, "github.com/gin-gonic/gin/")) {
		return true
	}
	domain := business && file != "ledgerforge.go" && !strings.HasPrefix(file, "internal/core/")
	if !domain {
		return false
	}
	for _, forbidden := range []string{module, module + "/api", module + "/internal/core"} {
		if dependency == forbidden || (forbidden != module && strings.HasPrefix(dependency, forbidden+"/")) {
			return true
		}
	}
	// The extracted services consume repository interfaces instead of concrete storage.
	if strings.HasPrefix(file, "internal/reconciliation/") || strings.HasPrefix(file, "internal/search/") {
		return dependency == module+"/database" || strings.HasPrefix(dependency, module+"/database/")
	}
	return false
}

func TestImportBoundaries(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate architecture test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	files := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			dependency, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if forbiddenImport(filepath.ToSlash(relative), dependency) {
				t.Errorf("%s: forbidden dependency %s", files.Position(imported.Pos()), dependency)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBoundaryRules(t *testing.T) {
	for _, file := range []string{"internal/reconciliation/service.go", "internal/search/jobs.go", "internal/policy/service.go", "internal/integrity/service.go", "internal/future/service.go"} {
		for _, dependency := range []string{module, module + "/api", module + "/api/model", module + "/internal/core", "github.com/gin-gonic/gin"} {
			if !forbiddenImport(file, dependency) {
				t.Errorf("%s should reject %s", file, dependency)
			}
		}
		if forbiddenImport(file, module+"/model") {
			t.Errorf("%s must accept shared domain models", file)
		}
	}
	if forbiddenImport("internal/mcpserver/server.go", module) {
		t.Fatal("explicit legacy transport exception rejected")
	}
	if !forbiddenImport("internal/future/service.go", module) {
		t.Fatal("transport exception leaked to a domain package")
	}
}
