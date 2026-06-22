package models

// Model is a selectable OpenRouter model — a subset of the /models response
// shaped for the Settings model picker. Prices are normalized to USD per 1M
// tokens (the raw API reports per-token strings).
type Model struct {
	ID              string  `json:"id"`   // e.g. "anthropic/claude-sonnet-4"
	Name            string  `json:"name"` // e.g. "Anthropic: Claude Sonnet 4"
	Description     string  `json:"description"`
	ContextLength   int     `json:"contextLength"`
	SupportsVision  bool    `json:"supportsVision"`  // input modalities include "image"
	IsFree          bool    `json:"isFree"`          // prompt and completion both priced at 0
	PromptPrice     float64 `json:"promptPrice"`     // USD per 1M input tokens
	CompletionPrice float64 `json:"completionPrice"` // USD per 1M output tokens
}
