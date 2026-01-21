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
	// TODO: parallelize these calls
	ddgResponse, ddgErr := engines.PerformDuckDuckGoSearch(context.WithoutCancel(ctx), query)
	braveResopnse, braveErr := engines.PerformBraveSearch(context.WithoutCancel(ctx), query)

	if ddgErr != nil && braveErr != nil {
		return []PageChunk{}, fmt.Errorf(
			"could not get response from search engines. Reasons: \n %w \n %w",
			ddgErr,
			braveErr)
	}

	searchEngineResponse := []engines.SearchEngineData{}

	half := pagesMax / 2
	if len(ddgResponse) > half {
		searchEngineResponse = append(searchEngineResponse, ddgResponse[:half]...)
	} else {
		searchEngineResponse = append(searchEngineResponse, ddgResponse...)
	}

	if len(braveResopnse) > half {
		searchEngineResponse = append(searchEngineResponse, braveResopnse[:half]...)
	} else {
		searchEngineResponse = append(searchEngineResponse, braveResopnse...)
	}

	if len(searchEngineResponse) == 0 {

		return []PageChunk{}, fmt.Errorf("failed to get search enginge data")
	}

	// TODO: use bm25 on the snippets first to take first 10 and only then fetch pages content

	var wg sync.WaitGroup
	loadedPages := make(chan WebPageDataExport)

	numPages := min(len(searchEngineResponse), pagesMax)

	for i := range numPages {
		searchResult := searchEngineResponse[i]
		if searchResult.Link == "" {
			continue
		}

		wg.Add(1)
		go func(result engines.SearchEngineData) {
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
