package llm

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
)

// ROUND-7 — the scrub introduced in this round wraps *url.Error.Err. That is
// exactly the fix-A-creates-B hazard (§11.4.1): url.Error.Timeout() does a
// DIRECT type assertion on e.Err, not errors.As, so a wrapper that does not
// forward Timeout()/Temporary() silently flips timeout classification for
// every caller that asserts net.Error on a transport error.
//
// ROUND-8 N-1 — this comment used to name internal/server/llm_generate.go as
// the consumer that "picks HTTP status codes off exactly that". Measured: it
// does not. providerResolveStatus keys purely on errors.Is against three
// sentinels, and internal/server has no non-test use of Timeout()/Temporary().
// The guard below is unchanged and still correct — *url.Error's net.Error
// behaviour is part of the PUBLIC CONTRACT this helper promises not to
// disturb, which binds for any caller, present or future. Naming a consumer
// that does not exist made a true guard rest on a false premise.
//
// The username here is chosen so the CAUSE text genuinely contains it
// ("context deadline exceeded"), which is what triggers the wrap.
func TestRedactEndpointsInError_ScrubbedCausePreservesNetErrorBehaviour(t *testing.T) {
	const secret = "deadline"
	ue := &url.Error{
		Op:  "Get",
		URL: "https://" + secret + ":pw@api.example.com/v1/models",
		Err: context.DeadlineExceeded,
	}
	got := RedactEndpointsInError(ue)

	if strings.Contains(got.Error(), "pw@") {
		t.Errorf("password survived: %s", got.Error())
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("errors.Is(context.DeadlineExceeded) broke after the cause was scrubbed")
	}
	ne, ok := got.(net.Error)
	if !ok {
		t.Fatal("no longer satisfies net.Error")
	}
	if !ne.Timeout() {
		t.Errorf("net.Error.Timeout() flipped to false once the cause was scrubbed; "+
			"url.Error.Timeout() type-asserts on e.Err directly, so the scrub wrapper MUST "+
			"forward Timeout(). error=%s", got.Error())
	}
}
