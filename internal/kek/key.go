//go:build !(macos || darwin)

package kek

func UnsealKey(sealedValue []byte) ([]byte, error) {
	return sealedValue, nil
}

func SealKey(plaintextValue []byte) ([]byte, error) {
	return plaintextValue, nil
}
