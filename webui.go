// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/decred/dcrutil"
)

// csvWriteMutex is the global write mutex for files to write for the HTTP
// server.
var csvWriteMutex sync.Mutex

// csvTicketPricesFn is the filename for the CSV of the ticket prices.
var csvTicketPricesFn = "prices.csv"

// csvMempoolFn is the filename for the CSV of the tickets in mempool.
var csvMempoolFn = "mempool.csv"

// csvPurchasedFn is the filename for the CSV tracking number of tickets
// purchased.
var csvPurchasedFn = "purchased.csv"

// csvFeesFn is the filename for the CSV tracking stake fees.
var csvFeesFn = "fees.csv"

// csvUpdateData contains all the information required to update the CSV
// files for the HTTP server.
type csvUpdateData struct {
	height     int32
	tpMinScale float64
	tpMaxScale float64
	tpAverage  float64
	tpNext     float64
	tpCurrent  float64
	tnAll      int
	tnOwn      int
	purchased  int
	tfMin      float64
	tfMax      float64
	tfMedian   float64
	tfMean     float64
	tfOwn      float64
}

const (
	// chartCutoffTicketPrice is the chart cutoff value for ticket prices
	// by height, from the top block.
	chartCutoffTicketPrice = 288

	// chartCutoffMempool is the chart cutoff value for mempool tickets
	// by height, from the top block.
	chartCutoffMempool = 144

	// chartCutoffMempool is the chart cutoff value for number of tickets
	// purchased, from the top block.
	chartCutoffPurchased = 144

	// chartCutoffFees is the chart cutoff value for ticket fee data,
	// from the top block.
	chartCutoffFees = 32
)

// initCsvFiles initializes the CSV files for use by the web server.
func initCsvFiles() error {
	// Check the path. If we're missing it, throw an error.
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		return fmt.Errorf("Failed to initialize webserver (does "+
			"./%s exist and contain the needed JS libs?", csvPath)
	}
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		return fmt.Errorf("Failed to initialize webserver (does "+
			"./%s exist and contain the needed JS libs?", csvPath)
	}

	// If any of the files are missing, overwrite the old files
	// and continues.
	missing := false
	if _, err := os.Stat(filepath.Join(csvPath,
		csvTicketPricesFn)); os.IsNotExist(err) {
		missing = true
	}
	if _, err := os.Stat(filepath.Join(csvPath,
		csvMempoolFn)); os.IsNotExist(err) {
		missing = true
	}
	if _, err := os.Stat(filepath.Join(csvPath,
		csvPurchasedFn)); os.IsNotExist(err) {
		missing = true
	}
	if _, err := os.Stat(filepath.Join(csvPath,
		csvFeesFn)); os.IsNotExist(err) {
		missing = true
	}
	if !missing {
		log.Tracef("HTTP server CSV files already exist, loading and continuing " +
			"from them")
		return nil
	}

	// Create the respective files and initialize the CSVs
	// with proper headers.
	f, err := os.Create(filepath.Join(csvPath, csvTicketPricesFn))
	if err != nil {
		return err
	}
	f.WriteString("Height,Type,Price\n")
	f.Close()

	f, err = os.Create(filepath.Join(csvPath, csvMempoolFn))
	if err != nil {
		return err
	}
	f.WriteString("Height,Type,Number\n")
	f.Close()

	f, err = os.Create(filepath.Join(csvPath, csvPurchasedFn))
	if err != nil {
		return err
	}
	f.WriteString("Height,Type,Number\n")
	f.Close()

	f, err = os.Create(filepath.Join(csvPath, csvFeesFn))
	if err != nil {
		return err
	}
	f.WriteString("Height,Type,FeesPerKB\n")
	f.Close()

	return nil
}

// strTicketPriceCsv converts data relating to future ticket prices into an
// easy-to-write string for the csv files.
func strTicketPriceCsv(height int32, minScale, maxScale, average, next,
	current float64) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("%v,MinScale,%v\n", height, minScale))
	buf.WriteString(fmt.Sprintf("%v,MaxScale,%v\n", height, maxScale))
	buf.WriteString(fmt.Sprintf("%v,Average,%v\n", height, average))
	buf.WriteString(fmt.Sprintf("%v,NextEst,%v\n", height, next))
	buf.WriteString(fmt.Sprintf("%v,Current,%v\n", height, current))

	return buf.String()
}

// strTicketNumCsv converts data relating to number of tickets in the mempool
// to an easy-to-write string for the csv files.
func strTicketNumCsv(height int32, all, own int) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("%v,All,%v\n", height, all))
	buf.WriteString(fmt.Sprintf("%v,Own,%v\n", height, own))

	return buf.String()
}

// strPurchasedCsv converts data relating to number of tickets purchased this
// block to an easy-to-write string for the csv files.
func strPurchasedCsv(height int32, purchased int) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("%v,Purchased,%v\n", height, purchased))

	return buf.String()
}

// strFeesCsv converts data relating to per KB fees of recently purchased tickets
// to an easy-to-write string for the csv files.
func strFeesCsv(height int32, min, max, median, mean, own float64) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("%v,Minimum,%v\n", height, min))
	buf.WriteString(fmt.Sprintf("%v,Maximum,%v\n", height, min))
	buf.WriteString(fmt.Sprintf("%v,Median,%v\n", height, median))
	buf.WriteString(fmt.Sprintf("%v,Mean,%v\n", height, mean))
	buf.WriteString(fmt.Sprintf("%v,Own,%v\n", height, own))

	return buf.String()
}

// writeToCsvFiles writes the update data for the CSVs to their respective files.
func writeToCsvFiles(csvUD csvUpdateData) error {
	csvWriteMutex.Lock()
	defer csvWriteMutex.Unlock()

	height := csvUD.height

	f, err := os.OpenFile(filepath.Join(csvPath, csvTicketPricesFn),
		os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(f)
	str := strTicketPriceCsv(height, csvUD.tpMinScale, csvUD.tpMaxScale,
		csvUD.tpAverage, csvUD.tpNext, csvUD.tpCurrent)
	_, err = writer.WriteString(str)
	if err != nil {
		return err
	}
	err = writer.Flush()
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}

	f, err = os.OpenFile(filepath.Join(csvPath, csvMempoolFn),
		os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return err
	}
	writer = bufio.NewWriter(f)
	str = strTicketNumCsv(height, csvUD.tnAll, csvUD.tnOwn)
	_, err = writer.WriteString(str)
	if err != nil {
		return err
	}
	err = writer.Flush()
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}

	f, err = os.OpenFile(filepath.Join(csvPath, csvPurchasedFn),
		os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return err
	}
	writer = bufio.NewWriter(f)
	str = strPurchasedCsv(height, csvUD.purchased)
	_, err = writer.WriteString(str)
	if err != nil {
		return err
	}
	err = writer.Flush()
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}

	f, err = os.OpenFile(filepath.Join(csvPath, csvFeesFn),
		os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return err
	}
	writer = bufio.NewWriter(f)
	str = strFeesCsv(height, csvUD.tfMin, csvUD.tfMax, csvUD.tfMedian,
		csvUD.tfMean, csvUD.tfOwn)
	_, err = writer.WriteString(str)
	if err != nil {
		return err
	}
	err = writer.Flush()
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}

	return nil
}

// addDimpleChart adds a simple dimple chart tracking multiple datasets labeled
// under an identifier.
func addDimpleChart(chartName, title, dataLoc, xItem, yItem,
	identifier string, heightCutoff int32, useStep bool) string {
	svgName := chartName + "Svg"
	chartTemplate :=
		`<div id="chartContainer">
	<script type="text/javascript">
	var SVG_NAME = dimple.newSvg("#chartContainer", 690, 430);

	d3.csv("CHART_DATA_URL", function (data) {
		data = data.filter(function(row) {
			return parseInt(row['Height'], 10) > HEIGHT_CUTOFF;
		})
		
	    var CHART_NAME = new dimple.chart(SVG_NAME, data);
	    CHART_NAME.setBounds(60, 60, 600, 305);
	    var x = CHART_NAME.addCategoryAxis("x", "X_ITEM");
		x.addOrderRule(function(a, b) {
			return d3.ascending(parseInt(a.Height, 10), parseInt(b.Height, 10));
		});
	    CHART_NAME.addMeasureAxis("y", "Y_ITEM");
	    var s = CHART_NAME.addSeries("IDENTIFIER", dimple.plot.line);
		USE_STEP
	    CHART_NAME.addLegend(60, 45, 600, 20, "right");
	    CHART_NAME.draw();
		
		SVG_NAME.append("text")
 			.attr("x", CHART_NAME._xPixels() + CHART_NAME._widthPixels() / 2)
		.attr("y", CHART_NAME._yPixels() - 25)
		.style("text-anchor", "middle")
		.style("font-family", "sans-serif")
		.style("font-weight", "bold")
		.text("CHART_TITLE");
    });
	</script>
	</div>`

	str := strings.Replace(chartTemplate, "SVG_NAME", svgName, -1)
	str = strings.Replace(str, "HEIGHT_CUTOFF",
		strconv.Itoa(int(heightCutoff)), -1)
	str = strings.Replace(str, "CHART_NAME", chartName, -1)
	str = strings.Replace(str, "X_ITEM", xItem, -1)
	str = strings.Replace(str, "Y_ITEM", yItem, -1)
	str = strings.Replace(str, "IDENTIFIER", identifier, -1)
	str = strings.Replace(str, "CHART_DATA_URL",
		dataLoc, -1)
	str = strings.Replace(str, "CHART_TITLE", title, -1)

	// Enable steps instead of simple lines.
	if useStep {
		str = strings.Replace(str, "USE_STEP", "s.interpolation = \"step\";", -1)
	} else {
		str = strings.Replace(str, "USE_STEP", "", -1)
	}

	return str
}

// writeMainGraphs writes the HTTP response feeding the end user graphs. It is
// protected under the mutex because we don't want a race to occur where the
// files are being written to when they're attempting to be read below.
func writeMainGraphs(w http.ResponseWriter, r *http.Request) {
	csvWriteMutex.Lock()
	defer csvWriteMutex.Unlock()

	// Load the chainHeight for use in filtering the graphs.
	height := atomic.LoadInt32(&glChainHeight)
	balance := atomic.LoadInt64(&glBalance)
	balanceAmt := dcrutil.Amount(balance)
	stakeDiff := atomic.LoadInt64(&glTicketPrice)
	stakeDiffAmt := dcrutil.Amount(stakeDiff)

	// Page components.
	mainHeaderStr := `<html>
	<head><meta http-equiv="refresh" content="10"></meta></head>
	<body bgcolor="#EAEAFA"><center>
	<h1><img src="webui/dcr_logo.png" width="30" height="25"></img>dcrticketbuyer</h1>`
	mainBodyStr := fmt.Sprintf("<p><b>Height</b>: %v | <b>Balance</b>: %v "+
		"| <b>Ticket Price</b>: %v</p>", height, balanceAmt.ToCoin(),
		stakeDiffAmt.ToCoin())
	importsStr := `<script src="webui/lib/d3/d3.v3.4.8.js"></script>
		    <script src="webui/lib/dimple/dimple.v2.2.0.min.js"></script>`
	mainFooterStr := `<p><font size="1">Created using <a href="http://dimplejs.org/">
	dimple</a> and <a href="https://d3js.org/">d3</a></font></p>
	</center></body></html>`

	// Write the HTML response. Dynamically write the charts.
	io.WriteString(w, mainHeaderStr)
	io.WriteString(w, importsStr)
	io.WriteString(w, mainBodyStr)
	io.WriteString(w, addDimpleChart("prices", "Ticket Prices",
		filepath.Join(csvPath, csvTicketPricesFn), "Height", "Price", "Type",
		height-chartCutoffTicketPrice, false))
	io.WriteString(w, addDimpleChart("mempool", "Tickets in Mempool",
		filepath.Join(csvPath, csvMempoolFn), "Height", "Number", "Type",
		height-chartCutoffMempool, false))
	io.WriteString(w, addDimpleChart("purchased", "Tickets Purchased",
		filepath.Join(csvPath, csvPurchasedFn), "Height", "Number", "Type",
		height-chartCutoffPurchased, true))
	io.WriteString(w, addDimpleChart("fees", "Ticket Fees",
		filepath.Join(csvPath, csvFeesFn), "Height", "FeesPerKB", "Type",
		height-chartCutoffFees, false))
	io.WriteString(w, mainFooterStr)
}
