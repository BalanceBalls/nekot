package engines

import (
	"context"
	"encoding/base64"
	"fmt"
	htmlpkg "html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/unicode/norm"
)

var regexStripTags = regexp.MustCompile("<.*?>")

func PerformBingSearch(ctx context.Context, query string) ([]SearchEngineData, error) {
	payload, err := buildPayload(query)
	if err != nil {
		return nil, err
	}

	searchURL := "https://www.bing.com/search"
	parsedURL, err := url.Parse(searchURL)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}

	requestURL := parsedURL.String() + "?" + payload.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", GetUserAgent())
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", response.Status)
	}

	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, err
	}

	results := extractResults(doc)
	return postProcessResults(results), nil
}

func unwrapBingURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	uVals := parsed.Query()["u"]
	if len(uVals) == 0 {
		return ""
	}

	u := uVals[0]
	if len(u) <= 2 {
		return ""
	}

	b64Part := u[2:]
	paddingNeeded := (4 - (len(b64Part) % 4)) % 4
	padding := strings.Repeat("=", paddingNeeded)
	decoded, err := base64.URLEncoding.DecodeString(b64Part + padding)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func normalizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw
	}
	return strings.ReplaceAll(decoded, " ", "+")
}

func normalizeText(raw string) string {
	if raw == "" {
		return ""
	}
	text := regexStripTags.ReplaceAllString(raw, "")
	text = htmlpkg.UnescapeString(text)
	text = norm.NFC.String(text)

	var builder strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.C, r) {
			continue
		}
		builder.WriteRune(r)
	}

	return strings.Join(strings.Fields(builder.String()), " ")
}

func buildPayload(query string) (url.Values, error) {
	payload := url.Values{}
	payload.Set("q", query)
	payload.Set("pq", query)

	return payload, nil
}

func extractResults(doc *goquery.Document) []SearchEngineData {
	items := doc.Find("li.b_algo")
	results := make([]SearchEngineData, 0, items.Length())

	items.Each(func(_ int, item *goquery.Selection) {
		var result SearchEngineData

		anchor := item.Find("h2 a").First()
		if anchor.Length() > 0 {
			result.Title = normalizeText(anchor.Text())
			result.Link = normalizeURL(anchor.AttrOr("href", ""))
		}

		var bodyParts []string
		item.Find("p").Each(func(_ int, paragraph *goquery.Selection) {
			text := strings.TrimSpace(paragraph.Text())
			if text != "" {
				bodyParts = append(bodyParts, text)
			}
		})
		if len(bodyParts) > 0 {
			result.Snippet = normalizeText(strings.Join(bodyParts, " "))
		}

		results = append(results, result)
	})

	return results
}

func postProcessResults(results []SearchEngineData) []SearchEngineData {
	filtered := make([]SearchEngineData, 0, len(results))
	for _, result := range results {
		href := result.Link
		if strings.HasPrefix(href, "https://www.bing.com/aclick?") {
			continue
		}
		if strings.HasPrefix(href, "https://www.bing.com/ck/a?") {
			if decoded := unwrapBingURL(href); decoded != "" {
				result.Link = normalizeURL(decoded)
			}
		}
		filtered = append(filtered, result)
	}
	return filtered
}
