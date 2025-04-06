package petnames

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/tcfw/otter/internal/utils"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/protos/petnames/pb"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1api "github.com/tcfw/otter/pkg/api"
)

const (
	workersPerLookup = 5
	maxBodySize      = 10 * 1024
)

func Client() ClientImpl {
	return pnh
}

func (p *PetnamesHandler) ForPublicID(pub id.PublicID) (ScopedClient, error) {
	pubds, err := p.o.Storage().Public(pub)
	if err != nil {
		return nil, err
	}

	privds, err := p.o.Storage().PrivateFromPublic(pub)
	if err != nil {
		return nil, err
	}

	c := &scopedClient{
		logger: p.l,
		o:      p.o,
		pub:    pub,
		pubDS:  pubds, privDS: privds,
		baseKey:        datastore.KeyWithNamespaces([]string{"petnames", string(pub)}),
		baseContactKey: datastore.KeyWithNamespaces([]string{"petnames", string(pub), "contacts"}),
	}

	return c, nil
}

type scopedClient struct {
	logger         *zap.Logger
	o              otter.Otter
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

	if c.Id == "" || c.PrivateName == nil || c.PrivateName.FirstName == "" {
		return errors.New("contact missing info")
	}

	existing, err := sc.GetLocalContact(ctx, id.PublicID(c.Id))
	if err != nil && !errors.Is(err, datastore.ErrNotFound) {
		return err
	}

	c.Added = timestamppb.Now()
	if existing != nil {
		c.Added = existing.Added
		c.NumberOfContacts = existing.NumberOfContacts

		c.LastUpdated = timestamppb.Now()
	}

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

func (sc *scopedClient) ListAllLocalContactsBy(ctx context.Context, cmp func(a, b *pb.Contact) int) (<-chan *pb.Contact, error) {
	q, err := sc.privDS.Query(ctx, query.Query{
		Prefix: sc.baseContactKey.String(),
		Orders: []query.Order{
			query.OrderByFunction(func(a, b query.Entry) int {
				c1, err := decodeContact(a.Value)
				if err != nil {
					return 0
				}
				c2, err := decodeContact(b.Value)
				if err != nil {
					return 0
				}

				return cmp(c1, c2)
			}),
		},
	})
	if err != nil {
		return nil, err
	}

	res := make(chan *pb.Contact)

	go func() {
		defer close(res)

		for {
			select {
			case <-ctx.Done():
				return
			case r, ok := <-q.Next():
				if !ok {
					return
				}

				c, err := decodeContact(r.Value)
				if err != nil {
					sc.logger.Error("decoding contact", zap.Error(err))
					return
				}
				res <- c
			}
		}
	}()

	return res, nil
}

func decodeContact(d []byte) (*pb.Contact, error) {
	c := &pb.Contact{}

	if err := proto.Unmarshal(d, c); err != nil {
		return nil, err
	}

	return c, nil
}

func (sc *scopedClient) ListLocalContacts(ctx context.Context, limit, offset int) ([]*pb.Contact, error) {
	q, err := sc.privDS.Query(ctx, query.Query{
		Prefix: sc.baseContactKey.String(),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	cs := []*pb.Contact{}

	for v := range q.Next() {
		c, err := decodeContact(v.Value)
		if err != nil {
			return nil, err
		}

		cs = append(cs, c)
	}

	return cs, nil
}

func (sc *scopedClient) CountLocalContacts(ctx context.Context) (int, error) {
	q, err := sc.privDS.Query(ctx, query.Query{Prefix: sc.baseContactKey.String(), KeysOnly: true})
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
	cl, err := sc.ListAllLocalContactsBy(ctx, sortByNumContactsDesc)
	if err != nil {
		return nil, err
	}

	visitedContacts := make(map[id.PublicID]struct{}, 100)
	var mu sync.Mutex

	idCh := make(chan id.PublicID)
	tryCh := make(chan []id.PublicID, 50)
	res := make(chan *pb.DOSName)

	probeProb := maxRequestProbability

	var wg sync.WaitGroup
	wg.Add(workersPerLookup)

	go func() {
		wg.Wait()
		close(tryCh)
		close(res)
	}()

	for range workersPerLookup {
		go func() {
			defer wg.Done()

			for id := range idCh {
				if id == "" {
					return
				}

				mu.Lock()
				_, ok := visitedContacts[id]
				mu.Unlock()
				if ok || id == sc.pub {
					continue
				}

				sc.logger.Debug("asking contact for edgenames", zap.Any("req", pub), zap.Any("contact", id))

				sn, tc, uc, err := sc.askIDForEdgeNames(ctx, id, pub, probeProb)
				if err != nil {
					sc.logger.Debug("contact lookup on node unsuccesful", zap.Error(err))
					continue
				}

				if err := sc.updateKnownContactCount(ctx, id, uc); err != nil {
					sc.logger.Debug("failed to update contact count", zap.Error(err))
				}

				mu.Lock()
				visitedContacts[id] = struct{}{}
				mu.Unlock()

				if len(tc) != 0 {
					select {
					case <-ctx.Done():
						return
					case tryCh <- tc:
					default:
					}
				}

				for _, s := range sn {
					if s.Id != string(pub) {
						continue
					}

					name := &pb.DOSName{
						Degree: uint32(maxRequestProbability / probeProb),
						Name:   s.SharedName,
					}

					select {
					case <-ctx.Done():
						return
					case res <- name:
					}
				}
			}
		}()
	}

	go func() {
		defer close(idCh)

		didOne := false

		for c := range cl {
			select {
			case <-ctx.Done():
				return
			case idCh <- id.PublicID(c.Id):
				didOne = true
			}
		}

		if !didOne {
			//we had no contacts :(
			sc.logger.Debug("no contacts in list to ask")
			return
		}

		//After initial probing, lower the number of ordered random
		// sampling from lookup requests
		probeProb = maxRequestProbability * 0.7

		for {
			select {
			case <-ctx.Done():
				return
			case try := <-tryCh:
				for _, c := range try {
					select {
					case <-ctx.Done():
						return
					case idCh <- c:
					}
				}
			}
		}
	}()

	return res, nil
}

func (sc *scopedClient) askIDForEdgeNames(ctx context.Context, pub id.PublicID, contact id.PublicID, prob float64) ([]*pb.SharedContact, []id.PublicID, uint32, error) {
	nodes, err := sc.o.ResolveOtterNodesForKey(ctx, pub)
	if err != nil {
		return nil, nil, 0, err
	}
	peer, err := utils.FirstOnlinePeer(ctx, nodes, sc.o.Protocols().P2P())
	if err != nil {
		return nil, nil, 0, err
	}

	s, err := sc.o.Protocols().DialContext(ctx, peer, protoID, pub, sc.pub)
	if err != nil {
		return nil, nil, 0, err
	}

	w := pbio.NewDelimitedWriter(s)
	r := pbio.NewDelimitedReader(s, reqMaxSize*10)
	defer w.Close()
	defer r.Close()

	req := &pb.LookupRequest{
		Id:                       []string{string(contact)},
		SuggestedNodeProbability: prob,
	}

	if err := w.WriteMsg(req); err != nil {
		return nil, nil, 0, err
	}

	resp := &pb.LookupResponse{}
	if err := r.ReadMsg(resp); err != nil {
		return nil, nil, 0, err
	}

	if resp.Error != pb.ErrorCode_SUCCESS {
		return nil, nil, 0, errors.New("node responded with " + resp.Error.String())
	}

	tryContacts := make([]id.PublicID, 0, len(resp.TryContactId))
	for _, c := range resp.TryContactId {
		tryContacts = append(tryContacts, id.PublicID(c))
	}

	return resp.SharedNames, tryContacts, resp.ContactSize, nil
}

func (sc *scopedClient) updateKnownContactCount(ctx context.Context, contact id.PublicID, count uint32) error {
	c, err := sc.GetLocalContact(ctx, contact)
	if err != nil {
		if errors.Is(err, datastore.ErrNotFound) {
			return nil
		}

		return err
	}

	c.NumberOfContacts = count
	c.LastUpdated = timestamppb.Now()

	return sc.SetLocalContact(ctx, c)
}

func (p *PetnamesHandler) apiHandle_ListContact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	auth, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sc, err := p.ForPublicID(auth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var limit, offset = 0, 0

	queryLimit := r.URL.Query().Get("limit")
	queryOffset := r.URL.Query().Get("offset")

	limit, err = strconv.Atoi(queryLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	offset, err = strconv.Atoi(queryOffset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if limit == 0 {
		limit = 200
	}

	if limit > 200 || limit < 0 {
		http.Error(w, fmt.Sprintf("%d is over limit 200", limit), http.StatusBadRequest)
		return
	}

	if offset < 0 {
		http.Error(w, fmt.Sprintf("%d negative", offset), http.StatusBadRequest)
		return
	}

	c, err := sc.ListLocalContacts(ctx, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)
}

func (p *PetnamesHandler) apiHandle_DeleteContact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	auth, err := v1api.GetAuthIDFromContext(ctx)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = sc.RemoveLocalContact(ctx, id.PublicID(c.Id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("ok"))
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
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
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

func (p *PetnamesHandler) apiHandle_SearchEdgeNames(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	auth, err := v1api.GetAuthIDFromContext(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sc, err := p.ForPublicID(auth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pid, err := io.ReadAll(io.LimitReader(r.Body, 100))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resCh, err := sc.SearchForEdgeNames(ctx, id.PublicID(pid))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stream := r.URL.Query().Has("stream")
	w.Header().Add("content-type", "application/json")

	res := []*pb.DOSName{}

	enc := json.NewEncoder(w)

	for r := range resCh {
		if stream {
			enc.Encode(r)

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		} else {
			res = append(res, r)
		}
	}

	if !stream {
		json.NewEncoder(w).Encode(res)
	}
}
