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
	// stakeInfoReqTries is the maximum number of times to try
	// GetStakeInfo before failing.
	stakeInfoReqTries = 10

	// windowsToConsider is the number of windows to consider
	// when there is not enough block information to determine
	// what the best fee should be.
	windowsToConsider = 10
)

var (
	// stakeInfoReqTryDelay is the time in seconds to wait before
	// doing another GetStakeInfo request.
	stakeInfoReqTryDelay = time.Second * 2

	// zeroUint32 is the zero value for a uint32.
	zeroUint32 = uint32(0)
)

// purchaseManager is the main handler of websocket notifications to
// pass to the purchaser and internal quit notifications.
type purchaseManager struct {
	purchaser          *ticketPurchaser
	blockConnectedChan chan int32
	quit               chan struct{}
}

// newPurchaseManager creates a new purchaseManager.
func newPurchaseManager(purchaser *ticketPurchaser,
	blockConnChan chan int32,
	quit chan struct{}) *purchaseManager {
	return &purchaseManager{
		purchaser:          purchaser,
		blockConnectedChan: blockConnChan,
		quit:               quit,
	}
}

// blockConnectedHandler handles block connected notifications, which trigger
// ticket purchases.
func (p *purchaseManager) blockConnectedHandler() {
out:
	for {
		select {
		case height := <-p.blockConnectedChan:
			daemonLog.Infof("Block height %v connected", height)
			err := p.purchaser.purchase(height)
			if err != nil {
				log.Errorf("Failed to purchase tickets this round: %s",
					err.Error())
			}
		// TODO Poll every couple minute to check if connected;
		// if not, try to reconnect.
		case <-p.quit:
			break out
		}
	}
}

// ticketPurchaser is the main handler for purchasing tickets. It decides
// whether or not to do so based on information obtained from daemon and
// wallet chain servers.
//
// The variables at the end handle a simple "queue" of tickets to purchase,
// which is equal to the number in toBuyDiffPeriod. toBuyDiffPeriod gets
// reset when we enter a new difficulty period because a new block has been
// connected that is outside the previous difficulty period. The variable
// purchasedDiffPeriod tracks the number purchased in this period.
type ticketPurchaser struct {
	cfg                 *config
	dcrdChainSvr        *dcrrpcclient.Client
	dcrwChainSvr        *dcrrpcclient.Client
	ticketAddress       dcrutil.Address
	poolAddress         dcrutil.Address
	firstStart          bool
	windowPeriod        int // The current window period
	idxDiffPeriod       int // Relative block index within the difficulty period
	toBuyDiffPeriod     int // Number to buy in this period
	purchasedDiffPeriod int // Number already bought in this period
}

// newTicketPurchaser creates a new ticketPurchaser.
func newTicketPurchaser(cfg *config,
	dcrdChainSvr *dcrrpcclient.Client,
	dcrwChainSvr *dcrrpcclient.Client) (*ticketPurchaser, error) {
	var ticketAddress dcrutil.Address
	var err error
	if cfg.TicketAddress != "" {
		ticketAddress, err = dcrutil.DecodeAddress(cfg.TicketAddress,
			activeNet.Params)
		if err != nil {
			return nil, err
		}
	}
	var poolAddress dcrutil.Address
	if cfg.PoolAddress != "" {
		poolAddress, err = dcrutil.DecodeNetworkAddress(cfg.PoolAddress)
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

// diffPeriodFee defines some statistics about a difficulty fee period
// compared to the current difficulty period.
type diffPeriodFee struct {
	difficulty float64
	difference float64 // Difference from current difficulty
	fee        float64
}

// diffPeriodFees is slice type definition used to satisfy the sorting
// interface.
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
	wtcUint32 := uint32(windowsToConsider)
	info, err := t.dcrdChainSvr.TicketFeeInfo(&zeroUint32, &wtcUint32)
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

// findMeanTicketFeeBlocks finds the mean of the mean of fees from BlocksToAvg
// many blocks using the ticketfeeinfo RPC API.
func (t *ticketPurchaser) findMeanTicketFeeBlocks() (float64, error) {
	btaUint32 := uint32(t.cfg.BlocksToAvg)
	info, err := t.dcrdChainSvr.TicketFeeInfo(&btaUint32, nil)
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
	// buying. Set the start up regular transaction fee here
	// too.
	winSize := int32(activeNet.StakeDiffWindowSize)
	fillTicketQueue := false
	if t.firstStart {
		t.idxDiffPeriod = int(height % winSize)
		t.windowPeriod = int(height / winSize)
		fillTicketQueue = true
		t.firstStart = false

		log.Tracef("First run time, initialized idxDiffPeriod to %v",
			t.idxDiffPeriod)

		txFeeAmt, err := dcrutil.NewAmount(t.cfg.TxFee)
		if err != nil {
			log.Errorf("Failed to decode tx fee amount %v from config",
				t.cfg.TxFee)
		} else {
			errSet := t.dcrwChainSvr.SetTxFee(txFeeAmt)
			if errSet != nil {
				log.Errorf("Failed to set tx fee amount %v in wallet",
					txFeeAmt)
			} else {
				log.Tracef("Setting of network regular tx relay fee to %v "+
					"was successful", txFeeAmt)
			}
		}
	}

	// The general case initialization for this function. It
	// sets our index in the difficulty period, and then
	// decides if it needs to fill the queue with tickets to
	// purchase.
	// Check to see if we're in a new difficulty period.
	// Roll over all of our variables if this is true.
	if (height+1)%winSize == 0 {
		log.Tracef("Resetting stake window ticket variables "+
			"at height %v", height)

		t.toBuyDiffPeriod = 0
		t.purchasedDiffPeriod = 0
		fillTicketQueue = true
	}

	// We may have disconnected and reconnected in a
	// different window period. If this is the case,
	// we need reset our variables too.
	thisWindowPeriod := int(height / winSize)
	if (height+1)%winSize != 0 &&
		thisWindowPeriod > t.windowPeriod {
		t.toBuyDiffPeriod = 0
		t.purchasedDiffPeriod = 0
		fillTicketQueue = true
	}

	// Move the respective cursors for our positions
	// in the blockchain.
	t.idxDiffPeriod = int(height % winSize)
	t.windowPeriod = int(height / winSize)

	// We need to figure out how many tickets to buy.
	// Apply an exponential decay penalty to prices
	// that are above the mean price for the entire
	// ticket pool.
	//
	// It can take a little while for the wallet to sync,
	// so loop this and recheck to see if we've got the
	// next block attached yet.
	var curStakeInfo *dcrjson.GetStakeInfoResult
	var err error
	for i := 0; i < stakeInfoReqTries; i++ {
		curStakeInfo, err = t.dcrwChainSvr.GetStakeInfo()
		if err != nil {
			log.Tracef("Failed to fetch stake information "+
				"on attempt %v: %v", i, err.Error())
			time.Sleep(stakeInfoReqTryDelay)
			continue
		}
	}
	if err != nil {
		return err
	}
	stakeDiffs, err := t.dcrwChainSvr.GetStakeDifficulty()
	if err != nil {
		return err
	}
	nextStakeDiff, err := dcrutil.NewAmount(stakeDiffs.NextStakeDifficulty)
	if err != nil {
		return err
	}
	maxPriceAmt, err := dcrutil.NewAmount(t.cfg.MaxPrice)
	if err != nil {
		return err
	}

	// Disable purchasing if the ticket price is too high.
	if nextStakeDiff > maxPriceAmt {
		log.Tracef("Aborting ticket purchases because the ticket price %v "+
			"is higher than the maximum price %v", nextStakeDiff,
			maxPriceAmt)
		return nil
	}
	balSpendable, err := t.dcrwChainSvr.GetBalanceMinConfType(t.cfg.AccountName,
		0, "spendable")
	if err != nil {
		return err
	}
	log.Debugf("Current spendable balance at height %v for account '%s': %v",
		height, t.cfg.AccountName, balSpendable)

	// This is the main portion that handles filling up the
	// queue of tickets to purchase (t.toBuyDiffPeriod).
	if fillTicketQueue {
		// First get the average price of a ticket in
		// the ticket pool. Then get the VWAP price aside
		// from the pool price. Calculate an average
		// price by finding the mean.
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
		avgPricePoolAmt := poolValue / dcrutil.Amount(poolSize)
		ticketVWAP, err := t.dcrdChainSvr.TicketVWAP(nil, nil)
		if err != nil {
			return err
		}
		avgPriceAmt := (ticketVWAP + avgPricePoolAmt) / 2
		avgPrice := avgPriceAmt.ToCoin()
		log.Tracef("Calculated average ticket price: %v", avgPriceAmt)

		// Calculate how many tickets we could possibly buy
		// at this difficulty.
		curPrice := nextStakeDiff
		couldBuy := math.Floor(balSpendable.ToCoin() / nextStakeDiff.ToCoin())

		// Decay exponentially if the price is above the average
		// price.
		// floor(penalty ^ -(abs(ticket price - average ticket price)))
		// Then multiply by the number of tickets we could possibly
		// buy.
		if curPrice.ToCoin() > avgPrice {
			toBuy := math.Floor(math.Pow(t.cfg.HighPricePenalty,
				-(math.Abs(curPrice.ToCoin()-avgPrice))) * couldBuy)
			t.toBuyDiffPeriod = int(float64(toBuy))

			log.Debugf("The current price %v is above the average price %v, "+
				"so the number of tickets to buy this window was "+
				"scaled from %v to %v", curPrice, avgPrice, couldBuy,
				t.toBuyDiffPeriod)
		} else {
			// Below or equal to the average price. Buy as many
			// tickets as possible.
			t.toBuyDiffPeriod = int(float64(couldBuy))

			log.Debugf("The stake difficulty %v was below the penalty "+
				"cutoff %v; %v many tickets have been queued for purchase",
				curPrice, avgPrice, t.toBuyDiffPeriod)
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
		meanFee, err = t.findClosestMeanFeeWindows(nextStakeDiff.ToCoin())
		if err != nil {
			return err
		}
	} else {
		meanFee, err = t.findMeanTicketFeeBlocks()
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
	if (balSpendable.ToCoin() - float64(toBuyForBlock)*nextStakeDiff.ToCoin()) <
		t.cfg.BalanceToMaintain {
		log.Tracef("Aborting purchasing of tickets because our balance "+
			"after buying tickets is estimated to be %v but balance "+
			"to maintain is set to %v",
			(balSpendable.ToCoin() - float64(toBuyForBlock)*
				nextStakeDiff.ToCoin()),
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

	// Purchase tickets.
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
			"fees per KB used)", tickets[i], nextStakeDiff.ToCoin(),
			feeToUseAmt.ToCoin())
	}

	log.Debugf("Tickets purchased so far in this window: %v",
		t.purchasedDiffPeriod)
	log.Debugf("Tickets remaining to be purchased in this window: %v",
		t.toBuyDiffPeriod-t.purchasedDiffPeriod)

	return nil
}
