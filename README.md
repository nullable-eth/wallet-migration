# Wallet Migration
I work in ethereum exchange and mobile wallet development, if you are like me you might have multiple software wallets with various assets in them for use or testing.  Perhaps you have mutliple assets spread across multiple wallets, maybe some of the wallets don't have `eth` to transfer the assets out, it is too time consuming to figure out the `eth` needs of each account, send each asset and them empty the `eth`.  Or maybe you are just concerned that your seed phrase has been compromised and you want to drain your account quickly to a _safe_ account.

This application allows to you take multiple wallets and consolidate them into one destination.  It determines if each account has enough `eth` to transfer the assets and sends `eth` from other accounts if necessary to cover the gas costs.  Then sends all tokens from all accounts to the destination and finally empties any `eth` left behind.

Obviously it is not a good idea to input your private keys or seed phrases in to the computer but if you are immediately condolidating them to a _safe_ destination then the risks are limited.  Whatever the reason for using the application you should _**never use the seed phrases/private keys again!**_

# Installation
```
install golang
clone repo
cd repo directory
go get
go build
```

# Running
>walletMigrate "{\"node_url\": \"https:\/\/mainnet.infura.io\/v3\/APIKEYGOESHERE\",\"destination_address\": \"0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B\",\"mnemonics\": [\"seed phrases go here usually twelve to twenty four words perhaps bicycle\"],\"private_keys\": [\"0xpr1vat3k3y1nh3xad3c1mal\"],\"gas_price_multiplier\": 1.5,\"simulate\": true,\"number_of_accounts\": 1,\"pending_nonce\": false,\"token_transfer_gas_limit\": 100000}"
```
{
  "node_url": "https:\/\/mainnet.infura.io\/v3\/APIKEYGOESHERE",
  "destination_address": "0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B",
  "mnemonics": ["seed phrases go here usually twelve to twenty four words perhaps bicycle"    
  ],
  "private_keys": ["0xpr1vat3k3y1nh3xad3c1mal"    
  ],
  "gas_price_multiplier": 1.5,
  "simulate": false,
  "number_of_accounts": 1,
  "pending_nonce": true,
  "token_transfer_gas_limit": 100000
}
```
>- node_url: this is a link to an ethereum node, you can sign up for a fre infura account and get an api key or run your own node
>- destination_address: where you want the consolidated accounts to go to
>- mnemonics: an array of strings with 12+ word seed phrases to account
>- private_keys: single private key hex with or without 0x prefix that is used for a single account
>- gas_price_multiplier: the ethereum node suggests a gas price, this multiplier allows you to increase the gas price you pay for these asset transfer and `eth` transfers.  To get all transactions mined quicker increase this 2, 2.5, 3 times the current _safe_ gas price.
>- simulate: just prints accounts and asset balances and the transactions that would be submitted
>- number_of_accounts: for each seed phrases many accounts can be generated, this is the number of accounts squared that will be generated.  Because not all `eth` wallets follow the same standard for generating account paths this increases both the `change` element and the `address index` element of the derivation path (m/44'/60'/0'/{change}/{address index}).  https://github.com/bitcoin/bips/blob/master/bip-0044.mediawiki#Path_levels
>- pending_nonce: if you want to preserve any pending transactions that may already be submitted for the account **NOTE: if you have a long pending tx on an account the migration of the assets will be delayed until this tranasction completes**
>- token_transfer_gas_limit: override the calculated gas limits for sending assets and use this number.  Use this if assets don't seem to transfer properly because the node/smart contract estimate the gas needed incorrectly.
