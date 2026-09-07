package llm

import (
	"testing"
)

func TestFactory_CreatesXiaomiProvider(t *testing.T) {
	// Xiaomi is a HOSTED provider (xiaomiDefaultBaseURL is
	// https://api.xiaomimimo.com/v1), so it is subject to the cloud gate now
	// that the NewOpenAICompatibleProvider bypass is closed. This test's
	// subject is FACTORY WIRING -- that NewProvider routes ProviderTypeXiaomi
	// to the Xiaomi provider -- not gate policy, so it opens the gate. Gate
	// policy itself is asserted by the cloud-gate tests. Never exempt Xiaomi in
	// the gate to make this pass: that would re-open the bypass.
	openCloudGateForTest(t)

	config := ProviderConfigEntry{
		Type:    ProviderTypeXiaomi,
		APIKey:  "sk-test123",
		Enabled: true,
		Models:  []string{"mimo-v2.5"},
	}
	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	if provider.GetType() != ProviderTypeXiaomi {
		t.Errorf("GetType() = %q, want %q", provider.GetType(), ProviderTypeXiaomi)
	}
}
