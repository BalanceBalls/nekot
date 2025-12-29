package websearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/util"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/PuerkitoBio/goquery"
	"github.com/tmc/langchaingo/textsplitter"
)

const pagesMax = 10
const chunksToInclude = 2

type WebSearchResult struct {
	Data  string  `json:"data"`
	Link  string  `json:"link"`
	Score float64 `json:"score"`
}

type SearchEngineData struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Link    string `json:"link"`
}

type WebPageDataExport struct {
	SearchEngineData
	ContentChunks []string
	Err           error
}

type PageChunk struct {
	SearchEngineData
	Content string
}

func PrepareContextFromWebSearch(ctx context.Context, query string) ([]WebSearchResult, error) {
	corpus, err := getDataChunksFromQuery(ctx, query)
	if err != nil {
		return []WebSearchResult{}, err
	}

	bm25 := NewBM25(corpus)
	rankedChunks := bm25.Search(query)
	util.SortByNumberDesc(rankedChunks, func(s SearchResult) float64 { return s.Score })

	topRankedChunks := rankedChunks[:chunksToInclude]

	results := []WebSearchResult{}
	for _, topChunk := range topRankedChunks {
		chunkData := corpus[topChunk.DocID]
		results = append(results, WebSearchResult{
			Link:  chunkData.Link,
			Data:  chunkData.Content,
			Score: topChunk.Score,
		})
	}

	return results, nil
}

func getDataChunksFromQuery(ctx context.Context, query string) ([]PageChunk, error) {
	searchEngineResponse, err := performDuckDuckGoSearch(ctx, query)

	if err != nil {
		return []PageChunk{}, err
	}

	if len(searchEngineResponse) == 0 {
		return []PageChunk{}, nil
	}

	var wg sync.WaitGroup
	loadedPages := make(chan WebPageDataExport)

	numPages := min(len(searchEngineResponse), pagesMax)

	for i := range numPages {
		searchResult := searchEngineResponse[i]

		wg.Add(1)
		go func(result SearchEngineData) {
			defer wg.Done()
			getWebPageData(ctx, result, loadedPages)
		}(searchResult)
	}

	go func() {
		wg.Wait()
		close(loadedPages)
	}()

	cleanChunks := []PageChunk{}
	for page := range loadedPages {
		if page.Err != nil {
			util.Slog.Warn("failed to load page data", "link", page.Link, "reason", page.Err.Error())
			continue
		}

		pageChunks := []PageChunk{}
		for _, chunk := range page.ContentChunks {
			pageChunks = append(pageChunks, PageChunk{
				SearchEngineData: page.SearchEngineData,
				Content:          chunk,
			})
		}

		cleanChunks = append(cleanChunks, pageChunks...)
	}

	return cleanChunks, err
}

func getWebPageData(
	ctx context.Context,
	searchResult SearchEngineData,
	results chan<- WebPageDataExport,
) {
	req, err := http.NewRequestWithContext(ctx, "GET", searchResult.Link, nil)
	if err != nil {
		results <- WebPageDataExport{SearchEngineData: searchResult, Err: err}
		return
	}
	client := &http.Client{Timeout: time.Second * 10}
	resp, err := client.Do(req)
	if err != nil {
		results <- WebPageDataExport{SearchEngineData: searchResult, Err: err}
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		results <- WebPageDataExport{SearchEngineData: searchResult, Err: err}
		return
	}

	content := string(body)
	markdown, err := htmltomarkdown.ConvertString(content)
	if err != nil {
		results <- WebPageDataExport{SearchEngineData: searchResult, Err: err}
		return
	}

	rawChunks, err := splitMarkdownString(markdown, 1500, 100)
	if err != nil {
		results <- WebPageDataExport{SearchEngineData: searchResult, Err: err}
		return
	}

	results <- WebPageDataExport{
		SearchEngineData: searchResult,
		ContentChunks:    rawChunks,
		Err:              nil,
	}
}

func splitMarkdownString(content string, size, overlap int) ([]string, error) {
	splitter := textsplitter.NewMarkdownTextSplitter()
	splitter.ChunkSize = size
	splitter.ChunkOverlap = overlap
	splitter.CodeBlocks = true

	chunks, err := splitter.SplitText(content)
	if err != nil {
		return nil, err
	}

	return chunks, err
}

func performDuckDuckGoSearch(ctx context.Context, query string) ([]SearchEngineData, error) {
	baseURL := "https://html.duckduckgo.com/html/?"
	params := url.Values{}
	params.Add("q", query)
	requestURL := baseURL + params.Encode()

	util.Slog.Debug("looking up the following query", "value", query)

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)AppleWebKit/537.36(KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
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
