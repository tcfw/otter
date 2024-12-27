package petnames

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ipfs/go-datastore"
	"github.com/tcfw/otter/pkg/id"
	"go.uber.org/zap"

	v1api "github.com/tcfw/otter/pkg/api"
)

const (
	maxBodySize = 10 * 1024
)

func Client() ClientImpl {
	return pnh
}

func (p *PetnamesHandler) ForPublicID(pub id.PublicID) (ScopedClient, error) {
	pubds, err := p.o.Storage().Public(pub)
	if err != nil {
		return nil, err
	}

	privds, err := p.o.Storage().Public(pub)
	if err != nil {
		return nil, err
	}

	c := &scopedClient{
		logger: p.l,
		pub:    pub,
		pubDS:  pubds, privDS: privds,
		baseKey: datastore.KeyWithNamespaces([]string{"petnames", string(pub)}),
	}

	return c, nil
}

type scopedClient struct {
	logger  *zap.Logger
	pub     id.PublicID
	baseKey datastore.Key
	pubDS   datastore.Datastore
	privDS  datastore.Datastore
}

func (sc *scopedClient) ProposedName() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := sc.pubDS.Get(ctx, sc.baseKey.Child(datastore.NewKey("proposed_name")))
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			return "", nil
		}

		return "", err
	}

	return string(val), nil
}

func (sc *scopedClient) SetProposedName(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := sc.pubDS.Put(ctx, sc.baseKey.Child(datastore.NewKey("proposed_name")), []byte(name))
	if err != nil {
		return err
	}

	return nil
}

func (sc *scopedClient) SetLocalContact(c *Contact) error {
	return errors.New("not implemented")
}

func (sc *scopedClient) GetLocalContact(pub id.PublicID) (string, error) {
	return "", errors.New("not implemented")
}

func (sc *scopedClient) RemoveLocalContact(pub id.PublicID) error {
	return errors.New("not implemented")
}

func (sc *scopedClient) ListLocalContacts(ctx context.Context, pub id.PublicID) ([]Contact, error) {
	return nil, errors.New("not implemented")
}

func (sc *scopedClient) SearchLocalContacts(ctx context.Context, pub string) ([]Contact, error) {
	return nil, errors.New("not implemented")
}

func (sc *scopedClient) SearchForEdgeNames(ctx context.Context, pub id.PublicID) (<-chan *SharedContact, error) {
	return nil, errors.New("not implemented")
}

func (p *PetnamesHandler) apiHandle_SetProposedName(w http.ResponseWriter, r *http.Request) {
	req := &SetProposedNameRequest{}

	err := json.NewDecoder(io.LimitReader(r.Body, maxBodySize)).Decode(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(req.Name) > MaxNameSize {
		http.Error(w, fmt.Sprintf("name too long, must be less than %d", MaxNameSize), http.StatusBadRequest)
		return
	}

	auth, err := v1api.GetAuthIDFromContext(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sc, err := p.ForPublicID(auth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := sc.SetProposedName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
