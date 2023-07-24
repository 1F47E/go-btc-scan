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
	ctx         context.Context
	mu          *sync.Mutex
	cli         *client.Client
	pool        *pool.Pool
	blocks      []*mblock.Block
	poolTxCh    chan *mtx.Tx
	poolTxResCh chan *mtx.Tx
}

func NewCore(ctx context.Context, cli *client.Client) *Core {
	return &Core{
		ctx:         ctx,
		mu:          &sync.Mutex{},
		cli:         cli,
		pool:        pool.NewPool(),
		blocks:      make([]*mblock.Block, 0),
		poolTxCh:    make(chan *mtx.Tx),
		poolTxResCh: make(chan *mtx.Tx),
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
	// TODO: make a batch of parsers
	go c.workerTxParser()

	go c.workerTxAdder()
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

func (c *Core) workerTxParser() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case tx := <-c.poolTxCh:
			// do tx parsing
			txUpdated, err := c.parsePoolTx(tx)
			if err != nil {
				log.Log.Errorf("error on parsePoolTx: %v\n", err)
				continue
			}
			c.poolTxResCh <- txUpdated
		}
	}
}

func (c *Core) workerTxAdder() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case tx := <-c.poolTxResCh:
			c.mu.Lock()
			c.pool.AddTx(tx)
			c.mu.Unlock()
		}
	}
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

			// reset pool if new block
			if c.pool.BlockHeight != info.Blocks {
				log.Log.Debugf("reset pool block height: %d\n", info.Blocks)
				c.pool.Reset(info.Blocks)
			}

			// ramap btc tx to model tx and send them to additional parsing
			for _, tx := range poolTxs {
				// skip if already in pool
				if c.pool.HasTx(tx.Hash) {
					continue
				}

				// remap to tx model
				mt := &mtx.Tx{
					Hash:   tx.Hash,
					Time:   time.Unix(tx.Time, 0),
					Size:   tx.Size,
					Vsize:  tx.Vsize,
					Weight: tx.Weight,
					Fee:    tx.Fee,
					FeeKb:  tx.FeePerKB,
				}
				c.poolTxCh <- mt
			}

			// c.parsePoolTxs(poolTxs, info.Blocks)
		}
	}
}

func (c *Core) parsePoolTx(tx *mtx.Tx) (*mtx.Tx, error) {
	btx, err := c.cli.TransactionGet(tx.Hash)
	if err != nil {
		return nil, err
	}
	// update amounts
	tx.AmountOut = btx.GetTotalOut()

	return tx, nil
}

// func (c *Core) parsePoolTxs(txs []txpool.TxPool, blockHeight int) {
// 	c.mu.Lock()
// 	log.Log.Debugf("parsing pool txs: %d\n", len(txs))
// 	if c.pool.BlockHeight != blockHeight {
// 		log.Log.Debugf("reset pool block height: %d\n", blockHeight)
// 		c.pool.Reset(blockHeight)
// 	}
// 	sizeOG := c.pool.Size()
// 	// debug. cut tx list
// 	// txs = txs[:10]
// 	// TODO: split into batches
//
// 	for _, tx := range txs {
// 		// parse only new
// 		if c.pool.HasTx(tx.Hash) {
// 			continue
// 		}
//
// 		// remap to tx model
// 		tx := &mtx.Tx{
// 			Hash:   tx.Hash,
// 			Time:   time.Unix(tx.Time, 0),
// 			Size:   tx.Size,
// 			Vsize:  tx.Vsize,
// 			Weight: tx.Weight,
// 			Fee:    tx.Fee,
// 			FeeKb:  tx.FeePerKB,
// 		}
//
// 		c.pool.AddTx(tx)
// 	}
// 	newSize := c.pool.Size()
// 	added := newSize - sizeOG
// 	if added > 0 {
// 		log.Log.Debugf("parsing done. new tx batch %d added, pool size: %d\n", added, c.pool.Size())
// 	} else {
// 		log.Log.Debugf("parsing done. no new txs added, pool size: %d\n", c.pool.Size())
// 	}
//
// 	c.mu.Unlock()
// 	// TODO: push new pool tx list to web socket
// }

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

// TODO: cache this
func (c *Core) GetTotalAmount() uint64 {
	var total uint64
	for _, tx := range c.pool.GetTxs() {
		total += tx.AmountOut
	}
	return total
}
