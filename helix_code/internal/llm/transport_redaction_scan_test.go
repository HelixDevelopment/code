package llm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// transport_redaction_scan_test.go — the mechanical half of the round-8
// BLOCKING finding, and the reason RedactEndpointsInError's doc comment is now
// allowed to make a claim about "every call site" at all.
//
// WHY THIS FILE EXISTS. The defect was not the sixteen missing wraps. It was
// that the helper's own doc comment asserted
//
//	"Every call site therefore redacts the error the moment it comes back
//	 from the transport, and wraps afterwards."
//
// while ten of the twelve wraps in the file DEFINING the helper did no such
// thing — and nothing anywhere could tell. A reader greps for the helper,
// reads the sentence, and stops looking. That is the same failure that let the
// KoboldAI gate site through one round earlier, one layer up: a scoped fix
// carrying a universal claim.
//
// The sibling scan (cloud_gate_redaction_scan_test.go) enforces the OTHER
// carrier — a configured endpoint quoted into an ErrCloudDisabled refusal —
// and is keyed on that sentinel, so it has, by construction, zero visibility
// into transport wraps. This file is the transport half.
//
// THE SCOPE RULE, and why it is not a hand-written list.
//
// A hand-written list of "covered files" is a list somebody must remember to
// extend, which is the weakness being fixed. A repo-wide rule would flag every
// provider that has never adopted the helper (ollama_provider.go among them —
// which carries no credential at all, so it has nothing to leak) and would
// therefore have to be silenced with exceptions, which is the same list
// wearing a different hat.
//
// So scope is SELF-SELECTING and mechanical: a non-test .go file that
// MENTIONS RedactEndpointsInError has adopted the discipline, and every
// transport-derived error it formats into an operator-facing message must go
// through the helper. PARTIAL ADOPTION IS THE DEFECT THIS CATCHES — and
// partial adoption is exactly what round 8 found, in the file that defines the
// helper.
//
// HONEST COVERAGE BOUNDARY (§11.4.6) — what this scan does NOT see:
//
//   - Files that never mention the helper. That is the scope rule, not an
//     oversight: such a file makes no claim to redact, and this scan makes
//     none on its behalf. As of this round the in-scope set is
//     internal/llm/openai_compatible_provider.go, internal/llm/koboldai_provider.go
//     and internal/llm/providers/helixagent/{helixagent,embeddings}.go; the
//     test LOGS the live set, so the set is observable rather than asserted.
//   - Taint is POSITION-ORDERED, not control-flow-aware: an identifier is
//     considered transport-derived from the assignment that made it so until
//     the next assignment to that same name, by source position. Straight-line
//     Go — which every one of these transport helpers is — is handled exactly;
//     a transport error carried across a branch into a later format call, or
//     out through a closure, is not tracked.
//   - Taint does not survive a second hop (e := err; fmt.Errorf(..., e)).
//   - Only fmt.Errorf / fmt.Sprintf / fmt.Sprint / fmt.Sprintln are treated as
//     operator-facing formatters. A custom formatter is invisible.
//
// The scan's own §1.1 proof is TestTransportRedactionScan_CatchesTheKnownDefectShapes:
// a scan nobody has watched FAIL is an assertion about a scan, not a scan.

// transportRedactionHelperName is the one function that renders a transport
// error safe to interpolate. A file that mentions it has adopted the
// discipline and is in scope.
const transportRedactionHelperName = "RedactEndpointsInError"

// transportProducerSelectors are the calls that yield an error carrying the
// request URL. `Do` is net/http's Client.Do (and every wrapper that keeps the
// name); the NewRequest pair returns &url.Error{Op:"parse", URL: rawURL} with
// the raw value untouched.
var transportProducerSelectors = map[string]bool{
	"Do":                    true,
	"NewRequest":            true,
	"NewRequestWithContext": true,
	"Get":                   true, // http.Get / client.Get
	"Post":                  true,
	"Head":                  true,
	"PostForm":              true,
}

// operatorFacingFormatters are the calls whose output an operator can read.
var operatorFacingFormatters = map[string]bool{
	"Errorf": true, "Sprintf": true, "Sprint": true, "Sprintln": true,
}

// transportLeakSite is one operator-facing format call that interpolates a
// transport-derived error WITHOUT passing it through the helper.
type transportLeakSite struct {
	file string
	line int
	fn   string
	name string // the leaking identifier
}

func (s transportLeakSite) String() string {
	return fmt.Sprintf("%s:%d (%s) formats transport-derived %q unredacted", s.file, s.line, s.fn, s.name)
}

// scanSourceForTransportRedactionLeaks reports every operator-facing format
// call in one file that interpolates a transport-derived error unredacted.
//
// Takes the source as bytes rather than a path so the scan can be proven
// against fixtures (§1.1).
func scanSourceForTransportRedactionLeaks(t *testing.T, path string, src []byte) []transportLeakSite {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var out []transportLeakSite
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		out = append(out, scanFuncForTransportLeaks(fset, path, funcIdentity(fn), fn.Body)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].line < out[j].line })
	return out
}

func funcIdentity(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		return "(recv)." + fn.Name.Name
	}
	return fn.Name.Name
}

// assignEvent is one assignment to an identifier, with whether its RHS was a
// transport producer.
type assignEvent struct {
	pos     token.Pos
	tainted bool
}

func scanFuncForTransportLeaks(fset *token.FileSet, path, fnName string, body *ast.BlockStmt) []transportLeakSite {
	// Pass 1 — record every assignment to every identifier, in source order,
	// tagged with whether the right-hand side is a transport producer.
	events := map[string][]assignEvent{}
	record := func(lhs []ast.Expr, rhs []ast.Expr, pos token.Pos) {
		tainted := false
		for _, r := range rhs {
			if callIsTransportProducer(r) {
				tainted = true
			}
		}
		for _, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			events[id.Name] = append(events[id.Name], assignEvent{pos: pos, tainted: tainted})
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			record(s.Lhs, s.Rhs, s.Pos())
		case *ast.DeclStmt:
			gd, ok := s.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				var lhs []ast.Expr
				for _, nm := range vs.Names {
					lhs = append(lhs, nm)
				}
				record(lhs, vs.Values, s.Pos())
			}
		}
		return true
	})
	for name := range events {
		ev := events[name]
		sort.Slice(ev, func(i, j int) bool { return ev[i].pos < ev[j].pos })
		events[name] = ev
	}
	taintedAt := func(name string, use token.Pos) bool {
		ev := events[name]
		tainted := false
		for _, e := range ev {
			if e.pos >= use {
				break
			}
			tainted = e.tainted
		}
		return tainted
	}

	// Pass 2 — every operator-facing format call, checking each BARE
	// identifier argument. An argument already wrapped in the helper is not a
	// bare identifier, so it is compliant by construction.
	var out []transportLeakSite
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isOperatorFacingFormatter(call.Fun) {
			return true
		}
		for _, arg := range call.Args {
			id, ok := arg.(*ast.Ident)
			if !ok {
				continue
			}
			if !taintedAt(id.Name, call.Pos()) {
				continue
			}
			out = append(out, transportLeakSite{
				file: path,
				line: fset.Position(call.Pos()).Line,
				fn:   fnName,
				name: id.Name,
			})
		}
		return true
	})
	return out
}

// callIsTransportProducer reports whether expr is a call whose error result
// carries the request URL.
func callIsTransportProducer(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if !transportProducerSelectors[sel.Sel.Name] {
		return false
	}
	// `Get`/`Post`/`Head` are common method names; require the receiver to
	// look like an http client or the http package, so a cache.Get or a
	// map-ish Get is not treated as a transport call. `Do` and the NewRequest
	// pair are distinctive enough to accept on any receiver, which errs toward
	// flagging — the direction that costs a contributor one line rather than a
	// credential.
	switch sel.Sel.Name {
	case "Do", "NewRequest", "NewRequestWithContext":
		return true
	}
	return receiverLooksLikeHTTP(sel.X)
}

func receiverLooksLikeHTTP(x ast.Expr) bool {
	var sb strings.Builder
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch v := e.(type) {
		case *ast.Ident:
			sb.WriteString(v.Name)
		case *ast.SelectorExpr:
			walk(v.X)
			sb.WriteString(".")
			sb.WriteString(v.Sel.Name)
		}
	}
	walk(x)
	s := strings.ToLower(sb.String())
	return strings.Contains(s, "http") || strings.Contains(s, "client")
}

func isOperatorFacingFormatter(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" {
		return false
	}
	return operatorFacingFormatters[sel.Sel.Name]
}

// transportGovernedSourceFiles walks the module and returns every non-test .go
// file that MENTIONS the helper — i.e. every file that has adopted the
// redaction discipline and must therefore apply it everywhere.
func transportGovernedSourceFiles(t *testing.T) map[string][]byte {
	t.Helper()
	root := "../.." // the test's cwd is internal/llm
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", "node_modules", "testdata", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(src), transportRedactionHelperName) {
			out[path] = src
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Fatalf("no source file under %s mentions %s — the walk is broken, so every "+
			"assertion built on it would be vacuously true", root, transportRedactionHelperName)
	}
	return out
}

// TestTransportWraps_RedactBeforeFormatting is the enforcement the helper's doc
// comment now cites. Every file that has adopted RedactEndpointsInError must
// route EVERY transport-derived error through it before formatting.
func TestTransportWraps_RedactBeforeFormatting(t *testing.T) {
	files := transportGovernedSourceFiles(t)

	var inScope []string
	var findings []transportLeakSite
	for path, src := range files {
		inScope = append(inScope, path)
		findings = append(findings, scanSourceForTransportRedactionLeaks(t, path, src)...)
	}
	sort.Strings(inScope)

	// The in-scope set is LOGGED, not asserted against a pinned list: a pinned
	// list would be the hand-maintained thing this scan exists to replace. It
	// is logged so a reviewer can see the boundary rather than infer it.
	t.Logf("in-scope files (mention %s, therefore fully governed): %d", transportRedactionHelperName, len(inScope))
	for _, p := range inScope {
		t.Logf("  %s", p)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Errorf("CONST-042 / Article XII §12.1: %d transport-derived error(s) are formatted "+
			"into an operator-facing message without passing through %s.\n"+
			"fmt.Errorf formats EAGERLY, so redacting after wrapping rewrites a string that "+
			"already contains the credential — the call must be %s(err), inside the format "+
			"call, not around it.%s",
			len(findings), transportRedactionHelperName, transportRedactionHelperName, b.String())
	}
}

// TestTransportRedactionScan_CatchesTheKnownDefectShapes is the scan's §1.1
// proof. Each golden-bad fixture is a shape that WAS live in this repo before
// round 8; each golden-good fixture is its fixed form plus the shapes that
// must NOT be flagged.
func TestTransportRedactionScan_CatchesTheKnownDefectShapes(t *testing.T) {
	const header = "package p\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n)\n\nvar _ = RedactEndpointsInError\n\n"

	cases := []struct {
		name      string
		body      string
		wantLeaks int
		why       string
	}{
		{
			name: "golden_bad_client_do_wrapped_raw",
			body: `func f(c *http.Client, req *http.Request) error {
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	_ = resp
	return nil
}`,
			wantLeaks: 1,
			why:       "the exact shape found at eight sites across the two providers in round 8",
		},
		{
			name: "golden_bad_newrequest_wrapped_raw",
			body: `func f(url string) error {
	req, err := http.NewRequestWithContext(nil, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	_ = req
	return nil
}`,
			wantLeaks: 1,
			why:       "the request-build carrier: url.Parse returns &url.Error{URL: rawURL} untouched",
		},
		{
			name: "golden_bad_sprintf_into_health_message",
			body: `func f(c *http.Client, req *http.Request) string {
	_, err := c.Do(req)
	return fmt.Sprintf("unreachable: %v", err)
}`,
			wantLeaks: 1,
			why:       "Sprintf into a health Message is the same carrier as Errorf",
		},
		{
			name: "golden_good_redacted_inside_the_format_call",
			body: `func f(c *http.Client, req *http.Request) error {
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", RedactEndpointsInError(err))
	}
	_ = resp
	return nil
}`,
			wantLeaks: 0,
			why:       "the fixed form",
		},
		{
			name: "golden_good_returned_bare_through_the_helper",
			body: `func f(c *http.Client, req *http.Request) error {
	_, err := c.Do(req)
	if err != nil {
		return RedactEndpointsInError(err)
	}
	return nil
}`,
			wantLeaks: 0,
			why:       "no format call at all, so nothing is interpolated",
		},
		{
			name: "golden_good_non_transport_error_reusing_the_name",
			body: `func f(c *http.Client, req *http.Request) error {
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("do: %w", RedactEndpointsInError(err))
	}
	body, err := readAll(resp)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	_ = body
	return nil
}`,
			wantLeaks: 0,
			why: "the false-positive direction: `err` is REASSIGNED from a non-transport call " +
				"before this format, so flagging it would force a pointless wrap and train " +
				"contributors to ignore the scan",
		},
		{
			name: "golden_good_non_http_get_is_not_a_transport_call",
			body: `func f(cache *store) error {
	v, err := cache.Get("k")
	if err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	_ = v
	return nil
}`,
			wantLeaks: 0,
			why:       "Get on a non-HTTP receiver must not be treated as a transport producer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanSourceForTransportRedactionLeaks(t, tc.name+".go", []byte(header+tc.body+"\n"))
			if len(got) != tc.wantLeaks {
				t.Fatalf("scan reported %d leak(s), want %d (%s).\n  findings: %v\n  source:\n%s",
					len(got), tc.wantLeaks, tc.why, got, tc.body)
			}
		})
	}
}
