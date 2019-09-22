package RPC

import (
	"context"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/crypto/sha3"
	"log"
	"math/big"
	"time"
	"walletMigrate/Accounts"
)

type TransactionWithOriginator struct {
	Address  common.Address
	SignedTx *types.Transaction
}

type Client struct {
	client *ethclient.Client
}

func NewClient(rpcURL string) Client {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatal(err)
	}
	return Client{client: client}
}

func (self Client) SendTx(transaction *types.Transaction) error {
	// Connect the client
	return self.client.SendTransaction(context.Background(), transaction)
}

func (self Client) GetGasPrice(modifier float64) *big.Int {
	gasPrice, err := self.client.SuggestGasPrice(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	floatGasPrice := new(big.Float).SetInt(gasPrice)
	floatGasPrice.Mul(floatGasPrice, big.NewFloat(modifier))
	floatGasPrice.Int(gasPrice)

	return gasPrice
}

func (self Client) GetUsedAccounts(accounts []Accounts.Account, pendingNonce bool, gasLimit int64) []Accounts.Account {
	allAccounts := self.getBalances(accounts, pendingNonce)
	return self.getTokenTransfers(allAccounts, gasLimit)
}

func (self Client) AwaitTransactions(transactions []TransactionWithOriginator) {
	time.Sleep(2 * time.Second) //wait a few seconds initially for the transactions to get propagated
	//can't do subscriptions with Infura so just poll every 15 seconds to check if transactions are mined
	for _, transaction := range transactions {
		_, isPending, err := self.client.TransactionByHash(context.Background(), transaction.SignedTx.Hash())
		if err != nil {
			//log.Println("ERROR(C1):", err)
			isPending = true
		}
		if isPending { //if any are still pending then wait break and wait ~for next block
			time.Sleep(15 * time.Second)
			continue
		}
	}
}

func (self Client) GetPendingBalances(accounts []Accounts.Account) []Accounts.Account {
	for x := range accounts {
		bal, err := self.client.PendingBalanceAt(context.Background(), accounts[x].Address)
		if err != nil {
			log.Println("ERROR(M3):", err)
			continue
		}
		accounts[x].Balance.Set(bal)
	}
	return accounts
}

func (self Client) getBalances(accounts []Accounts.Account, pendingNonce bool) []Accounts.Account {
	allAccounts := make([]Accounts.Account, 0)
	for x := range accounts {
		bal, err := self.client.BalanceAt(context.Background(), accounts[x].Address, nil)
		if err != nil {
			log.Println("ERROR(C2):", err)
		}

		var nonce uint64
		if pendingNonce {
			nonce, err = self.client.PendingNonceAt(context.Background(), accounts[x].Address)

		} else {
			nonce, err = self.client.NonceAt(context.Background(), accounts[x].Address, nil)
		}
		if err != nil {
			log.Println("ERROR(C3):", err)
		}

		chainID, err := self.client.NetworkID(context.Background())
		if err != nil {
			log.Println("ERROR(C4):", err)
		}

		accounts[x].Balance = bal
		accounts[x].Nonce = nonce
		accounts[x].ChainId = chainID
		allAccounts = append(allAccounts, accounts[x])
	}
	return allAccounts
}

func (self Client) getTokenTransfers(accounts []Accounts.Account, overrideGasLimit int64) []Accounts.Account {
	allAccounts := make([]Accounts.Account, 0)

	for x := range accounts {
		logsArray, err := self.client.FilterLogs(context.Background(), ethereum.FilterQuery{Topics: [][]common.Hash{
			{common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")}, //topic_0 is transfer
			{}, //anything in topic_1 (could have sent tokens but we are concerned with every token received)
			{accounts[x].Address.Hash()}}}) //topic_2 is recipient of transfer
		if err != nil {
			log.Println("ERROR(C5):", err)
		} else if len(logsArray) > 0 {
			tokens := make(map[string]Accounts.Token)
			for _, logEntry := range logsArray {
				tokenInstance, err := NewToken(logEntry.Address, self.client)
				if err != nil {
					log.Println("ERROR(C6):", err)
					continue
				}
				bal, err := tokenInstance.BalanceOf(&bind.CallOpts{}, accounts[x].Address)
				if err != nil {
					continue
				}
				symbol, err := tokenInstance.Symbol(&bind.CallOpts{})
				if err != nil {
					symbol = "???"
				}

				decimals, err := tokenInstance.Decimals(&bind.CallOpts{})
				if err != nil {
					decimals = 0
				}
				if bal != nil && bal.Cmp(big.NewInt(0)) != 0 {
					hash := sha3.NewLegacyKeccak256()
					hash.Write([]byte("transfer(address,uint256)"))
					methodID := hash.Sum(nil)[:4]

					var data []byte
					data = append(data, methodID...)
					data = append(data, accounts[x].Address.Hash().String()...)
					data = append(data, common.LeftPadBytes(bal.Bytes(), 32)...)

					gasLimit, err := self.client.EstimateGas(context.Background(), ethereum.CallMsg{To: &logEntry.Address, Data: data})
					if err != nil {
						//if we can't get an accurate estimate then we are going to have to guess,
						gasLimit = 40000
					}
					transferGas := int64(float64(gasLimit) * float64(1.7)) //gas estimates are not always correct and sometimes lower than necessary
					if gasLimit > 0 {
						transferGas = overrideGasLimit
					}
					accounts[x].TotalAssetTransfer.Add(accounts[x].TotalAssetTransfer, big.NewInt(transferGas))
					tokens[logEntry.Address.Hex()] = Accounts.Token{Contract: logEntry.Address, Symbol: symbol, Decimals: decimals, Balance: bal, GasLimit: uint64(transferGas)}
				}
			}
			if len(tokens) > 0 {
				for _, token := range tokens {
					accounts[x].Tokens = append(accounts[x].Tokens, token)
				}
			}
			if len(accounts[x].Tokens) > 0 || accounts[x].Balance.Cmp(big.NewInt(0)) != 0 {
				allAccounts = append(allAccounts, accounts[x])
			}
		}
	}

	return allAccounts
}
