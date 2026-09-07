package llm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// cloud_gate_redaction_scan_test.go — the PER-GATE-SITE half of the CONST-042
// endpoint-redaction guard (round-5 review, BLOCKER 1).
//
// WHY THIS FILE EXISTS. The first redaction guard was written against ONE
// constructor, and the follow-up instruction that produced it said "check the
// other sites IN THESE TWO FILES" — it scoped the sweep by FILE. The defect
// class is per GATE SITE: NewKoboldAIProvider was added by a different agent
// in the same batch, carried the identical raw-%q leak, and a file-scoped
// sweep structurally could not see it. A guard that needs a human to remember
// to add a table row has exactly the weakness that let the third site through.
//
// So this scan finds the sites the way the DEFECT is defined — every
// construction of the ErrCloudDisabled sentinel, anywhere under internal/,
// discovered by walking the tree rather than by naming files — and asserts
// mechanically that any endpoint-shaped value such a site quotes passes
// through RedactEndpointForMessage. A FOURTH gate site added in a package
// nobody thought to list is covered the moment it is written.
//
// It is an AST walk, not a line regexp, for the reason the sibling
// factory_typed_nil_test.go records: it judges the EXPRESSION, so
// parenthesisation, multi-line calls and gofmt reflows are all handled
// identically, and it follows the value through a local — which is precisely
// the shape the KoboldAI leak had:
//
//	detail := fmt.Sprintf("endpoint %q is not local", endpoint)   // tainted
//	return nil, fmt.Errorf("%w: … %s …", ErrCloudDisabled, detail)
//
// The scan's own §1.1 proof is TestCloudGateRedactionScan_CatchesEveryKnownDefectShape:
// a scan nobody has watched FAIL is an assertion about a scan, not a scan.

// gateSentinelName is the sentinel every cloud-gate refusal wraps. A call that
// mentions it — bare or package-qualified (llm.ErrCloudDisabled) — IS a gate
// refusal by construction.
const gateSentinelName = "ErrCloudDisabled"

// redactionHelperName is the one function that renders a configured endpoint
// safe to quote. An endpoint that reaches a refusal through it is safe; one
// that does not is the defect.
const redactionHelperName = "RedactEndpointForMessage"

// endpointIdentifierRe matches the NAME of a value that plausibly holds a
// configured endpoint. Deliberately a substring match over identifier and
// field names: "endpoint", "baseURL", "BaseURL", "rawEndpoint", "HealthURL",
// "effectiveBaseURL()" and "host" all qualify.
//
// It errs toward flagging: a non-endpoint value whose name happens to contain
// "url" and reaches a gate refusal unwrapped is reported and must either be
// wrapped or be renamed. That costs a contributor one line; the opposite error
// costs a credential.
var endpointIdentifierRe = regexp.MustCompile(`(?i)(endpoint|baseurl|base_url|host|url)`)

// configIdentifierRe matches a bare identifier that names a whole CONFIG
// STRUCT (round-6 N5).
//
// `fmt.Errorf("%w: %+v", ErrCloudDisabled, cfg)` renders every exported field
// of that struct — BaseURL and APIKey included — yet "cfg" matches none of the
// endpoint shapes above, so the scan saw nothing. Dumping a config struct into
// a refusal is strictly worse than quoting its endpoint, because the endpoint
// at least has a redaction helper; the API key has nothing.
//
// Whole-name match, not substring: "cfg" as a substring would hit ordinary
// identifiers, and the shape being caught is specifically "the config object
// itself was handed to the formatter".
var configIdentifierRe = regexp.MustCompile(`(?i)^(cfg|cfgs|config|configs|configuration|conf|opts|options|settings)$`)

// credentialIdentifierRe matches the NAME of a field or value that plausibly
// holds a CREDENTIAL rather than an endpoint (ROUND-7 A4).
//
// This is deliberately separate from endpointIdentifierRe because the two
// carry different remedies: an endpoint has RedactEndpointForMessage, whereas
// a credential has no safe rendering at all and simply must not be formatted
// into a refusal. A hit here is therefore always a defect, never a
// "wrap it in the helper" note.
//
// It is anchored on WORD FRAGMENTS, not whole names, so cfg.APIKey,
// opts.apiKeyOverride and conf.BearerToken all match. "Type", "BaseURL" and
// the other ordinary config fields the round-6 N5 mustNotFlag rows pin do not.
var credentialIdentifierRe = regexp.MustCompile(`(?i)(apikey|api_key|token|secret|password|passwd|credential|bearer)`)

// gateRefusalSite is one ErrCloudDisabled construction that mentions an
// endpoint-shaped value, whether or not that value is redacted.
type gateRefusalSite struct {
	file string
	line int
	fn   string // enclosing function — the SITE identity used by the cross-check
	// unredacted lists the endpoint-shaped expressions reaching the refusal
	// WITHOUT passing through RedactEndpointForMessage. Empty means compliant.
	unredacted []string
}

// scanSourceForGateRefusalEndpointLeaks reports every ErrCloudDisabled
// construction in one file that mentions an endpoint-shaped value.
//
// Takes the source as bytes rather than reading the path so the scan can be
// proven against fixtures (§1.1).
//
// HONEST COVERAGE BOUNDARY (§11.4.6) — shapes this does NOT see:
//
//   - a refusal built WITHOUT naming the sentinel in the same fmt call (e.g.
//     the message is assembled in a helper that takes the sentinel as a
//     parameter, or built with a formatter other than fmt.Errorf/fmt.Sprintf);
//   - taint through a SECOND assignment (a := endpoint; b := a; …) — taint is
//     one hop, not transitive;
//   - an endpoint held in a value whose name matches none of the shapes above
//     (a bare `v`, `cfg.Target`, `s`);
//   - a redaction performed by some function OTHER than the named helper —
//     treating an unknown wrapper as safe would be the fail-open this file
//     exists to prevent, so such a site is flagged and must be reviewed;
//   - a DOT-IMPORT of fmt or errors (`import . "fmt"`), which leaves the call
//     with no qualifier at all. Round-6 N5 fixed the ALIAS case by resolving
//     the file's import table; the dot-import case is left uncovered rather
//     than half-handled, because matching a bare `Errorf(...)` would need
//     type information this AST-only scan does not have.
//
// The table-driven runtime guard in endpoint_redaction_test.go is the half
// that proves the actual MESSAGE of each known site, on the code as it runs.
func scanSourceForGateRefusalEndpointLeaks(path string, src []byte) ([]gateRefusalSite, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	aliases := packageAliases(file)
	var sites []gateRefusalSite
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		tainted := taintedEndpointLocals(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !mentionsGateSentinel(call, aliases) {
				return true
			}
			leaks := unredactedEndpointLeaves(call.Args, tainted)
			if len(leaks) == 0 {
				return true
			}
			sites = append(sites, gateRefusalSite{
				file:       path,
				line:       fset.Position(call.Pos()).Line,
				fn:         fn.Name.Name,
				unredacted: leaks,
			})
			return true
		})
	}
	return sites, nil
}

// scanSourceForGateRefusalSites reports the enclosing function of every
// ErrCloudDisabled construction that mentions an endpoint-shaped value at all
// — redacted or not. This is the SITE INVENTORY the table cross-check uses;
// the leak scan above reports only the non-compliant subset.
func scanSourceForGateRefusalSites(path string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	aliases := packageAliases(file)
	pkg := file.Name.Name
	var fns []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// Any endpoint-shaped local, redacted or not, makes the function a
		// candidate carrier; the sentinel call makes it a gate site.
		all := allEndpointLocals(fn.Body)
		found := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !mentionsGateSentinel(call, aliases) {
				return true
			}
			if len(anyEndpointLeaves(call.Args, all)) > 0 {
				found = true
			}
			return true
		})
		if found {
			// ROUND-6 N7. Keyed by PACKAGE-qualified name: two packages can
			// each define a `NewProvider`, and a bare function name would let
			// one table row silently satisfy the cross-check for both.
			fns = append(fns, pkg+"."+fn.Name.Name)
		}
	}
	return fns, nil
}

// packageAliases maps each identifier a file uses for an imported package to
// that package's canonical name.
//
// ROUND-6 N5. The predicates below used to compare the qualifier against the
// literal "fmt", so a single line — `import f "fmt"` — made every
// `f.Errorf("%w: %s", ErrCloudDisabled, endpoint)` in that file invisible to a
// scan whose entire job is to find them. Resolving through the file's own
// import table removes the assumption rather than adding another literal.
//
// A dot-import (`import . "fmt"`) is NOT resolved: the call then has no
// qualifier at all and appears as a bare Ident, which these predicates do not
// match. That is recorded in the honest-boundary list rather than half-handled.
func packageAliases(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		canonical := path
		if i := strings.LastIndex(canonical, "/"); i >= 0 {
			canonical = canonical[i+1:]
		}
		local := canonical
		if spec.Name != nil {
			local = spec.Name.Name
		}
		if local == "_" || local == "." {
			continue
		}
		out[local] = canonical
	}
	return out
}

// qualifiedCallIs reports whether call is `<pkg>.<name>` where <pkg> resolves,
// through the file's import table, to wantPkg.
func qualifiedCallIs(call *ast.CallExpr, aliases map[string]string, wantPkg string, wantNames ...string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	canonical, known := aliases[pkg.Name]
	if !known {
		// No import table (fixture parsed without one) — fall back to the
		// identifier itself so the predicate still works on bare sources.
		canonical = pkg.Name
	}
	if canonical != wantPkg {
		return false
	}
	for _, want := range wantNames {
		if sel.Sel.Name == want {
			return true
		}
	}
	return false
}

// isMessageBuildingCall reports whether a call BUILDS a message — fmt.Errorf
// or fmt.Sprintf — or JOINS errors (errors.Join), which is the other way a
// refusal is assembled around the sentinel (round-6 N5).
//
// This distinction was added after the scan flagged
// BuildDynamicOpenAICompatibleProviders, whose `errors.Is(err, ErrCloudDisabled)`
// names the sentinel but renders nothing: a CONSUMER of the sentinel, not a
// producer of a message. Flagging it was a false refusal (§11.4.201) — the
// scan's own defect class, so the shape is pinned as a golden-good fixture
// below rather than merely fixed.
func isMessageBuildingCall(call *ast.CallExpr, aliases map[string]string) bool {
	if qualifiedCallIs(call, aliases, "fmt", "Errorf", "Sprintf") {
		return true
	}
	// errors.Join(ErrCloudDisabled, fmt.Errorf("… %s", cfg.BaseURL)) is a
	// refusal that carries the sentinel, and the leak lives in an argument the
	// leaf walk already descends into — it was simply never reached, because
	// the outer call was not recognised as a refusal at all.
	return qualifiedCallIs(call, aliases, "errors", "Join")
}

// mentionsGateSentinel reports whether a message-building call names
// ErrCloudDisabled among its arguments, bare or package-qualified.
func mentionsGateSentinel(call *ast.CallExpr, aliases map[string]string) bool {
	if !isMessageBuildingCall(call, aliases) {
		return false
	}
	for _, arg := range call.Args {
		switch e := arg.(type) {
		case *ast.Ident:
			if e.Name == gateSentinelName {
				return true
			}
		case *ast.SelectorExpr:
			if e.Sel != nil && e.Sel.Name == gateSentinelName {
				return true
			}
		}
	}
	return false
}

// isRedactionHelper reports whether a call expression is the redaction helper,
// bare or package-qualified (llm.RedactEndpointForMessage from the server).
func isRedactionHelper(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == redactionHelperName
	case *ast.SelectorExpr:
		return fn.Sel != nil && fn.Sel.Name == redactionHelperName
	}
	return false
}

// endpointLeafName renders an expression as an endpoint-shaped NAME, or "" if
// the expression is not one.
func endpointLeafName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		if endpointIdentifierRe.MatchString(v.Name) {
			return v.Name
		}
		// Round-6 N5: a whole config struct carries the endpoint AND the key.
		if configIdentifierRe.MatchString(v.Name) {
			return v.Name + " (config struct: renders every field, endpoint and API key alike)"
		}
	case *ast.SelectorExpr:
		if v.Sel != nil && endpointIdentifierRe.MatchString(v.Sel.Name) {
			return v.Sel.Name
		}
		// ROUND-7 A4. The round-6 N5 narrowing (judge the SELECTOR, then stop)
		// was written to stop `config.Type` producing a false refusal, and it
		// works — but it left the selector judged against the ENDPOINT regex
		// ALONE. A config field that IS a credential, `cfg.APIKey`, therefore
		// scored zero findings, and unlike an endpoint it has no redaction
		// helper at all, so nothing downstream would have caught it either.
		if v.Sel != nil && credentialIdentifierRe.MatchString(v.Sel.Name) {
			return v.Sel.Name + " (credential field: no redaction helper exists for it)"
		}
	case *ast.CallExpr:
		// A getter: config.effectiveBaseURL()
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel != nil {
			if endpointIdentifierRe.MatchString(sel.Sel.Name) {
				return sel.Sel.Name + "()"
			}
			// ROUND-7 A4, second half. A Stringer on the config renders every
			// field exactly as "%+v" does — which the whole_config_struct_dumped
			// rule already flags — but it arrives here as a CallExpr whose
			// selector is "String", matching no rule. The receiver must be
			// config-shaped so an ordinary `someEnum.String()` stays unflagged.
			if sel.Sel.Name == "String" || sel.Sel.Name == "GoString" {
				if recv, ok := sel.X.(*ast.Ident); ok && configIdentifierRe.MatchString(recv.Name) {
					return recv.Name + "." + sel.Sel.Name +
						"() (config stringer: renders every field, endpoint and API key alike)"
				}
			}
		}
	}
	return ""
}

// collectEndpointLeaves walks expressions and reports endpoint-shaped leaves.
// When stopAtHelper is true it does NOT descend into RedactEndpointForMessage
// calls, so a wrapped endpoint is invisible — which is what makes the result
// the set of UNREDACTED leaves.
func collectEndpointLeaves(exprs []ast.Expr, extra map[string]string, stopAtHelper bool) []string {
	var found []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			found = append(found, s)
		}
	}
	for _, expr := range exprs {
		ast.Inspect(expr, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if isCall && stopAtHelper && isRedactionHelper(call) {
				return false // everything under here is redacted
			}
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			add(endpointLeafName(e))
			if _, isSel := e.(*ast.SelectorExpr); isSel {
				// Judge the SELECTOR, then stop. Round-6 N5's config-struct
				// rule below matches a bare identifier, and descending into
				// `config.Type` would reach the bare `config` and report the
				// whole struct as dumped when only one field was named — a
				// false refusal of correct code (§11.4.201), which the real
				// NewProvider gate refusal is.
				return false
			}
			if id, ok := e.(*ast.Ident); ok {
				if origin, tainted := extra[id.Name]; tainted {
					add(fmt.Sprintf("%s (holds %s)", id.Name, origin))
				}
			}
			return true
		})
	}
	return found
}

func unredactedEndpointLeaves(args []ast.Expr, tainted map[string]string) []string {
	return collectEndpointLeaves(args, tainted, true)
}

func anyEndpointLeaves(args []ast.Expr, all map[string]string) []string {
	return collectEndpointLeaves(args, all, false)
}

// taintedEndpointLocals maps each local assigned from an expression carrying an
// UNREDACTED endpoint-shaped value to the name of that value. This is the hop
// the KoboldAI leak used: the endpoint was formatted into `detail`, and
// `detail` — not the endpoint — was what the refusal call received.
func taintedEndpointLocals(body *ast.BlockStmt) map[string]string {
	return endpointLocals(body, true)
}

// allEndpointLocals is the same, ignoring redaction: a local holding a REDACTED
// endpoint still makes its function a gate SITE for inventory purposes.
func allEndpointLocals(body *ast.BlockStmt) map[string]string {
	return endpointLocals(body, false)
}

func endpointLocals(body *ast.BlockStmt, stopAtHelper bool) map[string]string {
	out := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		leaves := collectEndpointLeaves(assign.Rhs, nil, stopAtHelper)
		if len(leaves) == 0 {
			return true
		}
		for _, lhs := range assign.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
				if _, already := out[id.Name]; !already {
					out[id.Name] = leaves[0]
				}
			}
		}
		return true
	})
	return out
}

// governedSourceFiles walks internal/ and returns every non-test .go file that
// mentions the gate sentinel. Finding the files by the DEFECT rather than by a
// hardcoded list is the whole point: a gate added in a package nobody listed is
// still scanned.
func governedSourceFiles(t *testing.T) map[string][]byte {
	t.Helper()
	// ROUND-6 N6. The walk used to start at internal/, but the sentinel is
	// already referenced from applications/terminal_ui/env_providers.go, so a
	// gate site added anywhere outside internal/ was structurally invisible to
	// a scan whose stated premise is "discovered by walking the tree rather
	// than by naming files". The root is the MODULE root.
	root := "../.." // the test's cwd is internal/llm
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Vendored and generated trees are not ours to gate, and walking
			// them costs time on every run for no possible finding.
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
		if strings.Contains(string(src), gateSentinelName) {
			out[path] = src
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Fatalf("no source file under %s mentions %s — the walk is broken, so every "+
			"assertion built on it would be vacuously true", root, gateSentinelName)
	}
	return out
}

// TestCloudGateRefusals_QuoteEndpointsOnlyThroughRedaction is the mechanical
// per-site guard: EVERY cloud-gate refusal, in every package, must quote a
// configured endpoint through RedactEndpointForMessage.
func TestCloudGateRefusals_QuoteEndpointsOnlyThroughRedaction(t *testing.T) {
	files := governedSourceFiles(t)
	scanned := 0
	for path, src := range files {
		sites, err := scanSourceForGateRefusalEndpointLeaks(path, src)
		if err != nil {
			t.Fatalf("scanning %s: %v", path, err)
		}
		scanned++
		for _, s := range sites {
			t.Errorf("CONST-042: %s:%d (%s) builds an %s refusal quoting %v WITHOUT "+
				"%s. That refusal reaches an HTTP response body, so an endpoint "+
				"configured as \"https://user:pw@host/v1\" would hand \"pw\" to whoever "+
				"can reach the endpoint. Wrap it: %s(<endpoint>)",
				s.file, s.line, s.fn, gateSentinelName, s.unredacted,
				redactionHelperName, redactionHelperName)
		}
	}
	t.Logf("scanned %d source file(s) mentioning %s", scanned, gateSentinelName)
}

// TestCloudGateSites_TableCoversEverySiteTheScannerFinds binds the two halves
// of the guard together. The static scan finds the sites; the runtime table in
// endpoint_redaction_test.go proves their MESSAGES. A site the scanner finds
// but the table does not drive is a site whose rendered message nobody checks
// — which is how the KoboldAI leak survived — so the mismatch FAILS here
// rather than passing silently.
func TestCloudGateSites_TableCoversEverySiteTheScannerFinds(t *testing.T) {
	found := map[string]bool{}
	for path, src := range governedSourceFiles(t) {
		fns, err := scanSourceForGateRefusalSites(path, src)
		if err != nil {
			t.Fatalf("scanning %s: %v", path, err)
		}
		for _, fn := range fns {
			found[fn] = true
		}
	}

	covered := map[string]bool{}
	for _, site := range cloudGateEndpointSites() {
		covered[site.scannerSymbol] = true
	}

	var missing, stale []string
	for fn := range found {
		if !covered[fn] {
			missing = append(missing, fn)
		}
	}
	for fn := range covered {
		if !found[fn] {
			stale = append(stale, fn)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)

	if len(missing) > 0 {
		t.Errorf("gate site(s) %v quote a configured endpoint in an %s refusal but have NO "+
			"row in cloudGateEndpointSites(), so no test ever reads their rendered message. "+
			"Add a row driving each one — that omission is exactly how the KoboldAI leak "+
			"reached round 5.", missing, gateSentinelName)
	}
	if len(stale) > 0 {
		t.Errorf("cloudGateEndpointSites() names %v, which the scanner no longer finds as an "+
			"endpoint-quoting %s site. Either the scannerSymbol is misspelled (the row is "+
			"then guarding nothing it claims to) or the site was removed and the row is "+
			"stale.", stale, gateSentinelName)
	}
	t.Logf("gate sites quoting an endpoint: %d, all covered by the runtime table", len(found))
}

// TestCloudGateRedactionScan_CatchesEveryKnownDefectShape is the scan's own
// §1.1 proof — golden-bad fixtures it MUST flag, golden-good fixtures it must
// NOT, because a false refusal is its own defect (§11.4.201).
func TestCloudGateRedactionScan_CatchesEveryKnownDefectShape(t *testing.T) {
	mustFlag := []struct{ name, src string }{
		{
			// The shape the OpenAI-compatible site had before round 4.
			name: "direct_endpoint_argument",
			src: `package llm
func New(cfg C) (Provider, error) {
	return nil, fmt.Errorf("%w: targets %q", ErrCloudDisabled, cfg.BaseURL)
}
`,
		},
		{
			// The exact shape the KoboldAI site had before round 5: the
			// endpoint reaches the refusal through a local.
			name: "endpoint_via_local_detail_string",
			src: `package llm
func New(cfg C) (Provider, error) {
	endpoint := cfg.effectiveBaseURL()
	detail := fmt.Sprintf("endpoint %q is not local", endpoint)
	return nil, fmt.Errorf("%w: %s", ErrCloudDisabled, detail)
}
`,
		},
		{
			name: "package_qualified_sentinel_from_the_server",
			src: `package server
func f(endpoint string) error {
	return fmt.Errorf("%w: %q", llm.ErrCloudDisabled, endpoint)
}
`,
		},
		{
			// ROUND-6 N5, alias half. One line — `import f "fmt"` — used to
			// make every refusal in the file invisible, because the qualifier
			// was compared against the literal "fmt".
			name: "aliased_fmt_import",
			src: `package llm

import f "fmt"

func New(cfg C) (Provider, error) {
	return nil, f.Errorf("%w: targets %q", ErrCloudDisabled, cfg.BaseURL)
}
`,
		},
		{
			// ROUND-6 N5, whole-struct half. "%+v" on the config renders every
			// exported field — the endpoint AND the API key, which has no
			// redaction helper at all.
			name: "whole_config_struct_dumped",
			src: `package llm
func New(cfg C) (Provider, error) {
	return nil, fmt.Errorf("%w: %+v", ErrCloudDisabled, cfg)
}
`,
		},
		{
			// ROUND-6 N5, errors.Join half. The sentinel is carried by a Join
			// rather than an Errorf, so the outer call was not recognised as a
			// refusal and the leak in its sibling argument was never reached.
			name: "sentinel_carried_by_errors_join",
			src: `package llm

import (
	"errors"
	"fmt"
)

func New(cfg C) (Provider, error) {
	return nil, errors.Join(ErrCloudDisabled, fmt.Errorf("targets %q", cfg.BaseURL))
}
`,
		},
		{
			// ROUND-7 A4. The round-6 N5 narrowing that stopped `config.Type`
			// producing a false refusal also blinded the scanner to a config
			// field that IS a credential. A SelectorExpr is judged only
			// against the ENDPOINT name regex, so `cfg.APIKey` — which has no
			// redaction helper at all — scored zero findings.
			name: "credential_field_named_directly",
			src: `package llm
func New(cfg C) (Provider, error) {
	return nil, fmt.Errorf("%w: provider key %s rejected", ErrCloudDisabled, cfg.APIKey)
}
`,
		},
		{
			// ROUND-7 A4, second half. A Stringer on the config renders every
			// field exactly as "%+v" does, but arrives as a CallExpr whose
			// selector is "String", which no rule matched.
			name: "config_stringer_called",
			src: `package llm
func New(cfg C) (Provider, error) {
	return nil, fmt.Errorf("%w: %s", ErrCloudDisabled, cfg.String())
}
`,
		},
		{
			name: "parenthesised_endpoint_argument",
			src: `package llm
func New(cfg C) (Provider, error) {
	return nil, fmt.Errorf("%w: %q", ErrCloudDisabled, (cfg.BaseURL))
}
`,
		},
	}
	for _, tc := range mustFlag {
		t.Run("flags_"+tc.name, func(t *testing.T) {
			got, err := scanSourceForGateRefusalEndpointLeaks("fixture.go", []byte(tc.src))
			if err != nil {
				t.Fatalf("scanning fixture: %v", err)
			}
			if len(got) == 0 {
				t.Fatalf("the %s spelling of the CONST-042 gate leak was NOT flagged — the "+
					"guard has a hole a contributor reaches by writing it this way:\n%s",
					tc.name, tc.src)
			}
		})
	}

	mustNotFlag := []struct{ name, src string }{
		{
			// The counterpart to whole_config_struct_dumped: naming ONE
			// non-endpoint FIELD is the real NewProvider refusal in
			// factory.go, and flagging it would be a false refusal. This row
			// is what stopped the round-6 N5 rule from being written as a
			// blanket "any config-shaped identifier anywhere in the subtree".
			name: "config_field_that_is_not_an_endpoint",
			src: `package llm
func New(config C) (Provider, error) {
	return nil, fmt.Errorf("%w: hosted provider %q will not be constructed", ErrCloudDisabled, config.Type)
}
`,
		},
		{
			name: "wrapped_direct",
			src: `package llm
func New(cfg C) (Provider, error) {
	return nil, fmt.Errorf("%w: targets %q", ErrCloudDisabled, RedactEndpointForMessage(cfg.BaseURL))
}
`,
		},
		{
			name: "wrapped_via_local",
			src: `package llm
func New(cfg C) (Provider, error) {
	endpoint := cfg.effectiveBaseURL()
	detail := fmt.Sprintf("endpoint %q is not local", RedactEndpointForMessage(endpoint))
	return nil, fmt.Errorf("%w: %s", ErrCloudDisabled, detail)
}
`,
		},
		{
			name: "package_qualified_helper_from_the_server",
			src: `package server
func f(endpoint string) error {
	return fmt.Errorf("%w: %q", llm.ErrCloudDisabled, llm.RedactEndpointForMessage(endpoint))
}
`,
		},
		{
			// The refusal names a PROVIDER TYPE, not an endpoint — the shape
			// factory.go and provider_factory.go use. Flagging it would be a
			// false refusal.
			name: "refusal_naming_only_a_provider_type",
			src: `package llm
func New(cfg C) (Provider, error) {
	return nil, fmt.Errorf("%w: hosted provider %q will not be constructed", ErrCloudDisabled, cfg.Type)
}
`,
		},
		{
			// THE FALSE POSITIVE THIS SCAN ACTUALLY PRODUCED on first run, kept
			// as a regression fixture (§11.4.135). errors.Is CONSUMES the
			// sentinel and renders nothing; `err` is only "endpoint-shaped"
			// because the constructor call that produced it mentioned BaseURL.
			// A guard that refuses correct code is its own defect (§11.4.201).
			name: "sentinel_consumer_errors_is",
			src: `package llm
func Build() []Provider {
	provider, err := NewOpenAICompatibleProvider(name, OpenAICompatibleConfig{BaseURL: rec.APIURL})
	if err != nil {
		if errors.Is(err, ErrCloudDisabled) {
			cloudGateRefused++
		}
	}
	return nil
}
`,
		},
		{
			// An endpoint quoted in an error that is NOT a gate refusal is out
			// of this scan's scope by design; the sentinel is what marks the
			// class it governs.
			name: "endpoint_in_a_non_gate_error",
			src: `package llm
func f(endpoint string) error {
	return fmt.Errorf("dial %q: unreachable", endpoint)
}
`,
		},
	}
	for _, tc := range mustNotFlag {
		t.Run("accepts_"+tc.name, func(t *testing.T) {
			got, err := scanSourceForGateRefusalEndpointLeaks("fixture.go", []byte(tc.src))
			if err != nil {
				t.Fatalf("scanning fixture: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("correct code was refused (%d finding(s), first=%v) — a false refusal "+
					"is its own defect:\n%s", len(got), got[0].unredacted, tc.src)
			}
		})
	}
}
