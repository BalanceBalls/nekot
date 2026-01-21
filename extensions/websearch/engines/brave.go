package engines

import (
	"context"
	"fmt"
	htmlpkg "html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/unicode/norm"
)

var regexStripTagsBrave = regexp.MustCompile("<.*?>")

func PerformBraveSearch(ctx context.Context, query string) ([]SearchEngineData, error) {
	region := "en-us"
	payload, cookies, err := buildBravePayload(query, region)
	if err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	searchURL := "https://search.brave.com/search"
	parsedURL, err := url.Parse(searchURL)
	if err != nil {
		return nil, err
	}
	jar.SetCookies(parsedURL, cookies)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Jar:     jar,
	}

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

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	return extractBraveResults(doc), nil
}

func normalizeBraveURL(raw string) string {
	if raw == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw
	}
	return strings.ReplaceAll(decoded, " ", "+")
}

func normalizeBraveText(raw string) string {
	if raw == "" {
		return ""
	}
	text := regexStripTagsBrave.ReplaceAllString(raw, "")
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

func buildBravePayload(query, region string) (url.Values, []*http.Cookie, error) {
	payload := url.Values{}
	payload.Set("q", query)
	payload.Set("source", "web")

	parts := strings.Split(strings.ToLower(region), "-")
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid region: %s", region)
	}
	country := parts[0]

	cookies := []*http.Cookie{
		{Name: country, Value: country},
		{Name: "useLocation", Value: "0"},
	}

	return payload, cookies, nil
}

func findTitleSelection(item *goquery.Selection) string {
	selection := item.Find("div.title, div.sitename-container")
	if selection.Length() == 0 {
		return ""
	}
	return selection.Last().Text()
}

func findHref(item *goquery.Selection) string {
	var href string
	item.Find("a").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if sel.Find("div.title").Length() > 0 {
			if value, ok := sel.Attr("href"); ok {
				href = value
				return false
			}
		}
		return true
	})
	return href
}

func extractBraveResults(doc *goquery.Document) []SearchEngineData {
	results := []SearchEngineData{}
	doc.Find("div[data-type='web']").Each(func(_ int, item *goquery.Selection) {
		result := SearchEngineData{}

		result.Title = normalizeBraveText(findTitleSelection(item))
		result.Link = "[BRAVE]" + normalizeBraveURL(findHref(item))
		result.Snippet = normalizeBraveText(item.Find("div.snippet div.content").Text())

		results = append(results, result)
	})

	return results
}
