package amz

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PAClient talks to the official Product Advertising API 5.0. It signs requests
// with SigV4 using only the standard library (no AWS SDK dependency).
type PAClient struct {
	hc         *http.Client
	accessKey  string
	secretKey  string
	partnerTag string
	host       string
	region     string
	marketURL  string
}

// ErrNoPACreds is returned when PA-API credentials are missing.
var ErrNoPACreds = errors.New("PA-API requires AMZ_PAAPI_ACCESS_KEY, AMZ_PAAPI_SECRET_KEY and AMZ_PAAPI_PARTNER_TAG")

// NewPAClient builds a PA-API client from config, or returns ErrNoPACreds.
func NewPAClient(cfg Config) (*PAClient, error) {
	if cfg.PAAPIAccessKey == "" || cfg.PAAPISecretKey == "" || cfg.PAAPIPartnerTag == "" {
		return nil, ErrNoPACreds
	}
	mkt, _ := LookupMarketplace(cfg.Marketplace)
	return &PAClient{
		hc:         &http.Client{Timeout: cfg.Timeout},
		accessKey:  cfg.PAAPIAccessKey,
		secretKey:  cfg.PAAPISecretKey,
		partnerTag: cfg.PAAPIPartnerTag,
		host:       cfg.PAAPIHost,
		region:     cfg.PAAPIRegion,
		marketURL:  mkt.BaseURL(),
	}, nil
}

const paService = "ProductAdvertisingAPI"

// GetItems fetches one or more ASINs via the official API and returns raw item
// maps (the caller maps them into Product records).
func (p *PAClient) GetItems(ctx context.Context, asins []string) ([]map[string]any, error) {
	payload := map[string]any{
		"ItemIds":     asins,
		"PartnerTag":  p.partnerTag,
		"PartnerType": "Associates",
		"Marketplace": strings.TrimPrefix(p.marketURL, "https://"),
		"Resources": []string{
			"ItemInfo.Title", "ItemInfo.ByLineInfo", "ItemInfo.Features",
			"ItemInfo.ProductInfo", "ItemInfo.ContentInfo", "Offers.Listings.Price",
			"Offers.Listings.Availability.Message", "Images.Primary.Large",
			"BrowseNodeInfo.BrowseNodes", "CustomerReviews.Count", "CustomerReviews.StarRating",
		},
	}
	out, err := p.call(ctx, "GetItems", payload)
	if err != nil {
		return nil, err
	}
	return extractItems(out), nil
}

// SearchItems runs a keyword search via the official API.
func (p *PAClient) SearchItems(ctx context.Context, keywords string, count int) ([]map[string]any, error) {
	if count <= 0 || count > 10 {
		count = 10
	}
	payload := map[string]any{
		"Keywords":    keywords,
		"ItemCount":   count,
		"PartnerTag":  p.partnerTag,
		"PartnerType": "Associates",
		"Marketplace": strings.TrimPrefix(p.marketURL, "https://"),
		"Resources": []string{
			"ItemInfo.Title", "ItemInfo.ByLineInfo", "Offers.Listings.Price",
			"Images.Primary.Large", "CustomerReviews.StarRating",
		},
	}
	out, err := p.call(ctx, "SearchItems", payload)
	if err != nil {
		return nil, err
	}
	return extractSearchItems(out), nil
}

func extractItems(out map[string]any) []map[string]any {
	res, _ := out["ItemsResult"].(map[string]any)
	if res == nil {
		return nil
	}
	items, _ := res["Items"].([]any)
	return toMaps(items)
}

func extractSearchItems(out map[string]any) []map[string]any {
	res, _ := out["SearchResult"].(map[string]any)
	if res == nil {
		return nil
	}
	items, _ := res["Items"].([]any)
	return toMaps(items)
}

func toMaps(items []any) []map[string]any {
	var rows []map[string]any
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			rows = append(rows, m)
		}
	}
	return rows
}

// call signs and sends one PA-API operation, returning the decoded JSON body.
func (p *PAClient) call(ctx context.Context, op string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	target := "com.amazon.paapi5.v1.ProductAdvertisingAPIv1." + op
	path := "/paapi5/" + strings.ToLower(op)
	amzDate, dateStamp := apiTimestamp(ctx)
	headers := map[string]string{
		"content-encoding": "amz-1.0",
		"content-type":     "application/json; charset=utf-8",
		"host":             p.host,
		"x-amz-date":       amzDate,
		"x-amz-target":     target,
	}
	auth := p.sign(headers, path, body, amzDate, dateStamp)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+p.host+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", auth)
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("PA-API %s: http %d: %s", op, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// sign builds the SigV4 Authorization header for a PA-API request.
func (p *PAClient) sign(headers map[string]string, path string, body []byte, amzDate, dateStamp string) string {
	signedHeaders := "content-encoding;content-type;host;x-amz-date;x-amz-target"
	var canon strings.Builder
	canon.WriteString("content-encoding:" + headers["content-encoding"] + "\n")
	canon.WriteString("content-type:" + headers["content-type"] + "\n")
	canon.WriteString("host:" + headers["host"] + "\n")
	canon.WriteString("x-amz-date:" + headers["x-amz-date"] + "\n")
	canon.WriteString("x-amz-target:" + headers["x-amz-target"] + "\n")
	payloadHash := sha256hex(body)
	canonicalRequest := strings.Join([]string{
		"POST", path, "", canon.String(), signedHeaders, payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, p.region, paService, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, scope, sha256hex([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+p.secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, p.region)
	kService := hmacSHA256(kRegion, paService)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	return fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.accessKey, scope, signedHeaders, signature)
}

func apiTimestamp(ctx context.Context) (amzDate, dateStamp string) {
	now := timeNow(ctx).UTC()
	return now.Format("20060102T150405Z"), now.Format("20060102")
}

// timeNow is overridable for tests; defaults to time.Now.
func timeNow(_ context.Context) time.Time { return time.Now() }

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
