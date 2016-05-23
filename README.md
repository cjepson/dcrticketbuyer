dcrticketbuyer
====

[![Build Status](https://travis-ci.org/decred/dcrticketbuyer.png?branch=master)]
(https://travis-ci.org/decred/dcrticketbuyer)

dcrticketbuyer is a smart ticket purchasing bot for the automatic purchase 
of tickets from a Decred wallet. It uses both the daemon and wallet RPC 
to collect data on the blockchain and mempool and use that to make decisions 
about buying tickets.

This project is currently under active development and is in an alpha state.

## Requirements

[Go](http://golang.org) 1.6 or newer.

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

- This project requires the package vendoring tool glide. To install glide, 
run the following:

```bash
$ go get -u github.com/Masterminds/glide
```

**NOTE:** If you are using Go 1.5, you must manually enable the vendor
experiment by setting the `GO15VENDOREXPERIMENT` environment variable to `1`.
This step is not required for Go 1.6.

- Run the following commands to obtain dcrticketbuyer, all dependencies, and 
install it:

```bash
$ cd $GOPATH/src/github.com/decred/
$ git clone https://github.com/decred/dcrticketbuyer
$ cd dcrticketbuyer
$ glide install
$ go install
```

- dcrticketbuyer (and utilities) will now be installed in either 
  ```$GOROOT/bin``` or ```$GOPATH/bin``` depending on your configuration.  If 
  you did not already add the bin directory to your system path during Go 
  installation, we recommend you do so now.

## Updating

#### Linux/BSD/MacOSX/POSIX - Build from Source

- Run the following commands to update dcrticketbuyer, all dependencies, and 
install it:

```bash
$ cd $GOPATH/src/github.com/decred/dcrticketbuyer
$ git pull origin master && glide install
$ go install
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
      --httpsvrbind=        IP to bind for the HTTP server that tracks ticket
                            purchase metrics (default: "" or localhost)
      --httpsvrport=        Server port for the HTTP server that tracks ticket
                            purchase metrics; disabled if 0 (default: 0)
      --httpuipath=         Path for the data and JavaScript libraries for
                            displaying/storing purchase metrics (default: "webui/")
                            (webui/)
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
      --maxpriceabsolute=   The absolute maximum price to pay for a ticket
                            (default: 100.0 Coin) (100)
      --maxpricescale=      Attempt to prevent the stake difficulty from going
                            above this multiplier (>1.0) by manipulation (default:
                            2.0, 0.0 to disable) (2)
      --minpricescale=      Attempt to prevent the stake difficulty from going
                            below this multiplier (<1.0) by manipulation (default:
                            0.7, 0.0 to disable) (0.7)
      --pricetarget=        A target to try to seek setting the stake price to
                            rather than meeting the average price (default: 0.0,
                            0.0 to disable)
      --maxfee=             Maximum ticket fee per KB (default: 1.0 Coin/KB) (1)
      --minfee=             Minimum ticket fee per KB (default: 0.01 Coin/KB) (0.01)
      --feesource=          The fee source to use for ticket fee per KB (median or
                            mean, default: mean) (mean)
      --txfee=              Default regular tx fee per KB, for consolidations
                            (default: 0.01 Coin/KB) (0.01)
      --maxperblock=        Maximum tickets per block, with negative numbers 
                            indicating buy one ticket every 1-in-n blocks (default: 
                            3)
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
sample configuration file is give below, with explanations about what the 
software will do. This is also found in the reposity itself as 
"ticketbuyer-example.conf".

```
#########################################
### Basic Connectivity and Monitoring ###
#########################################
# Login information for the daemon and wallet RPCs.
dcrduser=user
dcrdpass=pass
dcrdserv=127.0.0.1:19109
dcrdcert=path/to/.dcrd
dcrwuser=user
dcrwpass=pass
dcrwserv=127.0.0.1:19110
dcrwcert=path/to/.dcrwallet

# Enable the HTTP monitoring server bound to localhost,
# port 7770. Access in browser with:
#   http://localhost:7770
# The optional parameter httpsvrbind allows you to 
# bind the server externally or to another IP.
httpsvrport=7770

# The path to store monitoring logs in along with all 
# the libraries/data for the HTTP server. This folder 
# must constain the JavaScript libraries d3 and dimple 
# for this to function correctly.
# By default, the path is "webui/" in PWD.
httpuipath=webui/

# Enable testnet.
testnet=true

##################################
### Basic Wallet Configuration ###
##################################
# The wallet account to use to buy tickets. If unset, 
# it is the default account.
accountname=default

# Purchase at most 5 tickets per block. If this is set to 
# negative numbers, the purchaser purchases 1 ticket every
# abs(n) blocks.
maxperblock=5

# Stop buying tickets if the mempool has more than 
# 40 tickets in in.
maxinmempool=40

# Never spend more than 100.0 DCR on a ticket.
maxpriceabsolute=100.0

# Try to leave this many coins remaining in the 
# wallet.
balancetomaintain=500.0

# All purchased tickets will expire in 16 blocks 
# if they fail to exit the mempool and enter the 
# blockchain.
expirydelta=16

# Give ticket voting rights to this address. Comment 
# this out to use a local address from the wallet it 
# is connect to.
ticketaddress=TsfjLsBv6aKQoLmfPJUn3w6r6AB2JajoMrW

# Send 1.23% pool fees to this address. Comment this out 
# to disable pool mode.
pooladdress=TsYxLCKr7qtsDxYaJ32zAQg3rFao6aGAHXh
poolfees=1.23

########################################
### Price Manipulation and Targeting ###
########################################
# Do not purchase tickets if it will move the difficulty 
# above this multiplier for the 'ideal' ticket price. 
# e.g. if the ideal ticket price is 15.0 DCR, the program 
# will prevent purchasing tickets if it drives the next 
# stake difficulty above 30.0 DCR.
# The 'ideal' ticket price is calculated with every new 
# block as (VWAP + ticketPoolAvgValue)/2.
maxpricescale=2.0

# Force the wallet to purchase tickets if the price 
# is estimated to fall below this proportional amount 
# of the 'ideal' ticket price. This prevents the stake 
# difficulty from falling too low.
# e.g. if the ideal ticket price is 15.0 DCR, the program 
# will force the purchasing tickets if it detects that 
# the next stake difficulty will be below 10.5 DCR.
minpricescale=0.7

# The wallet normally targets the purchase of tickets 
# to meet the average price of the market. A wallet 
# with a large amount of DCR may wish to push the 
# price of tickets higher. e.g. if the current 
# average price of a ticket is 15 DCR, the user may 
# think that tickets are too cheap and wants to 
# move the average to 18 DCR. The user would then 
# set this value to 18 DCR.
# A value of 0.0 disables this feature, and it is 
# recommended to be left disabled unless you think 
# you have enough DCR to be able to strongly move 
# the average price.
pricetarget=0.0

###################################
### Ticket and Transaction Fees ###
###################################
# The maximum allowable fee in a competitive market 
# for tickets is 1.00 DCR/KB.
# Note that by default, the maximum the wallet will 
# allow you to set is 1.00 DCR/KB. You can allow 
# higher fees by turning on the --allowhighfees flag 
# when starting wallet.
maxfee=1.00

# The minimum allowable fee in a competitive market 
# for tickets is 0.01 DCR/KB.
minfee=0.01

# Use the mean of block or difficulty window periods 
# to determine the fees to use in your tickets.
# Alternatively the median may be used with 'median'.
feesource=mean

# The proportion to use above the mean/median for 
# your tickets. e.g. If the network mean for the 
# last 11 blocks is 0.10, use 105%*0.10 = 0.105 
# DCR/KB as your ticket fee.
feetargetscaling=1.05

# Set the transaction fees to 0.01 DCR/KB. This fee is 
# used when generating consolidations of smaller UTXOs 
# that are immediately consumed by a ticket purchase.
txfee=0.01

# The number of previous blocks to average to calculate 
# what fees to use. If there are not enough blocks in 
# this window yet, the software will scan old difficulty 
# periods for the one with the price closest to the 
# current price and use the average fees from that 
# period for deciding what fee to use.
blockstoavg=11
```

The program may then be run with

```bash
$ dcrtickeybuyer -C ticketbuyer.conf
```

To enable more explicit output, set the debug level 
for the ticket buyer to DEBUG or TRACE:
```bash
$ dcrtickeybuyer -C ticketbuyer.conf -d TKBY=debug
```


## IRC

- irc.freenode.net
- channel #decred-dev
- [webchat](https://webchat.freenode.net/?channels=decred-dev)

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrticketbuyer/issues)
is used for this project.

## License

dcrticketbuyer is licensed under the [copyfree](http://copyfree.org) ISC 
License.
