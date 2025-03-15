package internal

import (
	"context"
	stdCrypto "crypto"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/tcfw/otter/internal/kek"
	v1api "github.com/tcfw/otter/pkg/api"
	"github.com/tcfw/otter/pkg/config"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/keystore"
	"go.uber.org/zap"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/sha3"
)

const (
	dekSize       = 32
	storageKeyLen = 32
)

// HostKey gets or generates an Ed25519 key to be used by a libp2p host
func (o *Otter) HostKey() (crypto.PrivKey, error) {
	cv := o.GetConfig(config.P2P_HostKey)
	if cv == nil {
		//Generate
		priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generating new host key: %w", err)
		}

		pb, err := crypto.MarshalPrivateKey(priv)
		if err != nil {
			return nil, fmt.Errorf("generating new host key: %w", err)
		}

		if err := o.SetAndStoreConfig(config.P2P_HostKey, hex.EncodeToString(pb)); err != nil {
			return nil, fmt.Errorf("storing new host key: %w", err)
		}

		return priv, nil
	}

	hexVal, ok := cv.(string)
	if !ok {
		return nil, errUnsupportedSettingType
	}

	v, err := hex.DecodeString(hexVal)
	if err != nil {
		return nil, fmt.Errorf("parsing host key: %w", err)
	}

	return crypto.UnmarshalPrivateKey(v)
}

// DiskKey reads the sealed DEK from the config file, decrypts it using the KEK
// If a DEK does not exist, a new one is created and stored in the config
func (o *Otter) DiskKey() ([]byte, error) {
	sealedDEK := o.GetConfigAs("", config.Storage_SealedDEK).(string)
	if sealedDEK == "" {
		k, err := newDEK()
		if err != nil {
			return nil, fmt.Errorf("creating new DEK: %w", err)
		}
		sealedDEK, err := kek.SealKey(k)
		if err != nil {
			return nil, fmt.Errorf("sealing DEK for storage: %w", err)
		}
		err = o.SetAndStoreConfig(config.Storage_SealedDEK, hex.EncodeToString(sealedDEK))
		if err != nil {
			return nil, fmt.Errorf("storing sealing DEK: %w", err)
		}
		return k, nil
	}

	rDEK, err := hex.DecodeString(sealedDEK)
	if err != nil {
		return nil, fmt.Errorf("decoding sealed DEK: %w", err)
	}

	k, err := kek.UnsealKey(rDEK)
	if err != nil {
		return nil, fmt.Errorf("unsealing DEK: %w", err)
	}

	return k, nil
}

func (o *Otter) KeyStore() keystore.KeyStore { return o }

func (o *Otter) Keys(ctx context.Context) ([]id.PublicID, error) {
	ss, err := o.sc.System()
	if err != nil {
		return nil, err
	}

	q, err := ss.Query(ctx, query.Query{Prefix: systemPrefix_Keys, KeysOnly: true})
	if err != nil {
		return nil, err
	}

	keys, err := q.Rest()
	if err != nil {
		return nil, err
	}

	fkeys := []id.PublicID{}
	for _, k := range keys {
		fkeys = append(fkeys, id.PublicID(strings.TrimPrefix(k.Key, systemPrefix_Keys)))
	}

	return fkeys, nil
}

func (o *Otter) privateKeys(ctx context.Context) ([]id.PrivateKey, error) {
	ss, err := o.sc.System()
	if err != nil {
		return nil, err
	}

	q, err := ss.Query(ctx, query.Query{Prefix: systemPrefix_Keys})
	if err != nil {
		return nil, err
	}

	keys, err := q.Rest()
	if err != nil {
		return nil, err
	}

	fkeys := []id.PrivateKey{}
	for _, k := range keys {
		fkeys = append(fkeys, id.PrivateKey(strings.TrimPrefix(string(k.Value), systemPrefix_Keys)))
	}

	return fkeys, nil
}

// newDEK constructs a new Data Encryption Key (DEK)
func newDEK() ([]byte, error) {
	dek := make([]byte, dekSize)
	if n, err := rand.Read(dek); err != nil || n != dekSize {
		return nil, fmt.Errorf("reading crypto for DEK: %w", err)
	}

	return dek, nil
}

// privateStorageAEAD constructs a AEAD cipher (XChaCha20Poly1305) for encrypting private storage
func privateStorageAEAD(sk []byte) (cipher.AEAD, error) {
	aead, err := chacha20poly1305.NewX(sk)
	if err != nil {
		return nil, fmt.Errorf("newing Xchacha20: %w", err)
	}

	return aead, nil
}

// privateStorageSeal seals protected data via AEAD
func privateStorageSeal(ci cipher.AEAD, ad []byte) cryptoSealUnsealer {
	return func(ctx context.Context, b []byte) ([]byte, error) {
		nSize := ci.NonceSize()

		nonce := make([]byte, nSize)
		if n, err := rand.Read(nonce); n != nSize || err != nil {
			return nil, fmt.Errorf("reading nonce: %w", err)
		}

		sealed := ci.Seal(nil, nonce, b, ad)
		nonce = append(nonce, sealed...)

		return nonce, nil
	}
}

// privateStorageUnseal unseals protected data via AEAD
func privateStorageUnseal(ci cipher.AEAD, ad []byte) cryptoSealUnsealer {
	return func(ctx context.Context, b []byte) ([]byte, error) {
		nSize := ci.NonceSize()

		pt, err := ci.Open(nil, b[:nSize], b[nSize:], ad)
		if err != nil {
			return nil, fmt.Errorf("opening sealed private storage block: %w", err)
		}

		return pt, nil
	}
}

// privateKeytoStorageKey outputs a HKDF of at least 32 bytes to be used for storage/block encryption against the key
func privateKeytoStorageKey(pk id.PrivateKey, purpose []byte) ([]byte, error) {
	if purpose == nil {
		purpose = []byte("StorageKey")
	}

	r := hkdf.Expand(sha3.New512, []byte(pk), purpose)

	sk := make([]byte, storageKeyLen)
	n, err := r.Read(sk)
	if err != nil {
		return nil, fmt.Errorf("reading hfdk expansion: %w", err)
	}
	if n != storageKeyLen {
		return nil, errors.New("failed to read all required bytes")
	}

	return sk, nil
}

func (o *Otter) apiHandle_Keys_ImportKey(w http.ResponseWriter, r *http.Request) {
	req := &v1api.ImportKeyRequest{}

	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		apiJSONError(w, fmt.Errorf("decoding body: %w", err))
		return
	}

	pub, priv, err := id.RecoverKey(req.Mnemonic, req.Password)
	if err != nil {
		apiJSONError(w, fmt.Errorf("recovering key: %w", err))
		return
	}

	if err := o.storeKey(r.Context(), priv, req.Password); err != nil {
		apiJSONError(w, err)
		return
	}

	err = o.ensureSubsystemsForKey(pub)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	sync, err := o.GetOrNewAccountSyncer(r.Context(), pub)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	//Import existing hosts via IPNS
	peers, err := o.ResolveOtterNodesForKey(r.Context(), pub)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	o.logger.Info("resolved peers for importing key", zap.Any("peers", peers))

	err = o.setAllowedSyncerPeers(r.Context(), pub, peers)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	//wait for at least private syncer to find heads
	for {
		if sync.privateSyncer.InternalStats(r.Context()).MaxHeight != 0 {
			break
		}

		select {
		case <-r.Context().Done():
			return
		case <-time.After(3 * time.Second):
		}
	}

	o.logger.Info("new key added and sync reached head", zap.String("id", string(pub)))

	w.Write([]byte("ok"))
}

func (o *Otter) apiHandle_Keys_NewKey(w http.ResponseWriter, r *http.Request) {
	req := &v1api.NewKeyRequest{}

	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		apiJSONError(w, fmt.Errorf("decoding body: %w", err))
		return
	}

	if len(req.Password) < 8 {
		apiJSONErrorWithStatus(w, errors.New("password must be at least 8 characters"), http.StatusBadRequest)
		return
	}

	mn, pub, priv, err := id.NewKey(req.Password)
	if err != nil {
		apiJSONError(w, fmt.Errorf("creating new key: %w", err))
		return
	}

	if err := o.storeKey(r.Context(), priv, req.Password); err != nil {
		apiJSONError(w, err)
		return
	}

	o.apiJSONResponse(w, v1api.NewKeyResponse{
		Mnemonic: mn,
		PublicID: pub,
	})
}

func (o *Otter) storeKey(ctx context.Context, priv id.PrivateKey, pass string) error {
	ss, err := o.sc.System()
	if err != nil {
		return err
	}

	h, err := hashPassword(pass)
	if err != nil {
		return err
	}

	pub, err := priv.PublicKey()
	if err != nil {
		return err
	}

	if err := ss.Put(ctx, datastore.NewKey(systemPrefix_Keys+string(pub)), []byte(priv)); err != nil {
		return err
	}

	if err := ss.Put(ctx, datastore.NewKey(systemPrefix_Pass+string(pub)), []byte(h)); err != nil {
		return err
	}

	return nil
}

func (o *Otter) apiHandle_Keys_List(w http.ResponseWriter, r *http.Request) {
	ss, err := o.sc.System()
	if err != nil {
		apiJSONError(w, err)
		return
	}

	q, err := ss.Query(r.Context(), query.Query{Prefix: systemPrefix_Keys, KeysOnly: true})
	if err != nil {
		apiJSONError(w, err)
		return
	}

	keys, err := q.Rest()
	if err != nil {
		apiJSONError(w, err)
		return
	}

	resp := v1api.KeyListResponse{
		Keys: []id.PublicID{},
	}
	for _, k := range keys {
		resp.Keys = append(resp.Keys, id.PublicID(strings.TrimPrefix(k.Key, systemPrefix_Keys)))
	}

	o.apiJSONResponse(w, resp)
}

func (o *Otter) apiHandle_Keys_Delete(w http.ResponseWriter, r *http.Request) {
	req := &v1api.DeleteKeyRequest{}

	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		apiJSONError(w, err)
		return
	}

	if req.Password == "" || req.PublicID == "" {
		apiJSONErrorWithStatus(w, errors.New("missing fields"), http.StatusBadRequest)
		return
	}

	ss, err := o.sc.System()
	if err != nil {
		apiJSONError(w, err)
		return
	}

	keyRef := systemPrefix_Keys + string(req.PublicID)

	ok, err := ss.Has(r.Context(), datastore.NewKey(keyRef))
	if err != nil {
		apiJSONError(w, err)
		return
	}
	if !ok {
		apiJSONErrorWithStatus(w, errors.New("key does not exist"), http.StatusNotFound)
		return
	}

	if err := ss.Delete(r.Context(), datastore.NewKey(keyRef)); err != nil {
		apiJSONError(w, err)
		return
	}
}

func (o *Otter) apiHandle_Keys_Sign(w http.ResponseWriter, r *http.Request) {
	req := &v1api.SignRequest{}

	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		apiJSONError(w, err)
		return
	}

	if len(req.Data) == 0 {
		apiJSONErrorWithStatus(w, errors.New("no data supplied"), http.StatusBadRequest)
		return
	}

	if req.PublicID == "" {
		apiJSONErrorWithStatus(w, errors.New("no publicID supplied"), http.StatusBadRequest)
		return
	}

	ss, err := o.sc.System()
	if err != nil {
		apiJSONError(w, err)
		return
	}

	keyRef := systemPrefix_Keys + string(req.PublicID)

	ok, err := ss.Has(r.Context(), datastore.NewKey(keyRef))
	if err != nil {
		apiJSONError(w, err)
		return
	}
	if !ok {
		apiJSONErrorWithStatus(w, errors.New("key does not exist"), http.StatusNotFound)
		return
	}

	rpk, err := ss.Get(r.Context(), datastore.NewKey(keyRef))
	if err != nil {
		apiJSONError(w, err)
		return
	}

	sig, err := id.Sign(id.PrivateKey(rpk), req.Data, req.HashID)
	if err != nil {
		apiJSONError(w, err)
		return
	}

	o.apiJSONResponse(w, v1api.SignResponse{Sig: sig})
}

func (o *Otter) Sign(ctx context.Context, p id.PublicID, data []byte, hasher stdCrypto.Hash) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("no data supplied")
	}

	if p == "" {
		return nil, errors.New("no publicID supplied")
	}

	ss, err := o.sc.System()
	if err != nil {
		return nil, err
	}

	keyRef := systemPrefix_Keys + string(p)

	ok, err := ss.Has(ctx, datastore.NewKey(keyRef))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("key does not exist")
	}

	rpk, err := ss.Get(ctx, datastore.NewKey(keyRef))
	if err != nil {
		return nil, err
	}

	sig, err := id.Sign(id.PrivateKey(rpk), data, hasher)
	if err != nil {
		return nil, err
	}

	return sig, nil
}

func (o *Otter) getPK(ctx context.Context, p id.PublicID) (id.PrivateKey, error) {
	ss, err := o.sc.System()
	if err != nil {
		return id.PrivateKey(""), err
	}

	keyRef := systemPrefix_Keys + string(p)

	ok, err := ss.Has(ctx, datastore.NewKey(keyRef))
	if err != nil {
		return id.PrivateKey(""), err
	}
	if !ok {
		return id.PrivateKey(""), errors.New("key does not exist")
	}

	rpk, err := ss.Get(ctx, datastore.NewKey(keyRef))
	if err != nil {
		return id.PrivateKey(""), err
	}

	return id.PrivateKey(rpk), nil
}

func (o *Otter) PrivateSeal(ctx context.Context, p id.PublicID, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("no data supplied")
	}

	if p == "" {
		return nil, errors.New("no publicID supplied")
	}

	rpk, err := o.getPK(ctx, p)
	if err != nil {
		return nil, err
	}

	sk, err := privateKeytoStorageKey(id.PrivateKey(rpk), []byte("genericSeal"))
	if err != nil {
		return nil, fmt.Errorf("getting storage key: %w", err)
	}

	aead, err := privateStorageAEAD(sk)
	if err != nil {
		return nil, fmt.Errorf("creating AEAD: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ct := aead.Seal(nil, nonce, data, []byte(p))
	nonce = append(nonce, ct...)

	return nonce, nil
}

func (o *Otter) PrivateUnseal(ctx context.Context, p id.PublicID, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("no data supplied")
	}

	if p == "" {
		return nil, errors.New("no publicID supplied")
	}

	rpk, err := o.getPK(ctx, p)
	if err != nil {
		return nil, err
	}

	sk, err := privateKeytoStorageKey(id.PrivateKey(rpk), []byte("genericSeal"))
	if err != nil {
		return nil, fmt.Errorf("getting storage key: %w", err)
	}

	aead, err := privateStorageAEAD(sk)
	if err != nil {
		return nil, fmt.Errorf("creating AEAD: %w", err)
	}

	pt, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte(p))
	if err != nil {
		return nil, err
	}

	return pt, nil
}
