package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

// endpoint_error_redaction_test.go — guard for RedactEndpointsInError, the
// SECOND credential carrier (CONST-042 / Article XII §12.1).
//
// RedactEndpointForMessage cleans the CONFIGURED endpoint string. It cannot
// touch the endpoint net/http and net/url store inside the error they return,
// and that error is interpolated into the same operator-facing messages. The
// round-6 BLOCKING finding was exactly that: a base URL correctly redacted to
// `https://xxxxx@…` sitting in the same line as an unredacted
// `Get "https://sk-live-…@…"`.
//
// Two properties are guarded here, and the SECOND is the one that makes the
// fix safe rather than merely effective:
//
//  1. the URL inside every *url.Error in the chain is redacted; and
//  2. the chain is PRESERVED — errors.Is, errors.As, errors.Unwrap and the
//     net.Error Timeout()/Temporary() assertions still behave identically.
//
// (2) is load-bearing, not decorative: internal/server's providerResolveStatus
// picks 500 vs 403 vs 400 vs 503 purely by errors.Is on sentinel values. A
// redaction that rebuilt the chain would silently change those HTTP status
// codes. (The pinned line range that used to appear here was dropped in round
// 8: a line number is a fact with a short shelf life, and the function name is
// the durable half. The errors.Is claim itself was CHECKED and holds — unlike
// the sibling Timeout() claim in this batch, which did not.)

func TestRedactEndpointsInError_RedactsTokenAsUsername(t *testing.T) {
	const secret = "sk-live-abcdef0123456789"
	ue := &url.Error{
		Op:  "Get",
		URL: "https://" + secret + "@api.example.com/v1/models",
		Err: errors.New("connection refused"),
	}
	got := RedactEndpointsInError(ue)
	if strings.Contains(got.Error(), secret) {
		t.Fatalf("credential still present after redaction: %s", got.Error())
	}
	if !strings.Contains(got.Error(), "api.example.com") {
		t.Errorf("redaction destroyed the diagnostic; the host must survive: %s", got.Error())
	}
}

// TestRedactEndpointsInError_PreservesTheChain is the safety half.
func TestRedactEndpointsInError_PreservesTheChain(t *testing.T) {
	sentinel := errors.New("dial sentinel")
	ue := &url.Error{
		Op:  "Post",
		URL: "https://sk-live-token@api.example.com/v1/chat/completions",
		Err: sentinel,
	}
	wrapped := fmt.Errorf("helixagent: POST /v1/chat/completions: %w", ue)

	got := RedactEndpointsInError(wrapped)

	if got != error(wrapped) {
		t.Errorf("the returned error must be the SAME value, so callers holding the "+
			"original see the redaction too; got %#v", got)
	}
	if !errors.Is(got, sentinel) {
		t.Errorf("errors.Is no longer reaches the leaf sentinel; status classification in " +
			"internal/server/llm_generate.go depends on exactly this")
	}
	var asURLErr *url.Error
	if !errors.As(got, &asURLErr) {
		t.Fatal("errors.As no longer finds the *url.Error in the chain")
	}
	if asURLErr != ue {
		t.Errorf("errors.As returned a DIFFERENT *url.Error (%p) than the one passed in (%p); "+
			"the chain was rebuilt rather than redacted in place", asURLErr, ue)
	}
	if errors.Unwrap(got) != error(ue) {
		t.Errorf("errors.Unwrap no longer yields the *url.Error")
	}
	if strings.Contains(asURLErr.URL, "sk-live-token") {
		t.Errorf("the *url.Error URL was not redacted: %q", asURLErr.URL)
	}
}

// TestRedactEndpointsInError_PreservesSentinelAndNetErrorBehaviour pins the two
// classifications callers actually make: a context sentinel reached through the
// chain, and the net.Error Timeout() assertion on a *url.Error.
func TestRedactEndpointsInError_PreservesSentinelAndNetErrorBehaviour(t *testing.T) {
	ue := &url.Error{
		Op:  "Get",
		URL: "https://sk-live-token@api.example.com/v1/models",
		Err: context.DeadlineExceeded,
	}
	got := RedactEndpointsInError(ue)

	if !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("errors.Is(context.DeadlineExceeded) broke; timeout handling depends on it")
	}
	ne, ok := got.(net.Error)
	if !ok {
		t.Fatal("the result no longer satisfies net.Error")
	}
	if !ne.Timeout() {
		t.Errorf("net.Error.Timeout() flipped to false; *url.Error derives it from its Err, so " +
			"a rebuilt chain would silently change retry/backoff behaviour")
	}
}

// TestRedactEndpointsInError_VisitsEveryNodeNotJustTheFirst is why the walk
// does not use errors.As: errors.As stops at the FIRST match, and a chain can
// carry more than one carrier.
func TestRedactEndpointsInError_VisitsEveryNodeNotJustTheFirst(t *testing.T) {
	inner := &url.Error{Op: "Get", URL: "https://inner-tok3n@inner.example.com/v1", Err: errors.New("x")}
	outer := &url.Error{Op: "Get", URL: "https://outer-tok3n@outer.example.com/v1", Err: inner}
	joined := errors.Join(outer, &url.Error{
		Op: "Get", URL: "https://joined-tok3n@joined.example.com/v1", Err: errors.New("y"),
	})

	got := RedactEndpointsInError(joined).Error()
	for _, secret := range []string{"inner-tok3n", "outer-tok3n", "joined-tok3n"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q survived: the walk missed a node. errors.As alone would stop at the "+
				"first *url.Error and leave the rest.\n  %s", secret, got)
		}
	}
	for _, host := range []string{"inner.example.com", "outer.example.com", "joined.example.com"} {
		if !strings.Contains(got, host) {
			t.Errorf("host %q was destroyed by redaction: %s", host, got)
		}
	}
}

// TestRedactEndpointsInError_NilAndCredentialFreeAreUntouched guards the
// no-op paths: nil in, nil out, and an ordinary error rendered byte-identically.
func TestRedactEndpointsInError_NilAndCredentialFreeAreUntouched(t *testing.T) {
	if got := RedactEndpointsInError(nil); got != nil {
		t.Errorf("nil must stay nil, got %v", got)
	}

	ue := &url.Error{Op: "Get", URL: "http://127.0.0.1:18434/v1/models", Err: errors.New("connection refused")}
	before := ue.Error()
	if after := RedactEndpointsInError(ue).Error(); after != before {
		t.Errorf("a credential-free transport error must render byte-identically.\n  before: %s\n  after : %s",
			before, after)
	}

	plain := errors.New("helixagent: decode response: unexpected EOF")
	if got := RedactEndpointsInError(plain).Error(); got != plain.Error() {
		t.Errorf("an error carrying no *url.Error must be untouched: %q", got)
	}
}

// TestRedactEndpointsInError_DocumentedOrderingBoundary pins the honest
// boundary the doc comment states (§11.4.6): fmt.Errorf formats EAGERLY, so
// redacting AFTER wrapping leaves the credential in the wrapper's own message.
//
// This test asserts the LIMITATION, not a bug. It exists so that if a future
// change makes wrap-then-redact safe, this test fails and the doc comment gets
// corrected instead of quietly becoming a lie — and so a reader cannot mistake
// the helper for something that works in any call order.
func TestRedactEndpointsInError_DocumentedOrderingBoundary(t *testing.T) {
	const secret = "sk-live-ordering"
	ue := &url.Error{Op: "Get", URL: "https://" + secret + "@api.example.com/v1", Err: errors.New("refused")}

	wrappedFirst := fmt.Errorf("context: %w", ue) // message formatted NOW, credential included
	RedactEndpointsInError(wrappedFirst)

	if !strings.Contains(wrappedFirst.Error(), secret) {
		t.Fatalf("the documented ordering boundary no longer holds: wrapping before redacting " +
			"now yields a clean message. Update the RedactEndpointsInError doc comment and the " +
			"call-site guidance rather than deleting this test.")
	}
}
