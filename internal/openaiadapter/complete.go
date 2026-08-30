package openaiadapter

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

const completeMaxOutputTokens = 128

// Complete runs a single-turn text completion with optional system instructions.
func (a *ModelAdapter) Complete(ctx context.Context, modelName, system, user string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("openai adapter is not configured")
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if modelName != "" {
		ctx = WithModel(ctx, modelName)
	}

	req := &model.LLMRequest{
		Model: modelName,
		Contents: []*genai.Content{{
			Role:  genai.RoleUser,
			Parts: []*genai.Part{{Text: user}},
		}},
		Config: &genai.GenerateContentConfig{
			MaxOutputTokens: completeMaxOutputTokens,
		},
	}
	if system = strings.TrimSpace(system); system != "" {
		req.Config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: system}},
		}
	}

	var (
		out     strings.Builder
		lastErr error
	)
	for resp, err := range a.GenerateContent(ctx, req, false) {
		if err != nil {
			lastErr = err
			continue
		}
		if resp == nil || resp.Content == nil {
			continue
		}
		for _, part := range resp.Content.Parts {
			if part == nil || strings.TrimSpace(part.Text) == "" {
				continue
			}
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(part.Text)
		}
	}

	text := strings.TrimSpace(out.String())
	if text == "" {
		if lastErr != nil {
			return "", lastErr
		}
		return "", fmt.Errorf("empty model response")
	}
	return text, nil
}
