// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
)

const (
	// blockConnChanBuffer is the size of the block connected channel buffer.
	blockConnChanBuffer = 1000
)

func main() {
	// Parse the configuration file.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load wallet config: %s\n", err.Error())
		os.Exit(1)
	}
	defer backendLog.Flush()

	// Connect to dcrd RPC server using websockets. Set up the
	// notification handler to deliver blocks through a channel.
	connectChan := make(chan int32, blockConnChanBuffer)
	quit := make(chan struct{})
	ntfnHandlers := dcrrpcclient.NotificationHandlers{
		OnBlockConnected: func(hash *chainhash.Hash, height int32,
			time time.Time, vb uint16) {
			connectChan <- height
		},
	}

	dcrdCerts, err := ioutil.ReadFile(cfg.DcrdCert)
	if err != nil {
		fmt.Printf("Failed to read dcrd cert file at %s: %s\n", cfg.DcrdCert,
			err.Error())
		os.Exit(1)
	}
	log.Debugf("Attempting to connect to dcrd RPC %s as user %s, pass %s "+
		"using certificate located in %s",
		cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdPass, cfg.DcrdCert)
	connCfgDaemon := &dcrrpcclient.ConnConfig{
		Host:         cfg.DcrdServ,
		Endpoint:     "ws",
		User:         cfg.DcrdUser,
		Pass:         cfg.DcrdPass,
		Certificates: dcrdCerts,
	}
	dcrdClient, err := dcrrpcclient.New(connCfgDaemon, &ntfnHandlers)
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
	dcrwCerts, err := ioutil.ReadFile(cfg.DcrwCert)
	if err != nil {
		fmt.Printf("Failed to read dcrwallet cert file at %s: %s\n", cfg.DcrwCert,
			err.Error())
	}
	connCfgWallet := &dcrrpcclient.ConnConfig{
		Host:         cfg.DcrwServ,
		Endpoint:     "ws",
		User:         cfg.DcrwUser,
		Pass:         cfg.DcrwPass,
		Certificates: dcrwCerts,
	}
	log.Debugf("Attempting to connect to dcrwallet RPC %s as user %s, pass %s "+
		"using certificate located in %s",
		cfg.DcrwServ, cfg.DcrwUser, cfg.DcrwPass, cfg.DcrwCert)
	dcrwClient, err := dcrrpcclient.New(connCfgWallet, nil)
	if err != nil {
		fmt.Printf("Failed to start dcrd rpcclient: %s\n", err.Error())
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

	wsm := newWalletSvrManager(purchaser, connectChan, quit)
	go wsm.blockConnectedHandler()

	log.Infof("Daemon and wallet successfully connected, beginning " +
		"to purchase tickets")

	<-quit
	close(quit)
	fmt.Printf("\nClosing ticket buyer.\n")
	os.Exit(1)
}
