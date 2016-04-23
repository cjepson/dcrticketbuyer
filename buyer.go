// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

const (
	// windowsToConsider is the number of windows to consider
	// when there is not enough block information to determine
	// what the best fee should be.
	windowsToConsider = 10

	// stakeInfoReqTries is the maximum number of times to try
	// GetStakeInfo before failing.
	stakeInfoReqTries = 10

	// stakeInfoReqTryDelay is the time in seconds to wait before
	// doing another GetStakeInfo request.
	stakeInfoReqTryDelay = 1
)

// walletSvrManager
type walletSvrManager struct {
	purchaser          *ticketPurchaser
	blockConnectedChan chan int32
	quit               chan struct{}
}

// newwalletSvrManager
func newWalletSvrManager(purchaser *ticketPurchaser,
	blockConnChan chan int32,
	quit chan struct{}) *walletSvrManager {
	return &walletSvrManager{
		purchaser:          purchaser,
		blockConnectedChan: blockConnChan,
		quit:               quit,
	}
}

// blockConnectedHandler handles block connected notifications, which trigger
// ticket purchases.
func (w *walletSvrManager) blockConnectedHandler() {
out:
	for {
		select {
		case height := <-w.blockConnectedChan:
			daemonLog.Infof("Block height %v connected", height)
			err := w.purchaser.purchase(height)
			if err != nil {
				log.Errorf("Failed to purchase tickets this round: %s",
					err.Error())
			}
		// TODO Poll every couple minute to check if connected;
		// if not, try to reconnect.
		case <-w.quit:
			break out
		}
	}
}

// ticketPurchaser
type ticketPurchaser struct {
	cfg                 *config
	dcrdChainSvr        *dcrrpcclient.Client
	dcrwChainSvr        *dcrrpcclient.Client
	ticketAddress       dcrutil.Address
	poolAddress         dcrutil.Address
	firstStart          bool
	idxDiffPeriod       int
	toBuyDiffPeriod     int
	purchasedDiffPeriod int
}

// newTicketPurchaser
func newTicketPurchaser(cfg *config,
	dcrdChainSvr *dcrrpcclient.Client,
	dcrwChainSvr *dcrrpcclient.Client) (*ticketPurchaser, error) {
	var ticketAddress dcrutil.Address
	var err error
	if cfg.TicketAddress != "" {
		ticketAddress, err = dcrutil.DecodeAddress(cfg.TicketAddress, activeNet)
		if err != nil {
			return nil, err
		}
	}
	var poolAddress dcrutil.Address
	if cfg.PoolAddress != "" {
		poolAddress, err = dcrutil.DecodeAddress(cfg.PoolAddress, activeNet)
		if err != nil {
			return nil, err
		}
	}

	return &ticketPurchaser{
		cfg:           cfg,
		dcrdChainSvr:  dcrdChainSvr,
		dcrwChainSvr:  dcrwChainSvr,
		firstStart:    true,
		ticketAddress: ticketAddress,
		poolAddress:   poolAddress,
	}, nil
}

// diffPeriodFee
type diffPeriodFee struct {
	difficulty float64
	difference float64 // Difference from current difficulty
	fee        float64
}

// diffPeriodFees
type diffPeriodFees []*diffPeriodFee

func (p diffPeriodFees) Len() int { return len(p) }
func (p diffPeriodFees) Less(i, j int) bool {
	return p[i].difference < p[j].difference
}
func (p diffPeriodFees) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

// findClosestMeanFeeWindows is used when there is not enough block information
// from recent blocks to figure out what to set the user's ticket fees to.
// Instead, it uses data from the last windowsToConsider many windows and
// takes an average fee from the closest one.
func (t *ticketPurchaser) findClosestMeanFeeWindows(difficulty float64) (float64,
	error) {
	info, err := t.dcrdChainSvr.TicketFeeInfo(0, windowsToConsider)
	if err != nil {
		return 0.0, err
	}

	if len(info.FeeInfoWindows) == 0 {
		return 0.0, fmt.Errorf("not enough windows to find mean fee " +
			"available")
	}

	// Fetch all the mean fees and window difficulties. Calculate
	// the difference from the current window and sort, then use
	// the mean fee from the period that has the closest difficulty.
	sortable := make(diffPeriodFees, len(info.FeeInfoWindows))
	for i := range info.FeeInfoWindows {
		startHeight := int64(info.FeeInfoWindows[i].StartHeight)
		blH, err := t.dcrdChainSvr.GetBlockHash(startHeight)
		if err != nil {
			return 0.0, err
		}
		bl, err := t.dcrdChainSvr.GetBlock(blH)
		if err != nil {
			return 0.0, err
		}
		windowDiffAmt := dcrutil.Amount(bl.MsgBlock().Header.SBits)
		windowDiff := windowDiffAmt.ToCoin()

		dpf := &diffPeriodFee{
			difficulty: windowDiff,
			difference: math.Abs(windowDiff - difficulty),
			fee:        info.FeeInfoWindows[i].Mean,
		}
		sortable[i] = dpf
	}

	sort.Sort(sortable)

	return sortable[0].fee, nil
}

// findMeanFeeBlocks finds the mean of the mean of fees from BlocksToAvg many
// blocks using the ticketfeeinfo RPC API.
func (t *ticketPurchaser) findMeanFeeBlocks() (float64, error) {
	info, err := t.dcrdChainSvr.TicketFeeInfo(uint32(t.cfg.BlocksToAvg), 0)
	if err != nil {
		return 0.0, err
	}

	sum := 0.0
	for i := range info.FeeInfoBlocks {
		sum += info.FeeInfoBlocks[i].Mean
	}

	return sum / float64(t.cfg.BlocksToAvg), nil
}

// purchase is the main handler for purchasing tickets for the user.
// TODO Fix off by one bug in purchasing by height.
func (t *ticketPurchaser) purchase(height int32) error {
	// Just starting up, initialize our purchaser and start
	// buying.
	winSize := int32(activeNet.StakeDiffWindowSize)
	fillTicketQueue := false
	if t.firstStart {
		t.idxDiffPeriod = int(height % winSize)
		fillTicketQueue = true
		t.firstStart = false
	} else {
		// First we check to see if we're in a new difficulty period.
		// Roll over all of our variables if this is true.
		t.idxDiffPeriod = int(height % winSize)
		if height%winSize == 0 {
			log.Tracef("Resetting stake window ticket variables "+
				"at height %v", height)

			t.toBuyDiffPeriod = 0
			t.purchasedDiffPeriod = 0
			fillTicketQueue = true
		}
	}

	// We need to figure out how many tickets to buy.
	// Apply an exponential decay penalty to prices
	// that are above the mean price for the entire
	// ticket pool.
	// TODO We need the next block ticket difficulty,
	// not the current one. For now use the current
	// one until the RPC is wire to fetch the next
	// stake difficulty. That also means that instead
	// of beginning buying at the 0th block of the
	// window period, we begin buying at the -1 block
	// of the window period.

	// It can take a little while for the wallet to sync,
	// so loop this and
	var curStakeInfo *dcrjson.GetStakeInfoResult
	var err error
	for i := 0; i < stakeInfoReqTries; i++ {
		curStakeInfo, err = t.dcrwChainSvr.GetStakeInfo()
		if err != nil {
			log.Tracef("Failed to fetch stake information "+
				"on attempt %v: %v", i, err.Error())
			time.Sleep(time.Second * stakeInfoReqTryDelay)
			continue
		}
	}
	if err != nil {
		return err
	}

	// Disable purchasing if the ticket price is too hgih.
	if curStakeInfo.Difficulty > t.cfg.MaxPrice {
		log.Tracef("Aborting ticket purchases because the ticket price %v "+
			"is higher than the maximum price %v", curStakeInfo.Difficulty,
			t.cfg.MaxPrice)
		return nil
	}
	balSpendable, err := t.dcrwChainSvr.GetBalanceMinConfType("default", 0,
		"spendable")
	if err != nil {
		return err
	}

	if fillTicketQueue {
		// First get the average price of a ticket in
		// the ticket pool.
		poolValue, err := t.dcrdChainSvr.GetTicketPoolValue()
		if err != nil {
			return err
		}
		bestBlockH, err := t.dcrdChainSvr.GetBestBlockHash()
		if err != nil {
			return err
		}
		bestBlock, err := t.dcrdChainSvr.GetBlock(bestBlockH)
		if err != nil {
			return err
		}
		poolSize := bestBlock.MsgBlock().Header.PoolSize
		avgPrice := poolValue.ToCoin() / float64(poolSize)
		curPrice := curStakeInfo.Difficulty
		couldBuy := math.Floor(balSpendable.ToCoin() / curStakeInfo.Difficulty)

		// Decay exponentially if the price is above the average
		// price.
		// floor(penalty ^ -(abs(ticket price - average ticket price)))
		// Then multiply by the number of tickets we could possibly
		// buy.
		if curPrice > avgPrice {
			toBuy := math.Floor(math.Pow(t.cfg.HighPricePenalty,
				-(math.Abs(curPrice-avgPrice))) * couldBuy)
			t.toBuyDiffPeriod = int(float64(toBuy))

			log.Debugf("The current price %v is above the average price %v, "+
				"so the number of tickets to buy this window was "+
				"scaled from %v to %v", curPrice, avgPrice, couldBuy,
				t.toBuyDiffPeriod)
		} else {
			// Below or equal to the average price. Buy as many
			// tickets as possible.
			t.toBuyDiffPeriod = int(float64(couldBuy))
		}
	}

	// If we still have tickets in the memory pool, don't try
	// to buy even more tickets.
	if t.cfg.WaitForTickets {
		if curStakeInfo.OwnMempoolTix > 0 {
			log.Debugf("Currently waiting for %v tickets to enter the "+
				"blockchain before buying more tickets",
				curStakeInfo.OwnMempoolTix)
			return nil
		}
	}

	// If might be the case that there weren't enough recent
	// blocks to average fees from. Use data from the last
	// window with the closest difficulty.
	meanFee := 0.0
	if t.idxDiffPeriod < t.cfg.BlocksToAvg {
		meanFee, err = t.findClosestMeanFeeWindows(curStakeInfo.Difficulty)
		if err != nil {
			return err
		}
	} else {
		meanFee, err = t.findMeanFeeBlocks()
		if err != nil {
			return err
		}
	}

	// Scale the mean fee upwards according to what was asked
	// for by the user.
	feeToUse := meanFee * t.cfg.FeeTargetScaling
	if feeToUse > t.cfg.MaxFee {
		log.Tracef("Scaled fee is %v, but max fee is %v; using max",
			feeToUse, t.cfg.MaxFee)
		feeToUse = t.cfg.MaxFee
	}
	if feeToUse < t.cfg.MinFee {
		log.Tracef("Scaled fee is %v, but min fee is %v; using min",
			feeToUse, t.cfg.MinFee)
		feeToUse = t.cfg.MinFee
	}
	feeToUseAmt, err := dcrutil.NewAmount(feeToUse)
	if err != nil {
		return err
	}
	err = t.dcrwChainSvr.SetTicketFee(feeToUseAmt)
	if err != nil {
		return err
	}

	log.Debugf("Mean fee for the last blocks or window period was %v; "+
		"this was scaled to %v", meanFee, feeToUse)

	// Purchase tickets.
	toBuyForBlock := t.toBuyDiffPeriod - t.purchasedDiffPeriod
	if toBuyForBlock > t.cfg.MaxPerBlock {
		toBuyForBlock = t.cfg.MaxPerBlock
	}

	// We've already purchased all the tickets we need to.
	if toBuyForBlock == 0 {
		log.Tracef("All tickets have been purchased, aborting further " +
			"ticket purchases")
		return nil
	}

	// Check our balance and abort if we don't have enough moneys.
	if (balSpendable.ToCoin() - float64(toBuyForBlock)*curStakeInfo.Difficulty) <
		t.cfg.BalanceToMaintain {
		log.Tracef("Aborting purchasing of tickets because our balance "+
			"after buying tickets is estimated to be %v but balance "+
			"to maintain is set to %v",
			(balSpendable.ToCoin() - float64(toBuyForBlock)*
				curStakeInfo.Difficulty),
			t.cfg.BalanceToMaintain)
		return nil
	}

	// If an address wasn't passed, create an internal address in
	// the wallet for the ticket address.
	var ticketAddress dcrutil.Address
	if t.ticketAddress != nil {
		ticketAddress = t.ticketAddress
	} else {
		ticketAddress, err =
			t.dcrwChainSvr.GetRawChangeAddress(t.cfg.AccountName)
		if err != nil {
			return err
		}
	}

	maxPriceAmt, err := dcrutil.NewAmount(t.cfg.MaxPrice)
	if err != nil {
		return err
	}
	poolFeesAmt, err := dcrutil.NewAmount(t.cfg.PoolFees)
	if err != nil {
		return err
	}
	minConf := 0
	expiry := int(height) + t.cfg.ExpiryDelta
	tickets, err := t.dcrwChainSvr.PurchaseTicket(t.cfg.AccountName,
		maxPriceAmt,
		&minConf,
		ticketAddress,
		&toBuyForBlock,
		t.poolAddress,
		&poolFeesAmt,
		&expiry)
	if err != nil {
		return err
	}
	t.purchasedDiffPeriod += toBuyForBlock

	for i := range tickets {
		log.Infof("Purchased ticket %v at stake difficulty %v (%v "+
			"fees per KB used)", tickets[i], curStakeInfo.Difficulty,
			feeToUseAmt.ToCoin())
	}

	log.Debugf("Tickets purchased so far in this window: %v",
		t.purchasedDiffPeriod)
	log.Debugf("Tickets remaining to be purchased in this window: %v",
		t.toBuyDiffPeriod-t.purchasedDiffPeriod)

	return nil
}
