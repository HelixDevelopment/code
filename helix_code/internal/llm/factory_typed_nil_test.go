package llm

// TYPED-NIL GUARD for the two interface-returning provider factories,
// NewProvider (factory.go) and NewCloudProvider (provider_factory.go).
//
// THE DEFECT. Every New<X>Provider constructor in this package returns a
// concrete POINTER and returns (nil, err) on failure. A factory declared
// `(Provider, error)` that does `return NewXProvider(cfg)` hands that nil
// pointer back inside a Provider interface whose TYPE word is set. The
// interface value is therefore NOT nil: a caller's `if prov != nil` guard
// passes, and the first method call panics.
//
// WHY A GUARD RATHER THAN A FIX ALONE. The same defect has now been found and
// fixed FOUR times in this package — openai_compatible_catalogue.go, then
// newOpenAICompatibleFromConfig, then the KoboldAI arm of NewProvider, then the
// ~30 remaining arms of both factories. Fixing the fourth occurrence without
// leaving something that catches the fifth just schedules the fifth. So this
// file is deliberately TWO guards, because either alone is escapable:
//
//	(1) a DYNAMIC sweep that drives every provider type through both factories
//	    and asserts the interface is nil on every error return. It proves the
//	    invariant on the code as it actually runs, but only over the error
//	    paths a credential-free environment happens to reach.
//	(2) a STATIC scan of the two factory sources that fails on any return of
//	    a New<X>Provider result which does not go through providerOrNil. It is
//	    an AST walk, not a line regexp, so it catches every spelling of the
//	    same defect -- direct, parenthesised, and two-step through a local --
//	    rather than only the one shape a pattern happened to be written
//	    against. It covers every arm including those the sweep cannot reach,
//	    and it is the half that makes the fifth occurrence impossible rather
//	    than merely unlikely. Its own coverage is proven by fixtures (see
//	    TestFactoryStaticScan_CatchesEveryKnownDefectShape); the shapes it
//	    still cannot see are enumerated honestly at the scanner itself.
//
// MEASURED BEFORE THE FIX (gate open, credential environment cleared): 13 of
// NewProvider's error returns and 15 of NewCloudProvider's came back with a
// NON-nil Provider interface. After: 0 of 40.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// factoryProviderTypes is every ProviderType either factory has an arm for,
// plus one that neither recognises (the default arm's explicit-nil path).
var factoryProviderTypes = []ProviderType{
	ProviderTypeOpenAI, ProviderTypeAnthropic, ProviderTypeGemini, ProviderTypeOllama,
	ProviderTypeLlamaCpp, ProviderTypeQwen, ProviderTypeXAI, ProviderTypeOpenRouter,
	ProviderTypeCopilot, ProviderTypeAzure, ProviderTypeBedrock, ProviderTypeVertexAI,
	ProviderTypeGroq, ProviderTypeVLLM, ProviderTypeLocalAI, ProviderTypeFastChat,
	ProviderTypeTextGen, ProviderTypeLMStudio, ProviderTypeJan, ProviderTypeGPT4All,
	ProviderTypeTabbyAPI, ProviderTypeMLX, ProviderTypeMistralRS, ProviderTypeKoboldAI,
	ProviderTypeXiaomi, ProviderTypeReplicate, ProviderTypeMistral, ProviderTypeDeepSeek,
	ProviderType("definitely-not-a-provider-type"),
}

// credentialEnvSubstrings names the shapes of environment variable a provider
// constructor might read to decide it has enough configuration to succeed.
// Clearing them makes the sweep HERMETIC: the credential-rejection paths — the
// ones that actually exercise the boxing sites — are reached on a developer
// machine that happens to export real keys just as they are in CI.
var credentialEnvSubstrings = []string{
	"API_KEY", "APIKEY", "_TOKEN", "TOKEN_", "ENDPOINT", "PROJECT",
	"CREDENTIAL", "SECRET", "REGION", "_KEY",
}

func clearCredentialEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)
		for _, frag := range credentialEnvSubstrings {
			if strings.Contains(upper, frag) {
				t.Setenv(name, "")
				break
			}
		}
	}

	// Must come AFTER the blanking loop above, which would otherwise set this
	// to "" and hand VertexAI back to ADC discovery (metadata-server probe).
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "absent.json"))
}

// isExplicitNilPath reports whether an error came from an arm that never
// touches a concrete constructor at all — the cloud gate's own refusal and the
// two "unrecognised type" defaults. Those returns were always `nil, err`, so
// they are counted separately: a sweep that only ever hit THOSE would assert
// the invariant without exercising a single boxing site.
func isExplicitNilPath(err error) bool {
	if errors.Is(err, ErrCloudDisabled) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unsupported provider type:") ||
		strings.Contains(msg, "is not a cloud provider type")
}

func TestFactories_ErrorReturnNeverBoxesATypedNil(t *testing.T) {
	clearCredentialEnv(t)

	prevGate := CloudEnabled()
	t.Cleanup(func() { SetCloudEnabled(prevGate) })

	// constructorErrors counts error returns that came from a REAL per-type
	// constructor — the boxing sites this guard exists for.
	constructorErrors := 0

	for _, gateOpen := range []bool{true, false} {
		SetCloudEnabled(gateOpen)

		for _, pt := range factoryProviderTypes {
			// HERMETICITY (this is what makes the file header's claim true):
			// several constructors — the whole OpenAI-compatible family, Xiaomi
			// among them — run discoverModels() SYNCHRONOUSLY before returning.
			// With no Endpoint set they fall back to the vendor's real base URL,
			// so this sweep performed live DNS + TLS + GET /models against
			// third-party APIs (and stalled up to 10s each on a black-holed
			// network). Port 1 on loopback is refused instantly by the kernel,
			// which keeps every arm on the error path the sweep is here to check
			// while sending nothing off-host.
			entry := ProviderConfigEntry{Type: pt, Enabled: true, Endpoint: "http://127.0.0.1:1"}

			for _, f := range []struct {
				name string
				call func() (Provider, error)
			}{
				{"NewProvider", func() (Provider, error) { return NewProvider(entry) }},
				{"NewCloudProvider", func() (Provider, error) { return NewCloudProvider(pt, entry) }},
			} {
				prov, err := f.call()
				if err == nil {
					if prov != nil {
						_ = prov.Close()
					}
					continue
				}
				if prov != nil {
					t.Errorf("%s(%q) with the cloud gate open=%v returned err=%v "+
						"AND a NON-nil Provider interface. That is a nil concrete "+
						"pointer boxed into an interface: every caller's "+
						"`if prov != nil` guard passes and the first method call "+
						"panics. Route the arm through providerOrNil.",
						f.name, pt, gateOpen, err)
					continue
				}
				if !isExplicitNilPath(err) {
					constructorErrors++
				}
			}
		}
	}

	// SELF-VALIDATION. Without this the test would still pass on a build where
	// every constructor succeeded, or where the credential-clearing above
	// silently stopped working — it would assert the invariant over zero
	// boxing sites and look exactly as green. 20 is a deliberate floor under
	// the 28 measured when this guard was written, leaving room for a
	// provider whose constructor legitimately stops needing credentials
	// without leaving room for the sweep to collapse.
	const minConstructorErrors = 20
	if constructorErrors < minConstructorErrors {
		t.Fatalf("the sweep reached only %d real constructor error returns, want >= %d. "+
			"The assertions above are therefore vacuous — either the credential "+
			"environment is no longer being cleared, or the factories no longer "+
			"reach their per-type constructors.",
			constructorErrors, minConstructorErrors)
	}
	t.Logf("swept %d real constructor error returns; every one returned a nil Provider",
		constructorErrors)
}

// ---------------------------------------------------------------------------
// STATIC SCAN (guard 2).
//
// Previously a single line-oriented regexp `^\s*return\s+(New[A-Za-z0-9]*Provider)\(`.
// It caught the ONE shape it was written against and silently missed two other
// spellings of the identical defect:
//
//	return (NewFooProvider(cfg))          // parenthesised
//	p, err := NewFooProvider(cfg)         // two-step; `return p, err` boxes the
//	return p, err                         // same nil pointer
//
// A guard whose whole purpose is to make a FIFTH occurrence impossible cannot
// have holes a contributor reaches by adding one pair of parentheses. The scan
// is therefore an AST walk, which is shape-independent by construction: it
// judges the RETURNED EXPRESSION, not the text of a line, so multi-line
// returns, gofmt reflows and parenthesisation are all handled identically.
// ---------------------------------------------------------------------------

// providerConstructorName matches the concrete per-backend constructors —
// every one of them returns a concrete POINTER, which is what makes an
// unwrapped return a boxing site.
var providerConstructorName = regexp.MustCompile(`^New[A-Za-z0-9]*Provider$`)

// providerBoxingHelper is the one function that converts (concrete, error)
// into a genuinely-nil (Provider, error). A return that goes through it is
// safe by construction; a return that does not is the defect.
const providerBoxingHelper = "providerOrNil"

// factoryReturnViolation is one flagged return statement.
type factoryReturnViolation struct {
	line int
	form string // which spelling of the defect was found
	text string // the offending expression, rendered
}

// constructorTaint records a local assigned from a provider constructor, along
// with the name the constructor's ERROR was bound to.
//
// The error binding is what makes the single-result and bare-return shapes
// decidable: `p, _ := NewFooProvider(c)` discards the error, so nothing
// downstream can know p is nil, while `p, err := NewFooProvider(c)` followed by
// an `if err != nil` guard is the manually-inlined providerOrNil that must NOT
// be refused.
type constructorTaint struct {
	// errVar is the identifier the constructor's error was bound to, or "" if
	// it was discarded (`_`) or never bound at all.
	errVar string
}

// scanFactorySourceForUnwrappedConstructors reports every return statement, in
// a function declared to return the Provider INTERFACE, that hands back a
// concrete constructor's result without passing through providerOrNil.
//
// It takes the source as bytes rather than reading the path itself so the
// guard can be proven against fixtures — a scan nobody has watched FAIL is an
// assertion about a scan, not a scan (§1.1).
//
// interfaceReturningCtors names constructors that, despite matching the
// New<X>Provider shape, are declared to return the Provider INTERFACE rather
// than a concrete pointer. Returning one of those directly is CORRECT code: no
// boxing happens, because the callee already produced a proper interface value.
// NewHostedOpenAICompatibleProvider (openai_compatible_catalogue.go) is exactly
// such a function, and flagging it would be a false refusal (§11.4.201). The
// set is built mechanically from the package's own sources by
// interfaceReturningProviderConstructors, so it maintains itself; pass nil to
// scan with no exemptions.
//
// Five shapes are recognised:
//
//   - "direct"          — return NewFooProvider(cfg)
//   - "parenthesised"   — return (NewFooProvider(cfg))   (any depth of parens)
//   - "via-variable"    — p, err := NewFooProvider(cfg) … return p, err
//   - "error-discarded" — p, _ := NewFooProvider(cfg)   … return p[, nil]
//   - "bare-return"     — func (p Provider, err error) { p, err = New…; return }
//
// HONEST COVERAGE BOUNDARY (§11.4.6) — shapes this does NOT catch, stated
// rather than implied away:
//
//   - a constructor reached through a VALUE: `ctor := NewFooProvider; return
//     ctor(cfg)` — the callee is not an identifier matching the pattern;
//   - a constructor reached through a METHOD or another package's selector;
//   - a taint that flows through a SECOND assignment (`p, err := New…; q := p;
//     return q, err`) — taint is not propagated transitively;
//   - a return inside a nested function literal, which is not descended into;
//   - a wrapper other than providerOrNil that itself boxes (`return
//     someOtherWrap(NewFooProvider(cfg))`) — flagging every unknown wrapper
//     would refuse correct code, and a false refusal is its own defect;
//   - a single-result or bare return whose error WAS bound and IS guarded by
//     an `if <err> != nil` somewhere in the function, but where that guard does
//     not actually cover this return. Deciding that needs flow analysis; the
//     guard-presence test is a syntactic approximation chosen because the
//     opposite error — refusing the ordinary `if err != nil { return nil,
//     err }` shape the whole package uses — would be worse than the gap.
//
// Each is a deliberately-accepted gap, not an untested claim: the dynamic
// sweep above is the half that covers arms this scan cannot see.
func scanFactorySourceForUnwrappedConstructors(
	path string, src []byte, interfaceReturningCtors map[string]bool,
) ([]factoryReturnViolation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	var violations []factoryReturnViolation
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !declaresProviderInterfaceResult(fn) {
			continue
		}
		tainted := taintedConstructorLocals(fn.Body, interfaceReturningCtors)
		guarded := guardedErrorNames(fn.Body)
		named := namedResultNames(fn)
		unreset := bareReturnsNeedingNilReset(fn.Body, tainted, named)
		for _, ret := range returnStatementsOf(fn.Body) {
			for _, v := range judgeReturn(ret, tainted, guarded, named, interfaceReturningCtors, unreset) {
				v.line = fset.Position(ret.Pos()).Line
				violations = append(violations, v)
			}
		}
	}
	return violations, nil
}

// declaresProviderInterfaceResult reports whether fn returns the bare
// `Provider` interface. A factory returning a CONCRETE type (*FooProvider) may
// return a constructor directly — no boxing happens — and is deliberately out
// of scope.
func declaresProviderInterfaceResult(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, field := range fn.Type.Results.List {
		if id, ok := field.Type.(*ast.Ident); ok && id.Name == "Provider" {
			return true
		}
	}
	return false
}

// namedResultNames collects the NAMES of a function's named results, so a bare
// `return` can be judged against what it implicitly returns.
func namedResultNames(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	if fn.Type.Results == nil {
		return out
	}
	for _, field := range fn.Type.Results.List {
		for _, name := range field.Names {
			if name != nil && name.Name != "_" {
				out[name.Name] = true
			}
		}
	}
	return out
}

// guardedErrorNames collects every identifier compared `!= nil` in an if
// condition anywhere in the body — the syntactic mark of the manually-inlined
// providerOrNil (`if err != nil { return nil, err }`).
func guardedErrorNames(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		bin, ok := unparen(ifStmt.Cond).(*ast.BinaryExpr)
		if !ok || bin.Op != token.NEQ {
			return true
		}
		lhs, lok := unparen(bin.X).(*ast.Ident)
		rhs, rok := unparen(bin.Y).(*ast.Ident)
		if lok && rok && rhs.Name == "nil" {
			out[lhs.Name] = true
		}
		return true
	})
	return out
}

// bareReturnsNeedingNilReset finds every BARE return that sits inside an
// `if <errVar> != nil { … }` guard WITHOUT the named result having been reset
// to nil first, and reports, per return statement, which named results are
// still holding the failed constructor's typed-nil pointer.
//
// ROUND-6 N1 — why the `guarded` set alone is not enough. `guarded` answers
// "was this error compared against nil anywhere in the body?". For a bare
// return that is the wrong question, because the guard's own body is exactly
// where the named result is STILL the typed-nil the constructor handed back:
//
//	p, err = NewFooProvider(c)
//	if err != nil {
//	        return          // p is the typed-nil *FooProvider — the defect
//	}
//
// The accepting fixture differs by one line, `p = nil`, and that line is the
// whole difference between correct and broken. So the reset is what gets
// checked, not the mere existence of the comparison.
//
// ORDERING is by token position within the guard block rather than by
// statement-list index, so a reset in a nested block still counts as long as
// it lexically precedes the return. That is an APPROXIMATION of reachability
// (§11.4.6): a reset inside a sibling branch that does not actually execute
// before this return would be accepted. It is the conservative direction for a
// guard whose false-refusal cost is a contributor being told correct code is
// broken, and the shape it does catch is the one that reached the tree.
func bareReturnsNeedingNilReset(
	body *ast.BlockStmt, tainted map[string]constructorTaint, namedResults map[string]bool,
) map[*ast.ReturnStmt]map[string]bool {
	out := map[*ast.ReturnStmt]map[string]bool{}

	ast.Inspect(body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		bin, ok := unparen(ifStmt.Cond).(*ast.BinaryExpr)
		if !ok || bin.Op != token.NEQ {
			return true
		}
		lhs, lok := unparen(bin.X).(*ast.Ident)
		rhs, rok := unparen(bin.Y).(*ast.Ident)
		if !lok || !rok || rhs.Name != "nil" {
			return true
		}
		// Which tainted named results does THIS guard cover?
		var covered []string
		for name, taint := range tainted {
			if namedResults[name] && taint.errVar == lhs.Name {
				covered = append(covered, name)
			}
		}
		if len(covered) == 0 {
			return true
		}
		resets := nilResetPositions(ifStmt.Body)
		for _, ret := range returnStatementsOf(ifStmt.Body) {
			if len(ret.Results) != 0 {
				continue // an explicit `return nil, err` says what it returns.
			}
			for _, name := range covered {
				if pos, ok := resets[name]; ok && pos < ret.Pos() {
					continue
				}
				if out[ret] == nil {
					out[ret] = map[string]bool{}
				}
				out[ret][name] = true
			}
		}
		return true
	})
	return out
}

// nilResetPositions records, per identifier, the position of the FIRST
// `<ident> = nil` assignment that is a DIRECT statement of block.
//
// ROUND-7 A5 — direct statements only, deliberately.
//
// This used ast.Inspect over the whole subtree, which counted a reset ANYWHERE
// lexically before the return, including one inside a sibling branch that does
// not execute on the path to it:
//
//	if err != nil {
//	        if c == 999 {
//	                p = nil     // counted, but only runs for c == 999
//	        }
//	        return              // every other c returns the typed-nil
//	}
//
// That is a FAIL-OPEN: the guard reports correct code for a value that really
// does escape. Only statements the guard block executes unconditionally before
// the return are counted now, which makes reset-then-return the one accepted
// shape.
//
// COST, stated rather than implied away (§11.4.6 / §11.4.201): a reset that is
// genuinely unconditional but written inside a nested block is now FLAGGED —
// a false refusal whose remedy is hoisting one line to the guard body. That is
// the cheap direction; the alternative is a scan that certifies a live
// typed-nil escape as correct.
func nilResetPositions(block *ast.BlockStmt) map[string]token.Pos {
	out := map[string]token.Pos{}
	for _, stmt := range block.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}
		for i, lhs := range assign.Lhs {
			id, ok := unparen(lhs).(*ast.Ident)
			if !ok || i >= len(assign.Rhs) {
				continue
			}
			rhs, ok := unparen(assign.Rhs[i]).(*ast.Ident)
			if !ok || rhs.Name != "nil" {
				continue
			}
			if _, seen := out[id.Name]; !seen {
				out[id.Name] = assign.Pos()
			}
		}
	}
	return out
}

// isConcreteProviderConstructor reports whether callee names a constructor
// that returns a CONCRETE pointer — i.e. one whose result would be boxed.
func isConcreteProviderConstructor(name string, interfaceReturningCtors map[string]bool) bool {
	if !providerConstructorName.MatchString(name) {
		return false
	}
	return !interfaceReturningCtors[name]
}

// taintedConstructorLocals collects the locals assigned directly from a
// provider constructor — the first LHS slot only, which is the one carrying
// the concrete pointer — recording what the error was bound to. A
// `p, err := providerOrNil(...)` does NOT taint: that value is already a proper
// interface, and neither does a constructor that returns the interface itself.
func taintedConstructorLocals(
	body *ast.BlockStmt, interfaceReturningCtors map[string]bool,
) map[string]constructorTaint {
	tainted := map[string]constructorTaint{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
			return true
		}
		call, ok := unparen(assign.Rhs[0]).(*ast.CallExpr)
		if !ok {
			return true
		}
		callee, ok := unparen(call.Fun).(*ast.Ident)
		if !ok || !isConcreteProviderConstructor(callee.Name, interfaceReturningCtors) {
			return true
		}
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || lhs.Name == "_" {
			return true
		}
		taint := constructorTaint{}
		if len(assign.Lhs) > 1 {
			if errIdent, ok := assign.Lhs[1].(*ast.Ident); ok && errIdent.Name != "_" {
				taint.errVar = errIdent.Name
			}
		}
		tainted[lhs.Name] = taint
		return true
	})
	return tainted
}

// returnStatementsOf collects every return in the body, INCLUDING those nested
// in if/switch/for blocks (where the factory arms actually live).
func returnStatementsOf(body *ast.BlockStmt) []*ast.ReturnStmt {
	var out []*ast.ReturnStmt
	ast.Inspect(body, func(n ast.Node) bool {
		// Do not descend into nested function literals: their returns belong
		// to a different signature, so judging them against this one would be
		// a false refusal.
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		if ret, ok := n.(*ast.ReturnStmt); ok {
			out = append(out, ret)
		}
		return true
	})
	return out
}

// judgeReturn classifies ONE return statement.
//
// It judges the STATEMENT, not each expression in isolation, because the
// via-variable shape is only a defect in combination: `return p, err` boxes a
// possibly-nil pointer, while `return p, nil` — the manually-inlined form of
// providerOrNil that follows an explicit `if err != nil { return nil, err }` —
// is correct code and must not be refused. Flagging the latter would be a
// false refusal, which is its own defect, not a stricter guard.
func judgeReturn(
	ret *ast.ReturnStmt,
	tainted map[string]constructorTaint,
	guarded map[string]bool,
	namedResults map[string]bool,
	interfaceReturningCtors map[string]bool,
	unresetBareReturns map[*ast.ReturnStmt]map[string]bool,
) []factoryReturnViolation {
	var out []factoryReturnViolation

	// BARE RETURN with named results. `return` here hands back whatever the
	// named results currently hold, so a named result assigned from a
	// constructor is boxed just as surely as if it had been written out — and
	// no expression appears in the statement for the loop below to see.
	if len(ret.Results) == 0 {
		for name, taint := range tainted {
			if namedResults[name] && taintIsUnguarded(taint, guarded) {
				out = append(out, factoryReturnViolation{form: "bare-return", text: name})
			}
		}
		// ROUND-6 N1. A bare return INSIDE the error guard is the shape the
		// clause above cannot see: the error IS guarded — that is what put us
		// in this block — and the named result is still the typed-nil the
		// failed constructor returned unless it was explicitly reset.
		for name := range unresetBareReturns[ret] {
			out = append(out, factoryReturnViolation{
				form: "bare-return-in-error-guard", text: name,
			})
		}
		return out
	}

	taintedIdx := -1
	for i, res := range ret.Results {
		stripped := unparen(res)
		parenthesised := stripped != res

		switch e := stripped.(type) {
		case *ast.CallExpr:
			callee, ok := unparen(e.Fun).(*ast.Ident)
			if !ok || callee.Name == providerBoxingHelper {
				continue // not an identifier callee, or the safe path
			}
			if isConcreteProviderConstructor(callee.Name, interfaceReturningCtors) {
				form := "direct"
				if parenthesised {
					form = "parenthesised"
				}
				out = append(out, factoryReturnViolation{form: form, text: renderExpr(res)})
			}
		case *ast.Ident:
			if _, isTainted := tainted[e.Name]; isTainted {
				taintedIdx = i
			}
		}
	}

	if taintedIdx < 0 {
		return out
	}
	name := unparen(ret.Results[taintedIdx]).(*ast.Ident).Name
	taint := tainted[name]

	switch {
	// A tainted value travelling WITH a live error: the original defect.
	case hasNonNilCompanion(ret.Results, taintedIdx):
		out = append(out, factoryReturnViolation{
			form: "via-variable", text: renderExpr(ret.Results[taintedIdx]),
		})
	// The error was DISCARDED at the assignment, so nothing downstream — here
	// or in the caller — can know the pointer is nil. hasNonNilCompanion says
	// nothing about this shape: `p, _ := New…; return p, nil` has a companion
	// that IS nil, and `func() Provider { … return p }` has no companion at all.
	case taint.errVar == "":
		out = append(out, factoryReturnViolation{
			form: "error-discarded", text: renderExpr(ret.Results[taintedIdx]),
		})
	// A SINGLE-result return has no companion for hasNonNilCompanion to judge,
	// so it vacuously returned false and the shape was invisible. With the
	// error bound but never guarded, the pointer reaches the caller unchecked.
	case len(ret.Results) == 1 && !guarded[taint.errVar]:
		out = append(out, factoryReturnViolation{
			form: "single-result", text: renderExpr(ret.Results[taintedIdx]),
		})
	}
	return out
}

// taintIsUnguarded reports whether a constructor taint reaches a return with
// nothing having checked the constructor's error.
func taintIsUnguarded(taint constructorTaint, guarded map[string]bool) bool {
	return taint.errVar == "" || !guarded[taint.errVar]
}

// hasNonNilCompanion reports whether any result other than the one at skipIdx
// is something other than the untyped `nil` identifier.
//
// Note it is VACUOUSLY false for a one-result return — there is no companion
// to inspect. That is why judgeReturn tests the single-result and
// error-discarded shapes separately rather than relying on this alone.
func hasNonNilCompanion(results []ast.Expr, skipIdx int) bool {
	for i, res := range results {
		if i == skipIdx {
			continue
		}
		id, ok := unparen(res).(*ast.Ident)
		if !ok || id.Name != "nil" {
			return true
		}
	}
	return false
}

// unparen strips any depth of parentheses — the single line of code the old
// regexp was missing.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// renderExpr produces a short readable form of an expression for the failure
// message. Only the shapes this scan flags need to render well.
func renderExpr(e ast.Expr) string {
	switch v := unparen(e).(type) {
	case *ast.Ident:
		return v.Name
	case *ast.CallExpr:
		if id, ok := unparen(v.Fun).(*ast.Ident); ok {
			return id.Name + "(...)"
		}
	}
	return "<expression>"
}

// TestFactorySources_AllArmsGoThroughProviderOrNil is the half of the guard
// that covers the arms the dynamic sweep cannot reach — an arm whose
// constructor succeeds in a credential-free environment today would be
// invisible to the sweep and would still be a boxing site the moment that
// constructor gains a failure mode.
func TestFactorySources_AllArmsGoThroughProviderOrNil(t *testing.T) {
	// The two files whose top-level factories are declared `(Provider, error)`.
	// A file returning a CONCRETE type may return a constructor directly and is
	// deliberately not scanned.
	exempt := interfaceReturningProviderConstructors(t)
	if len(exempt) == 0 {
		t.Fatalf("no New<X>Provider function in this package was found to return the Provider " +
			"INTERFACE. NewProvider and NewCloudProvider both do, so an empty set means the " +
			"collector is broken and the N3 exemption below would be silently inert.")
	}
	t.Logf("interface-returning constructors exempted from the boxing rule: %d", len(exempt))

	for _, path := range []string{"factory.go", "provider_factory.go"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		violations, err := scanFactorySourceForUnwrappedConstructors(path, src, exempt)
		if err != nil {
			t.Fatalf("scanning %s: %v", path, err)
		}
		for _, v := range violations {
			t.Errorf("%s:%d: %s (%s form) reaches an interface-returning factory's "+
				"return without passing through %s. On the error path that boxes a "+
				"nil pointer into a non-nil Provider: every caller's `if prov != nil` "+
				"guard passes and the first method call panics. Wrap it: "+
				"return %s(...)",
				path, v.line, v.text, v.form, providerBoxingHelper, providerBoxingHelper)
		}
	}
}

// TestFactoryStaticScan_CatchesEveryKnownDefectShape is the scan's own §1.1
// proof. Without it the scan above is an assertion nobody has watched FAIL —
// exactly how the previous regexp shipped with two holes in it.
func TestFactoryStaticScan_CatchesEveryKnownDefectShape(t *testing.T) {
	mustFlag := []struct {
		name     string
		wantForm string
		exempt   map[string]bool
		src      string
	}{
		{
			name:     "direct",
			wantForm: "direct",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	return NewFooProvider(c)
}
`,
		},
		{
			name:     "parenthesised",
			wantForm: "parenthesised",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	return (NewFooProvider(c))
}
`,
		},
		{
			name:     "double_parenthesised",
			wantForm: "parenthesised",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	return ((NewFooProvider(c)))
}
`,
		},
		{
			name:     "two_step_via_variable",
			wantForm: "via-variable",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	p, err := NewFooProvider(c)
	return p, err
}
`,
		},
		{
			// N2(i): a BARE return with named results. No expression appears
			// in the statement, so an expression-only judgement saw nothing —
			// while the named result still carries the concrete pointer.
			name:     "bare_return_with_named_results",
			wantForm: "bare-return",
			src: `package llm
func NewProvider(c int) (p Provider, err error) {
	p, err = NewFooProvider(c)
	return
}
`,
		},
		{
			// N2(ii): a SINGLE-result function. hasNonNilCompanion has no
			// companion to inspect and returned false vacuously, so the shape
			// was invisible.
			name:     "single_result_error_discarded",
			wantForm: "error-discarded",
			src: `package llm
func NewProvider(c int) Provider {
	p, _ := NewFooProvider(c)
	return p
}
`,
		},
		{
			// The same discard with a nil companion: `return p, nil` reads as
			// the safe manually-inlined shape but nothing ever checked the
			// error, so p may still be a nil pointer.
			name:     "error_discarded_with_nil_companion",
			wantForm: "error-discarded",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	p, _ := NewFooProvider(c)
	return p, nil
}
`,
		},
		{
			// Single result, error bound but never guarded — the pointer
			// reaches the caller unchecked.
			name:     "single_result_unguarded_error",
			wantForm: "single-result",
			src: `package llm
func NewProvider(c int) Provider {
	p, err := NewFooProvider(c)
	_ = err
	return p
}
`,
		},
		{
			// ROUND-6 N1. The bare return here sits INSIDE the error guard and
			// resets nothing, so it hands back the named result `p` still
			// holding the typed-nil *FooProvider the failed constructor
			// returned — a non-nil interface wrapping a nil pointer, which is
			// exactly the defect this scan exists to catch.
			//
			// The scan accepted it because `err` IS compared against nil
			// somewhere in the body, and taintIsUnguarded asks only that
			// question. The accepting fixture below
			// ("bare_return_with_error_guard") differs from this one by a
			// single line — its `p = nil` — and that line is the whole
			// difference between correct and broken, so the guard has to look
			// at it.
			name:     "bare_return_inside_error_guard_without_nil_reset",
			wantForm: "bare-return-in-error-guard",
			src: `package llm
func NewProvider(c int) (p Provider, err error) {
	p, err = NewFooProvider(c)
	if err != nil {
		return
	}
	return
}
`,
		},
		{
			// ROUND-7 A5. The reset is real but sits in a SIBLING branch that
			// does not dominate the return: when c != 999 the guard returns
			// with `p` still holding the typed-nil the failed constructor
			// handed back. nilResetPositions used ast.Inspect over the whole
			// guard block, so any lexically-earlier reset counted regardless
			// of whether it executes — the approximation the round-6 comment
			// declared, here made falsifiable instead of prose.
			name:     "dead_sibling_reset_inside_error_guard",
			wantForm: "bare-return-in-error-guard",
			src: `package llm
func NewProvider(c int) (p Provider, err error) {
	p, err = NewFooProvider(c)
	if err != nil {
		if c == 999 {
			p = nil
		}
		return
	}
	return
}
`,
		},
		{
			name:     "two_step_inside_switch_arm",
			wantForm: "via-variable",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	switch c {
	case 1:
		p, err := NewFooProvider(c)
		return p, err
	}
	return nil, nil
}
`,
		},
	}
	for _, tc := range mustFlag {
		t.Run("flags_"+tc.name, func(t *testing.T) {
			got, err := scanFactorySourceForUnwrappedConstructors("fixture.go", []byte(tc.src), tc.exempt)
			if err != nil {
				t.Fatalf("scanning fixture: %v", err)
			}
			if len(got) == 0 {
				t.Fatalf("the %s spelling of the typed-nil defect was NOT flagged — the "+
					"guard has a hole a contributor reaches by writing it this way:\n%s",
					tc.name, tc.src)
			}
			if got[0].form != tc.wantForm {
				t.Errorf("flagged with form %q, want %q", got[0].form, tc.wantForm)
			}
		})
	}

	mustNotFlag := []struct {
		name   string
		exempt map[string]bool
		src    string
	}{
		{
			// ROUND-7 A5 — a DECLARED RESIDUAL, pinned rather than prose.
			//
			// This fixture IS the defect: the guard body contains no return,
			// so the typed-nil `p` escapes through the TRAILING bare return
			// outside the guard. bareReturnsNeedingNilReset only examines
			// returns INSIDE the guard body, so it does not see this one.
			//
			// It sits in mustNotFlag because that is what the scan actually
			// does today, NOT because the code is correct. Asserting it here
			// makes the boundary machine-checked: if a future change closes
			// the gap this row FAILS, and the honest-limits comment gets
			// corrected instead of quietly becoming an understatement.
			name: "DECLARED_GAP_trailing_bare_return_after_guard_without_return",
			src: `package llm
func NewProvider(c int) (p Provider, err error) {
	p, err = NewFooProvider(c)
	if err != nil {
		err = fmt.Errorf("wrap: %w", err)
	}
	return
}
`,
		},
		{
			name: "wrapped_direct",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	return providerOrNil(NewFooProvider(c))
}
`,
		},
		{
			name: "wrapped_two_step",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	p, err := NewFooProvider(c)
	return providerOrNil(p, err)
}
`,
		},
		{
			name: "concrete_returning_factory_is_out_of_scope",
			src: `package llm
func newFoo(c int) (*FooProvider, error) {
	return NewFooProvider(c)
}
`,
		},
		{
			// The manually-inlined providerOrNil — the exact shape
			// newOpenAICompatibleFromConfig uses in factory.go. Refusing this
			// would be a false positive, so it is pinned as a fixture.
			name: "two_step_with_explicit_error_check",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	p, err := NewFooProvider(c)
	if err != nil {
		return nil, err
	}
	return p, nil
}
`,
		},
		{
			// N3: NewHostedOpenAICompatibleProvider matches the
			// New<X>Provider shape but is DECLARED to return the Provider
			// INTERFACE, so returning it directly boxes nothing. Latent today
			// — no scanned file returns it yet — and pinned here so the day
			// one does, the guard does not refuse correct code (§11.4.201).
			name:   "interface_returning_callee_is_exempt",
			exempt: map[string]bool{"NewHostedOpenAICompatibleProvider": true},
			src: `package llm
func NewProvider(c int) (Provider, error) {
	return NewHostedOpenAICompatibleProvider(c)
}
`,
		},
		{
			// The counterpart proving the exemption is what does the work: the
			// SAME source WITHOUT the exemption is flagged, so a future
			// mis-built set cannot silently blind the scan. (Asserted
			// explicitly in TestFactoryStaticScan_ExemptionIsLoadBearing.)
			name: "single_result_with_error_guard",
			src: `package llm
func NewProvider(c int) Provider {
	p, err := NewFooProvider(c)
	if err != nil {
		return nil
	}
	return p
}
`,
		},
		{
			// A bare return reached only after the error was guarded is the
			// named-result spelling of the manually-inlined providerOrNil.
			name: "bare_return_with_error_guard",
			src: `package llm
func NewProvider(c int) (p Provider, err error) {
	p, err = NewFooProvider(c)
	if err != nil {
		p = nil
		return
	}
	return
}
`,
		},
		{
			name: "explicit_nil_error_return",
			src: `package llm
func NewProvider(c int) (Provider, error) {
	return nil, errSomething
}
`,
		},
	}
	for _, tc := range mustNotFlag {
		t.Run("accepts_"+tc.name, func(t *testing.T) {
			got, err := scanFactorySourceForUnwrappedConstructors("fixture.go", []byte(tc.src), tc.exempt)
			if err != nil {
				t.Fatalf("scanning fixture: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("correct code was refused (%d finding(s), first=%q %s) — a false "+
					"refusal is its own defect:\n%s",
					len(got), got[0].text, got[0].form, tc.src)
			}
		})
	}
}

// interfaceReturningProviderConstructors collects every New<X>Provider in THIS
// package that is declared to return the Provider INTERFACE rather than a
// concrete pointer.
//
// Built mechanically from the package's own sources rather than hardcoded, so
// it maintains itself: a constructor whose signature changes from *FooProvider
// to Provider joins the set on the next run, and one that changes back leaves
// it. A hand-written allowlist would drift, and a drifted allowlist here is a
// blind spot in a guard whose whole purpose is not to have any.
func interfaceReturningProviderConstructors(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !providerConstructorName.MatchString(fn.Name.Name) {
				continue
			}
			if declaresProviderInterfaceResult(fn) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// TestFactoryStaticScan_ExemptionIsLoadBearing proves the N3 exemption is what
// prevents the false positive, not an accident of the fixture. The SAME source
// must be flagged with an empty set and accepted with the constructor exempted
// — an exemption that changes nothing would be decoration (§1.1).
func TestFactoryStaticScan_ExemptionIsLoadBearing(t *testing.T) {
	const src = `package llm
func NewProvider(c int) (Provider, error) {
	return NewHostedOpenAICompatibleProvider(c)
}
`
	withoutExemption, err := scanFactorySourceForUnwrappedConstructors("fixture.go", []byte(src), nil)
	if err != nil {
		t.Fatalf("scanning fixture: %v", err)
	}
	if len(withoutExemption) == 0 {
		t.Fatalf("with NO exemption set the constructor-shaped callee was not flagged at all, so " +
			"the exemption below cannot be what makes the difference — the fixture proves nothing")
	}

	withExemption, err := scanFactorySourceForUnwrappedConstructors("fixture.go", []byte(src),
		map[string]bool{"NewHostedOpenAICompatibleProvider": true})
	if err != nil {
		t.Fatalf("scanning fixture: %v", err)
	}
	if len(withExemption) != 0 {
		t.Fatalf("an interface-returning callee was still refused despite the exemption (%d "+
			"finding(s)) — a false refusal is its own defect", len(withExemption))
	}
}

// TestInterfaceReturningConstructors_FindsTheRealOnes proves the collector
// reads real signatures rather than returning a plausible-looking empty or
// everything set.
func TestInterfaceReturningConstructors_FindsTheRealOnes(t *testing.T) {
	got := interfaceReturningProviderConstructors(t)

	// Declared `(Provider, error)` in this package — must be exempt.
	for _, want := range []string{"NewProvider", "NewCloudProvider", "NewHostedOpenAICompatibleProvider"} {
		if !got[want] {
			t.Errorf("%s is declared to return the Provider interface but the collector missed it; "+
				"returning it directly would be wrongly flagged as a boxing site", want)
		}
	}
	// Declared `(*OpenAICompatibleProvider, error)` — must NOT be exempt, or the
	// guard would stop catching the very defect it exists for.
	for _, mustNot := range []string{"NewOpenAICompatibleProvider", "NewKoboldAIProvider"} {
		if got[mustNot] {
			t.Errorf("%s returns a CONCRETE pointer but was collected as interface-returning; "+
				"exempting it would blind the typed-nil guard at that constructor", mustNot)
		}
	}
}
