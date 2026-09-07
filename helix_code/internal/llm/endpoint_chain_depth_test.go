package llm

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// endpoint_chain_depth_test.go — ROUND-7 A3.
//
// errorChainRedactionMaxDepth bounds the cause-chain walk so a cyclic chain
// cannot hang an error path. A chain DEEPER than the bound is a SILENT
// fail-open: the walk stops and every *url.Error below the cut keeps its
// credential, with nothing reporting that redaction was skipped.
//
// The bound was 32, which ordinary wrapping can plausibly approach. It is 256
// now. These two tests make BOTH facts machine-checked rather than prose: that
// the walk really does reach a deep node, and that the residual fail-open
// beyond the bound is exactly where the constant says it is.

// wrapN nests err inside n lazily-unwrapping wrappers.
func wrapN(err error, n int) error {
	for i := 0; i < n; i++ {
		err = fmt.Errorf("layer %d: %w", i, err)
	}
	return err
}

// TestRedactEndpointsInError_ReachesDeepNodesWithinTheBound proves the raised
// bound is real reach, not a raised number.
func TestRedactEndpointsInError_ReachesDeepNodesWithinTheBound(t *testing.T) {
	const secret = "sk-live-deep-but-within-bound"
	leaf := &url.Error{
		Op:  "Get",
		URL: "https://" + secret + "@api.example.com/v1",
		Err: errors.New("refused"),
	}
	// One layer short of the bound, so the leaf sits AT the last visited depth.
	RedactEndpointsInError(wrapN(leaf, errorChainRedactionMaxDepth-1))

	if strings.Contains(leaf.URL, secret) {
		t.Fatalf("a *url.Error at depth %d was NOT redacted; the walk does not reach the "+
			"depth the bound claims. url=%q", errorChainRedactionMaxDepth-1, leaf.URL)
	}
}

// TestRedactEndpointsInError_DeclaredResidualBeyondTheBound asserts the
// LIMITATION, not a bug — the §11.4.6 honest boundary the constant's comment
// states. If a future change makes the walk unbounded this test FAILS, and the
// comment gets corrected rather than quietly becoming a lie.
func TestRedactEndpointsInError_DeclaredResidualBeyondTheBound(t *testing.T) {
	const secret = "sk-live-past-the-declared-bound"
	leaf := &url.Error{
		Op:  "Get",
		URL: "https://" + secret + "@api.example.com/v1",
		Err: errors.New("refused"),
	}
	RedactEndpointsInError(wrapN(leaf, errorChainRedactionMaxDepth+1))

	if !strings.Contains(leaf.URL, secret) {
		t.Fatalf("the declared residual no longer holds: a *url.Error BEYOND "+
			"errorChainRedactionMaxDepth (%d) is now redacted. That is an improvement, but the "+
			"constant's doc comment still declares this case as an accepted fail-open — update "+
			"the comment and this test together rather than deleting either.",
			errorChainRedactionMaxDepth)
	}
}
