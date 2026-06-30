package campusos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultHostAPIBaseURL = "http://127.0.0.1:18080"

type HostClient struct {
	baseURL    string
	pluginName string
	httpClient *http.Client
}

type HostClientOption func(*HostClient)

func WithHTTPClient(client *http.Client) HostClientOption {
	return func(c *HostClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func NewHostClient(pluginName string, opts ...HostClientOption) *HostClient {
	return NewHostClientWithBaseURL(DefaultHostAPIBaseURL, pluginName, opts...)
}

func NewHostClientWithBaseURL(baseURL, pluginName string, opts ...HostClientOption) *HostClient {
	client := &HostClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		pluginName: pluginName,
		httpClient: http.DefaultClient,
	}
	if client.baseURL == "" {
		client.baseURL = DefaultHostAPIBaseURL
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

func (c *HostClient) Call(ctx context.Context, method string, request interface{}, response interface{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal host api request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/host/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create host api request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.pluginName != "" {
		req.Header.Set("X-CampusOS-Plugin", c.pluginName)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call host api %s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read host api response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("host api %s failed: status=%d body=%s", method, resp.StatusCode, string(respBody))
	}
	if response == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, response); err != nil {
		return fmt.Errorf("decode host api response: %w", err)
	}
	return nil
}

type GetConfigRequest struct {
	Key string `json:"key,omitempty"`
}

type GetConfigResponse struct {
	Config map[string]interface{} `json:"config,omitempty"`
	Value  interface{}            `json:"value,omitempty"`
	Found  bool                   `json:"found"`
}

func (c *HostClient) GetConfig(ctx context.Context, key string) (interface{}, bool, error) {
	var response GetConfigResponse
	if err := c.Call(ctx, "GetConfig", GetConfigRequest{Key: key}, &response); err != nil {
		return nil, false, err
	}
	return response.Value, response.Found, nil
}

func (c *HostClient) GetAllConfig(ctx context.Context) (map[string]interface{}, error) {
	var response GetConfigResponse
	if err := c.Call(ctx, "GetConfig", GetConfigRequest{}, &response); err != nil {
		return nil, err
	}
	if response.Config == nil {
		return map[string]interface{}{}, nil
	}
	return response.Config, nil
}

type StorageGetRequest struct {
	PluginName string `json:"plugin_name"`
	Key        string `json:"key"`
}

type StorageGetResponse struct {
	Value string `json:"value"`
	Found bool   `json:"found"`
}

type StorageSetRequest struct {
	PluginName string `json:"plugin_name"`
	Key        string `json:"key"`
	Value      string `json:"value"`
}

type StorageDeleteRequest struct {
	PluginName string `json:"plugin_name"`
	Key        string `json:"key"`
}

func (c *HostClient) StorageGet(ctx context.Context, key string) (string, bool, error) {
	var response StorageGetResponse
	err := c.Call(ctx, "StorageGet", StorageGetRequest{PluginName: c.pluginName, Key: key}, &response)
	return response.Value, response.Found, err
}

func (c *HostClient) StorageSet(ctx context.Context, key, value string) error {
	return c.Call(ctx, "StorageSet", StorageSetRequest{PluginName: c.pluginName, Key: key, Value: value}, nil)
}

func (c *HostClient) StorageDelete(ctx context.Context, key string) error {
	return c.Call(ctx, "StorageDelete", StorageDeleteRequest{PluginName: c.pluginName, Key: key}, nil)
}
