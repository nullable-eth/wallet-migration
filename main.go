package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/crypto/sha3"
	"log"
	"math/big"
	"os"
	"sort"
	"walletMigrate/Accounts"
	"walletMigrate/RPC"
)

type settings struct {
	NodeURL            string   `json:"node_url"`                 //your infura access url
	DestinationAddress string   `json:"destination_address"`      //the address to consolidate the funds too
	Mnemonics          []string `json:"mnemonics"`                //seed phrases to generate accounts to consolidate
	PrivateKeys        []string `json:"private_keys"`             //private keys to single accounts
	GasPriceMultiplier float64  `json:"gas_price_multiplier"`     //multiplier for the suggested gas price
	Simulate           bool     `json:"simulate"`                 //do nothing but print out the tx details of what would be done
	NumberOfAccounts   int      `json:"number_of_accounts"`       //for mnemonic phrases this is the number of accounts squared that will be generated
	PendingNonce       bool     `json:"pending_nonce"`            //should begin process with pending nonce (any pending tx must complete before liquidation can occur)
	TransferGasLimit   int64    `json:"token_transfer_gas_limit"` //override calculated token transfer gas limits
}

func main() {
	args := os.Args[1:]
	if len(args) != 1 {
		return
	}

	in := settings{}
	err := json.Unmarshal([]byte(args[0]), &in)
	if err != nil {
		log.Fatal(err)
	}
	if in.NodeURL == "" || !common.IsHexAddress(in.DestinationAddress) || (len(in.Mnemonics) == 0 && len(in.PrivateKeys) == 0) {
		return
	}
	if in.NumberOfAccounts == 0 {
		in.NumberOfAccounts = 3 //default to 3 accounts if not set in input settings
	}

	client := RPC.NewClient(in.NodeURL)
	gasPrice := client.GetGasPrice(in.GasPriceMultiplier) //multiply the suggested gas price by x times
	allAccounts := client.GetUsedAccounts(Accounts.GetAccounts(in.Mnemonics, in.PrivateKeys, in.NumberOfAccounts), in.PendingNonce, in.TransferGasLimit)

	for _, account := range allAccounts {
		fmt.Printf("Address: %s, Nonce: %4d, Token Transfer Gas Needed: %.8f ETH, Balance: %.8f ETH\n", account.Address.Hex(), account.Nonce, Accounts.Eth(account.TotalAssetTransferPrice(gasPrice)), Accounts.Eth(account.Balance))
		for _, token := range account.Tokens {
			fmt.Printf("\tContract Address: %s, Gas Needed: %.8f ETH, Balance(%6v): %.8f\n", token.Contract.Hex(), Accounts.Eth(token.TotalTransferPrice(gasPrice)), token.Symbol, token.DecimalBalance())
		}
		fmt.Println()
	}

	updatedAccounts, gasTransactions := transferGas(gasPrice, allAccounts, make([]RPC.TransactionWithOriginator, 0))
	sendTransactions(client, gasTransactions, in.Simulate)

	tokenTransactions := transferTokens(common.HexToAddress(in.DestinationAddress), gasPrice, updatedAccounts, make([]RPC.TransactionWithOriginator, 0))
	sendTransactions(client, tokenTransactions, in.Simulate)

	if in.Simulate && len(tokenTransactions) > 0 {
		fmt.Println("\nThese transactions might change based on gas left in accounts after token transactions are actually mined:")
	}
	balanceEmptyingTransactions := transferBalances(client, common.HexToAddress(in.DestinationAddress), gasPrice, updatedAccounts, in.Simulate, make([]RPC.TransactionWithOriginator, 0))
	sendTransactions(client, balanceEmptyingTransactions, in.Simulate)
}

func sendTransactions(client RPC.Client, transactions []RPC.TransactionWithOriginator, simulate bool) {
	for _, transaction := range transactions {
		fmt.Printf("From: %s, Nonce: %4d, To: %s, Gas Limit: %6d, Gas Price: %.2f Gwei, Value: %.8f ETH, TxHash: %s, Data: 0x%s \n", transaction.Address.Hex(), transaction.SignedTx.Nonce(), transaction.SignedTx.To().Hex(), transaction.SignedTx.Gas(), Accounts.Gwei(transaction.SignedTx.GasPrice()), Accounts.Eth(transaction.SignedTx.Value()), transaction.SignedTx.Hash().Hex(), hex.EncodeToString(transaction.SignedTx.Data()))
		if simulate {
			continue
		}
		err := client.SendTx(transaction.SignedTx)
		if err != nil {
			log.Println("ERROR(M1):", err)
			continue
		}
	}
	if !simulate {
		client.AwaitTransactions(transactions) //await transactions here
	}
}

func transferGas(gasPrice *big.Int, accounts []Accounts.Account, transactions []RPC.TransactionWithOriginator) ([]Accounts.Account, []RPC.TransactionWithOriginator) {
	var negatives []Accounts.Account
	var positives []Accounts.Account
	//separate accounts based on whether they have enough balance to pay the gas to transfer all their assets out
	for i := range accounts {
		if accounts[i].TotalAssetTransferPrice(gasPrice).Cmp(accounts[i].Balance) > 0 {
			negatives = append(negatives, accounts[i])
			accounts[i].Available.Sub(accounts[i].Balance, accounts[i].TotalAssetTransferPrice(gasPrice))
		} else {
			accounts[i].Available.Sub(accounts[i].Balance, accounts[i].TotalAssetTransferPrice(gasPrice))
			positives = append(positives, accounts[i])
		}
	}

	//sort positives with highest balance first
	sort.Slice(positives, func(i, j int) bool {
		return positives[i].Available.Cmp(positives[j].Available) >= 0
	})
	//sort negatives with the least 'need' first in order to empty as many accounts as possible
	sort.Slice(negatives, func(i, j int) bool {
		return negatives[i].Available.Cmp(negatives[j].Available) <= 0
	})

	//this is the amount it will cost any of the positive accounts just to transfer any gas to a deficient account, each transfer
	transferCost := new(big.Int).Mul(gasPrice, big.NewInt(int64(21000)))
	for x := range negatives {
		for y := range positives {
			totalAmountNeeded := negatives[x].TotalAssetTransferPrice(gasPrice)

			//the amount the positive account needs to give up to the negative account PLUS the cost to transfer it
			totalAmountNeededToTransfer := new(big.Int).Add(totalAmountNeeded, transferCost)

			//excess value that the positive account will have left after transferring to the negative account
			availableAfterTransfer := new(big.Int).Sub(positives[y].Available, totalAmountNeededToTransfer)

			//this account does not have enough to transfer all the negative account needs
			if availableAfterTransfer.Sign() < 0 {
				//figure out how much this account can give
				totalAmountNeeded.Sub(positives[y].Available, transferCost)
				availableAfterTransfer = big.NewInt(0) //this account is going to transfer everything except what it needs
			}

			//this account has something to transfer to the negative account
			if availableAfterTransfer.Sign() >= 0 {
				//create, sign and add a transaction to the gas transfer transactions that will be returned
				tx := types.NewTransaction(positives[y].Nonce, negatives[x].Address, totalAmountNeeded, 21000, gasPrice, nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(positives[y].ChainId), positives[y].PrivateKey)
				if err != nil {
					log.Fatal(err)
				}

				//update the positive balance (even though the tx has not occurred) this will be used in the next iterations of this method to transfer to other negative accounts
				positives[y].Balance.Sub(positives[y].Available, totalAmountNeededToTransfer) //subtract the total cost from the positive accounts balance
				positives[y].Nonce += 1                                                       //each outgoing transaction increases the nonce
				negatives[x].Balance.Add(negatives[x].Balance, totalAmountNeeded)             //the negative account now has some gas
				transactions = append(transactions, RPC.TransactionWithOriginator{Address: positives[y].Address, SignedTx: signedTx})

				//continually keep recursing, sorting and transferring balance until there are no negative accounts left
				//OR there are no positive accounts with any gas left to give (i.e. we did the best we could)
				return transferGas(gasPrice, append(negatives, positives...), transactions)
			}
		}
	}

	return accounts, transactions
}

func transferTokens(destinationAddress common.Address, gasPrice *big.Int, accounts []Accounts.Account, transactions []RPC.TransactionWithOriginator) []RPC.TransactionWithOriginator {
	hash := sha3.NewLegacyKeccak256()
	hash.Write([]byte("transfer(address,uint256)"))
	methodID := hash.Sum(nil)[:4]
	for x := range accounts {
		//sort tokens by greatest balance so we get the most tokens out in case we run out of gas
		sort.Slice(accounts[x].Tokens, func(i, j int) bool {
			return accounts[x].Tokens[i].Balance.Cmp(accounts[x].Tokens[j].Balance) >= 0
		})
		for y := range accounts[x].Tokens {
			transferCost := new(big.Int).Mul(gasPrice, big.NewInt(int64(accounts[x].Tokens[y].GasLimit)))
			//does this account have enough gas to perform this transfer (if we ran out of ETH to transfer for gas we may not be able to get out all tokens)
			if accounts[x].Balance.Cmp(transferCost) >= 0 {
				var data []byte //build the transfer signature to transfer these tokens
				data = append(data, methodID...)
				data = append(data, destinationAddress.Hash().Bytes()...)
				data = append(data, common.LeftPadBytes(accounts[x].Tokens[y].Balance.Bytes(), 32)...)

				//call the token contract (sending 0 eth) but with data transferring all the tokens to the new address
				tx := types.NewTransaction(accounts[x].Nonce, accounts[x].Tokens[y].Contract, big.NewInt(0), accounts[x].Tokens[y].GasLimit, gasPrice, data)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(accounts[x].ChainId), accounts[x].PrivateKey)
				if err != nil {
					log.Println("ERROR(M2):", err)
					continue
				}
				accounts[x].Nonce += 1
				accounts[x].Balance.Sub(accounts[x].Balance, transferCost)
				transactions = append(transactions, RPC.TransactionWithOriginator{Address: accounts[x].Address, SignedTx: signedTx})
			}
		}
	}

	return transactions
}

//all previous pending tx should be mined before calling so we know the correct total balance to transfer out
func transferBalances(client RPC.Client, destinationAddress common.Address, gasPrice *big.Int, accounts []Accounts.Account, simulate bool, transactions []RPC.TransactionWithOriginator) []RPC.TransactionWithOriginator {
	if !simulate {
		accounts = client.GetPendingBalances(accounts)
	}
	for _, account := range accounts {
		signedTx := getBalanceTx(destinationAddress, gasPrice, account)
		if signedTx != nil {
			transactions = append(transactions, RPC.TransactionWithOriginator{Address: account.Address, SignedTx: signedTx})
		}
	}

	return transactions
}

//get a transaction extracting the balance (if the transfer cost exceeds the balance decreasing the gas price until we can extract even the 'dust' left)
func getBalanceTx(destinationAddress common.Address, gasPrice *big.Int, account Accounts.Account) *types.Transaction {
	//how much it costs to send a tx
	transferCost := new(big.Int).Mul(gasPrice, big.NewInt(int64(21000)))
	//what's left after the cost of the transaction
	totalAmountToTransfer := new(big.Int).Sub(account.Balance, transferCost)

	//if there is any amount to transfer then create a tx
	if totalAmountToTransfer.Sign() > 0 && gasPrice.Sign() > 0 {
		tx := types.NewTransaction(account.Nonce, destinationAddress, totalAmountToTransfer, 21000, gasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(account.ChainId), account.PrivateKey)
		if err != nil {
			log.Fatal(err)
		}
		return signedTx
	} else if gasPrice.Sign() > 0 { //if the amount to transfer was negative or zero then decrease the gas price(by 1 WEI) until we can get everything out
		return getBalanceTx(destinationAddress, new(big.Int).Sub(gasPrice, big.NewInt(1000000)), account)
	}

	//if we can't decrease the gas price enough that there is anything left after the cost of the transfer then
	//there is no point in transferring anything
	return nil
}
