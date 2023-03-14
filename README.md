# Bundle SDK Ethclient

This is a modified version of the [go-ethereum/ethclient](https://github.com/ethereum/go-ethereum/tree/master/ethclient) package. It is modified to work with the `unifra bundle api`

## Installation

```bash
go get github.com/unifra20/ethclient
```

## Features

Modified functions:

- HeaderByNumber - Fetches a header by number from `unifra bundle api` and caches headers range in client side
- BlockByNumber - Fetches a block by number from `unifra bundle api` and caches blocks range in client side

Added functions:

- BlockReceiptsByNumber - Fetches a block receipts by number from `unifra bundle api` and caches receipts range in client side
- BlockTraceByNumber - Fetches a block trace by number from `unifra bundle api` and caches traces range in client side
- TransactionTraceByHash - Fetches a transaction trace by hash from `unifra node`

## Usage

[Sample](./sample/README.md)

```go
package main

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/unifra20/ethclient"
)

func main() {
	_unifraBundleAPI := "https://eth-mainnet.unifra.io/v1/f7530c7a69314a6da8c430d96f10de64"
	ec, _ := ethclient.Dial(_unifraBundleAPI)
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)

	// BlockByNumber
	bn := big.NewInt(10000000)
	block, err := ec.BlockByNumber(ctx, bn)
	if err != nil {
		panic(err)
	}
	fmt.Printf("BlockByNumber Hash: %s\n", block.Hash().Hex())

	// HeaderByNumber
	header, err := ec.HeaderByNumber(ctx, bn)
	if err != nil {
		panic(err)
	}
	fmt.Printf("HeaderByNumber Hash: %s\n", header.Hash().Hex())

	// BlockReceipts
	receipts, err := ec.BlockReceiptsByNumber(ctx, bn)
	if err != nil {
		panic(err)
	}
	fmt.Printf("BlockReceiptsByNumber Frist Hash: %s\n", receipts[0].BlockHash.Hex())

	// BlockTraceByNumber
	btrace, err := ec.BlockTraceByNumber(ctx, bn)
	if err != nil {
		panic(err)
	}
	fmt.Printf("BlockTraceByNumber Frist Hash: %s\n", btrace[0].BlockHash.Hex())

	// TransactionTraceByHash
	txtrace, err := ec.TransactionTraceByHash(ctx, *btrace[0].TransactionHash)
	if err != nil {
		panic(err)
	}
	fmt.Printf("TransactionTraceByHash Frist Hash: %s\n", txtrace[0].BlockHash.Hex())
}
```
