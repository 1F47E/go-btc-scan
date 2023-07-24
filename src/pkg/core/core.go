package core

import (
	"context"
	"go-btc-scan/src/pkg/client"
	"go-btc-scan/src/pkg/core/pool"
	log "go-btc-scan/src/pkg/logger"

	"go-btc-scan/src/pkg/entity/btc/info"
	mblock "go-btc-scan/src/pkg/entity/models/block"
	mtx "go-btc-scan/src/pkg/entity/models/tx"
	"sync"
	"time"
)

type Core struct {
	ctx    context.Context
	mu     *sync.Mutex
	cli    *client.Client
	pool   *pool.Pool
	blocks []*mblock.Block
}

func NewCore(ctx context.Context, cli *client.Client) *Core {
	return &Core{
		ctx:    ctx,
		mu:     &sync.Mutex{},
		cli:    cli,
		pool:   pool.NewPool(),
		blocks: make([]*mblock.Block, 0),
	}
}

func (c *Core) Start() {
	// set the pool block height
	info, err := c.cli.GetInfo()
	if err != nil {
		log.Log.Errorf("error on getinfo: %v\n", err)
	} else {
		// even if its fails - having block 0 will update pool txs list every time
		// its just for performance reasons
		c.pool.BlockHeight = info.Blocks
	}
	go c.workerPool()

}

func (c *Core) GetNodeInfo() (*info.Info, error) {
	return c.cli.GetInfo()
}

// parse last N blocks
func (c *Core) bootstrap() {
	// TODO: bootstap blocks
	// get best block
	// download header, get prev block, repeat N times
	// parse every block
}

func (c *Core) workerPool() {
	ticker := time.NewTicker(3 * time.Second)
	defer func() {
		ticker.Stop()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// check the block height
			info, err := c.cli.GetInfo()
			if err != nil {
				log.Log.Errorf("error on getinfo: %v\n", err)
				continue
			}
			log.Log.Debugf("block height: %d\n", info.Blocks)

			// get ordered list of pool tsx. new first
			poolTxs, err := c.cli.RawMempool()
			if err != nil {
				log.Log.Errorf("error on rawmempool: %v\n", err)
				continue
			}
			log.Log.Debugf("pool txs: %d\n", len(poolTxs))

			c.parsePoolTxs(poolTxs, info.Blocks)
		}
	}
}

func (c *Core) parsePoolTxs(txs []string, blockHeight int) {
	c.mu.Lock()
	log.Log.Debugf("parsing pool txs: %d\n", len(txs))
	if c.pool.BlockHeight != blockHeight {
		log.Log.Debugf("reset pool block height: %d\n", blockHeight)
		c.pool.Reset(blockHeight)
	}
	sizeOG := c.pool.Size()
	// debug. cut tx list
	// txs = txs[:10]
	// TODO: split into batches

	for _, txid := range txs {
		// parse only new
		if c.pool.HasTx(txid) {
			continue
		}

		rpcTx, err := c.cli.TransactionGet(txid)
		if err != nil {
			log.Log.Errorf("error on get tx %s: %s\n", txid, err)
			continue
		}
		// calc vin via parsing vin txid
		amountIn, err := c.cli.TransactionGetVin(rpcTx)
		if err != nil {
			log.Log.Errorf("error on get vin tx %s: %s\n", txid, err)
			continue
		}

		// construct tx model
		tx := &mtx.Tx{
			Hash:      txid,
			Time:      time.Now(),
			Size:      rpcTx.Size,
			Weight:    rpcTx.Weight,
			AmountIn:  amountIn,
			AmountOut: rpcTx.GetTotalOut(),
		}
		if tx.AmountIn != 0 && tx.AmountOut != 0 {
			tx.Fee = tx.AmountIn - tx.AmountOut
		}

		c.pool.AddTx(tx)
	}
	newSize := c.pool.Size()
	added := newSize - sizeOG
	if added > 0 {
		log.Log.Debugf("parsing done. new tx batch %d added, pool size: %d\n", added, c.pool.Size())
	} else {
		log.Log.Debugf("parsing done. no new txs added, pool size: %d\n", c.pool.Size())
	}

	c.mu.Unlock()
	// TODO: push new pool tx list to web socket
}

// pool access from API

func (c *Core) GetPoolTxs() []*mtx.Tx {
	return c.pool.GetTxs()
}

func (c *Core) GetPoolSize() int {
	return c.pool.Size()
}

func (c *Core) GetPoolTxsRecent(limit int) []*mtx.Tx {
	return c.pool.GetTxsRecent(limit)
}

func (c *Core) GetPoolHeight() int {
	return c.pool.BlockHeight
}
