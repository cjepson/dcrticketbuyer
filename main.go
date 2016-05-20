// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

// Global variables are displayed below that are mainly used for the purposes
// of the HTTP server.
var (
	// chainHeight is the global chainHeight. It must be accessed with
	// atomic operators.
	glChainHeight = int32(0)

	// glBalance is the global balance. It is updated at start up and
	// after every round of ticket purchases. It must be accessed with
	// atomic operators.
	glBalance = int64(0)

	// glTicketPrice is the global ticket price. It is updated at
	// start up and after every round of ticket purchases.
	glTicketPrice = int64(0)
)

const (
	// blockConnChanBuffer is the size of the block connected channel buffer.
	blockConnChanBuffer = 100
)

// syncGlobalsStartup syncs the globals for the HTTP server on startup.
func syncGlobalsStartup(dcrdClient *dcrrpcclient.Client,
	dcrwClient *dcrrpcclient.Client, cfg *config) error {
	_, height, err := dcrdClient.GetBestBlock()
	if err != nil {
		return err
	}
	atomic.StoreInt32(&glChainHeight, height)

	bal, err := dcrwClient.GetBalanceMinConfType(cfg.AccountName,
		0, "spendable")
	if err != nil {
		return err
	}
	atomic.StoreInt64(&glBalance, int64(bal))

	sd, err := dcrdClient.GetStakeDifficulty()
	if err != nil {
		return err
	}
	nsdAmt, err := dcrutil.NewAmount(sd.NextStakeDifficulty)
	if err != nil {
		return err
	}
	atomic.StoreInt64(&glTicketPrice, int64(nsdAmt))

	return nil
}

func main() {
	// Parse the configuration file.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load ticketbuyer config: %s\n", err.Error())
		os.Exit(1)
	}
	defer backendLog.Flush()

	dcrrpcclient.UseLogger(clientLog)

	// Initialize the CSV files for the HTTP server if needed.
	if cfg.HttpSvrPort != 0 {
		err := initCsvFiles()
		if err != nil {
			fmt.Printf("Failed to init https files: %s\n", err.Error())
			os.Exit(1)
		}
	}

	// Connect to dcrd RPC server using websockets. Set up the
	// notification handler to deliver blocks through a channel.
	connectChan := make(chan int32, blockConnChanBuffer)
	quit := make(chan struct{})
	ntfnHandlersDaemon := dcrrpcclient.NotificationHandlers{
		OnBlockConnected: func(hash *chainhash.Hash, height int32,
			time time.Time, vb uint16) {
			connectChan <- height
		},
	}

	var dcrdCerts []byte
	if !cfg.DisableClientTLS {
		dcrdCerts, err = ioutil.ReadFile(cfg.DcrdCert)
		if err != nil {
			fmt.Printf("Failed to read dcrd cert file at %s: %s\n", cfg.DcrdCert,
				err.Error())
			os.Exit(1)
		}
	}
	log.Debugf("Attempting to connect to dcrd RPC %s as user %s "+
		"using certificate located in %s",
		cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdCert)
	connCfgDaemon := &dcrrpcclient.ConnConfig{
		Host:         cfg.DcrdServ,
		Endpoint:     "ws",
		User:         cfg.DcrdUser,
		Pass:         cfg.DcrdPass,
		Certificates: dcrdCerts,
		DisableTLS:   cfg.DisableClientTLS,
	}
	dcrdClient, err := dcrrpcclient.New(connCfgDaemon, &ntfnHandlersDaemon)
	if err != nil {
		fmt.Printf("Failed to start dcrd rpcclient: %s\n", err.Error())
		os.Exit(1)
	}

	// Register for block connection notifications.
	if err := dcrdClient.NotifyBlocks(); err != nil {
		fmt.Printf("Failed to start register daemon rpc client for  "+
			"block notifications: %s\n", err.Error())
		os.Exit(1)
	}

	// Connect to the dcrwallet server RPC client.
	var dcrwCerts []byte
	if !cfg.DisableClientTLS {
		dcrwCerts, err = ioutil.ReadFile(cfg.DcrwCert)
		if err != nil {
			fmt.Printf("Failed to read dcrwallet cert file at %s: %s\n",
				cfg.DcrwCert, err.Error())
		}
	}
	connCfgWallet := &dcrrpcclient.ConnConfig{
		Host:         cfg.DcrwServ,
		Endpoint:     "ws",
		User:         cfg.DcrwUser,
		Pass:         cfg.DcrwPass,
		Certificates: dcrwCerts,
		DisableTLS:   cfg.DisableClientTLS,
	}
	log.Debugf("Attempting to connect to dcrwallet RPC %s as user %s "+
		"using certificate located in %s",
		cfg.DcrwServ, cfg.DcrwUser, cfg.DcrwCert)
	dcrwClient, err := dcrrpcclient.New(connCfgWallet, nil)
	if err != nil {
		fmt.Printf("Failed to start dcrd rpcclient: %s\n", err.Error())
		os.Exit(1)
	}

	err = syncGlobalsStartup(dcrdClient, dcrwClient, cfg)
	if err != nil {
		fmt.Printf("Failed to start sync globals on startup: %s\n", err.Error())
		os.Exit(1)
	}

	// Ctrl-C to kill.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			quit <- struct{}{}
		}
	}()

	purchaser, err := newTicketPurchaser(cfg, dcrdClient, dcrwClient)
	if err != nil {
		fmt.Printf("Failed to start purchaser: %s\n", err.Error())
		os.Exit(1)
	}

	wsm := newPurchaseManager(purchaser, connectChan, quit)
	go wsm.blockConnectedHandler()

	log.Infof("Daemon and wallet successfully connected, beginning " +
		"to purchase tickets")

	// If the HTTP server is enabled, spin it up and begin
	// displaying the front page locally.
	if cfg.HttpSvrPort > 0 {
		go func() {
			port := strconv.Itoa(cfg.HttpSvrPort)
			http.HandleFunc("/", writeMainGraphs)
			http.HandleFunc("/webui/", func(w http.ResponseWriter,
				r *http.Request) {
				http.ServeFile(w, r, r.URL.Path[1:])
			})
			err := http.ListenAndServe(cfg.HttpSvrBind+":"+port, nil)
			if err != nil {
				log.Errorf("Failed to bind http server: %s", err.Error())
			}
		}()
	}

	<-quit
	close(quit)
	dcrdClient.Disconnect()
	dcrwClient.Disconnect()
	fmt.Printf("\nClosing ticket buyer.\n")
	os.Exit(1)
}
