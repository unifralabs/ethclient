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

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common/hexutil"
	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	CacheBlockPrefix   = "block:"
	CacheReceiptPrefix = "receipt:"
	CacheTracePrefix   = "trace:"
)

// CallContext dispatches the bundle method to the unifra bundle API or the standard method to the underlying RPC client.
func (ec *Client) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	if isBundleMethod(method) && isBundleArgs(args...) {
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

			data, err := fetchBundleDataWithContext(ctx, url)
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

func isBundleArgs(args ...interface{}) bool {
	if len(args) == 0 {
		return false
	}

	// only hex and number are supported
	if args[0] == nil {
		return false
	}

	switch args[0].(type) {
	case string:
		// hex
		if strings.HasPrefix(args[0].(string), "0x") {
			return true
		}
		// number
		if _, err := strconv.Atoi(args[0].(string)); err == nil {
			return true
		}
		return false
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
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
		return CacheReceiptPrefix + args[0].(string)
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
		blockNumber := hexutil.MustDecodeUint64(args[0].(string))
		return fmt.Sprintf("%s/blocks/%d", baseUrl, blockNumber), nil
	case "eth_getBlockReceipts":
		blockNumber := hexutil.MustDecodeUint64(args[0].(string))
		return fmt.Sprintf("%s/receipts/%d", baseUrl, blockNumber), nil
	case "trace_block":
		blockNumber := hexutil.MustDecodeUint64(args[0].(string))
		return fmt.Sprintf("%s/traces/%d", baseUrl, blockNumber), nil
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

func fetchBundleDataWithContext(ctx context.Context, url string) ([]byte, error) {
	// Fetch metadata
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, ethereum.NotFound
		}
		return nil, fmt.Errorf("fetchBundleDataWithContext: status code %d", resp.StatusCode)
	}

	var meta metadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}

	if meta.Error != "" {
		return nil, errors.New(meta.Error)
	}

	// Fetch bundle data and uncompress
	downloadReq, err := http.NewRequestWithContext(ctx, "GET", meta.DownloadLink, nil)
	if err != nil {
		return nil, err
	}
	resp, err = http.DefaultClient.Do(downloadReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetchBundleDataWithContext: status code %d", resp.StatusCode)
	}

	r, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func cacheData(cache *lru.ARCCache[string, []byte], method string, cacheKey string, data []byte) ([]byte, error) {
	var matched []byte

	var blockInfosList []interface{}
	switch method {
	case "trace_block", "eth_getBlockReceipts":
		if err := json.Unmarshal(data, &blockInfosList); err != nil {
			return nil, err
		}
	case "eth_getBlockByNumber":
		if err := json.Unmarshal(data, &blockInfosList); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("invalid bundle type: %s", method)
	}

	for _, blockInfos := range blockInfosList {
		var (
			blockNumberHex string
			err            error
			k              string
		)
		if method == "trace_block" {
			binfos := blockInfos.([]interface{})
			if len(binfos) == 0 {
				continue
			}
			bN, ok := binfos[0].(map[string]interface{})["blockNumber"]
			if !ok {
				return nil, fmt.Errorf("failed to parse block number, blockInfos: %v", blockInfos)
			}
			blockNumberHex = hexutil.EncodeUint64(uint64(bN.(float64)))
			k = fmt.Sprintf("%s%s", CacheTracePrefix, blockNumberHex)
		} else if method == "eth_getBlockReceipts" {
			binfos := blockInfos.([]interface{})
			if len(binfos) == 0 {
				continue
			}
			blockNumberHex, ok := binfos[0].(map[string]interface{})["blockNumber"].(string)
			if !ok {
				return nil, fmt.Errorf("failed to parse block number, blockInfos: %v", blockInfos)
			}
			k = fmt.Sprintf("%s%s", CacheReceiptPrefix, blockNumberHex)
		} else if method == "eth_getBlockByNumber" {
			blockNumberHex = blockInfos.(map[string]interface{})["number"].(string)
			k = fmt.Sprintf("%s%s", CacheBlockPrefix, blockNumberHex)
		} else {
			return nil, fmt.Errorf("invalid bundle method: %s", method)
		}
		cacheDataBytes, err := json.Marshal(blockInfos)
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
