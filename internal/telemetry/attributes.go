// attributes.go centralises all gen_ai.* and gateway.* attribute keys.
// When OTel GenAI semconv stabilises and renames attributes, this is
// the single file to update — callers import constants, never raw strings.
package telemetry

import "go.opentelemetry.io/otel/attribute"

// GenAI semantic convention attribute keys (Development status, v1.26.0).
// Reference: https://opentelemetry.io/docs/specs/semconv/gen-ai/
var (
	// AttrGenAISystem identifies the model provider/system.
	// Values: "llama_cpp", "deepseek", "z_ai", "openrouter"
	AttrGenAISystem = attribute.Key("gen_ai.system")

	// AttrGenAIRequestModel is the model name/alias used for the request.
	AttrGenAIRequestModel = attribute.Key("gen_ai.request.model")

	// AttrGenAITokenType distinguishes input vs output token counts.
	// Values: "input", "output"
	AttrGenAITokenType = attribute.Key("gen_ai.token.type")

	// AttrGenAIOperationName is the type of GenAI operation.
	// Values: "chat", "text_completion"
	AttrGenAIOperationName = attribute.Key("gen_ai.operation.name")
)

// Gateway-specific attribute keys (stable — gateway owns these).
var (
	// AttrGatewayTier is the routing tier used for this request.
	// Values: "local_ornith", "local_qwen", "remote_deepseek", "remote_glm"
	AttrGatewayTier = attribute.Key("gateway.tier")

	// AttrGatewayComplexity is the task complexity that drove tier selection.
	// Values: "scaffold", "single_file", "multi_file", "recovery", "text_op"
	AttrGatewayComplexity = attribute.Key("gateway.complexity")

	// AttrGatewayDataClass is the data classification for security routing.
	// Values: "public", "internal", "confidential", "secret"
	AttrGatewayDataClass = attribute.Key("gateway.data.classification")
)

// Token type values.
const (
	TokenTypeInput  = "input"
	TokenTypeOutput = "output"
)

// GenAI system values — maps gateway tiers to OTel system names.
var TierToSystem = map[string]string{
	"local_ornith":    "llama_cpp",
	"local_qwen":      "llama_cpp",
	"local_glm":       "llama_cpp",
	"remote_deepseek": "deepseek",
	"remote_glm":      "z_ai",
}

// SystemForTier returns the gen_ai.system value for a gateway tier.
// Falls back to "unknown" for unrecognised tiers.
func SystemForTier(tier string) string {
	if s, ok := TierToSystem[tier]; ok {
		return s
	}
	return "unknown"
}
