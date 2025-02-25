package email

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/emersion/go-msgauth/authres"
	"github.com/emersion/go-msgauth/dkim"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/tcfw/otter/pkg/protos/email/pb"
	spfexpand "github.com/tcfw/otter/pkg/protos/email/spf-expand"
)

func (eh *EmailHandler) checkSpam(p peer.ID, msg *pb.ReceiveEmail) (string, bool, error) {
	var spamResult bool

	remoteHost, _, err := net.SplitHostPort(msg.Ip)
	if err != nil {
		return "", spamResult, err
	}

	remoteAddr := net.ParseIP(remoteHost)
	if remoteAddr == nil {
		return "", spamResult, errors.New("invalid remote IP")
	}

	spfResult, err := eh.checkSPF(remoteAddr, msg.From, msg.Hello)
	if err != nil {
		return "", spamResult, err
	}
	if spfResult.Value == authres.ResultHardFail ||
		spfResult.Value == authres.ResultSoftFail ||
		spfResult.Value == authres.ResultFail ||
		spfResult.Value == authres.ResultPermError {
		spamResult = true
	}

	dkimResult, err := eh.checkDKIM(string(msg.Envelope))
	if err != nil {
		return "", spamResult, err
	}

	results := []authres.Result{
		&authres.GenericResult{Value: authres.ResultPass, Method: "otter-pb-sig", Params: map[string]string{"peer": p.String()}},
		spfResult,
	}
	for _, r := range dkimResult {
		results = append(results, r)

		if r.Value == authres.ResultFail ||
			r.Value == authres.ResultPermError {
			spamResult = true
		}
	}

	return authres.Format(eh.o.HostID().String(), results), spamResult, nil
}

func (eh *EmailHandler) checkSPF(addr net.IP, from string, helo string) (*authres.SPFResult, error) {
	ar := &authres.SPFResult{
		From: from,
		Helo: helo,
	}

	r, err := spfexpand.CheckHost(addr, from)
	if err != nil {
		ar.Value = authres.ResultTempError
		return ar, nil
	}

	switch r {
	case spfexpand.ResultPass:
		ar.Value = authres.ResultPass
	case spfexpand.ResultFail:
		ar.Value = authres.ResultFail
	case spfexpand.ResultSoftFail:
		ar.Value = authres.ResultSoftFail
	case spfexpand.ResultNeutral:
		ar.Value = authres.ResultNeutral
	case spfexpand.ResultNone:
		ar.Value = authres.ResultNone
	case spfexpand.ResultTempError:
		ar.Value = authres.ResultTempError
	case spfexpand.ResultPermError:
		ar.Value = authres.ResultPermError
	default:
		return ar, fmt.Errorf("unknown result type: %s", r)
	}

	return ar, nil
}

func (eh *EmailHandler) checkDKIM(msg string) ([]*authres.DKIMResult, error) {
	r := strings.NewReader(msg)

	verifications, err := dkim.Verify(r)
	if err != nil {
		return nil, err
	}

	res := []*authres.DKIMResult{}

	for _, ver := range verifications {
		r := &authres.DKIMResult{
			Domain:     ver.Domain,
			Identifier: ver.Identifier,
		}
		if ver.Err != nil {
			r.Reason = ver.Err.Error()
			if dkim.IsPermFail(err) {
				r.Value = authres.ResultPermError
			} else if dkim.IsTempFail(err) {
				r.Value = authres.ResultTempError
			} else {
				r.Value = authres.ResultFail
			}
		} else {
			r.Value = authres.ResultPass
		}

		res = append(res, r)
	}

	return res, nil
}
