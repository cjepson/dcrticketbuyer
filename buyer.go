// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"
	"time"

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

	// useMedianStr is the string indicating that the median ticket fee
	// should be used when determining ticket fee.
	useMedianStr = "median"
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
	windowPeriod        int  // The current window period
	idxDiffPeriod       int  // Relative block index within the difficulty period
	toBuyDiffPeriod     int  // Number to buy in this period
	purchasedDiffPeriod int  // Number already bought in this period
	maintainMaxPrice    bool // Flag for maximum price manipulation
	maintainMinPrice    bool // Flag for minimum price manipulation
	useMedian           bool // Flag for using median for ticket fees
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

	maintainMaxPrice := false
	if cfg.MaxPriceScale > 0.0 {
		maintainMaxPrice = true
	}

	maintainMinPrice := false
	if cfg.MinPriceScale > 0.0 {
		maintainMinPrice = true
	}

	return &ticketPurchaser{
		cfg:              cfg,
		dcrdChainSvr:     dcrdChainSvr,
		dcrwChainSvr:     dcrwChainSvr,
		firstStart:       true,
		ticketAddress:    ticketAddress,
		poolAddress:      poolAddress,
		maintainMaxPrice: maintainMaxPrice,
		maintainMinPrice: maintainMinPrice,
		useMedian:        cfg.FeeSource == useMedianStr,
	}, nil
}

// purchase is the main handler for purchasing tickets for the user.
// TODO Not make this an inlined pile of crap.
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

	// Move the respective cursors for our positions
	// in the blockchain.
	t.idxDiffPeriod = int(height % winSize)
	t.windowPeriod = int(height / winSize)

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
		log.Tracef("Detected assymetry in this window period versus "+
			"stored window period, resetting purchase orders at "+
			"height %v", height)

		t.toBuyDiffPeriod = 0
		t.purchasedDiffPeriod = 0
		fillTicketQueue = true
	}

	// Parse the ticket purchase frequency. Positive numbers mean
	// that many tickets per block. Negative numbers mean to only
	// purchase one ticket once every abs(num) blocks.
	maxPerBlock := 0
	switch {
	case t.cfg.MaxPerBlock == 0:
		return nil
	case t.cfg.MaxPerBlock > 1:
		maxPerBlock = t.cfg.MaxPerBlock
	case t.cfg.MaxPerBlock < 0:
		if int(height)%t.cfg.MaxPerBlock != 0 {
			return nil
		}
		maxPerBlock = 1
	}

	// Make sure that our wallet is connected to the daemon and the
	// wallet is unlocked, otherwise abort.
	walletInfo, err := t.dcrwChainSvr.WalletInfo()
	if err != nil {
		return err
	}
	if !walletInfo.DaemonConnected {
		return fmt.Errorf("Wallet not connected to daemon")
	}
	if !walletInfo.Unlocked {
		return fmt.Errorf("Wallet not unlocked to allow ticket purchases")
	}

	// Pull and store relevant data about the blockchain. Calculate a
	// "reasonable" ticket price by using the VWAP for the last 10 days
	// (mainnet) combined with the average price of all tickets in the
	// ticket pool. Scale this according to the configuration parameters
	// to find minimum and maximum prices for users that are electing to
	// attempting to manipulate the stake difficulty.
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

	// Do not allow zero pool sizes to prevent a possible
	// panic below.
	if poolSize == 0 {
		poolSize += 1
	}

	avgPricePoolAmt := poolValue / dcrutil.Amount(poolSize)
	ticketVWAP, err := t.dcrdChainSvr.TicketVWAP(nil, nil)
	if err != nil {
		return err
	}
	avgPriceAmt := (ticketVWAP + avgPricePoolAmt) / 2
	avgPrice := avgPriceAmt.ToCoin()
	log.Tracef("Calculated average ticket price: %v", avgPriceAmt)

	stakeDiffs, err := t.dcrwChainSvr.GetStakeDifficulty()
	if err != nil {
		return err
	}
	nextStakeDiff, err := dcrutil.NewAmount(stakeDiffs.NextStakeDifficulty)
	if err != nil {
		return err
	}
	sDiffEsts, err := t.dcrdChainSvr.EstimateStakeDiff(nil)
	if err != nil {
		return err
	}
	maxPriceAbsAmt, err := dcrutil.NewAmount(t.cfg.MaxPriceAbsolute)
	if err != nil {
		return err
	}
	maxPriceScaledAmt, err := dcrutil.NewAmount(t.cfg.MaxPriceScale * avgPrice)
	if err != nil {
		return err
	}
	if t.maintainMaxPrice {
		log.Tracef("The maximum price to maintain for this round is set to %v",
			maxPriceScaledAmt)
	}
	minPriceScaledAmt, err := dcrutil.NewAmount(t.cfg.MinPriceScale * avgPrice)
	if err != nil {
		return err
	}
	if t.maintainMinPrice {
		log.Tracef("The minimum price to maintain for this round is set to %v",
			minPriceScaledAmt)
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
		// Calculate how many tickets we could possibly buy
		// at this difficulty.
		curPrice := nextStakeDiff
		couldBuy := math.Floor(balSpendable.ToCoin() / nextStakeDiff.ToCoin())

		// Override the target price being the average price if
		// the user has elected to attempt to modify the ticket
		// price.
		targetPrice := avgPrice
		if t.cfg.PriceTarget > 0.0 {
			targetPrice = t.cfg.PriceTarget
		}

		// The target price can not be above the maximum scaled
		// price of tickets that the user has elected to maintain.
		// If it is, set the target to the scaled maximum instead
		// and warn the user.
		if t.maintainMaxPrice && targetPrice > maxPriceScaledAmt.ToCoin() {
			targetPrice = maxPriceScaledAmt.ToCoin()
			log.Warnf("The target price %v that was set to be maintained "+
				"was above the allowable scaled maximum of %v, so the "+
				"scaled maximum is being used as the target",
				t.cfg.PriceTarget, maxPriceScaledAmt)
		}

		// Decay exponentially if the price is above the ideal or target
		// price.
		// floor(penalty ^ -(abs(ticket price - average ticket price)))
		// Then multiply by the number of tickets we could possibly
		// buy.
		if curPrice.ToCoin() > targetPrice {
			toBuy := math.Floor(math.Pow(t.cfg.HighPricePenalty,
				-(math.Abs(curPrice.ToCoin()-targetPrice))) * couldBuy)
			t.toBuyDiffPeriod = int(float64(toBuy))

			log.Debugf("The current price %v is above the target price %v, "+
				"so the number of tickets to buy this window was "+
				"scaled from %v to %v", curPrice, targetPrice, couldBuy,
				t.toBuyDiffPeriod)
		} else {
			// Below or equal to the average price. Buy as many
			// tickets as possible.
			t.toBuyDiffPeriod = int(float64(couldBuy))

			log.Debugf("The stake difficulty %v was below the target penalty "+
				"cutoff %v; %v many tickets have been queued for purchase",
				curPrice, targetPrice, t.toBuyDiffPeriod)
		}
	}

	// Disable purchasing if the ticket price is too high based on
	// the absolute cutoff or if the estimated ticket price is above
	// our scaled cutoff based on the ideal ticket price.
	if nextStakeDiff > maxPriceAbsAmt {
		log.Tracef("Aborting ticket purchases because the ticket price %v "+
			"is higher than the maximum absolute price %v", nextStakeDiff,
			maxPriceAbsAmt)
		return nil
	}
	if t.maintainMaxPrice && (sDiffEsts.Expected > maxPriceScaledAmt.ToCoin()) {
		log.Tracef("Aborting ticket purchases because the ticket price "+
			"next window estimate %v is higher than the maximum scaled "+
			"price %v", sDiffEsts.Expected, maxPriceScaledAmt)
		return nil
	}

	// If we still have tickets in the memory pool, don't try
	// to buy even more tickets.
	if !t.cfg.DontWaitForTickets {
		inMP, err := t.ownTicketsInMempool()
		if err != nil {
			return err
		}

		if inMP > t.cfg.MaxInMempool {
			log.Debugf("Currently waiting for %v tickets to enter the "+
				"blockchain before buying more tickets (in mempool: %v,"+
				" max allowed in mempool %v)", inMP-t.cfg.MaxInMempool,
				inMP, t.cfg.MaxInMempool)
			return nil
		}
	}

	// If might be the case that there weren't enough recent
	// blocks to average fees from. Use data from the last
	// window with the closest difficulty.
	chainFee := 0.0
	if t.idxDiffPeriod < t.cfg.BlocksToAvg {
		chainFee, err = t.findClosestFeeWindows(nextStakeDiff.ToCoin(),
			t.useMedian)
		if err != nil {
			return err
		}
	} else {
		chainFee, err = t.findTicketFeeBlocks(t.useMedian)
		if err != nil {
			return err
		}
	}

	// Scale the mean fee upwards according to what was asked
	// for by the user.
	feeToUse := chainFee * t.cfg.FeeTargetScaling
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
		"this was scaled to %v", chainFee, feeToUse)

	// Only the maximum number of tickets at each block
	// should be purchased, as specified by the user.
	toBuyForBlock := t.toBuyDiffPeriod - t.purchasedDiffPeriod
	if toBuyForBlock > maxPerBlock {
		toBuyForBlock = maxPerBlock
	}

	// Hijack the number to purchase for this block if we have minimum
	// ticket price manipulation enabled.
	if t.maintainMinPrice && toBuyForBlock < maxPerBlock {
		if sDiffEsts.Expected < minPriceScaledAmt.ToCoin() {
			toBuyForBlock = maxPerBlock
			log.Debugf("Attempting to manipulate the stake difficulty "+
				"so that the price does not fall below the set minimum "+
				"%v (current estimate for next stake difficulty: %v) by "+
				"purchasing an additional round of tickets",
				minPriceScaledAmt, sDiffEsts.Expected)
		}
	}

	// We've already purchased all the tickets we need to.
	if toBuyForBlock <= 0 {
		log.Tracef("All tickets have been purchased, aborting further " +
			"ticket purchases")
		return nil
	}

	// Check our balance versus the amount of tickets we need to buy.
	// If there is not enough money, decrement and recheck the balance
	// to see if fewer tickets may be purchased. Abort if we don't
	// have enough moneys.
	notEnough := func(bal dcrutil.Amount, toBuy int, sd dcrutil.Amount) bool {
		return (bal.ToCoin() - float64(toBuy)*sd.ToCoin()) <
			t.cfg.BalanceToMaintain
	}
	if notEnough(balSpendable, toBuyForBlock, nextStakeDiff) {
		for notEnough(balSpendable, toBuyForBlock, nextStakeDiff) {
			if toBuyForBlock == 0 {
				break
			}

			toBuyForBlock--
		}

		if toBuyForBlock == 0 {
			log.Tracef("Aborting purchasing of tickets because our balance "+
				"after buying tickets is estimated to be %v but balance "+
				"to maintain is set to %v",
				(balSpendable.ToCoin() - float64(toBuyForBlock)*
					nextStakeDiff.ToCoin()),
				t.cfg.BalanceToMaintain)
			return nil
		}
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
		maxPriceAbsAmt,
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

	balSpendable, err = t.dcrwChainSvr.GetBalanceMinConfType(t.cfg.AccountName,
		0, "spendable")
	if err != nil {
		return err
	}
	log.Debugf("Final spendable balance at height %v for account '%s' "+
		"after ticket purchases: %v", height, t.cfg.AccountName, balSpendable)

	return nil
}
