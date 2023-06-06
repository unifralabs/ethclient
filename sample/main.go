package main

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/unifra20/ethclient"
)

func main() {
	_unifraBundleAPI := "https://eth-mainnet.unifra.io/v1/38376e1ad73b4b0c8bf6edc7896ce272"
	ec, _ := ethclient.Dial(_unifraBundleAPI)
	ctx, _ := context.WithTimeout(context.Background(), 1000*time.Second)

	// BlockByNumber
	bn := big.NewInt(1000000)
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
	fmt.Printf("HeaderByNumber Hash: %s - number: %d\n", header.Hash().Hex(), header.Number.Int64())

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
