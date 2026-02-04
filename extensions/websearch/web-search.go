package websearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/BalanceBalls/nekot/extensions/websearch/engines"
	"github.com/BalanceBalls/nekot/util"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/tmc/langchaingo/textsplitter"
)

const pagesMax = 10
const chunksToInclude = 2
const maxBodySize = 3 * 1024 * 1024 // 3MB limit

type WebSearchResult struct {
	Data  string  `json:"data"`
	Link  string  `json:"link"`
	Score float64 `json:"score"`
}

type WebPageDataExport struct {
	engines.SearchEngineData
	ContentChunks []string
	Err           error
}

type PageChunk struct {
	engines.SearchEngineData
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

	if len(rankedChunks) == 0 {
		return []WebSearchResult{}, nil
	}

	topRankedChunks := rankedChunks
	if len(rankedChunks) > chunksToInclude {
		topRankedChunks = rankedChunks[:chunksToInclude]
	}

	results := []WebSearchResult{}
	for _, topChunk := range topRankedChunks {
		chunkData := corpus[topChunk.DocID]
		util.Slog.Warn("Appended search result", "data", chunkData.SearchEngineData)

		results = append(results, WebSearchResult{
			Link:  chunkData.Link,
			Data:  chunkData.Content,
			Score: topChunk.Score,
		})
	}

	return results, nil
}

func getDataChunksFromQuery(ctx context.Context, query string) ([]PageChunk, error) {
	var (
		ddgResponse   []engines.SearchEngineData
		braveResponse []engines.SearchEngineData
		ddgErr        error
		braveErr      error
		wg            sync.WaitGroup
	)

	// TODO: add google
	wg.Add(2)

	go func() {
		defer wg.Done()
		ddgResponse, ddgErr = engines.PerformDuckDuckGoSearch(context.WithoutCancel(ctx), query)
	}()

	go func() {
		defer wg.Done()
		braveResponse, braveErr = engines.PerformBraveSearch(context.WithoutCancel(ctx), query)
	}()

	wg.Wait()

	if ddgErr != nil {
		util.Slog.Warn("DuckDuckGo search failed", "error", ddgErr)
	}
	if braveErr != nil {
		util.Slog.Warn("Brave search failed", "error", braveErr)
	}

	if ddgErr != nil && braveErr != nil {
		return []PageChunk{}, fmt.Errorf(
			"could not get response from search engines. \n DuckDuckGo: \n %w \n Brave: \n %w",
			ddgErr,
			braveErr)
	}

	allResults := append(ddgResponse, braveResponse...)

	if len(allResults) == 0 {
		return []PageChunk{}, fmt.Errorf("failed to get search engine data: no results found")
	}

	snippetChunks := make([]PageChunk, len(allResults))
	for i, res := range allResults {
		snippetChunks[i] = PageChunk{
			SearchEngineData: res,
			Content:          res.Title + " " + res.Snippet,
		}
	}

	// TODO: add LLM reranking
	// TODO: parse urls from tools and fetch them instead of going to search engines
	bm25 := NewBM25(snippetChunks)
	rankedResults := bm25.Search(query)

	keepSearchResultsAmount := len(rankedResults) / 2
	if keepSearchResultsAmount == 0 && len(rankedResults) > 0 {
		keepSearchResultsAmount = 1
	}

	if keepSearchResultsAmount > pagesMax {
		keepSearchResultsAmount = pagesMax
	}

	finalSelection := make([]engines.SearchEngineData, 0, keepSearchResultsAmount)
	for i := 0; i < keepSearchResultsAmount; i++ {
		docID := rankedResults[i].DocID
		finalSelection = append(finalSelection, allResults[docID])
	}

	util.Slog.Debug("final snippets selection for fetching pages", "data", finalSelection)

	var contentWg sync.WaitGroup
	loadedPages := make(chan WebPageDataExport, len(finalSelection))

	for _, result := range finalSelection {
		if result.Link == "" {
			continue
		}

		contentWg.Add(1)
		go func(r engines.SearchEngineData) {
			defer contentWg.Done()
			getWebPageData(ctx, r, loadedPages)
		}(result)
	}

	go func() {
		contentWg.Wait()
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

	return cleanChunks, nil
}

func getWebPageData(
	ctx context.Context,
	searchResult engines.SearchEngineData,
	results chan<- WebPageDataExport,
) {
	req, err := http.NewRequestWithContext(ctx, "GET", searchResult.Link, nil)
	if err != nil {
		results <- WebPageDataExport{
			SearchEngineData: searchResult,
			Err: fmt.Errorf("failed to prepare request. Link: [%s] , Reason: %w",
				searchResult.Link,
				err),
		}
		return
	}

	req.Header.Set("User-Agent", engines.GetUserAgent())

	client := &http.Client{Timeout: time.Second * 10}
	resp, err := client.Do(req)
	if err != nil {
		results <- WebPageDataExport{
			SearchEngineData: searchResult,
			Err: fmt.Errorf("failed to execute request. Link: [%s] , Title: [%s] , Reason: %w",
				searchResult.Link,
				searchResult.Title,
				err),
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		results <- WebPageDataExport{
			SearchEngineData: searchResult,
			Err: fmt.Errorf("HTTP %d: failed to fetch page",
				resp.StatusCode),
		}
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
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
