package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/sipeed/picoclaw/pkg/tools"
	"golang.org/x/net/publicsuffix"
)

const newsFailure = "今朝未能完成新聞核實，暫時無法提供可靠摘要。"
const newsInstruction = `
Return ONLY a JSON object: {"status":"news"|"no_news"|"verification_failed","text":"final user-facing summary","source_urls":["https://..."],"items":[{"text":"one important news item","source_urls":["https://...","https://..."]}]}.
Read sources using web_fetch in THIS run. Search snippets are not read sources. Each important news item needs the required number of independent sites. A no_news conclusion also needs that many sites in source_urls. Check dates, facts and reliability yourself. Do not send intermediate messages or promise later delivery. For news, every important claim must be in items; text is unused. If verification fails, use verification_failed.
`

func validateNews(raw string, minimum int, evidence *tools.WebEvidence) (string, error) {
	var result struct {
		Status  string   `json:"status"`
		Text    string   `json:"text"`
		Sources []string `json:"source_urls"`
		Items   []struct {
			Text    string   `json:"text"`
			Sources []string `json:"source_urls"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
		return "", fmt.Errorf("invalid news result")
	}
	check := func(sources []string) error {
		sites := map[string]bool{}
		for _, source := range sources {
			final, ok := evidence.VerifiedURL(source)
			if !ok {
				return fmt.Errorf("news source was not successfully read in this run")
			}
			parsed, err := url.Parse(final)
			if err != nil {
				return err
			}
			site, err := publicsuffix.EffectiveTLDPlusOne(parsed.Hostname())
			if err != nil {
				site = parsed.Hostname()
			}
			if site != "" {
				sites[strings.ToLower(site)] = true
			}
		}
		if len(sites) < minimum {
			return fmt.Errorf("insufficient independent verified news sources")
		}
		return nil
	}
	switch result.Status {
	case "no_news":
		if err := check(result.Sources); err != nil {
			return "", err
		}
		if strings.TrimSpace(result.Text) == "" {
			return "", fmt.Errorf("empty news conclusion")
		}
		return result.Text + "\n" + strings.Join(result.Sources, "\n"), nil
	case "news":
		if len(result.Items) == 0 {
			return "", fmt.Errorf("missing news items")
		}
		var parts []string
		for _, item := range result.Items {
			if strings.TrimSpace(item.Text) == "" {
				return "", fmt.Errorf("empty news item")
			}
			if err := check(item.Sources); err != nil {
				return "", err
			}
			parts = append(parts, item.Text+"\n"+strings.Join(item.Sources, "\n"))
		}
		return strings.Join(parts, "\n\n"), nil
	default:
		return "", fmt.Errorf("news verification failed")
	}
}
