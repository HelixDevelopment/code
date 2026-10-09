package helixagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"dev.helix.code/internal/llm"
)

// Compile-time assertion that *Provider implements the OPTIONAL llm.Embedder
// capability interface. Embed is intentionally NOT part of the core
// llm.Provider interface — a future edit dropping it fails the build here, not
// at a distant caller.
var _ llm.Embedder = (*Provider)(nil)

// DefaultEmbeddingModel is the logical model used for /v1/embeddings when the
// caller does not specify one. HelixAgent fronts the embedding engine; the
// concrete model is resolved server-side.
const DefaultEmbeddingModel = "helixagent-embed"

// embeddingsRequest is the OpenAI-compatible POST /v1/embeddings body shape.
type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embeddingsResponse is the OpenAI-compatible /v1/embeddings response shape:
//
//	{"data":[{"index":0,"embedding":[0.1,0.2,...]}, ...], "model":"..."}
type embeddingsResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Embed turns each input string into a dense vector via the HelixAgent server's
// OpenAI-compatible POST /v1/embeddings endpoint, returning one vector per input
// in input order. The embedding model defaults to DefaultEmbeddingModel.
//
// This is a REAL HTTP call to the running HelixAgent server — there is no
// simulation and no hardcoded vector. An empty input returns an empty
// (non-nil) result with a nil error.
func (p *Provider) Embed(ctx context.Context, input []string) ([][]float32, error) {
	if len(input) == 0 {
		return [][]float32{}, nil
	}

	body, err := json.Marshal(embeddingsRequest{Model: DefaultEmbeddingModel, Input: input})
	if err != nil {
		return nil, fmt.Errorf("helixagent: marshal embeddings request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("helixagent: build embeddings request: %w", llm.RedactEndpointsInError(err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("helixagent: POST /v1/embeddings: %w", llm.RedactEndpointsInError(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("helixagent: read embeddings body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("helixagent: embeddings failed (HTTP %d): %s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var er embeddingsResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("helixagent: decode embeddings response: %w", err)
	}
	if er.Error != nil && er.Error.Message != "" {
		return nil, fmt.Errorf("helixagent: embeddings engine error: %s", er.Error.Message)
	}
	if len(er.Data) != len(input) {
		return nil, fmt.Errorf("helixagent: embeddings count mismatch: got %d vectors for %d inputs",
			len(er.Data), len(input))
	}

	// Reassemble by the index field so the result order matches input order even
	// if the server returns data out of order.
	out := make([][]float32, len(input))
	for _, d := range er.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("helixagent: embeddings index %d out of range", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	for i, v := range out {
		if len(v) == 0 {
			return nil, fmt.Errorf("helixagent: embeddings missing vector for input %d", i)
		}
	}
	return out, nil
}

// CosineSimilarity returns the cosine similarity of two equal-length vectors in
// [-1, 1]. It returns 0 when either vector is zero-length or zero-magnitude, or
// when the lengths differ. Exposed so a front-end embed_text tool can render a
// real similarity demo from the live vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// EmbedTextResult is the structured output of the read-only embed_text helper:
// the embedded text's vector dimension plus a real cosine-similarity demo
// against a second reference text.
type EmbedTextResult struct {
	Text          string  `json:"text"`
	Dimension     int     `json:"dimension"`
	CompareText   string  `json:"compare_text"`
	CosineToFirst float64 `json:"cosine_to_compare"`
}

// EmbedText is a small, read-only helper a front-end (or an agent tool named
// `embed_text`) calls to answer prompts like "embed this text and tell me the
// vector size". It embeds text (and, when compareText is non-empty, a second
// string) through the live Embedder, then reports the vector DIMENSION and a
// real cosine-similarity demo between the two.
//
// It is read-only (no mutation, no writes) and makes a REAL embeddings call.
func EmbedText(ctx context.Context, e llm.Embedder, text, compareText string) (*EmbedTextResult, error) {
	if e == nil {
		return nil, fmt.Errorf("embed_text: no embedder configured")
	}
	inputs := []string{text}
	if strings.TrimSpace(compareText) != "" {
		inputs = append(inputs, compareText)
	}
	vecs, err := e.Embed(ctx, inputs)
	if err != nil {
		return nil, err
	}
	res := &EmbedTextResult{
		Text:      text,
		Dimension: len(vecs[0]),
	}
	if len(vecs) == 2 {
		res.CompareText = compareText
		res.CosineToFirst = CosineSimilarity(vecs[0], vecs[1])
	}
	return res, nil
}
