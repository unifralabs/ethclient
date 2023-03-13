package ethclient

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	CacheBlockPrefix    = "block:"
	CacheReceiptsPrefix = "receipt:"
	CacheTracePrefix    = "trace:"
)

// CallContext dispatches the bundle method to the unifra bundle API or the standard method to the underlying RPC client.
func (ec *Client) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	if isBundleMethod(method) {
		// Handle Bundle method
		cacheKey := getCacheKey(method, args...)
		if cacheValue, ok := ec.bundleCache.Get(cacheKey); ok {
			// Return value from cache
			return decodeResult(cacheValue, result)
		} else {
			// Fetch data from Bundle APIf
			url, err := getBundleURL(ec.apiEndpoint, method, args...)
			if err != nil {
				return err
			}

			data, err := fetchBundleDataWithContext(ctx, ec.httpClient, url)
			if err != nil {
				return err
			}

			// Parse data as array of JSON objects and store each object in the cache, through the blockNumber cache key
			matched, err := cacheData(ec.bundleCache, method, cacheKey, data)
			if err != nil {
				return err
			}

			// Return matched value
			return decodeResult(matched, result)
		}
	} else {
		// Handle standard method using underlying RPC client
		return ec.c.CallContext(ctx, result, method, args...)
	}
}

func isBundleMethod(method string) bool {
	switch method {
	case "eth_getBlockByNumber", "eth_getBlockReceipts", "trace_block":
		return true
	default:
		return false
	}
}

// getCacheKey returns the cache key for the given method and arguments.
// Format: block:<blockNumber>, receipt:<blockNumber>, trace:<blockNumber>
func getCacheKey(method string, args ...interface{}) string {
	switch method {
	case "eth_getBlockByNumber":
		return CacheBlockPrefix + args[0].(string)
	case "eth_getBlockReceipts":
		return CacheReceiptsPrefix + args[0].(string)
	case "trace_block":
		return CacheTracePrefix + args[0].(string)
	default:
		return ""
	}
}

// decodeResult decodes the given data into the given result.
func decodeResult(data []byte, result interface{}) error {
	return json.Unmarshal(data, result)
}

// getBundleURL returns the URL for the given bundle method and arguments.
func getBundleURL(apiEndpoint string, method string, args ...interface{}) (string, error) {
	if apiEndpoint == "" {
		return "", errors.New("getBundleURL: apiEndpoint is empty")
	}
	// handle / at the end of the apiEndpoint
	if apiEndpoint[len(apiEndpoint)-1] == '/' {
		apiEndpoint = apiEndpoint[:len(apiEndpoint)-1]
	}
	baseUrl := fmt.Sprintf("%s/bundle-api", apiEndpoint)

	switch method {
	case "eth_getBlockByNumber":
		blockNumber := args[0].(string)
		return fmt.Sprintf("%s/blocks/%s", baseUrl, blockNumber), nil
	case "eth_getBlockReceipts":
		blockNumber := args[0].(string)
		return fmt.Sprintf("%s/receipts/%s", baseUrl, blockNumber), nil
	case "trace_block":
		blockNumber := args[0].(string)
		return fmt.Sprintf("%s/traces/%s", baseUrl, blockNumber), nil
	default:
		return "", errors.New("getBundleURL: unsupported method")
	}
}

type metadata struct {
	Type         string `json:"type"`
	Range        string `json:"range"`
	DownloadLink string `json:"download_link"`
	Error        string `json:"error"`
}

func fetchBundleDataWithContext(ctx context.Context, httpClient *http.Client, url string) ([]byte, error) {
	// fetch metadata
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetchBundleDataWithContext: status code %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	if meta.Error != "" {
		return nil, errors.New(meta.Error)
	}

	// fetch bundle data and uncompress
	req, err = http.NewRequestWithContext(ctx, "GET", meta.DownloadLink, nil)
	if err != nil {
		return nil, err
	}

	resp, err = httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetchBundleDataWithContext: status code %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	data, err = io.ReadAll(gz)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func cacheData(cache *lru.ARCCache[string, []byte], bundleType string, cacheKey string, data []byte) ([]byte, error) {
	var matched []byte

	var blockInfosList []interface{}
	switch bundleType {
	case "traces", "receipts":
		if err := json.Unmarshal(data, &blockInfosList); err != nil {
			return nil, err
		}
	case "blocks":
		if err := json.Unmarshal(data, &blockInfosList); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("invalid bundle type: %s", bundleType)
	}

	for _, blockInfos := range blockInfosList {
		var (
			blockNumberHex string
			err            error
		)
		if bundleType == "receipts" || bundleType == "traces" {
			binfos := blockInfos.([]interface{})
			if len(binfos) == 0 {
				continue
			}
			var ok bool
			blockNumberStr, ok := binfos[0].(map[string]interface{})["number"].(string)
			if !ok {
				return nil, fmt.Errorf("failed to parse block number, blockInfos: %v", blockInfos)
			}
			if !strings.HasPrefix(blockNumberStr, "0x") {
				// parse int64 string to hex string
				var blockNumber int64
				blockNumber, err = strconv.ParseInt(blockNumberStr, 10, 64)
				if err != nil {
					return nil, err
				}
				blockNumberHex = fmt.Sprintf("0x%x", blockNumber)
			}
		} else if bundleType == "blocks" {
			blockNumberHex = blockInfos.(map[string]interface{})["number"].(string)
		} else {
			return nil, fmt.Errorf("invalid bundle type: %s", bundleType)
		}
		k := fmt.Sprintf("%s:%s", CacheBlockPrefix, blockNumberHex)
		cacheDataBytes, err := json.Marshal(blockInfosList)
		if err != nil {
			return nil, err
		}
		cache.Add(k, cacheDataBytes)
		if k == cacheKey {
			matched = cacheDataBytes
		}
	}

	return matched, nil
}
