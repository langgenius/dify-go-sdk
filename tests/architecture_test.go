package tests

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/langgenius/dify-go-sdk"

// mayImport is the dependency rule between the layers under internal/: each
// imports only layers inside it. The compiler already refuses a cycle; this
// refuses the direction that compiles but is wrong, such as a use case
// reaching for infra instead of the port.
var mayImport = map[string][]string{
	"kernel":  {},
	"entity":  {"kernel"},
	"codec":   {"kernel", "entity"},
	"port":    {"kernel", "entity"},
	"usecase": {"kernel", "entity", "codec", "port"},
	"infra":   {"kernel", "entity", "codec", "port"},
}

func TestEachLayerImportsOnlyTheLayersInsideIt(t *testing.T) {
	dirs, err := os.ReadDir("../internal")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		layer := d.Name()
		allowed, known := mayImport[layer]
		if !known {
			t.Errorf("internal/%s is in no layer; add it to mayImport, and to the table in AGENTS.md", layer)
			continue
		}
		ok := map[string]bool{}
		for _, a := range allowed {
			ok[module+"/internal/"+a] = true
		}
		files, _ := filepath.Glob(filepath.Join("../internal", layer, "*.go"))
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, im := range f.Imports {
				path, _ := strconv.Unquote(im.Path.Value)
				if strings.HasPrefix(path, module) && !ok[path] {
					t.Errorf("%s imports %s; %s may import only %v", file, path, layer, allowed)
				}
			}
		}
	}
}

// A resource hands Dify's answer to a decoder, or returns it whole with Raw;
// it does not pick fields out of it. That keeps every lenient read, and every
// fallback between two spellings of one field, in internal/codec.
func TestResourcesHandDifysJSONToADecoderWithoutReadingIt(t *testing.T) {
	fset, files, info := typeCheck(t, "../internal/usecase", module+"/internal/usecase")
	passThrough := map[string]bool{"Object": true, "Raw": true, "OrEmpty": true}
	kernel := module + "/internal/kernel"
	var found []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				obj := info.Uses[n]
				if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != kernel || passThrough[obj.Name()] {
					return true
				}
				if fn, ok := obj.(*types.Func); ok && fn.Type().(*types.Signature).Recv() != nil || isShapeHelper(obj) {
					found = append(found, fset.Position(n.Pos()).String()+": reads Dify's JSON with "+obj.Name())
				}
			case *ast.CompositeLit:
				if isKernelObject(info.TypeOf(n), kernel) {
					found = append(found, fset.Position(n.Pos()).String()+": builds a kernel.Object")
				}
			case *ast.CallExpr:
				if tv, ok := info.Types[n.Fun]; ok && tv.IsType() && isKernelObject(tv.Type, kernel) {
					found = append(found, fset.Position(n.Pos()).String()+": converts to a kernel.Object")
				}
			}
			return true
		})
	}
	sort.Strings(found)
	for _, f := range found {
		t.Errorf("%s; move it into an internal/codec decoder", f)
	}
}

func isShapeHelper(obj types.Object) bool {
	switch obj.Name() {
	case "AsString", "AsInt64", "AsFloat", "Truthy":
		return true
	}
	return false
}

func isKernelObject(t types.Type, kernel string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Name() == "Object" && n.Obj().Pkg().Path() == kernel
}

// typeCheck checks one package of this module from source, importing its
// dependencies from the export data `go list` builds.
func typeCheck(t *testing.T, dir, path string) (*token.FileSet, []*ast.File, *types.Info) {
	t.Helper()
	cmd := exec.Command("go", "list", "-export", "-deps", "-f", "{{.ImportPath}}={{.Export}}", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	exports := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			exports[k] = v
		}
	}
	fset := token.NewFileSet()
	names, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	imp := importer.ForCompiler(fset, "gc", func(p string) (io.ReadCloser, error) { return os.Open(exports[p]) })
	if _, err := (&types.Config{Importer: imp}).Check(path, fset, files, info); err != nil {
		t.Fatal(err)
	}
	return fset, files, info
}
