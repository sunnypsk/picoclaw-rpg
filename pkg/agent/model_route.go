package agent

import (
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type modelRouteCandidate struct {
	Alias          string
	ProviderName   string
	Model          string
	Provider       providers.LLMProvider
	SupportsVision bool
}

func (c modelRouteCandidate) fallbackCandidate() providers.FallbackCandidate {
	return providers.FallbackCandidate{
		Provider: c.ProviderName,
		Model:    c.Model,
	}
}

type modelRoute struct {
	Name       string
	Candidates []modelRouteCandidate
}

func (r modelRoute) configured() bool {
	return len(r.Candidates) > 0
}

func (r modelRoute) primary() modelRouteCandidate {
	if len(r.Candidates) == 0 {
		return modelRouteCandidate{}
	}
	return r.Candidates[0]
}

func (r modelRoute) fallbackCandidates() []providers.FallbackCandidate {
	out := make([]providers.FallbackCandidate, 0, len(r.Candidates))
	for _, candidate := range r.Candidates {
		out = append(out, candidate.fallbackCandidate())
	}
	return out
}

func (r modelRoute) find(providerName, model string) (modelRouteCandidate, bool) {
	key := providers.ModelKey(providerName, model)
	for _, candidate := range r.Candidates {
		if providers.ModelKey(candidate.ProviderName, candidate.Model) == key {
			return candidate, true
		}
	}
	return modelRouteCandidate{}, false
}

func (a *AgentInstance) routeForTurn(hasImageInput bool) modelRoute {
	if !hasImageInput {
		return a.TextRoute
	}
	if a.VisionRoute.configured() {
		return a.VisionRoute
	}

	primary := a.TextRoute.primary()
	if !primary.SupportsVision {
		logger.WarnCF("agent", "Image input received without a configured vision model route", map[string]any{
			"agent_id": a.ID,
			"model":    primary.Model,
		})
	}
	return a.TextRoute
}

func buildModelRoute(
	name string,
	cfg *config.Config,
	defaultProvider string,
	primary string,
	fallbacks []string,
	defaultLLM providers.LLMProvider,
	requireVision bool,
) (modelRoute, error) {
	route := modelRoute{Name: name}
	seen := make(map[string]bool)

	add := func(raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}

		candidate, err := buildModelRouteCandidate(cfg, defaultProvider, raw, defaultLLM)
		if err != nil {
			return err
		}
		if requireVision && !candidate.SupportsVision {
			return fmt.Errorf("model %q in %s route must have supports_vision=true", raw, name)
		}
		if candidate.Provider == nil {
			return fmt.Errorf("model %q in %s route has no provider", raw, name)
		}

		key := providers.ModelKey(candidate.ProviderName, candidate.Model)
		if seen[key] {
			return nil
		}
		seen[key] = true
		route.Candidates = append(route.Candidates, candidate)
		return nil
	}

	if err := add(primary); err != nil {
		return modelRoute{}, err
	}
	for _, fallback := range fallbacks {
		if err := add(fallback); err != nil {
			return modelRoute{}, err
		}
	}
	return route, nil
}

func buildModelRouteCandidate(
	cfg *config.Config,
	defaultProvider string,
	raw string,
	defaultLLM providers.LLMProvider,
) (modelRouteCandidate, error) {
	if cfg != nil {
		if modelCfg, ok := resolveRouteModelConfig(cfg, raw); ok {
			cfgCopy := *modelCfg
			if cfgCopy.Workspace == "" {
				cfgCopy.Workspace = cfg.WorkspacePath()
			}
			provider, modelID, err := providers.CreateProviderFromConfig(&cfgCopy)
			if err != nil {
				return modelRouteCandidate{}, err
			}
			protocol, _ := providers.ExtractProtocol(cfgCopy.Model)
			return modelRouteCandidate{
				Alias:          cfgCopy.ModelName,
				ProviderName:   providers.NormalizeProvider(protocol),
				Model:          modelID,
				Provider:       provider,
				SupportsVision: cfgCopy.SupportsVision,
			}, nil
		}
	}

	ref := providers.ParseModelRef(raw, defaultProvider)
	if ref == nil {
		return modelRouteCandidate{}, fmt.Errorf("invalid model reference %q", raw)
	}
	return modelRouteCandidate{
		Alias:        raw,
		ProviderName: ref.Provider,
		Model:        ref.Model,
		Provider:     defaultLLM,
	}, nil
}

func resolveRouteModelConfig(cfg *config.Config, raw string) (*config.ModelConfig, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || cfg == nil {
		return nil, false
	}

	if modelCfg, err := cfg.GetModelConfig(raw); err == nil && modelCfg != nil {
		return modelCfg, true
	}

	for i := range cfg.ModelList {
		fullModel := strings.TrimSpace(cfg.ModelList[i].Model)
		if fullModel == "" {
			continue
		}
		if fullModel == raw {
			return &cfg.ModelList[i], true
		}
		_, modelID := providers.ExtractProtocol(fullModel)
		if modelID == raw {
			return &cfg.ModelList[i], true
		}
	}

	return nil, false
}
