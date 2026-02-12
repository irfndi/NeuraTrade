package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://gamma-api.polymarket.com"
	DefaultTimeout = 30 * time.Second
)

type Client struct {
	httpClient *http.Client
	baseURL    string
}

type ClientOption func(*Client)

func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = strings.TrimSuffix(baseURL, "/")
	}
}

func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: DefaultTimeout},
		baseURL:    DefaultBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type Market struct {
	ID                    string    `json:"id"`
	ConditionID           string    `json:"conditionId"`
	Slug                  string    `json:"slug"`
	Question              string    `json:"question"`
	Description           string    `json:"description"`
	Outcomes              []string  `json:"outcomes"`
	OutcomePrices         []string  `json:"outcomePrices"`
	OutcomeProbabilities  []float64 `json:"outcomeProbabilities"`
	Volume                string    `json:"volume"`
	VolumeNum             float64   `json:"volumeNum"`
	Liquidity             string    `json:"liquidity"`
	LiquidityNum          float64   `json:"liquidityNum"`
	Active                bool      `json:"active"`
	Closed                bool      `json:"closed"`
	Archived              bool      `json:"archived"`
	AcceptingOrders       bool      `json:"acceptingOrdersTimestamp"`
	Enabled               bool      `json:"enabled"`
	Featured              bool      `json:"featured"`
	New                   bool      `json:"new"`
	StartDate             string    `json:"startDate"`
	EndDate               string    `json:"endDate"`
	Image                 string    `json:"image"`
	Icon                  string    `json:"icon"`
	Categories            []string  `json:"categories"`
	Tags                  []string  `json:"tags"`
	MarketMakerAddress    string    `json:"marketMakerAddress"`
	CollateralVolume      string    `json:"collateralVolume"`
	CollateralLiquidity   string    `json:"collateralLiquidity"`
	TotalLiquidity        string    `json:"totalLiquidity"`
	OrderPriceMinTickSize float64   `json:"orderPriceMinTickSize"`
	OrderMinSize          float64   `json:"orderMinSize"`
	CreatedAt             string    `json:"createdAt"`
	UpdatedAt             string    `json:"updatedAt"`
}

type MarketsFilter struct {
	Active      *bool
	Closed      *bool
	Archived    *bool
	Limit       int
	Offset      int
	Tag         string
	Slug        string
	ConditionID string
	Query       string
	Sort        string
	Order       string
}

func (f *MarketsFilter) ToURLValues() url.Values {
	v := url.Values{}
	if f.Active != nil {
		v.Set("active", strconv.FormatBool(*f.Active))
	}
	if f.Closed != nil {
		v.Set("closed", strconv.FormatBool(*f.Closed))
	}
	if f.Archived != nil {
		v.Set("archived", strconv.FormatBool(*f.Archived))
	}
	if f.Limit > 0 {
		v.Set("limit", strconv.Itoa(f.Limit))
	}
	if f.Offset > 0 {
		v.Set("offset", strconv.Itoa(f.Offset))
	}
	if f.Tag != "" {
		v.Set("tag", f.Tag)
	}
	if f.Slug != "" {
		v.Set("slug", f.Slug)
	}
	if f.ConditionID != "" {
		v.Set("conditionId", f.ConditionID)
	}
	if f.Query != "" {
		v.Set("query", f.Query)
	}
	if f.Sort != "" {
		v.Set("_s", f.Sort)
	}
	if f.Order != "" {
		v.Set("_o", f.Order)
	}
	return v
}

func (c *Client) GetMarkets(ctx context.Context, filter *MarketsFilter) ([]Market, error) {
	path := "/markets"
	if filter != nil {
		params := filter.ToURLValues()
		if len(params) > 0 {
			path = path + "?" + params.Encode()
		}
	}

	var markets []Market
	if err := c.doRequest(ctx, "GET", path, nil, &markets); err != nil {
		return nil, fmt.Errorf("failed to get markets: %w", err)
	}
	return markets, nil
}

func (c *Client) GetMarket(ctx context.Context, conditionID string) (*Market, error) {
	path := fmt.Sprintf("/markets/%s", conditionID)

	var market Market
	if err := c.doRequest(ctx, "GET", path, nil, &market); err != nil {
		return nil, fmt.Errorf("failed to get market %s: %w", conditionID, err)
	}
	return &market, nil
}

func (c *Client) GetMarketBySlug(ctx context.Context, slug string) (*Market, error) {
	filter := &MarketsFilter{Slug: slug, Limit: 1}
	markets, err := c.GetMarkets(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get market by slug %s: %w", slug, err)
	}
	if len(markets) == 0 {
		return nil, fmt.Errorf("market with slug %s not found", slug)
	}
	return &markets[0], nil
}

func (c *Client) SearchMarkets(ctx context.Context, query string, limit int) ([]Market, error) {
	filter := &MarketsFilter{
		Query:  query,
		Limit:  limit,
		Active: boolPtr(true),
		Closed: boolPtr(false),
	}
	return c.GetMarkets(ctx, filter)
}

func (c *Client) GetMarketsByTag(ctx context.Context, tag string, limit int) ([]Market, error) {
	filter := &MarketsFilter{
		Tag:    tag,
		Limit:  limit,
		Active: boolPtr(true),
		Closed: boolPtr(false),
	}
	return c.GetMarkets(ctx, filter)
}

func (c *Client) GetFeaturedMarkets(ctx context.Context, limit int) ([]Market, error) {
	filter := &MarketsFilter{
		Limit:  limit,
		Active: boolPtr(true),
		Closed: boolPtr(false),
		Sort:   "featured",
		Order:  "desc",
	}
	return c.GetMarkets(ctx, filter)
}

func (c *Client) GetTrendingMarkets(ctx context.Context, limit int) ([]Market, error) {
	filter := &MarketsFilter{
		Limit:  limit,
		Active: boolPtr(true),
		Closed: boolPtr(false),
		Sort:   "volume",
		Order:  "desc",
	}
	return c.GetMarkets(ctx, filter)
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, v interface{}) error {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NeuraTrade/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func boolPtr(b bool) *bool {
	return &b
}

type SumToOneArbitrage struct {
	ConditionID     string
	YesPrice        float64
	NoPrice         float64
	TotalPrice      float64
	ProfitMargin    float64
	YesOutcomeIndex int
	NoOutcomeIndex  int
	Volume          float64
	Liquidity       float64
	Market          *Market
}

func (c *Client) FindSumToOneArbitrage(ctx context.Context, minVolume, minLiquidity float64, limit int) ([]SumToOneArbitrage, error) {
	filter := &MarketsFilter{
		Active: boolPtr(true),
		Closed: boolPtr(false),
		Limit:  limit,
		Sort:   "volume",
		Order:  "desc",
	}

	markets, err := c.GetMarkets(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get markets for arbitrage scan: %w", err)
	}

	var opportunities []SumToOneArbitrage
	for _, market := range markets {
		if market.VolumeNum < minVolume || market.LiquidityNum < minLiquidity {
			continue
		}

		if len(market.Outcomes) != 2 || len(market.OutcomePrices) != 2 {
			continue
		}

		yesPrice, err := strconv.ParseFloat(market.OutcomePrices[0], 64)
		if err != nil {
			continue
		}
		noPrice, err := strconv.ParseFloat(market.OutcomePrices[1], 64)
		if err != nil {
			continue
		}

		totalPrice := yesPrice + noPrice
		if totalPrice >= 1.0 {
			continue
		}

		profitMargin := (1.0 - totalPrice) / totalPrice * 100

		opportunities = append(opportunities, SumToOneArbitrage{
			ConditionID:     market.ConditionID,
			YesPrice:        yesPrice,
			NoPrice:         noPrice,
			TotalPrice:      totalPrice,
			ProfitMargin:    profitMargin,
			YesOutcomeIndex: 0,
			NoOutcomeIndex:  1,
			Volume:          market.VolumeNum,
			Liquidity:       market.LiquidityNum,
			Market:          &market,
		})
	}

	return opportunities, nil
}

func (c *Client) HealthCheck(ctx context.Context) error {
	filter := &MarketsFilter{Limit: 1}
	_, err := c.GetMarkets(ctx, filter)
	return err
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) Close() {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
}
