package petnames

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/protos/petnames/pb"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

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
		baseKey:        datastore.KeyWithNamespaces([]string{"petnames", string(pub)}),
		baseContactKey: datastore.KeyWithNamespaces([]string{"petnames", string(pub), "contacts"}),
	}

	return c, nil
}

type scopedClient struct {
	logger         *zap.Logger
	pub            id.PublicID
	baseKey        datastore.Key
	baseContactKey datastore.Key
	pubDS          datastore.Datastore
	privDS         datastore.Datastore
}

func (sc *scopedClient) ProposedName() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := sc.pubDS.Get(ctx, sc.baseKey.ChildString("proposed_name"))
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			return "", nil
		}

		return "", err
	}

	return string(val), nil
}

func (sc *scopedClient) SetProposedName(ctx context.Context, name string) error {
	err := sc.pubDS.Put(ctx, sc.baseKey.ChildString("proposed_name"), []byte(name))
	if err != nil {
		return err
	}

	return nil
}

func (sc *scopedClient) SetLocalContact(ctx context.Context, c *pb.Contact) error {
	k := sc.baseContactKey.ChildString(c.Id)

	b, err := proto.Marshal(c)
	if err != nil {
		return err
	}

	return sc.privDS.Put(ctx, k, b)
}

func (sc *scopedClient) GetLocalContact(ctx context.Context, pub id.PublicID) (*pb.Contact, error) {
	k := sc.baseContactKey.ChildString(string(pub))

	v, err := sc.privDS.Get(ctx, k)
	if err != nil {
		return nil, err
	}

	c := &pb.Contact{}
	if err := proto.Unmarshal(v, c); err != nil {
		return nil, err
	}

	return c, nil
}

func (sc *scopedClient) RemoveLocalContact(ctx context.Context, pub id.PublicID) error {
	k := sc.baseContactKey.ChildString(string(pub))

	return sc.privDS.Delete(ctx, k)
}

func (sc *scopedClient) ListLocalContacts(ctx context.Context) ([]*pb.Contact, error) {
	q, err := sc.privDS.Query(ctx, query.Query{Prefix: sc.baseKey.String()})
	if err != nil {
		return nil, err
	}

	cs := []*pb.Contact{}

	for v := range q.Next() {
		c := &pb.Contact{}

		if err := proto.Unmarshal(v.Value, c); err != nil {
			return nil, err
		}

		cs = append(cs, c)
	}

	return cs, nil
}

func (sc *scopedClient) CountLocalContacts(ctx context.Context) (int, error) {
	q, err := sc.privDS.Query(ctx, query.Query{Prefix: sc.baseKey.String(), KeysOnly: true})
	if err != nil {
		return 0, err
	}

	c := 0
	for range q.Next() {
		c++
	}

	return c, nil
}

func (sc *scopedClient) SearchLocalContacts(ctx context.Context, query string) ([]*pb.Contact, error) {
	return nil, errors.New("not implemented")
}

func (sc *scopedClient) SearchForEdgeNames(ctx context.Context, pub id.PublicID) (<-chan *pb.DOSName, error) {
	return nil, errors.New("not implemented")
}

func (p *PetnamesHandler) apiHandle_ListContact(w http.ResponseWriter, r *http.Request) {
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

	c, err := sc.ListLocalContacts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)
}

func (p *PetnamesHandler) apiHandle_SetContact(w http.ResponseWriter, r *http.Request) {
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

	c := &pb.Contact{}
	err = json.NewDecoder(r.Body).Decode(c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = sc.SetLocalContact(r.Context(), c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("ok"))
}

func (p *PetnamesHandler) apiHandle_GetProposedName(w http.ResponseWriter, r *http.Request) {
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

	name, err := sc.ProposedName()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(name))
}

func (p *PetnamesHandler) apiHandle_SetProposedName(w http.ResponseWriter, r *http.Request) {
	req := &pb.SetProposedNameRequest{}

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

	if err := sc.SetProposedName(r.Context(), req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (p *PetnamesHandler) apiHandle_SearchDHT(w http.ResponseWriter, r *http.Request) {
	val := r.URL.Query().Get("v")
	limitStr := r.URL.Query().Get("l")

	if val == "" {
		http.Error(w, "value (v) param was empty", http.StatusBadRequest)
		return
	}

	if limitStr == "" {
		limitStr = "10"
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	provs, err := p.Search(r.Context(), val, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := json.NewEncoder(w).Encode(provs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
