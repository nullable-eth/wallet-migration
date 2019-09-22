package Accounts

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil/hdkeychain"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/tyler-smith/go-bip39"
	"log"
	"math"
	"math/big"
	"strings"
)

type Account struct {
	PrivateKey         *ecdsa.PrivateKey
	PublicKey          *ecdsa.PublicKey
	Address            common.Address
	Tokens             []Token
	Balance            *big.Int
	TotalAssetTransfer *big.Int
	Available          *big.Int
	Nonce              uint64
	ChainId            *big.Int
}

type Token struct {
	Contract common.Address
	Balance  *big.Int
	Symbol   string
	Decimals uint8
	GasLimit uint64
}

func (self Token) TotalTransferPrice(gasPrice *big.Int) *big.Int {
	return new(big.Int).Mul(gasPrice, big.NewInt(int64(self.GasLimit)))
}

func (self Token) DecimalBalance() *big.Float {
	if self.Decimals == 0 {
		return new(big.Float).SetInt(self.Balance)
	}
	return new(big.Float).Quo(new(big.Float).SetInt(self.Balance), big.NewFloat(math.Pow10(int(self.Decimals))))
}

func (self Account) TotalAssetTransferPrice(gasPrice *big.Int) *big.Int {
	return new(big.Int).Mul(gasPrice, self.TotalAssetTransfer)
}

func Gwei(amount *big.Int) *big.Float {
	return new(big.Float).Quo(new(big.Float).SetInt(amount), new(big.Float).SetInt(big.NewInt(params.GWei)))
}
func Eth(amount *big.Int) *big.Float {
	return new(big.Float).Quo(new(big.Float).SetInt(amount), new(big.Float).SetInt(big.NewInt(params.Ether)))
}

func GetAccounts(mnemonics []string, privateKeys []string, numberOfAccounts int) []Account {
	mapAccounts := make(map[string]Account, 0)

	for _, mnemonic := range mnemonics {
		accounts, err := accountsFromMnemonic(mnemonic, numberOfAccounts)
		if err != nil {
			log.Fatal(err)
		}
		for _, account := range accounts {
			mapAccounts[account.Address.Hex()] = account
		}
	}

	for _, privateKey := range privateKeys {
		account, err := accountFromPrivateKey(privateKey)
		if err != nil {
			log.Fatal(err)
		}
		mapAccounts[account.Address.Hex()] = *account
	}

	allAccounts := make([]Account, 0)

	for _, account := range mapAccounts {
		allAccounts = append(allAccounts, account)
	}
	return allAccounts
}

//because there is no standard used in ethereum on whether to vary the change or address_index to create new accounts
//(i.e. metamask uses one method and commonly mobile wallets use another) this will actually generate numberOfAccounts squared
//we will then have to check the balance or nonce to determine if they are used.
func accountsFromMnemonic(mnemonic string, numberOfAccounts int) ([]Account, error) {
	if mnemonic == "" {
		return nil, errors.New("mnemonic is required")
	}

	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, errors.New("mnemonic is invalid:" + mnemonic)

	}

	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")

	if err != nil {
		return nil, err
	}

	masterKey, err := hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
	if err != nil {
		return nil, err
	}

	allAccounts := make([]Account, 0)
	for account := 0; account <= 0; account++ {
		for change := 0; change < numberOfAccounts; change++ {
			for addressIndex := 0; addressIndex < numberOfAccounts; addressIndex++ {
				//https://github.com/bitcoin/bips/blob/master/bip-0044.mediawiki#Path_levels
				dPath, err := accounts.ParseDerivationPath(fmt.Sprintf("m/44'/60'/%d'/%d/%d", account, change, addressIndex))
				if err != nil {
					return nil, err
				}
				privateKey, err := derivePrivateKey(masterKey, dPath)
				if err != nil {
					return nil, err
				}
				publicKey, err := derivePublicKey(privateKey)
				if err != nil {
					return nil, err
				}
				address, err := deriveAddress(publicKey)
				if err != nil {
					return nil, err
				}

				allAccounts = append(allAccounts, Account{PrivateKey: privateKey, PublicKey: publicKey, Address: address, Tokens: make([]Token, 0), TotalAssetTransfer: big.NewInt(0), Balance: big.NewInt(0), Available: big.NewInt(0)})
			}
		}
	}

	return allAccounts, nil
}

func accountFromPrivateKey(pkString string) (*Account, error) {
	pkString = strings.Replace(pkString, "0x", "", 1)
	privateKey, err := crypto.HexToECDSA(pkString)
	if err != nil {
		return nil, err
	}
	publicKey, err := derivePublicKey(privateKey)
	if err != nil {
		return nil, err
	}
	address, err := deriveAddress(publicKey)
	if err != nil {
		return nil, err
	}

	return &Account{PrivateKey: privateKey, PublicKey: publicKey, Address: address, Tokens: make([]Token, 0), TotalAssetTransfer: big.NewInt(0), Balance: big.NewInt(0), Available: big.NewInt(0)}, nil
}

// DerivePrivateKey derives the private key of the derivation path.
func derivePrivateKey(key *hdkeychain.ExtendedKey, path accounts.DerivationPath) (*ecdsa.PrivateKey, error) {
	var err error
	for _, n := range path {
		key, err = key.Child(n)
		if err != nil {
			return nil, err
		}
	}

	privateKey, err := key.ECPrivKey()
	privateKeyECDSA := privateKey.ToECDSA()
	if err != nil {
		return nil, err
	}

	return privateKeyECDSA, nil
}

// DerivePublicKey derives the public key of the derivation path.
func derivePublicKey(privateKey *ecdsa.PrivateKey) (*ecdsa.PublicKey, error) {
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("failed to get public key")
	}

	return publicKeyECDSA, nil
}

// DeriveAddress derives the account address of the derivation path.
func deriveAddress(publicKeyECDSA *ecdsa.PublicKey) (common.Address, error) {
	address := crypto.PubkeyToAddress(*publicKeyECDSA)
	return address, nil
}
