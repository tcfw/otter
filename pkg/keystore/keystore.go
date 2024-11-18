package keystore

type KeyStore interface {
	Unlock() error
	Lock() error
}
