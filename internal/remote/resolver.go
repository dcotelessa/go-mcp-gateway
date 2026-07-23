package remote

import (
	"fmt"
	"os"
)

// Resolver maps gateway tier names to configured Adapter instances.
type Resolver struct {
	adapters map[string]Adapter
}

// NewResolver builds a Resolver from environment variables.
// Fails fast with a named-variable error if required keys are missing.
func NewResolver() (*Resolver, error) {
	openRouterKey := os.Getenv("OPENROUTER_API_KEY")
	if openRouterKey == "" {
		openRouterKey = os.Getenv("DEEPSEEK_API_KEY") // fallback alias
	}

	zaiKey := os.Getenv("ZAI_API_KEY")
	if zaiKey == "" {
		zaiKey = os.Getenv("ZHIPU_API_KEY") // Mastra uses ZHIPU_API_KEY
	}
	if zaiKey == "" {
		zaiKey = os.Getenv("GLM_API_KEY")
	}

	adapters := make(map[string]Adapter)

	// DeepSeek via OpenRouter
	if openRouterKey != "" {
		ds, err := NewDeepSeekAdapter(openRouterKey)
		if err != nil {
			return nil, fmt.Errorf("remote: resolver: %w", err)
		}

		fallback, err := NewOpenRouterFallbackAdapter(openRouterKey)
		if err != nil {
			return nil, fmt.Errorf("remote: resolver: %w", err)
		}

		adapters["remote_deepseek"] = WithFallback(ds, fallback)
	}

	// GLM via Z.ai Coding Plan
	if zaiKey != "" {
		glm, err := NewGLMAdapter(zaiKey)
		if err != nil {
			return nil, fmt.Errorf("remote: resolver: %w", err)
		}

		// GLM fallback also uses OpenRouter if key available
		if openRouterKey != "" {
			fallback, _ := NewOpenRouterFallbackAdapter(openRouterKey)
			adapters["remote_glm"] = WithFallback(glm, fallback)
		} else {
			adapters["remote_glm"] = glm
		}
	}

	return &Resolver{adapters: adapters}, nil
}

// Resolve returns the adapter for a given tier.
// Returns an error if the tier is unknown or no API key was configured for it.
func (r *Resolver) Resolve(tier string) (Adapter, error) {
	adapter, ok := r.adapters[tier]
	if !ok {
		return nil, fmt.Errorf("remote: no adapter configured for tier %q — check API key env vars", tier)
	}
	return adapter, nil
}

// Available returns the list of configured tier names.
func (r *Resolver) Available() []string {
	tiers := make([]string, 0, len(r.adapters))
	for t := range r.adapters {
		tiers = append(tiers, t)
	}
	return tiers
}
