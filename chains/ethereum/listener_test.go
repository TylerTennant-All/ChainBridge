// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package ethereum

import (
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	msg "github.com/ChainSafe/ChainBridge/message"
)

type MockRouter struct {
	msgs chan msg.Message
}

func (r *MockRouter) Send(message msg.Message) error {
	r.msgs <- message
	return nil
}

func createTestListener(t *testing.T, config *Config, contracts *DeployedContracts) (*Listener, *MockRouter) {
	// Create copy and add deployed contract addresses
	newConfig := *config
	newConfig.contract = contracts.BridgeAddress
	newConfig.erc20HandlerContract = contracts.ERC20HandlerAddress

	conn := newLocalConnection(t, &newConfig)
	bridgeContract, err := createBridgeContract(newConfig.contract, conn)
	if err != nil {
		t.Fatal(err)
	}
	erc20HandlerContract, err := createErc20HandlerContract(newConfig.erc20HandlerContract, conn)
	if err != nil {
		t.Fatal(err)
	}

	router := &MockRouter{msgs: make(chan msg.Message)}
	listener := NewListener(conn, &newConfig, TestLogger)
	listener.SetContracts(bridgeContract, erc20HandlerContract)
	listener.SetRouter(router)
	// Start the listener
	err = listener.Start()
	if err != nil {
		t.Fatal(err)
	}
	err = conn.checkBridgeContract(newConfig.contract)
	if err != nil {
		t.Fatal(err)
	}

	return listener, router
}

func TestListener_start_stop(t *testing.T) {
	contracts := deployTestContracts(t, aliceTestConfig.id)
	l, _ := createTestListener(t, aliceTestConfig, contracts)

	err := l.Start()
	if err != nil {
		t.Fatal(err)
	}

	err = l.Stop()
	if err != nil {
		t.Fatal(err)
	}
}

func TestListener_depositEvent(t *testing.T) {
	contracts := deployTestContracts(t, aliceTestConfig.id)
	l, router := createTestListener(t, aliceTestConfig, contracts)

	// For debugging
	go watchEvent(l.conn, Deposit)

	// Get transaction ready
	opts, nonce, err := l.conn.newTransactOpts(big.NewInt(0), big.NewInt(DefaultGasLimit), big.NewInt(DefaultGasPrice))
	nonce.lock.Unlock() // We manual increment nonce in tests
	if err != nil {
		t.Fatal(err)
	}

	amount := big.NewInt(10)
	destId := big.NewInt(1)

	expectedMessage := msg.Message{
		Source:       msg.ChainId(0),
		Destination:  msg.ChainId(destId.Uint64()),
		Type:         msg.FungibleTransfer,
		DepositNonce: uint32(1),
		Metadata: []interface{}{
			common.HexToAddress(BobKp.Address()),
			amount.Bytes(),
			l.cfg.erc20HandlerContract,
		},
	}

	erc20Contract := deployMintApproveErc20(t, l.conn, opts)

	// Create an ERC20 Deposit
	if err := createErc20Deposit(
		l.bridgeContract,
		opts,
		erc20Contract,
		l.cfg.erc20HandlerContract,

		l.cfg.erc20HandlerContract,
		erc20Contract,
		common.HexToAddress(BobKp.Address()),
		destId,
		amount,
	); err != nil {
		t.Fatal(err)
	}

	// Verify message
	select {
	case m := <-router.msgs:
		if !reflect.DeepEqual(expectedMessage, m) {
			t.Fatalf("Unexpected message.\n\tExpected: %#v\n\tGot: %#v\n", expectedMessage, m)
		}
	case <-time.After(TestTimeout):
		t.Fatalf("test timed out")
	}
}
