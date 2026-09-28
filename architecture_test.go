package dify

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The package is one package so that callers write dify.X, which leaves the
// compiler nothing to enforce between layers. These tests do it instead: each
// file belongs to one layer, and a layer may use only what its own and the
// inner layers declare.

type layer string

const (
	kernel   layer = "kernel"   // lenient JSON reads, errors: usable anywhere
	entity   layer = "entity"   // what calls return, and its behaviour
	codec    layer = "codec"    // Dify's wire shapes into entities
	portL    layer = "port"     // what a resource may ask of the wire
	usecase  layer = "use case" // the resources and their verbs
	infra    layer = "infra"    // HTTP, retries, credentials
	rootL    layer = "root"     // constructors: the one place that wires infra to ports
	unlisted layer = ""
)

var layers = map[string]layer{
	"doc.go":    kernel,
	"shape.go":  kernel,
	"errors.go": kernel,
	"clock.go":  kernel,
	"zero.go":   kernel,

	"model_app.go":       entity,
	"model_knowledge.go": entity,
	"usage.go":           entity,

	"decode_app.go":       codec,
	"decode_knowledge.go": codec,
	"stream.go":           codec,
	"paging.go":           codec,

	"port.go": portL,

	"app.go":                 usecase,
	"annotations.go":         usecase,
	"audio.go":               usecase,
	"completions.go":         usecase,
	"conversations.go":       usecase,
	"files.go":               usecase,
	"forms.go":               usecase,
	"messages.go":            usecase,
	"runs.go":                usecase,
	"knowledge.go":           usecase,
	"knowledge_datasets.go":  usecase,
	"knowledge_documents.go": usecase,
	"knowledge_metadata.go":  usecase,
	"knowledge_pipeline.go":  usecase,
	"knowledge_search.go":    usecase,
	"knowledge_segments.go":  usecase,
	"knowledge_tags.go":      usecase,

	"transport.go": infra,
	"secret.go":    infra,
	"version.go":   infra,

	"client.go": rootL,
}

// mayUse is the dependency rule. Infra implements the port and may use the
// codec to read an error body; a use case reaches the wire only through the
// port. Nothing inside the root may name what the root wires together.
var mayUse = map[layer][]layer{
	kernel:  {kernel},
	entity:  {kernel, entity},
	codec:   {kernel, entity, codec},
	portL:   {kernel, entity, portL},
	usecase: {kernel, entity, codec, portL, usecase},
	infra:   {kernel, entity, codec, portL, infra},
	rootL:   {kernel, entity, codec, portL, usecase, infra, rootL},
}

type checkedPackage struct {
	fset  *token.FileSet
	files map[string]*ast.File
	info  *types.Info
}

func typeCheck(t *testing.T) *checkedPackage {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	p := &checkedPackage{
		fset:  token.NewFileSet(),
		files: map[string]*ast.File{},
		info:  &types.Info{Uses: map[*ast.Ident]types.Object{}},
	}
	var parsed []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(p.fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		p.files[name] = f
		parsed = append(parsed, f)
	}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("github.com/langgenius/dify-go-sdk", p.fset, parsed, p.info); err != nil {
		t.Fatal(err)
	}
	return p
}

// declaredIn is the file an object of this package was declared in, or ""
// for anything from another package or the universe.
func (p *checkedPackage) declaredIn(obj types.Object) string {
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "github.com/langgenius/dify-go-sdk" {
		return ""
	}
	return filepath.Base(p.fset.Position(obj.Pos()).Filename)
}

func (p *checkedPackage) sortedFiles() []string {
	out := make([]string, 0, len(p.files))
	for name := range p.files {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func TestEveryFileBelongsToALayer(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if layers[name] == unlisted {
			t.Errorf("%s is in no layer; add it to layers in architecture_test.go, and to the table in AGENTS.md", name)
		}
	}
	for name := range layers {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("layers lists %s, which does not exist", name)
		}
	}
}

func TestEveryLayerUsesOnlyItsOwnAndInnerLayers(t *testing.T) {
	p := typeCheck(t)
	for _, name := range p.sortedFiles() {
		from := layers[name]
		allowed := map[layer]bool{}
		for _, l := range mayUse[from] {
			allowed[l] = true
		}
		reported := map[string]bool{}
		ast.Inspect(p.files[name], func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := p.info.Uses[id]
			decl := p.declaredIn(obj)
			if decl == "" || decl == name {
				return true
			}
			to := layers[decl]
			if allowed[to] || reported[obj.Name()] {
				return true
			}
			reported[obj.Name()] = true
			t.Errorf("%s: %s (%s) uses %s from %s (%s)", p.fset.Position(id.Pos()), name, from, obj.Name(), decl, to)
			return true
		})
	}
}

// A resource passes Dify's answer to a decoder, or hands it back whole; it
// does not pick fields out of it. That is what keeps every lenient read, and
// every fallback between two spellings of a field, in the codec files.
func TestResourcesHandDifysJSONToADecoderWithoutReadingIt(t *testing.T) {
	passThrough := map[string]bool{"object": true, "raw": true, "orEmpty": true}
	p := typeCheck(t)
	for _, name := range p.sortedFiles() {
		if layers[name] != usecase {
			continue
		}
		ast.Inspect(p.files[name], func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := p.info.Uses[id]
			if p.declaredIn(obj) != "shape.go" || passThrough[obj.Name()] {
				return true
			}
			t.Errorf("%s: %s reads Dify's JSON with %s; move the read into a decode_*.go function", p.fset.Position(id.Pos()), name, obj.Name())
			return true
		})
		// Building or converting to an object is decoding too, even though
		// the type name itself may appear in a fetch signature.
		ast.Inspect(p.files[name], func(n ast.Node) bool {
			var typ ast.Expr
			switch n := n.(type) {
			case *ast.CompositeLit:
				typ = n.Type
			case *ast.CallExpr:
				typ = n.Fun
			default:
				return true
			}
			if id, ok := typ.(*ast.Ident); ok && id.Name == "object" && p.declaredIn(p.info.Uses[id]) == "shape.go" {
				t.Errorf("%s: %s builds an object; move it into a decode_*.go function", p.fset.Position(id.Pos()), name)
			}
			return true
		})
	}
}
