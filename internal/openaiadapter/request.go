package openaiadapter

import (
	"encoding/json"
	"strings"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func getRequestInput(req *model.LLMRequest) responses.ResponseNewParamsInputUnion {
	var input responses.ResponseNewParamsInputUnion
	if req == nil || req.Contents == nil {
		input.OfString = param.NewOpt(" ")
		return input
	}
	for _, content := range req.Contents {
		items := getRequestContentItems(content)
		for _, item := range items {
			if isEmptyInputItem(item) {
				continue
			}
			input.OfInputItemList = append(input.OfInputItemList, item)
		}
	}
	if len(input.OfInputItemList) == 0 {
		fallback := strings.TrimSpace(extractTextContents(req.Contents))
		if fallback == "" {
			fallback = " "
		}
		input.OfString = param.NewOpt(fallback)
	}
	return input
}

func extractTextContents(contents []*genai.Content) string {
	var out strings.Builder
	for _, content := range contents {
		if content == nil {
			continue
		}
		for _, part := range content.Parts {
			if part == nil || strings.TrimSpace(part.Text) == "" {
				continue
			}
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(part.Text)
		}
	}
	return out.String()
}

func getRequestContentItems(content *genai.Content) []responses.ResponseInputItemUnionParam {
	var items []responses.ResponseInputItemUnionParam
	role := roleADKToOpenAI(content.Role)

	for _, part := range content.Parts {
		var item responses.ResponseInputItemUnionParam
		if part.Text != "" {
			if role == roleAssistant {
				item.OfOutputMessage = &responses.ResponseOutputMessageParam{
					Content: []responses.ResponseOutputMessageContentUnionParam{
						{OfOutputText: &responses.ResponseOutputTextParam{Text: part.Text}},
					},
					Status: responses.ResponseOutputMessageStatusCompleted,
				}
			} else {
				item.OfInputMessage = &responses.ResponseInputItemMessageParam{
					Content: responses.ResponseInputMessageContentListParam{
						{OfInputText: &responses.ResponseInputTextParam{Text: part.Text}},
					},
					Role: role,
					Type: typeMessage,
				}
			}
			items = append(items, item)
			continue
		}
		if part.FunctionCall != nil {
			item.OfFunctionCall = &responses.ResponseFunctionToolCallParam{
				CallID:    part.FunctionCall.ID,
				Name:      part.FunctionCall.Name,
				Arguments: marshalArgs(part.FunctionCall.Args),
			}
			items = append(items, item)
			continue
		}
		if part.FunctionResponse != nil {
			item.OfFunctionCallOutput = &responses.ResponseInputItemFunctionCallOutputParam{
				CallID: part.FunctionResponse.ID,
				Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
					OfString: param.NewOpt(marshalOutput(part.FunctionResponse.Response)),
				},
			}
			items = append(items, item)
		}
	}
	return items
}

func getSystemInstructions(req *model.LLMRequest) string {
	if req == nil || req.Config == nil || req.Config.SystemInstruction == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range req.Config.SystemInstruction.Parts {
		sb.WriteString(part.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

func getRequestTools(req *model.LLMRequest) []responses.ToolUnionParam {
	if req == nil || req.Config == nil || req.Config.Tools == nil {
		return nil
	}
	var tools []responses.ToolUnionParam
	for _, tool := range req.Config.Tools {
		if tool.FunctionDeclarations == nil {
			continue
		}
		for _, fn := range tool.FunctionDeclarations {
			parameters := getToolParameters(fn)
			functionTool := responses.FunctionToolParam{
				Name:       fn.Name,
				Parameters: parameters,
				Strict:     param.NewOpt(false),
			}
			if fn.Description != "" {
				functionTool.Description = param.NewOpt(fn.Description)
			}
			tools = append(tools, responses.ToolUnionParam{OfFunction: &functionTool})
		}
	}
	return tools
}

func getToolParameters(fn *genai.FunctionDeclaration) map[string]any {
	if fn.ParametersJsonSchema != nil {
		if params, ok := fn.ParametersJsonSchema.(map[string]any); ok {
			return ensureObjectHasProperties(params)
		}
		if data, err := json.Marshal(fn.ParametersJsonSchema); err == nil {
			var params map[string]any
			if err := json.Unmarshal(data, &params); err == nil {
				return ensureObjectHasProperties(params)
			}
		}
	}
	if fn.Parameters != nil {
		return ensureObjectHasProperties(schemaToMap(fn.Parameters))
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func ensureObjectHasProperties(params map[string]any) map[string]any {
	if params == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if schemaType, ok := params["type"].(string); ok && schemaType == "object" {
		if _, ok := params["properties"]; !ok {
			params["properties"] = map[string]any{}
		}
	}
	return params
}

func schemaToMap(schema *genai.Schema) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	result := map[string]any{}
	if schema.Type != "" {
		result["type"] = schema.Type
	}
	if schema.Description != "" {
		result["description"] = schema.Description
	}
	if len(schema.Properties) > 0 {
		props := map[string]any{}
		for key, prop := range schema.Properties {
			props[key] = schemaToMap(prop)
		}
		result["properties"] = props
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}
	if schema.Items != nil {
		result["items"] = schemaToMap(schema.Items)
	}
	return result
}

func isEmptyInputItem(item responses.ResponseInputItemUnionParam) bool {
	return item.OfInputMessage == nil && item.OfOutputMessage == nil && item.OfFunctionCall == nil && item.OfFunctionCallOutput == nil
}

func marshalArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func marshalOutput(output map[string]any) string {
	if len(output) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
