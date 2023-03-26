// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// Verify that Client implements the ethereum interfaces.
var (
	_ = ethereum.ChainReader(&Client{})
	_ = ethereum.TransactionReader(&Client{})
	_ = ethereum.ChainStateReader(&Client{})
	_ = ethereum.ChainSyncReader(&Client{})
	_ = ethereum.ContractCaller(&Client{})
	_ = ethereum.GasEstimator(&Client{})
	_ = ethereum.GasPricer(&Client{})
	_ = ethereum.LogFilterer(&Client{})
	_ = ethereum.PendingStateReader(&Client{})
	// _ = ethereum.PendingStateEventer(&Client{})
	_ = ethereum.PendingContractCaller(&Client{})

	_unifraBundleAPI = "https://eth-mainnet.unifra.io/v1/f7530c7a69314a6da8c430d96f10de64"
)

func TestToFilterArg(t *testing.T) {
	blockHashErr := fmt.Errorf("cannot specify both BlockHash and FromBlock/ToBlock")
	addresses := []common.Address{
		common.HexToAddress("0xD36722ADeC3EdCB29c8e7b5a47f352D701393462"),
	}
	blockHash := common.HexToHash(
		"0xeb94bb7d78b73657a9d7a99792413f50c0a45c51fc62bdcb08a53f18e9a2b4eb",
	)

	for _, testCase := range []struct {
		name   string
		input  ethereum.FilterQuery
		output interface{}
		err    error
	}{
		{
			"without BlockHash",
			ethereum.FilterQuery{
				Addresses: addresses,
				FromBlock: big.NewInt(1),
				ToBlock:   big.NewInt(2),
				Topics:    [][]common.Hash{},
			},
			map[string]interface{}{
				"address":   addresses,
				"fromBlock": "0x1",
				"toBlock":   "0x2",
				"topics":    [][]common.Hash{},
			},
			nil,
		},
		{
			"with nil fromBlock and nil toBlock",
			ethereum.FilterQuery{
				Addresses: addresses,
				Topics:    [][]common.Hash{},
			},
			map[string]interface{}{
				"address":   addresses,
				"fromBlock": "0x0",
				"toBlock":   "latest",
				"topics":    [][]common.Hash{},
			},
			nil,
		},
		{
			"with negative fromBlock and negative toBlock",
			ethereum.FilterQuery{
				Addresses: addresses,
				FromBlock: big.NewInt(-1),
				ToBlock:   big.NewInt(-1),
				Topics:    [][]common.Hash{},
			},
			map[string]interface{}{
				"address":   addresses,
				"fromBlock": "pending",
				"toBlock":   "pending",
				"topics":    [][]common.Hash{},
			},
			nil,
		},
		{
			"with blockhash",
			ethereum.FilterQuery{
				Addresses: addresses,
				BlockHash: &blockHash,
				Topics:    [][]common.Hash{},
			},
			map[string]interface{}{
				"address":   addresses,
				"blockHash": blockHash,
				"topics":    [][]common.Hash{},
			},
			nil,
		},
		{
			"with blockhash and from block",
			ethereum.FilterQuery{
				Addresses: addresses,
				BlockHash: &blockHash,
				FromBlock: big.NewInt(1),
				Topics:    [][]common.Hash{},
			},
			nil,
			blockHashErr,
		},
		{
			"with blockhash and to block",
			ethereum.FilterQuery{
				Addresses: addresses,
				BlockHash: &blockHash,
				ToBlock:   big.NewInt(1),
				Topics:    [][]common.Hash{},
			},
			nil,
			blockHashErr,
		},
		{
			"with blockhash and both from / to block",
			ethereum.FilterQuery{
				Addresses: addresses,
				BlockHash: &blockHash,
				FromBlock: big.NewInt(1),
				ToBlock:   big.NewInt(2),
				Topics:    [][]common.Hash{},
			},
			nil,
			blockHashErr,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := toFilterArg(testCase.input)
			if (testCase.err == nil) != (err == nil) {
				t.Fatalf("expected error %v but got %v", testCase.err, err)
			}
			if testCase.err != nil {
				if testCase.err.Error() != err.Error() {
					t.Fatalf("expected error %v but got %v", testCase.err, err)
				}
			} else if !reflect.DeepEqual(testCase.output, output) {
				t.Fatalf("expected filter arg %v but got %v", testCase.output, output)
			}
		})
	}
}

var (
	testKey, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddr    = crypto.PubkeyToAddress(testKey.PublicKey)
	testBalance = big.NewInt(2e15)
)

var genesis = &core.Genesis{
	Config:    params.AllEthashProtocolChanges,
	Alloc:     core.GenesisAlloc{testAddr: {Balance: testBalance}},
	ExtraData: []byte("test genesis"),
	Timestamp: 9000,
	BaseFee:   big.NewInt(params.InitialBaseFee),
}

var testTx1 = types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
	Nonce:    0,
	Value:    big.NewInt(12),
	GasPrice: big.NewInt(params.InitialBaseFee),
	Gas:      params.TxGas,
	To:       &common.Address{2},
})

var testTx2 = types.MustSignNewTx(testKey, types.LatestSigner(genesis.Config), &types.LegacyTx{
	Nonce:    1,
	Value:    big.NewInt(8),
	GasPrice: big.NewInt(params.InitialBaseFee),
	Gas:      params.TxGas,
	To:       &common.Address{2},
})

func newTestBackend(t *testing.T) (*node.Node, []*types.Block) {
	// Generate test chain.
	blocks := generateTestChain()

	// Create node
	n, err := node.New(&node.Config{})
	if err != nil {
		t.Fatalf("can't create new node: %v", err)
	}
	// Create Ethereum Service
	config := &ethconfig.Config{Genesis: genesis}
	config.Ethash.PowMode = ethash.ModeFake
	ethservice, err := eth.New(n, config)
	if err != nil {
		t.Fatalf("can't create new ethereum service: %v", err)
	}
	// Import the test chain.
	if err := n.Start(); err != nil {
		t.Fatalf("can't start test node: %v", err)
	}
	if _, err := ethservice.BlockChain().InsertChain(blocks[1:]); err != nil {
		t.Fatalf("can't import test blocks: %v", err)
	}
	return n, blocks
}

func generateTestChain() []*types.Block {
	generate := func(i int, g *core.BlockGen) {
		g.OffsetTime(5)
		g.SetExtra([]byte("test"))
		if i == 1 {
			// Test transactions are included in block #2.
			g.AddTx(testTx1)
			g.AddTx(testTx2)
		}
	}
	_, blocks, _ := core.GenerateChainWithGenesis(genesis, ethash.NewFaker(), 2, generate)
	return append([]*types.Block{genesis.ToBlock()}, blocks...)
}

func TestEthClient(t *testing.T) {
	backend, chain := newTestBackend(t)
	client, _ := backend.Attach()
	defer backend.Close()
	defer client.Close()

	tests := map[string]struct {
		test func(t *testing.T)
	}{
		"Header": {
			func(t *testing.T) { testHeader(t, chain, client) },
		},
		"BalanceAt": {
			func(t *testing.T) { testBalanceAt(t, client) },
		},
		"TxInBlockInterrupted": {
			func(t *testing.T) { testTransactionInBlockInterrupted(t, client) },
		},
		"ChainID": {
			func(t *testing.T) { testChainID(t, client) },
		},
		"GetBlock": {
			func(t *testing.T) { testGetBlock(t, client) },
		},
		"StatusFunctions": {
			func(t *testing.T) { testStatusFunctions(t, client) },
		},
		"CallContract": {
			func(t *testing.T) { testCallContract(t, client) },
		},
		"CallContractAtHash": {
			func(t *testing.T) { testCallContractAtHash(t, client) },
		},
		"AtFunctions": {
			func(t *testing.T) { testAtFunctions(t, client) },
		},
		"TransactionSender": {
			func(t *testing.T) { testTransactionSender(t, client) },
		},
		"BlockTraceByNumber": {
			func(t *testing.T) { testBlockTraceByNumber(t) },
		},
		"BlockReceiptsByNumber": {
			func(t *testing.T) { testBlockReceiptsByNumber(t) },
		},
	}

	t.Parallel()
	for name, tt := range tests {
		t.Run(name, tt.test)
	}
}

func testHeader(t *testing.T, chain []*types.Block, client *rpc.Client) {
	tests := map[string]struct {
		block   *big.Int
		want    *types.Header
		wantErr error
	}{
		// TODO: fix me
		// "genesis": {
		// 	block: big.NewInt(0),
		// 	want:  chain[0].Header(),
		// },
		"first_block": {
			block: big.NewInt(1),
			want:  chain[1].Header(),
		},
		"future_block": {
			block:   big.NewInt(1000000000),
			want:    nil,
			wantErr: ethereum.NotFound,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ec := NewClient(client, _unifraBundleAPI)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			got, err := ec.HeaderByNumber(ctx, tt.block)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("HeaderByNumber(%v) error = %q, want %q", tt.block, err, tt.wantErr)
			}
			if got != nil && got.Number != nil && got.Number.Sign() == 0 {
				got.Number = big.NewInt(0) // hack to make DeepEqual work
			}
			// TODO: fix me
			// if !reflect.DeepEqual(got, tt.want) {
			// 	t.Fatalf("HeaderByNumber(%v)\n   = %v\nwant %v", tt.block, got, tt.want)
			// }
		})
	}
}

func testBalanceAt(t *testing.T, client *rpc.Client) {
	tests := map[string]struct {
		account common.Address
		block   *big.Int
		want    *big.Int
		wantErr error
	}{
		"valid_account_genesis": {
			account: testAddr,
			block:   big.NewInt(0),
			want:    testBalance,
		},
		"valid_account": {
			account: testAddr,
			block:   big.NewInt(1),
			want:    testBalance,
		},
		"non_existent_account": {
			account: common.Address{1},
			block:   big.NewInt(1),
			want:    big.NewInt(0),
		},
		"future_block": {
			account: testAddr,
			block:   big.NewInt(1000000000),
			want:    big.NewInt(0),
			wantErr: errors.New("header not found"),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ec := NewClient(client, _unifraBundleAPI)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			got, err := ec.BalanceAt(ctx, tt.account, tt.block)
			if tt.wantErr != nil && (err == nil || err.Error() != tt.wantErr.Error()) {
				t.Fatalf("BalanceAt(%x, %v) error = %q, want %q", tt.account, tt.block, err, tt.wantErr)
			}
			if got.Cmp(tt.want) != 0 {
				t.Fatalf("BalanceAt(%x, %v) = %v, want %v", tt.account, tt.block, got, tt.want)
			}
		})
	}
}

func testTransactionInBlockInterrupted(t *testing.T, client *rpc.Client) {
	ec := NewClient(client, _unifraBundleAPI)

	// Get current block by number.
	block, err := ec.BlockByNumber(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test tx in block interrupted.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx, err := ec.TransactionInBlock(ctx, block.Hash(), 0)
	if tx != nil {
		t.Fatal("transaction should be nil")
	}
	if err == nil || err == ethereum.NotFound {
		t.Fatal("error should not be nil/notfound")
	}

	// Test tx in block not found.
	if _, err := ec.TransactionInBlock(context.Background(), block.Hash(), 20); err != ethereum.NotFound {
		t.Fatal("error should be ethereum.NotFound")
	}
}

func testChainID(t *testing.T, client *rpc.Client) {
	ec := NewClient(client, _unifraBundleAPI)
	id, err := ec.ChainID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil || id.Cmp(params.AllEthashProtocolChanges.ChainID) != 0 {
		t.Fatalf("ChainID returned wrong number: %+v", id)
	}
}

func testGetBlock(t *testing.T, client *rpc.Client) {
	ec := NewClient(client, _unifraBundleAPI)

	// Get current block number
	blockNumber, err := ec.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blockNumber != 2 {
		t.Fatalf("BlockNumber returned wrong number: %d", blockNumber)
	}
	// Get current block by number
	block, err := ec.BlockByNumber(context.Background(), new(big.Int).SetUint64(blockNumber))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.NumberU64() != blockNumber {
		t.Fatalf("BlockByNumber returned wrong block: want %d got %d", blockNumber, block.NumberU64())
	}
	// Get current block by hash
	hash := common.HexToHash("0x4bab762ae8457561f28e3359a98e323c4f6ae5dce54442cd24eefd8886d2d838")
	blockH, err := ec.BlockByHash(context.Background(), hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != blockH.Hash() {
		t.Fatalf("BlockByHash returned wrong block: want %v got %v", hash.Hex(), blockH.Hash().Hex())
	}
	// Get header by number
	header, err := ec.HeaderByNumber(context.Background(), new(big.Int).SetUint64(blockNumber))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Header().Hash() != header.Hash() {
		t.Fatalf("HeaderByNumber returned wrong header: want %v got %v", block.Header().Hash().Hex(), header.Hash().Hex())
	}
	// Get header by hash
	headerH, err := ec.HeaderByHash(context.Background(), hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != headerH.Hash() {
		t.Fatalf("HeaderByHash returned wrong header: want %v got %v", hash, headerH.Hash().Hex())
	}
}

func testStatusFunctions(t *testing.T, client *rpc.Client) {
	ec := NewClient(client, _unifraBundleAPI)

	// Sync progress
	progress, err := ec.SyncProgress(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if progress != nil {
		t.Fatalf("unexpected progress: %v", progress)
	}

	// NetworkID
	networkID, err := ec.NetworkID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if networkID.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("unexpected networkID: %v", networkID)
	}

	// SuggestGasPrice
	gasPrice, err := ec.SuggestGasPrice(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gasPrice.Cmp(big.NewInt(1000000000)) != 0 {
		t.Fatalf("unexpected gas price: %v", gasPrice)
	}

	// SuggestGasTipCap
	gasTipCap, err := ec.SuggestGasTipCap(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gasTipCap.Cmp(big.NewInt(234375000)) != 0 {
		t.Fatalf("unexpected gas tip cap: %v", gasTipCap)
	}

	// FeeHistory
	history, err := ec.FeeHistory(context.Background(), 1, big.NewInt(2), []float64{95, 99})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &ethereum.FeeHistory{
		OldestBlock: big.NewInt(2),
		Reward: [][]*big.Int{
			{
				big.NewInt(234375000),
				big.NewInt(234375000),
			},
		},
		BaseFee: []*big.Int{
			big.NewInt(765625000),
			big.NewInt(671627818),
		},
		GasUsedRatio: []float64{0.008912678667376286},
	}
	if !reflect.DeepEqual(history, want) {
		t.Fatalf("FeeHistory result doesn't match expected: (got: %v, want: %v)", history, want)
	}
}

func testCallContractAtHash(t *testing.T, client *rpc.Client) {
	ec := NewClient(client, _unifraBundleAPI)

	// EstimateGas
	msg := ethereum.CallMsg{
		From:  testAddr,
		To:    &common.Address{},
		Gas:   21000,
		Value: big.NewInt(1),
	}
	gas, err := ec.EstimateGas(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 21000 {
		t.Fatalf("unexpected gas price: %v", gas)
	}
	_, err = ec.HeaderByNumber(context.Background(), big.NewInt(1))
	if err != nil {
		t.Fatalf("BlockByNumber error: %v", err)
	}
	// CallContract
	// TODO: fix me
	// if _, err := ec.CallContractAtHash(context.Background(), msg, block.Hash()); err != nil {
	// 	t.Fatalf("unexpected error: %v", err)
	// }
}

func testCallContract(t *testing.T, client *rpc.Client) {
	ec := NewClient(client, _unifraBundleAPI)

	// EstimateGas
	msg := ethereum.CallMsg{
		From:  testAddr,
		To:    &common.Address{},
		Gas:   21000,
		Value: big.NewInt(1),
	}
	gas, err := ec.EstimateGas(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 21000 {
		t.Fatalf("unexpected gas price: %v", gas)
	}
	// CallContract
	if _, err := ec.CallContract(context.Background(), msg, big.NewInt(1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// PendingCallContract
	if _, err := ec.PendingCallContract(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testAtFunctions(t *testing.T, client *rpc.Client) {
	ec := NewClient(client, _unifraBundleAPI)

	// send a transaction for some interesting pending status
	sendTransaction(ec)
	time.Sleep(100 * time.Millisecond)

	// Check pending transaction count
	pending, err := ec.PendingTransactionCount(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pending != 1 {
		t.Fatalf("unexpected pending, wanted 1 got: %v", pending)
	}
	// Query balance
	balance, err := ec.BalanceAt(context.Background(), testAddr, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	penBalance, err := ec.PendingBalanceAt(context.Background(), testAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if balance.Cmp(penBalance) == 0 {
		t.Fatalf("unexpected balance: %v %v", balance, penBalance)
	}
	// NonceAt
	nonce, err := ec.NonceAt(context.Background(), testAddr, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	penNonce, err := ec.PendingNonceAt(context.Background(), testAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if penNonce != nonce+1 {
		t.Fatalf("unexpected nonce: %v %v", nonce, penNonce)
	}
	// StorageAt
	storage, err := ec.StorageAt(context.Background(), testAddr, common.Hash{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	penStorage, err := ec.PendingStorageAt(context.Background(), testAddr, common.Hash{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(storage, penStorage) {
		t.Fatalf("unexpected storage: %v %v", storage, penStorage)
	}
	// CodeAt
	code, err := ec.CodeAt(context.Background(), testAddr, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	penCode, err := ec.PendingCodeAt(context.Background(), testAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(code, penCode) {
		t.Fatalf("unexpected code: %v %v", code, penCode)
	}
}

func testTransactionSender(t *testing.T, client *rpc.Client) {
	ec, _ := Dial(_unifraBundleAPI)
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)

	// Retrieve testTx1 via RPC.
	block2, err := ec.HeaderByNumber(ctx, big.NewInt(1000000))
	if err != nil {
		t.Fatal("can't get block 1:", err)
	}
	tx1, err := ec.TransactionInBlock(ctx, block2.Hash(), 0)
	if err != nil {
		t.Fatal("can't get tx:", err)
	}
	// TODO: fix me
	// if tx1.Hash() != testTx1.Hash() {
	// 	t.Fatalf("wrong tx hash %v, want %v", tx1.Hash(), testTx1.Hash())
	// }

	// The sender address is cached in tx1, so no additional RPC should be required in
	// TransactionSender. Ensure the server is not asked by canceling the context here.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ec.TransactionSender(canceledCtx, tx1, block2.Hash(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// TODO: fix me
	// if sender1 != testAddr {
	// 	t.Fatal("wrong sender:", sender1)
	// }

	// Now try to get the sender of testTx2, which was not fetched through RPC.
	// TransactionSender should query the server here.
	// TODO: fix me
	// sender2, err := ec.TransactionSender(ctx, testTx2, block2.Hash(), 1)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// if sender2 != testAddr {
	// 	t.Fatal("wrong sender:", sender2)
	// }
}

func sendTransaction(ec *Client) error {
	chainID, err := ec.ChainID(context.Background())
	if err != nil {
		return err
	}
	nonce, err := ec.PendingNonceAt(context.Background(), testAddr)
	if err != nil {
		return err
	}

	signer := types.LatestSignerForChainID(chainID)
	tx, err := types.SignNewTx(testKey, signer, &types.LegacyTx{
		Nonce:    nonce,
		To:       &common.Address{2},
		Value:    big.NewInt(1),
		Gas:      22000,
		GasPrice: big.NewInt(params.InitialBaseFee),
	})
	if err != nil {
		return err
	}
	return ec.SendTransaction(context.Background(), tx)
}

func testBlockTraceByNumber(t *testing.T) {
	ec, _ := Dial(_unifraBundleAPI)
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)

	traces, err := ec.BlockTraceByNumber(ctx, big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	if traces == nil {
		t.Fatal("block trace is nil")
	}
	if len(traces) == 0 {
		t.Fatal("block trace is empty")
	}
}

func testBlockReceiptsByNumber(t *testing.T) {
	ec, _ := Dial(_unifraBundleAPI)
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)

	receipts, err := ec.BlockReceiptsByNumber(ctx, big.NewInt(1000000))
	if err != nil {
		t.Fatal(err)
	}
	if receipts == nil {
		t.Fatal("block receipts is nil")
	}
	if len(receipts) == 0 {
		t.Fatal("block receipts is empty")
	}
}

func TestAuthrization(t *testing.T) {
	fu := func(blockNum int64) {

		ec, _ := Dial("https://staging-eth-mainnet.unifra.io/v1/c045aaea2e944216bc34a6516f62bff4")

		// this is a staging eth-mainnet app key
		ctx := context.Background()
		defer ec.Close()
		block, err := ec.BlockByNumber(ctx, big.NewInt(blockNum))
		if err != nil {
			t.Fatalf("correct apikey BlockByNumber(%d) should success.%v", blockNum, err)
		}
		if block == nil || block.Number().Int64() != blockNum {
			log.Fatalf("block nil or blockNum not correct")
		}
		_, err = ec.HeaderByNumber(ctx, big.NewInt(blockNum))
		if err != nil {
			t.Fatalf("wrong apikey HeaderByNumber should fail")
		}

		receipts, err := ec.BlockReceiptsByNumber(ctx, big.NewInt(blockNum))
		if err != nil {
			if err.Error() != "not found" {
				t.Fatalf("correct apikey BlockReceiptsByNumber should success:%v", err)
			}
		} else {
			if receipts == nil {
				t.Fatal("block receipts is nil")
			}
		}

		_, err = ec.BlockTraceByNumber(ctx, big.NewInt(blockNum))
		if err != nil {
			if err.Error() != "not found" {
				t.Fatalf("wrong apikey BlockTraceByNumber should fail")
			}
		}

		//wrong apikey
		ec2, _ := Dial("https://eth-mainnet.unifra.io/v1/25b2aec5c8a049b98ccb2a2468bc0b8f")
		receipts, err = ec2.BlockReceiptsByNumber(ctx, big.NewInt(blockNum))
		if err == nil {
			t.Fatalf("wrong apikey should fail")
		}

		_, err = ec2.BlockByNumber(ctx, big.NewInt(blockNum))
		if err == nil {
			t.Fatalf("wrong apikey BlockByNumber should fail")
		}

		_, err = ec2.BlockTraceByNumber(ctx, big.NewInt(blockNum))
		if err == nil {
			t.Fatalf("wrong apikey BlockTraceByNumber should fail")
		}

		_, err = ec2.HeaderByNumber(ctx, big.NewInt(blockNum))
		if err == nil {
			t.Fatalf("wrong apikey HeaderByNumber should fail")
		}

		ec3, _ := Dial("https://eth-mainnet.unifra.io/v1/")
		receipts, err = ec3.BlockReceiptsByNumber(ctx, big.NewInt(blockNum))
		if err == nil {
			t.Fatalf("empty apikey BlockReceiptsByNumber should fail")
		}

		_, err = ec3.BlockByNumber(ctx, big.NewInt(blockNum))
		if err == nil {
			t.Fatalf("empty apikey BlockByNumber should fail")
		}

		_, err = ec3.BlockTraceByNumber(ctx, big.NewInt(blockNum))
		if err == nil {
			t.Fatalf("empty apikey BlockTraceByNumber should fail")
		}

		_, err = ec3.HeaderByNumber(ctx, big.NewInt(blockNum))
		if err == nil {
			t.Fatalf("empty apikey HeaderByNumber should fail")
		}

	}
	fu(1)
	fu(10)
	fu(100)
	fu(1000)
	fu(10000)
	fu(100000)
	fu(100_0000)
	fu(1000_0000)

	//test latest
	ec, _ := Dial("https://staging-eth-mainnet.unifra.io/v1/c045aaea2e944216bc34a6516f62bff4")
	defer ec.Close()

	ctx := context.Background()
	latest, err := ec.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("get latest number fail")
	}
	_, err = ec.BlockByNumber(context.Background(), big.NewInt(int64(latest)))
	if err == nil {
		t.Fatalf("free account should not able to download file.")
	}

	_, err = ec.BlockTraceByNumber(ctx, big.NewInt(int64(latest)))
	if err == nil {
		t.Fatalf("empty apikey BlockTraceByNumber should fail")
	}

	_, err = ec.HeaderByNumber(ctx, big.NewInt(int64(latest)))
	if err == nil {
		t.Fatalf("empty apikey HeaderByNumber should fail")
	}
}
