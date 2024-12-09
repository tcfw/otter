package activitypub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ipfs/boxo/ipld/merkledag"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/tcfw/otter/internal/utils"
	"github.com/tcfw/otter/pkg/id"
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/protos/activitypub/pb"
	"go.uber.org/zap"

	dnslink "github.com/dnslink-std/go"

	"github.com/ipld/go-ipld-prime/codec/dagjson"
)

const (
	protoID     protocol.ID = "/otter/activitypub/0.0.1"
	serviceName             = "otter.activitypub"

	maxResolveAttempts     = 3
	maxHandleResolveSize   = 10240
	webfingerWellKnownPath = "/.well-known/webfinger"

	poisGateway = "pois.pds.directory"
	poisPrefix  = "/activityPub/"

	requestMaxSize  = 8 * 1024
	responseMaxSize = 1 << 20
)

var (
	aph = &ActivityPubHandler{}
)

func Register(o otter.Otter) {
	aph.o = o
	aph.logger = o.Logger("activityPub")
	o.Protocols().RegisterP2PHandler(protoID, aph.p2pHandle)

	o.Protocols().RegisterPOISHandlers(func(r *mux.Router) {
		sr := r.PathPrefix(poisPrefix + "{handle}").Subrouter()
		sr.HandleFunc("/profile", aph.poisProfile).Methods(http.MethodGet)
		sr.HandleFunc("/inbox", aph.poisInbox).Methods(http.MethodPost)
		sr.HandleFunc("/outbox", aph.poisOutbox).Methods(http.MethodGet)
		sr.HandleFunc("/followers", aph.poisFollowers).Methods(http.MethodGet)
		sr.HandleFunc("/following", aph.poisFollowing).Methods(http.MethodGet)
		sr.HandleFunc("/liked", aph.poisLiked).Methods(http.MethodGet)

		r.HandleFunc("/.well-known/webfinger", aph.poisResolve)
		r.HandleFunc("/resolve", aph.poisResolve)
	})

	go aph.publishWebFingers()
}

type Unmarshaler func(data []byte, v any) error

type ActivityPubHandler struct {
	o      otter.Otter
	logger *zap.Logger
}

func (a *ActivityPubHandler) publishWebFingers() {
	t := time.NewTicker(30 * time.Second)
	for range t.C {
		go a.doPublishWebFingers()
	}
}

func (a *ActivityPubHandler) p2pHandle(s network.Stream) {
	if err := s.Scope().SetService(serviceName); err != nil {
		a.logger.Error("attaching stream to activitypub service", zap.Error(err))
		s.Reset()
		return
	}

	if err := s.Scope().ReserveMemory(requestMaxSize, network.ReservationPriorityHigh); err != nil {
		a.logger.Error("reserving memory for activitypub stream", zap.Error(err))
		s.Reset()
		return
	}
	defer s.Scope().ReleaseMemory(requestMaxSize)

	req := &pb.Request{}

	r := pbio.NewDelimitedReader(s, requestMaxSize)
	w := pbio.NewDelimitedWriter(s)

	if err := r.ReadMsg(req); err != nil {
		a.logger.Error("reading msg for activitypub stream", zap.Error(err))
		s.Reset()
		return
	}

	resp, err := a.handleRequest(req)
	if err != nil {
		w.WriteMsg(&pb.Response{Error: err.Error()})
		return
	}

	if err := w.WriteMsg(resp); err != nil {
		a.logger.Error("sending response msg for activitypub stream", zap.Error(err))
		s.Reset()
		return
	}
}

func (a *ActivityPubHandler) handleRequest(r *pb.Request) (*pb.Response, error) {
	a.logger.Info("req", zap.Any("req", r))

	return nil, fmt.Errorf("not implemented")
}

func (a *ActivityPubHandler) doPublishWebFingers() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	keys, err := a.o.Crypto().KeyStore().Keys(ctx)
	if err != nil {
		a.logger.Error("getting keys", zap.Error(err))
	}

	for _, k := range keys {
		err := a.doPublishWebFinger(ctx, k)
		if err != nil {
			a.o.Logger("acitivy").Error("publishin webfinger", zap.Error(err))
		}
	}
}

func (a *ActivityPubHandler) doPublishWebFinger(ctx context.Context, key id.PublicID) error {
	directoryLink := url.URL{}
	directoryLink.Scheme = "https"
	directoryLink.Host = poisGateway
	directoryLink.Path = poisPrefix + string(key) + "/"
	dURL := directoryLink.String()

	finger := &WebFingerJRD{
		Aliases: []string{dURL},
		Links: []WebFingerJRDLink{
			{Href: dURL, Rel: "self", Type: "application/activity+json"},
			{Href: dURL, Rel: "self", Type: "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\""},
		},
	}

	ts, err := ipld.LoadSchemaBytes([]byte(`
		type WebFingerJRD struct {
			subject String   
			aliases [String]
			links [WebFingerJRDLink]
		}

		type WebFingerJRDLink struct {
			rel  String 
			type String 
			href String 
		}
	`))
	if err != nil {
		panic(err)
	}
	schemaType := ts.TypeByName("WebFingerJRD")

	enc, err := ipld.Marshal(dagjson.Encode, finger, schemaType)
	if err != nil {
		return fmt.Errorf("encoding dag-json: %w", err)
	}
	// enc, err := json.Marshal(finger)
	// if err != nil {
	// 	return fmt.Errorf("marshaling finger: %w", err)
	// }

	cidb := cid.V1Builder{
		Codec:    uint64(multicodec.DagJson),
		MhType:   multihash.SHA2_256,
		MhLength: multihash.DefaultLengths[multihash.SHA2_256],
	}

	c, err := cidb.Sum(enc)
	if err != nil {
		return fmt.Errorf("creating cid: %w", err)
	}

	blk, err := blocks.NewBlockWithCid(enc, c)
	if err != nil {
		return err
	}
	// Once you "share" a block, it should be immutable. Therefore, we can just use this block as-is.
	n := &merkledag.RawNode{blk, basicnode.NewBytes(enc)}

	if err := a.o.IPLD().Add(ctx, n); err != nil {
		return err
	}

	a.logger.Debug("published webfinger", zap.Any("publicID", key), zap.Any("cid", n.Cid().String()))

	return nil
}

func (a *ActivityPubHandler) resolveWebFinger(ctx context.Context, handle string) (*WebFingerJRD, error) {
	handle = strings.TrimPrefix(handle, "acct:")
	handle = strings.TrimPrefix(handle, "@")

	handleParts := strings.SplitN(handle, "@", 2)

	if len(handleParts) != 2 {
		return nil, errors.New("invalid handle")
	}

	url := url.URL{}
	url.Host = handleParts[1]
	url.Scheme = "https"
	url.Path = webfingerWellKnownPath
	q := url.Query()
	q.Add("resource", "acct:"+handle)
	url.RawQuery = q.Encode()

	a.logger.Debug("webfinger resolve", zap.Any("url", url.String()))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("newing http request: %w", err)
	}

	req.Header.Add("accept", "application/jrd+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting webfinger response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected response code, expected 200, got '%d'", resp.StatusCode)
	}

	contentTypeParts := strings.SplitAfter(resp.Header.Get("content-type"), ";")
	if len(contentTypeParts) == 0 {
		return nil, fmt.Errorf("no content type returned")
	}
	contentType := strings.TrimRight(contentTypeParts[0], ";")
	if contentType != "application/jrd+json" && contentType != "application/json" {
		return nil, fmt.Errorf("unexpected content-type, expected 'application/jrd+json', got '%s'", contentType)
	}

	actor := &WebFingerJRD{}
	r := io.LimitReader(resp.Body, maxHandleResolveSize)

	if err := json.NewDecoder(r).Decode(actor); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return actor, nil
}

func (a *ActivityPubHandler) resolveDNSLink(ctx context.Context, handle string) (*WebFingerJRD, error) {
	handleParts := strings.SplitN(handle, "@", 2)

	if len(handleParts) == 0 || handle == "" {
		return nil, errors.New("invalid handle")
	}

	var domain string

	if len(handleParts) == 1 {
		domain = handleParts[0]
	} else if len(handleParts) == 2 {
		domain = strings.Join(handleParts, ".")
	} else {
		return nil, errors.New("handle should have max 2 parts")
	}

	var resolvedIdentifier string

	for i := 0; i < maxResolveAttempts; i++ {
		results, err := dnslink.Resolve(domain)
		if err != nil {
			return nil, fmt.Errorf("resolving dnslink: %w", err)
		}

		if ipns, ok := results.Links["ipns"]; ok {
			for _, ipns := range ipns {
				domain = "/ipns/" + ipns.Identifier
				break
			}
		}

		if ipfs, ok := results.Links["ipfs"]; resolvedIdentifier == "" && ok {
			if len(ipfs) == 0 {
				continue
			}
			resolvedIdentifier = "/ipfs/" + ipfs[0].Identifier
		}

		if resolvedIdentifier != "" {
			break
		}
	}

	if !strings.HasPrefix(resolvedIdentifier, "/ipfs/") {
		return nil, fmt.Errorf("failed to resolve to a CID: got %s", resolvedIdentifier)
	}

	rawCid := strings.TrimPrefix(resolvedIdentifier, "/ipfs/")
	c, err := cid.Decode(rawCid)
	if err != nil {
		return nil, fmt.Errorf("parsing resolved CID: %w", err)
	}

	n, err := a.o.IPLD().Get(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("getting cid content: %w", err)
	}

	bfsize, err := n.Size()
	if err != nil {
		return nil, fmt.Errorf("getting cid size: %w", err)
	}
	if bfsize > maxHandleResolveSize {
		return nil, fmt.Errorf("handle size too large, got %d", bfsize)
	}

	var unmarshaler Unmarshaler

	switch n.Cid().Type() {
	case uint64(multicodec.DagJson):
		unmarshaler = json.Unmarshal
	default:
		return nil, fmt.Errorf("unknown codec type: %d", n.Cid().Type())
	}

	actor := &WebFingerJRD{}
	if err := unmarshaler(n.RawData(), actor); err != nil {
		return nil, fmt.Errorf("unmarshalling actor: %w", err)
	}

	actor.Subject = "acct:" + handle

	return actor, nil
}

func (a *ActivityPubHandler) poisResolve(w http.ResponseWriter, r *http.Request) {
	res := r.URL.Query().Get("resource")
	if res == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !strings.HasPrefix(res, "acct:") && !strings.HasPrefix(res, "@") {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("resource should start with `acct:` or `@`"))
		return
	}

	res = strings.TrimPrefix(res, "acct:")
	res = strings.TrimPrefix(res, "@")

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var actor *WebFingerJRD
	actor, err := a.resolveWebFinger(ctx, res)
	if err != nil {
		a.logger.Error("resolving via webfinger, trying dnslink", zap.Any("actor", res), zap.Error(err))

		errCh := make(chan error, 1)
		actorCh := make(chan *WebFingerJRD, 1)

		go func() {
			actor, err := a.resolveDNSLink(ctx, res)
			if err != nil {
				a.logger.Error("resolving dnslink", zap.Error(err))
				errCh <- err
				return
			}

			actorCh <- actor
		}()

		select {
		case err := <-errCh:
			w.Write([]byte(err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			return
		case actor = <-actorCh:
		case <-ctx.Done():
			w.WriteHeader(http.StatusGatewayTimeout)
			w.Write([]byte("content deadline exceeded"))
			return
		}
	}
	w.Header().Add("content-type", "application/jrd+json")
	json.NewEncoder(w).Encode(actor)
}

func (a *ActivityPubHandler) poisProfile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	handle, ok := vars["handle"]
	if !ok || handle == "" {
		http.Error(w, "missing handle", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	peer, err := a.findNode(ctx, id.PublicID(handle))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req := &pb.Request{
		Type: pb.RequestType_PROFILE,
		HttpSignature: &pb.HTTPSignature{
			Signature:     r.Header.Get("Signature"),
			Input:         r.Header.Get("Signature-Input"),
			ContentDigest: r.Header.Get("Content-Digest"),
		},
		Forwarded: a.forwardedHeaderFromRequest(r),
	}

	resp, err := a.sendRequest(ctx, peer, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.Error != "" {
		http.Error(w, resp.Error, http.StatusInternalServerError)
		return
	}

	w.Write([]byte(resp.JsonLD))
}

func (a *ActivityPubHandler) sendRequest(ctx context.Context, host peer.ID, req *pb.Request) (*pb.Response, error) {
	if host == a.o.HostID() {
		//loopback
		return a.handleRequest(req)
	}

	s, err := a.o.Protocols().P2P().NewStream(ctx, host, protoID)
	if err != nil {
		return nil, err
	}

	if err := s.Scope().SetService(serviceName); err != nil {
		a.logger.Error("attaching stream to activitypub service", zap.Error(err))
		s.Reset()
		return nil, err
	}

	if err := s.Scope().ReserveMemory(responseMaxSize, network.ReservationPriorityAlways); err != nil {
		a.logger.Error("reserving memory for activitypub service", zap.Error(err))
		s.Reset()
		return nil, err
	}
	defer s.Scope().ReleaseMemory(responseMaxSize)

	w := pbio.NewDelimitedWriter(s)

	if w.WriteMsg(req); err != nil {
		s.Reset()
		return nil, err
	}

	r := pbio.NewDelimitedReader(s, responseMaxSize)

	resp := &pb.Response{}
	if err := r.ReadMsg(resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (a *ActivityPubHandler) forwardedHeaderFromRequest(r *http.Request) string {
	parts := map[string]string{
		"by":    "otter-pois/" + a.o.HostID().String(),
		"host":  r.Host,
		"proto": r.Proto,
	}

	addr, _, _ := net.SplitHostPort(r.RemoteAddr)

	addrIP := net.ParseIP(addr)
	if ipv4 := addrIP.To4(); ipv4 != nil {
		parts["for"] = ipv4.String()
	} else {
		parts["for"] = fmt.Sprintf(`"[%s]"`, addrIP.To16().String())
	}

	kvp := []string{}

	for k, v := range parts {
		kvp = append(kvp, fmt.Sprintf("%s=%s", k, v))
	}

	header := strings.Join(kvp, ";")

	if existingForwarded := r.Header.Get("Forwarded"); existingForwarded != "" {
		header += ", " + existingForwarded
	}

	return header
}

func (a *ActivityPubHandler) findNode(ctx context.Context, pubk id.PublicID) (peer.ID, error) {
	nodes, err := a.o.ResolveOtterNodesForKey(ctx, pubk)
	if err != nil {
		return peer.ID(""), fmt.Errorf("resolving otter nodes: %w", err)
	}

	p, err := utils.FirstOnlinePeer(ctx, nodes, a.o.Protocols().P2P())
	if err != nil {
		return peer.ID(""), fmt.Errorf("getting first otter node: %w", err)
	}

	return p, nil
}

func (a *ActivityPubHandler) poisInbox(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(403)
}

func (a *ActivityPubHandler) poisOutbox(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{
  "@context": "https://www.w3.org/ns/activitystreams",
  "type": "OrderedCollection",
  "totalItems": 0,
  "orderedItems": []
}`))
}

func (a *ActivityPubHandler) poisFollowers(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{
  "@context": "https://www.w3.org/ns/activitystreams",
  "type": "OrderedCollection",
  "totalItems": 0,
  "orderedItems": []
}`))
}

func (a *ActivityPubHandler) poisFollowing(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{
  "@context": "https://www.w3.org/ns/activitystreams",
  "type": "OrderedCollection",
  "totalItems": 0,
  "orderedItems": []
}`))
}

func (a *ActivityPubHandler) poisLiked(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{
  "@context": "https://www.w3.org/ns/activitystreams",
  "type": "OrderedCollection",
  "totalItems": 0,
  "orderedItems": []
}`))
}
