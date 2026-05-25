package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// HTTPClient HTTP客户端
type HTTPClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

// SetToken 设置认证 token
func (c *HTTPClient) SetToken(token string) {
	c.Token = token
}

// authHeader 返回 Authorization header 值，无 token 时返回空
func (c *HTTPClient) authHeader() (string, string) {
	if c.Token == "" {
		return "", ""
	}
	return "Authorization", "Bearer " + c.Token
}

// NewHTTPClient 创建HTTP客户端，支持 tcp:// 和 unix:// 地址格式
//
//	tcp://localhost:8080  → TCP 连接
//	unix:///tmp/nuts.sock → Unix socket 连接
func NewHTTPClient(addr string) *HTTPClient {
	if addr == "" {
		addr = "tcp://localhost:8080"
	}

	parsed, err := common.ParseAddr(addr)
	if err != nil {
		// 兼容旧的 http://host:port 格式
		return &HTTPClient{
			BaseURL: addr,
			HTTPClient: &http.Client{
				Timeout: 30 * time.Second,
			},
		}
	}

	var baseURL string
	var transport http.RoundTripper

	switch parsed.Scheme {
	case "tcp":
		baseURL = "http://" + parsed.Host
	case "unix":
		baseURL = "http://unix"
		transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", parsed.Host)
			},
		}
	default:
		baseURL = addr
	}

	return &HTTPClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

// Get 发送GET请求
func (c *HTTPClient) Get(path string) (*http.Response, error) {
	url := c.BaseURL + path
	fmt.Printf("[CLI] GET %s\n", url)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if k, v := c.authHeader(); k != "" {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[CLI] Response Status: %d\n", resp.StatusCode)
	return resp, nil
}

// Post 发送POST请求
func (c *HTTPClient) Post(path string, body interface{}) (*http.Response, error) {
	url := c.BaseURL + path
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
		fmt.Printf("[CLI] POST %s\nBody: %s\n", url, string(jsonBody))
	} else {
		fmt.Printf("[CLI] POST %s\nBody: (empty)\n", url)
	}
	req, err := http.NewRequest(http.MethodPost, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if k, v := c.authHeader(); k != "" {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[CLI] Response Status: %d\n", resp.StatusCode)
	return resp, nil
}

// Delete 发送DELETE请求
func (c *HTTPClient) Delete(path string) (*http.Response, error) {
	url := c.BaseURL + path
	fmt.Printf("[CLI] DELETE %s\n", url)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}
	if k, v := c.authHeader(); k != "" {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[CLI] Response Status: %d\n", resp.StatusCode)
	return resp, nil
}

// APIResponse API响应结构
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// PrintResponse 打印API响应
func PrintResponse(resp *http.Response) error {
	defer resp.Body.Close()

	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	fmt.Printf("[CLI] API Response: code=%d, message=%s\n", apiResp.Code, apiResp.Message)

	if apiResp.Code != 0 {
		return fmt.Errorf("API error: %s", apiResp.Message)
	}

	// 处理 nil 数据的情况
	if apiResp.Data == nil {
		fmt.Println("Success")
		return nil
	}

	// 格式化输出（禁用 HTML 转义）
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(apiResp.Data); err != nil {
		fmt.Println(apiResp.Data)
	} else {
		fmt.Print(buf.String())
	}
	return nil
}
