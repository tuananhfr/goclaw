package providers

// VoyageEmbeddingProvider wraps OpenAIEmbeddingProvider with Voyage AI base URL.
// Voyage is Anthropic's embedding partner and uses the same wire format as OpenAI.
type VoyageEmbeddingProvider struct {
	*OpenAIEmbeddingProvider
}

// NewVoyageEmbeddingProvider creates an embedding provider for Voyage AI.
// Default model: voyage-3 (1024 dim), matching the system's pgvector(1024) column.
func NewVoyageEmbeddingProvider(apiKey, model string) *VoyageEmbeddingProvider {
	if model == "" {
		model = "voyage-3" // 1024 dimensions, matches pgvector column
	}
	p := NewOpenAIEmbeddingProvider(apiKey, "https://api.voyageai.com/v1", model)
	p.providerName = "voyage"
	return &VoyageEmbeddingProvider{OpenAIEmbeddingProvider: p}
}
