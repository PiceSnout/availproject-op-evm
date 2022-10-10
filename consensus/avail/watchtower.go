package avail

import (
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	avail_types "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/maticnetwork/avail-settlement/consensus/avail/watchtower"
	"github.com/maticnetwork/avail-settlement/pkg/avail"
	"github.com/maticnetwork/avail-settlement/pkg/block"
)

func (d *Avail) runWatchTower(watchTowerAccount accounts.Account, watchTowerPK *keystore.Key) {
	availBlockStream := avail.NewBlockStream(d.availClient, d.logger, avail.BridgeAppID, 1)
	availSender := avail.NewSender(d.availClient, signature.TestKeyringPairAlice)
	logger := d.logger.Named("watchtower")
	watchTower := watchtower.New(d.blockchain, d.executor, types.Address(watchTowerAccount.Address), watchTowerPK.PrivateKey)

	callIdx, err := avail.FindCallIndex(d.availClient)
	if err != nil {
		panic(err)
	}

	logger.Info("watchtower started")

	// TODO: Figure out where do we need state cycle and how to implement it.
	// Current version only starts the cycles for the future, doing nothing with it.
	for {
		var availBlk *avail_types.SignedBlock

		select {
		case <-d.closeCh:
			availBlockStream.Close()
			return
		case availBlk = <-availBlockStream.Chan():
		}

		blk, err := block.FromAvail(availBlk, avail.BridgeAppID, callIdx)
		if err != nil {
			logger.Error("cannot extract Edge block from Avail block", "blocknumber", availBlk.Block.Header.Number, "error", err)
			continue
		}

		err = watchTower.Check(blk)
		if err != nil {
			logger.Debug("block verification failed. constructing fraudproof", "blocknumber", blk.Header.Number, "blockhash", blk.Header.Hash, "error", err)

			fp, err := watchTower.ConstructFraudproof(blk)
			if err != nil {
				logger.Error("failed to construct fraudproof for block", "blocknumber", blk.Header.Number, "blockhash", blk.Header.Hash, "error", err)
				continue
			}

			logger.Debug("submitting fraudproof", "blockhash", fp.Header.Hash)
			f := availSender.SubmitDataAndWaitForStatus(fp.MarshalRLP(), avail_types.ExtrinsicStatus{IsInBlock: true})
			go func() {
				if _, err := f.Result(); err != nil {
					logger.Error("submitting fraud proof to avail failed", err)
				}
				logger.Debug("submitted fraudproof", "blockhash", fp.Header.Hash)
			}()

			// TODO: Write fraudproof to local chain

			continue
		}

		err = watchTower.Apply(blk)
		if err != nil {
			logger.Error("cannot apply block to blockchain", "blocknumber", blk.Header.Number, "blocknumber", blk.Header.Hash, "error", err)
		}
	}
}
