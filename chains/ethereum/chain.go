// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package ethereum

import (
	bridge "github.com/ChainSafe/ChainBridge/bindings/Bridge"
	erc20Handler "github.com/ChainSafe/ChainBridge/bindings/ERC20Handler"
	"github.com/ChainSafe/ChainBridge/blockstore"
	"github.com/ChainSafe/ChainBridge/chains"
	"github.com/ChainSafe/ChainBridge/core"
	"github.com/ChainSafe/ChainBridge/crypto/secp256k1"
	"github.com/ChainSafe/ChainBridge/keystore"
	msg "github.com/ChainSafe/ChainBridge/message"
	"github.com/ChainSafe/ChainBridge/router"
	"github.com/ChainSafe/log15"
	"github.com/ethereum/go-ethereum/common"
)

type Chain struct {
	cfg      *core.ChainConfig // The config of the chain
	conn     *Connection       // THe chains connection
	listener *Listener         // The listener of this chain
	writer   *Writer           // The writer of the chain
}

func createBridgeContract(addr common.Address, conn *Connection) (*bridge.Bridge, error) {
	return bridge.NewBridge(addr, conn.conn)
}

func createErc20HandlerContract(addr common.Address, conn *Connection) (*erc20Handler.ERC20Handler, error) {
	return erc20Handler.NewERC20Handler(addr, conn.conn)
}

// checkBlockstore queries the blockstore for the latest known block. If the latest block is
// greater than cfg.startBlock, then cfg.startBlock is replaced with the latest known block.
func setupBlockstore(cfg *Config, kp *secp256k1.Keypair) (*blockstore.Blockstore, error) {
	bs, err := blockstore.NewBlockstore(cfg.blockstorePath, cfg.id, kp.Address())
	if err != nil {
		return nil, err
	}

	if !cfg.freshStart {
		latestBlock, err := bs.TryLoadLatestBlock()
		if err != nil {
			return nil, err
		}

		if latestBlock.Cmp(cfg.startBlock) == 1 {
			cfg.startBlock = latestBlock
		}
	}

	return bs, nil
}

func InitializeChain(chainCfg *core.ChainConfig, logger log15.Logger) (*Chain, error) {
	cfg, err := parseChainConfig(chainCfg)
	if err != nil {
		return nil, err
	}

	kpI, err := keystore.KeypairFromAddress(cfg.from, keystore.EthChain, cfg.keystorePath, chainCfg.Insecure)
	if err != nil {
		return nil, err
	}
	kp, _ := kpI.(*secp256k1.Keypair)

	bs, err := setupBlockstore(cfg, kp)
	if err != nil {
		return nil, err
	}

	conn := NewConnection(cfg, kp, logger)
	err = conn.Connect()
	if err != nil {
		return nil, err
	}

	err = conn.ensureHasBytecode(cfg.bridgeContract)
	if err != nil {
		return nil, err
	}
	err = conn.ensureHasBytecode(cfg.erc20HandlerContract)
	if err != nil {
		return nil, err
	}
	err = conn.ensureHasBytecode(cfg.genericHandlerContract)
	if err != nil {
		return nil, err
	}

	bridgeContract, err := createBridgeContract(cfg.bridgeContract, conn)
	if err != nil {
		return nil, err
	}

	erc20HandlerContract, err := createErc20HandlerContract(cfg.erc20HandlerContract, conn)
	if err != nil {
		return nil, err
	}

	listener := NewListener(conn, cfg, logger, bs)
	listener.SetContracts(bridgeContract, erc20HandlerContract)

	writer := NewWriter(conn, cfg, logger)
	writer.SetContract(bridgeContract)

	return &Chain{
		cfg:      chainCfg,
		conn:     conn,
		writer:   writer,
		listener: listener,
	}, nil
}

func (c *Chain) SetRouter(r *router.Router) {
	r.Listen(c.cfg.Id, c.writer)
	c.listener.SetRouter(r)
}

func (c *Chain) Start() error {
	err := c.listener.Start()
	if err != nil {
		return err
	}

	err = c.writer.Start()
	if err != nil {
		return err
	}

	c.writer.log.Debug("Successfully started chain")
	return nil
}

func (c *Chain) Id() msg.ChainId {
	return c.cfg.Id
}

func (c *Chain) Name() string {
	return c.cfg.Name
}

func (c *Chain) GetWriter() chains.Writer {
	return c.writer
}

func (c *Chain) Stop() error {
	err := c.listener.Stop()
	if err != nil {
		return err
	}

	err = c.writer.Stop()
	if err != nil {
		return err
	}

	c.conn.Close()

	return nil
}
