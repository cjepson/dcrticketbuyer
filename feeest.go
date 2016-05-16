// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/decred/dcrutil"
)

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

// findClosestFeeWindows is used when there is not enough block information
// from recent blocks to figure out what to set the user's ticket fees to.
// Instead, it uses data from the last windowsToConsider many windows and
// takes an average fee from the closest one.
func (t *ticketPurchaser) findClosestFeeWindows(difficulty float64,
	useMedian bool) (float64, error) {
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

		fee := float64(0.0)
		if !useMedian {
			fee = info.FeeInfoWindows[i].Mean
		} else {
			fee = info.FeeInfoWindows[i].Median
		}

		dpf := &diffPeriodFee{
			difficulty: windowDiff,
			difference: math.Abs(windowDiff - difficulty),
			fee:        fee,
		}
		sortable[i] = dpf
	}

	sort.Sort(sortable)

	return sortable[0].fee, nil
}

// findMeanTicketFeeBlocks finds the mean of the mean of fees from BlocksToAvg
// many blocks using the ticketfeeinfo RPC API.
func (t *ticketPurchaser) findTicketFeeBlocks(useMedian bool) (float64, error) {
	btaUint32 := uint32(t.cfg.BlocksToAvg)
	info, err := t.dcrdChainSvr.TicketFeeInfo(&btaUint32, nil)
	if err != nil {
		return 0.0, err
	}

	sum := 0.0
	for i := range info.FeeInfoBlocks {
		if !useMedian {
			sum += info.FeeInfoBlocks[i].Mean
		} else {
			sum += info.FeeInfoBlocks[i].Median
		}
	}

	return sum / float64(t.cfg.BlocksToAvg), nil
}
