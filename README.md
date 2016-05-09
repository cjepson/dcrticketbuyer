dcrticketbuyer
====

[![Build Status](https://travis-ci.org/cjepson/dcrticketbuyer.png?branch=master)]
(https://travis-ci.org/cjepson/dcrticketbuyer)

dcrticketbuyer is a smart ticket purchasing bot for the automatic purchase 
of tickets from a Decred wallet. It uses both the daemon and wallet RPC 
to collect data on the blockchain and mempool and use that to make decisions 
about buying tickets.

This project is currently under active development and is in an alpha state.

## Requirements

[Go](http://golang.org) 1.5 or newer.

## Installation

#### Linux/BSD/MacOSX/POSIX - Build from Source

- Install Go according to the installation instructions here:
  http://golang.org/doc/install

- Ensure Go was installed properly and is a supported version:

```bash
$ go version
$ go env GOROOT GOPATH
```

NOTE: The `GOROOT` and `GOPATH` above must not be the same path.  It is
recommended that `GOPATH` is set to a directory in your home directory such as
`~/goprojects` to avoid write permission issues.

- Run the following command to obtain dcrticketbuyer, all dependencies, and 
install it:

```bash
$ go get -u -v github.com/cjepson/dcrticketbuyer/...
```

- dcrticketbuyer (and utilities) will now be installed in either 
  ```$GOROOT/bin``` or ```$GOPATH/bin``` depending on your configuration.  If 
  you did not already add the bin directory to your system path during Go 
  installation, we recommend you do so now.

## Updating

#### Linux/BSD/MacOSX/POSIX - Build from Source

- Run the following command to update btcd, all dependencies, and install it:

```bash
$ go get -u -v github.com/cjepson/dcrticketbuyer/...
```

## Getting Started

dcrticketbuyer has several configuration options avilable to tweak how it 
runs. The user should read over these options carefully and create a 
configuration file that specifies exactly the type of behavior they would 
prefer for ticket purchase. For reference, these options are given below.

```
  -C, --configfile=         Path to configuration file
                            (../dcrticketbuyer/ticketbuyer.conf)
  -V, --version             Display version information and exit
      --testnet             Use the test network (default mainnet)
      --simnet              Use the simulation test network (default mainnet)
  -d, --debuglevel=         Logging level {trace, debug, info, warn, error,
                            critical} (info)
      --logdir=             Directory to log output
                            (../dcrticketbuyer/logs)
      --dcrduser=           Daemon RPC user name
      --dcrdpass=           Daemon RPC password
      --dcrdserv=           Hostname/IP and port of dcrd RPC server to connect to
                            (default localhost:9109, testnet: localhost:19109,
                            simnet: localhost:18556)
      --dcrdcert=           File containing the dcrd certificate file
                            (../.dcrd/rpc.cert)
      --dcrwuser=           Wallet RPC user name
      --dcrwpass=           Wallet RPC password
      --dcrwserv=           Hostname/IP and port of dcrwallet RPC server to connect
                            to (default localhost:9110, testnet: localhost:19110,
                            simnet: localhost:18557)
      --dcrwcert=           File containing the dcrwallet certificate file
                            (../.dcrwallet/rpc.cert)
      --noclienttls         Disable TLS for the RPC client -- NOTE: This is only
                            allowed if the RPC client is connecting to localhost
      --accountname=        Name of the account to buy tickets from (default:
                            default) (default)
      --ticketaddress=      Address to give ticket voting rights to
      --pooladdress=        Address to give pool fees rights to
      --poolfees=           The pool fee base rate for a given pool as a percentage
                            (0.01 to 100.00%)
      --maxprice=           Maximum price to pay for a ticket (default: 100.0 Coin)
                            (100)
      --minprice=           Attempt to prevent the stake difficulty from going
                            below this value by manipulation (default: 0.0 Coin)
      --maxfee=             Maximum ticket fee per KB (default: 1.0 Coin/KB) (1)
      --minfee=             Minimum ticket fee per KB (default: 0.01 Coin/KB) (0.01)
      --feesource=          The fee source to use for ticket fee per KB (median or
                            mean, default: mean) (mean)
      --txfee=              Default regular tx fee per KB, for consolidations
                            (default: 0.01 Coin/KB) (0.01)
      --maxperblock=        Maximum tickets per block (default: 3) (3)
      --balancetomaintain=  Balance to try to maintain in the wallet
      --highpricepenalty=   The exponential penalty to apply to the number of
                            tickets to purchase above the mean ticket pool price
                            (default: 1.3) (1.3)
      --blockstoavg=        Number of blocks to average for fees calculation
                            (default: 11) (11)
      --feetargetscaling=   The amount above the mean fee in the previous blocks to
                            purchase tickets with, proportional e.g. 1.05 = 105%
                            (default: 1.05) (1.05)
      --dontwaitfortickets  Don't wait until your last round of tickets have
                            entered the blockchain to attempt to purchase more
      --maxinmempool=       The maximum number of tickets allowed in mempool before
                            purchasing more tickets (default: 0)
      --expirydelta=        Number of blocks in the future before the ticket expires
                            (default: 16) (16)
```

#### Linux/BSD/POSIX/Source

It is recommended to use a configuration file to fine tune the software. A 
sample configuration file is give below.

```
dcrduser=USER
dcrdpass=PASSWORD
dcrdserv=127.0.0.1:9876
dcrwuser=USER
dcrwpass=PASSWORD
dcrwserv=127.0.0.1:4321
simnet=true
maxperblock=5
ticketaddress=SsfRowMCXPEc3gmt3KVo8q5h9dfRqqxkqM5
pooladdress=Ssha8j5pm79HcPoQiEEYcP7dp7pUBVbiTJu
poolfees=1.23
txfee=0.00001
```

The program may then be run with

```bash
$ dcrtickeybuyer -C ticketbuyer.conf
```

## IRC

- irc.freenode.net
- channel #decred-dev
- [webchat](https://webchat.freenode.net/?channels=decred-dev)

## Issue Tracker

The [integrated github issue tracker](https://github.com/cjepson/dcrticketbuyer/issues)
is used for this project.

## License

dcrticketbuyer is licensed under the [copyfree](http://copyfree.org) ISC 
License.
