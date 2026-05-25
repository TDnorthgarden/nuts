package tui

import (
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
	baseURL string
	client  *http.Client
	token   string
}

// NewHTTPClient 创建新的HTTP客户端，支持 tcp:// 和 unix:// 地址格式
//
//	tcp://localhost:8080  → TCP 连接
//	unix:///tmp/nuts.sock → Unix socket 连接
func NewHTTPClient(addr string) *HTTPClient {
	return NewHTTPClientWithToken(addr, "")
}

// NewHTTPClientWithToken 创建新的HTTP客户端，支持 tcp:// 和 unix:// 地址格式，并指定认证 token
//
//	tcp://localhost:8080  → TCP 连接
//	unix:///tmp/nuts.sock → Unix socket 连接
func NewHTTPClientWithToken(addr, token string) *HTTPClient {
	if addr == "" {
		addr = "tcp://localhost:8080"
	}

	parsed, err := common.ParseAddr(addr)
	if err != nil {
		// 兼容旧的 http://host:port 格式
		return &HTTPClient{
			baseURL: addr,
			client: &http.Client{
				Timeout: 10 * time.Second,
			},
			token: token,
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
		baseURL: baseURL,
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
		token: token,
	}
}

// SetToken 设置认证 token
func (c *HTTPClient) SetToken(token string) {
	c.token = token
}

// Get 发送GET请求
func (c *HTTPClient) Get(path string) (*APIResponse, error) {
	url := c.baseURL + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	return c.parseResponse(resp)
}

// Post 发送POST请求
func (c *HTTPClient) Post(path string, body interface{}) (*APIResponse, error) {
	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = io.NopCloser(&jsonBuffer{data})
	}

	req, err := http.NewRequest(http.MethodPost, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	return c.parseResponse(resp)
}

// parseResponse 解析HTTP响应
func (c *HTTPClient) parseResponse(resp *http.Response) (*APIResponse, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &apiResp, nil
}

// APIResponse API响应结构
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// IsSuccess 检查响应是否成功
func (r *APIResponse) IsSuccess() bool {
	return r.Code == 0
}

// jsonBuffer JSON缓冲区
type jsonBuffer struct {
	data []byte
}

func (b *jsonBuffer) Read(p []byte) (n int, err error) {
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n = copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}
