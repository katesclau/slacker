package openaiadapter

import (
	"context"
	"encoding/json"

	"github.com/openai/openai-go/v3/responses"
	"github.com/sirupsen/logrus"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func getADKResponse(response *responses.Response) *model.LLMResponse {
	if response == nil || response.Output == nil {
		return nil
	}
	adkresp := &model.LLMResponse{ModelVersion: response.Model}
	var allParts []*genai.Part
	for _, out := range response.Output {
		allParts = append(allParts, getADKParts(out)...)
	}
	if len(allParts) > 0 {
		adkresp.Content = &genai.Content{
			Role:  genai.RoleModel,
			Parts: allParts,
		}
	}
	return adkresp
}

func getADKParts(output responses.ResponseOutputItemUnion) []*genai.Part {
	var parts []*genai.Part
	switch output.Type {
	case "function_call":
		if part := getFunctionCallPart(output); part != nil {
			parts = append(parts, part)
		}
	case "message":
		for _, content := range output.Content {
			if content.Text != "" {
				parts = append(parts, &genai.Part{Text: content.Text})
			}
		}
	default:
		for _, content := range output.Content {
			if content.Text != "" {
				parts = append(parts, &genai.Part{Text: content.Text})
			}
		}
	}
	return parts
}

func getFunctionCallPart(output responses.ResponseOutputItemUnion) *genai.Part {
	args := map[string]any{}
	if raw := output.Arguments.OfString; raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			logrus.WithContext(context.TODO()).WithError(err).Error("failed to parse function call arguments")
			args = map[string]any{}
		}
	}
	return &genai.Part{
		FunctionCall: &genai.FunctionCall{
			ID:   output.CallID,
			Name: output.Name,
			Args: args,
		},
	}
}
