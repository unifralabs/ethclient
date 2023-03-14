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

[Sample Usage](./sample/README.md)
