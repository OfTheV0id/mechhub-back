package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// SearchArgs is the schema the LLM sees for web_search.
type SearchArgs struct {
	Query      string `json:"query" description:"要检索的查询词,用自然语言描述要查的信息"`
	MaxResults int    `json:"max_results,omitempty" description:"返回结果条数,默认 5,最多 10"`
}

// SearchResult 单条来源,前端据此渲染来源卡片。
type SearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score,omitempty"`
}

// SearchOutput 是 web_search 工具的结构化返回。
type SearchOutput struct {
	Query   string         `json:"query"`
	Answer  string         `json:"answer,omitempty"`
	Results []SearchResult `json:"results"`
}

// NewSearchTool 注册 web_search 工具,内部走 Tavily Search API。
// 与 OCR/grader 不同,它不依赖 session 缓存,是无状态的外部检索。
func NewSearchTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "web_search",
		Description: "联网检索最新或站外信息(新闻、实时数据、文档、事实核查等)。返回结构化结果(answer 摘要 + results[] 来源标题/链接/摘要)。需要时效性信息或自身知识可能过时时调用;纯概念讲解/已知信息不必调用。",
	}, runSearch)
}

// tavilyRequest / tavilyResponse 对齐 Tavily POST /search 的请求与响应。
type tavilyRequest struct {
	APIKey        string `json:"api_key"`
	Query         string `json:"query"`
	MaxResults    int    `json:"max_results"`
	SearchDepth   string `json:"search_depth"`
	IncludeAnswer bool   `json:"include_answer"`
}

type tavilyResponse struct {
	Answer  string `json:"answer"`
	Results []struct {
		Title   string  `json:"title"`
		URL     string  `json:"url"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	} `json:"results"`
}

func runSearch(tctx tool.Context, args SearchArgs) (SearchOutput, error) {
	if args.Query == "" {
		return SearchOutput{}, fmt.Errorf("query is empty")
	}

	apiKey := os.Getenv("TAVILY_API_KEY")
	if apiKey == "" {
		return SearchOutput{}, fmt.Errorf("missing TAVILY_API_KEY env")
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 10 {
		maxResults = 10
	}

	reqBody, err := json.Marshal(tavilyRequest{
		APIKey:        apiKey,
		Query:         args.Query,
		MaxResults:    maxResults,
		SearchDepth:   "basic",
		IncludeAnswer: true,
	})
	if err != nil {
		return SearchOutput{}, err
	}

	httpReq, err := http.NewRequestWithContext(tctx, http.MethodPost,
		"https://api.tavily.com/search", bytes.NewReader(reqBody))
	if err != nil {
		return SearchOutput{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := searchHTTPClient().Do(httpReq)
	if err != nil {
		return SearchOutput{}, fmt.Errorf("tavily request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return SearchOutput{}, fmt.Errorf("tavily returned status %d", resp.StatusCode)
	}

	var tr tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return SearchOutput{}, fmt.Errorf("parse tavily response: %w", err)
	}

	out := SearchOutput{Query: args.Query, Answer: tr.Answer}
	for _, r := range tr.Results {
		out.Results = append(out.Results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
			Score:   r.Score,
		})
	}
	return out, nil
}

var cachedSearchClient *http.Client

func searchHTTPClient() *http.Client {
	if cachedSearchClient == nil {
		cachedSearchClient = &http.Client{Timeout: 20 * time.Second}
	}
	return cachedSearchClient
}
