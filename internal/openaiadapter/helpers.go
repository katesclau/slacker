package openaiadapter

import "google.golang.org/genai"

const (
	typeMessage   = "message"
	roleUser      = "user"
	roleAssistant = "assistant"
)

func roleADKToOpenAI(adkRole string) string {
	switch adkRole {
	case genai.RoleModel:
		return roleAssistant
	default:
		return roleUser
	}
}
