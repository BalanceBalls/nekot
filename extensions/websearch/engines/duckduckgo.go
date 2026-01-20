package engines

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/BalanceBalls/nekot/util"
	"github.com/PuerkitoBio/goquery"
)

func PerformDuckDuckGoSearch(ctx context.Context, query string) ([]SearchEngineData, error) {
	baseURL := "https://html.duckduckgo.com/html/?"
	params := url.Values{}
	params.Add("q", query)
	requestURL := baseURL + params.Encode()

	util.Slog.Debug("looking up the following query", "value", query)

	client := &http.Client{Timeout: time.Second * 10}
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", GetUserAgent())
	// req.Header.Set("Referer", "https://html.duckduckgo.com/")
	// req.Header.Set("Sec-Fetch-User", "?1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {

		if resp.StatusCode == 202 || resp.StatusCode == 429 {
			return nil, fmt.Errorf("duckduckgo requests have been rate limited, wait for the limit to reset or temporarily disable web-search (ctrl+w)")
		}

		return nil, fmt.Errorf("duckduckgo web search returned a non-200 status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []SearchEngineData

	doc.Find(".result.results_links.results_links_deep.web-result").
		EachWithBreak(func(i int, s *goquery.Selection) bool {
			if i >= 5 {
				return false
			}

			title := strings.TrimSpace(s.Find("h2.result__title a.result__a").Text())
			linkHref, _ := s.Find("h2.result__title a.result__a").Attr("href")
			link := ""
			if strings.Contains(linkHref, "/l/?uddg=") {
				unescapedURL, err := url.Parse(linkHref)
				if err == nil {
					link = unescapedURL.Query().Get("uddg")
				} else {
					link = linkHref
				}

			} else {
				link = linkHref
			}

			snippet := strings.TrimSpace(s.Find("a.result__snippet").Text())

			if title != "" && link != "" {
				results = append(results, SearchEngineData{
					Title:   title,
					Snippet: snippet,
					Link:    link,
				})
			}
			return true
		})

	return results, nil
}
