// Package performance holds the load-generating performance suites.
//
// Every _test.go file here carries `//go:build loadtest`, which keeps these
// suites OUT of the default `go test ./...` unit pass — see any of those files
// for the measured reason (host ephemeral-port exhaustion under concurrent
// load) and docs/qa/2026-09-07-ephemeral-port-exhaustion/ANALYSIS.md.
//
// THIS FILE EXISTS TO KEEP THE PACKAGE RESOLVABLE. It carries no build
// constraint deliberately. Without it every file in the directory is
// constrained out, and `go test ./tests/performance` fails with
//
//	build constraints exclude all Go files in .../tests/performance
//	FAIL dev.helix.code/tests/performance [setup failed]
//
// which is a confusing error for anyone querying the package directly (`./...`
// silently drops it instead, so the unit pass was unaffected either way). With
// this file present the same command reports the accurate "no test files".
// The three sibling load packages each already have a non-test file and so do
// not need one.
//
// To run these suites:
//
//	make test-loadtest
package performance
