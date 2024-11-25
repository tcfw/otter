//go:build macos || darwin

package kek

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/keybase/go-keychain"
)

const (
	service     = "otter"
	account     = "datastore"
	accessGroup = "ooshoo7ijoog1eeZahpait6G.au.tcfw.otter"
)

var (
	errNotExists = errors.New("key does not exist")
)

func UnsealKey(sealedValue []byte) ([]byte, error) {
	k, err := getKeyChainSealingKey()
	if err != nil {
		return nil, fmt.Errorf("getting KEK: %w", err)
	}
	aesK, err := aes.NewCipher(k)
	if err != nil {
		return nil, fmt.Errorf("creating KEK cipher: %w", err)
	}

	aeadC, err := cipher.NewGCM(aesK)
	if err != nil {
		return nil, fmt.Errorf("creating AES-GCM cipher for KEK: %w", err)
	}

	ns := aeadC.NonceSize()
	nonce := sealedValue[:ns]
	data := sealedValue[ns:]
	pt, err := aeadC.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("unsealing key: %w", err)
	}

	return pt, nil
}

func SealKey(plaintextValue []byte) ([]byte, error) {
	k, err := getKeyChainSealingKey()
	if err != nil {
		if errors.Is(err, errNotExists) {
			iv, err := createSealingKey()
			if err != nil {
				return nil, fmt.Errorf("creating new KEK: %w", err)
			}
			if err := createKeyChainSealingKey(iv); err != nil {
				return nil, fmt.Errorf("storing KEK in keychain: %w", err)
			}
			k = iv
		}
	}

	aesK, err := aes.NewCipher(k)
	if err != nil {
		return nil, fmt.Errorf("creating KEK cipher: %w", err)
	}

	aeadC, err := cipher.NewGCM(aesK)
	if err != nil {
		return nil, fmt.Errorf("creating AES-GCM cipher for KEK: %w", err)
	}

	nonce := make([]byte, aeadC.NonceSize())
	if n, err := rand.Read(nonce); err != nil || n != aeadC.NonceSize() {
		return nil, fmt.Errorf("creating nonce: %w", err)
	}

	ct := aeadC.Seal(nil, nonce, plaintextValue, nil)
	nonce = append(nonce, ct...)

	return nonce, nil
}

func createSealingKey() ([]byte, error) {
	key := make([]byte, 32)
	n, err := rand.Read(key)
	if err != nil {
		return nil, fmt.Errorf("reading crypto generator: %w", err)
	}

	if n != 32 {
		return nil, errors.New("didn't read full generation into buffer")
	}

	return key, nil
}

func createKeyChainSealingKey(k []byte) error {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(service)
	item.SetAccount(account)
	item.SetAccessGroup(accessGroup)
	item.SetData([]byte(hex.EncodeToString(k)))
	item.SetSynchronizable(keychain.SynchronizableNo)
	item.SetAccessible(keychain.AccessibleWhenUnlocked)
	if err := keychain.AddItem(item); err != nil {
		return fmt.Errorf("adding sealing key to keychain")
	}

	return nil
}

func getKeyChainSealingKey() ([]byte, error) {
	query := keychain.NewItem()
	query.SetSecClass(keychain.SecClassGenericPassword)
	query.SetService(service)
	query.SetAccount(account)
	query.SetAccessGroup(accessGroup)
	query.SetMatchLimit(keychain.MatchLimitOne)
	query.SetReturnData(true)
	query.SetReturnAttributes(true)
	results, err := keychain.QueryItem(query)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, errNotExists
	}

	nhv, err := hex.DecodeString(string(results[0].Data))
	if err != nil {
		return nil, fmt.Errorf("decoding KEK from hex: %w", err)
	}

	return nhv, nil
}

func deleteKeyChainSealingKey(really bool) error {
	if !really {
		return nil
	}

	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(service)
	item.SetAccount(account)
	return keychain.DeleteItem(item)
}
