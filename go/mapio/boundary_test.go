package mapio

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTheLibraryTouchesNothingOutsideItsArguments is the module's boundary as a check, over the AST
// of every shipped source file: no file or environment access, no network, no process, no logging,
// no goroutine, no init. Validation therefore cannot start itself, and a diagnostic can only be a
// returned value. schemasync is a developer command and is not linked into anything.
func TestTheLibraryTouchesNothingOutsideItsArguments(t *testing.T) {
	forbiddenImports := map[string]bool{
		"os": true, "os/exec": true, "os/signal": true, "net": true, "net/http": true,
		"log": true, "log/slog": true, "io/ioutil": true, "syscall": true,
	}
	files := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" || path == filepath.Join("internal", "schemasync") || path == filepath.Join("internal", "apmaptest") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(files, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		checked++
		for _, spec := range parsed.Imports {
			name, _ := strconv.Unquote(spec.Path.Value)
			if forbiddenImports[name] {
				t.Errorf("%s imports %s", path, name)
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.GoStmt:
				t.Errorf("%s starts a goroutine", files.Position(typed.Pos()))
			case *ast.FuncDecl:
				if typed.Recv == nil && typed.Name.Name == "init" {
					t.Errorf("%s has an init function", files.Position(typed.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 30 {
		t.Fatalf("only %d source files were checked; the walk is not seeing the module", checked)
	}
}
