package spfexpand

import (
	"context"
	"net"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

const (
	maxLevel     = 20
	maxRedirects = 5
)

type Result int

const (
	ResultPass Result = iota + 1
	ResultFail
	ResultSoftFail
	ResultNeutral
	ResultNone
	ResultTempError
	ResultPermError
)

func (r Result) String() string {
	switch r {
	case ResultPass:
		return "pass"
	case ResultFail:
		return "fail"
	case ResultSoftFail:
		return "softfail"
	case ResultNeutral:
		return "neutral"
	case ResultNone:
		return "none"
	case ResultTempError:
		return "temperror"
	case ResultPermError:
		return "permerror"
	default:
		return "Unknown"
	}
}

var resolver *net.Resolver

func init() {
	resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: 5 * time.Second,
			}
			return d.DialContext(ctx, network, "1.1.1.1:53")
		},
	}
}

func CheckHost(addr net.IP, from string) (Result, error) {
	mailAddr, err := mail.ParseAddress(from)
	if err != nil {
		return ResultPermError, err
	}

	_, domain, err := splitAddrUserDomain(mailAddr.Address)
	if err != nil {
		return ResultPermError, err
	}

	redirectTries := 0

	var r *SPFRecord

	for redirectTries < maxRedirects {
		spfTxt, err := GetSPF(domain)
		if err != nil {
			if errors.Is(err, errSPFNotFound) {
				return ResultNone, nil
			}

			return ResultTempError, errors.Wrap(err, "failed to get domain SPF record")
		}

		r, err = ParseSPF(spfTxt, domain)
		if err != nil {
			return ResultTempError, errors.Wrap(err, "failed to parse SPF record")
		}

		if r.ShouldRedirect() {
			redirectTries++

			if redirectTries >= maxRedirects {
				return ResultTempError, errors.New("too many redirects")
			}

			domain = r.Redirect
			continue
		}

		break
	}

	if err := r.Populate(); err != nil {
		return ResultTempError, err
	}

	allowedIPs := []*net.IPNet{}

	foldUpIPs(r, &allowedIPs)

	var found bool

	for _, ip := range allowedIPs {
		if ip.Contains(addr) {
			found = true
		}
	}

	if found {
		return ResultPass, nil
	}

	if r.Fail.All {
		return ResultFail, nil
	} else if r.SoftFail.All {
		return ResultSoftFail, nil
	} else if r.Neutral.All {
		return ResultNeutral, nil
	} else if r.Pass.All {
		return ResultPass, nil
	}

	return ResultNone, nil
}

func foldUpIPs(r *SPFRecord, ips *[]*net.IPNet) {
	*ips = append(*ips, r.Pass.AIPs...)
	*ips = append(*ips, r.Pass.IP4...)
	*ips = append(*ips, r.Pass.IP6...)
	*ips = append(*ips, r.Pass.MXIPs...)
	*ips = append(*ips, r.Neutral.AIPs...)
	*ips = append(*ips, r.Neutral.IP4...)
	*ips = append(*ips, r.Neutral.IP6...)
	*ips = append(*ips, r.Neutral.MXIPs...)

	for _, inc := range r.Pass.Includes {
		foldUpIPs(inc, ips)
	}

	for _, inc := range r.Neutral.Includes {
		foldUpIPs(inc, ips)
	}
}

type SPFRecord struct {
	Domain   string
	Original string

	Pass     SPFMechanism
	Fail     SPFMechanism
	Neutral  SPFMechanism
	SoftFail SPFMechanism

	Redirect string
}

func (s *SPFRecord) ShouldRedirect() bool {
	return s.Redirect != "" &&
		s.Pass.IsEmpty() &&
		s.Fail.IsEmpty() &&
		s.Neutral.IsEmpty() &&
		s.SoftFail.IsEmpty()
}

func (s *SPFRecord) Populate() error {
	if err := s.Pass.Populate(0); err != nil {
		return err
	}

	if err := s.Fail.Populate(0); err != nil {
		return err
	}

	if err := s.Neutral.Populate(0); err != nil {
		return err
	}

	if err := s.SoftFail.Populate(0); err != nil {
		return err
	}

	return nil
}

type SPFMechanism struct {
	r *SPFRecord

	mu sync.Mutex

	Includes map[string]*SPFRecord

	IP4 []*net.IPNet
	IP6 []*net.IPNet

	Ptr       bool
	PtrDomain []string

	A       bool
	ADomain []string
	AIPs    []*net.IPNet

	MX       bool
	MXDomain []string
	MXIPs    []*net.IPNet

	Explanation []string

	All bool
}

func (m *SPFMechanism) Populate(level int) error {
	if level > maxLevel {
		return errors.New("reached max expand level")
	}

	if err := m.PopulateRecords(); err != nil {
		return errors.Wrap(err, "expanding A and MX records")
	}

	if err := m.ExpandIncludes(); err != nil {
		return errors.Wrap(err, "expanding includes")
	}

	for _, inc := range m.Includes {
		if err := inc.Pass.Populate(level + 1); err != nil {
			return errors.Wrap(err, "failed to populate Pass mechanisms")
		}
		if err := inc.Fail.Populate(level + 1); err != nil {
			return errors.Wrap(err, "failed to populate Fail mechanisms")
		}
		if err := inc.Neutral.Populate(level + 1); err != nil {
			return errors.Wrap(err, "failed to populate Neutral mechanisms")
		}
		if err := inc.SoftFail.Populate(level + 1); err != nil {
			return errors.Wrap(err, "failed to populate SoftFail mechanisms")
		}
	}

	return nil
}

func (m *SPFMechanism) IsEmpty() bool {
	return !m.All &&
		!m.Ptr &&
		!m.A &&
		!m.MX &&
		len(m.ADomain) == 0 &&
		len(m.AIPs) == 0 &&
		len(m.IP4) == 0 &&
		len(m.IP6) == 0 &&
		len(m.Includes) == 0 &&
		len(m.MXDomain) == 0 &&
		len(m.MXIPs) == 0 &&
		len(m.PtrDomain) == 0
}

func (m *SPFMechanism) ExpandIncludes() error {
	var wg sync.WaitGroup

	errCh := make(chan error, len(m.Includes))

	for domain, inc := range m.Includes {
		if inc != nil {
			continue
		}

		wg.Add(1)
		go func(domain string) {
			defer wg.Done()

			spfr, err := GetSPF(domain)
			if err != nil {
				errCh <- err
				return
			}

			r, err := ParseSPF(spfr, domain)
			if err != nil {
				errCh <- err
				return
			}

			m.mu.Lock()
			defer m.mu.Unlock()

			m.Includes[domain] = r
		}(domain)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (m *SPFMechanism) PopulateRecords() error {
	if err := m.populateA(); err != nil {
		return errors.Wrap(err, "populating A IPs")
	}

	if err := m.populateMX(); err != nil {
		return errors.Wrap(err, "populating MX IPs")
	}

	return nil
}

func (m *SPFMechanism) populateA() error {
	m.AIPs = []*net.IPNet{}

	domains := []string{}
	domains = append(domains, m.ADomain...)

	if m.A {
		domains = append(domains, m.r.Domain)
	}

	for _, aref := range domains {
		ips, err := resolver.LookupIPAddr(context.Background(), aref)
		if err != nil {
			return errors.Wrap(err, "looking up A")
		}

		for _, ip := range ips {
			maskSize := 32
			if ip.IP.To4() == nil {
				//IPv6
				maskSize = 128
			}

			m.AIPs = append(m.AIPs, &net.IPNet{IP: ip.IP, Mask: net.CIDRMask(maskSize, maskSize)})
		}
	}

	return nil
}

func (m *SPFMechanism) populateMX() error {
	m.MXIPs = []*net.IPNet{}

	mxrefs := []string{}
	mxrefs = append(mxrefs, m.MXDomain...)

	if m.MX {
		directMX, err := GetMX(m.r.Domain)
		if err != nil {
			return errors.Wrap(err, "looking up direct MX")
		}
		mxrefs = append(mxrefs, directMX...)
	}

	for _, mx := range mxrefs {
		ips, err := resolver.LookupIPAddr(context.Background(), mx)
		if err != nil {
			return errors.Wrap(err, "looking up A")
		}

		for _, ip := range ips {
			maskSize := 32
			if ip.IP.To4() == nil {
				//IPv6
				maskSize = 128
			}

			m.MXIPs = append(m.MXIPs, &net.IPNet{IP: ip.IP, Mask: net.CIDRMask(maskSize, maskSize)})
		}
	}

	return nil
}

func ParseSPF(spf string, domain string) (*SPFRecord, error) {
	if !strings.HasPrefix(spf, "v=spf1 ") {
		return nil, errors.New("invalid prefix")
	}

	record := &SPFRecord{
		Domain:   domain,
		Original: spf,
	}
	record.Pass.r = record
	record.Fail.r = record
	record.Neutral.r = record
	record.SoftFail.r = record

	mechs := strings.Split(spf[len("v=spf1 "):], " ")

	for _, mech := range mechs {
		var set *SPFMechanism

		switch mech[0] {
		case '+':
			set = &record.Pass
			mech = mech[1:]
		case '-': //Fail
			set = &record.Fail
			mech = mech[1:]
		case '~': //SoftFail
			set = &record.SoftFail
			mech = mech[1:]
		case '?': //Neutral
			set = &record.Neutral
			mech = mech[1:]
		default: //Pass
			set = &record.Pass
		}

		switch mech {
		case "a":
			set.A = true
			continue
		case "mx":
			set.MX = true
			continue
		case "ptr":
			set.Ptr = true
			continue
		case "all":
			set.All = true
			continue
		}

		switch true {
		case strings.HasPrefix(mech, "ip4:"):
			ipnet, err := parseIPNet(mech[len("ip4:"):])
			if err != nil {
				return nil, errors.Wrap(err, "parsing IP6")
			}
			if set.IP4 == nil {
				set.IP4 = []*net.IPNet{}
			}

			set.IP4 = append(set.IP4, ipnet)
		case strings.HasPrefix(mech, "ip6:"):
			ipnet, err := parseIPNet(mech[len("ip6:"):])
			if err != nil {
				return nil, errors.Wrap(err, "parsing IP6")
			}
			if set.IP6 == nil {
				set.IP6 = []*net.IPNet{}
			}
			set.IP6 = append(set.IP6, ipnet)
		case strings.HasPrefix(mech, "mx:"):
			if set.MXDomain == nil {
				set.MXDomain = []string{}
			}
			set.MXDomain = append(set.MXDomain, mech[len("mx:"):])
		case strings.HasPrefix(mech, "ptr:"):
			if set.PtrDomain == nil {
				set.PtrDomain = []string{}
			}
			set.PtrDomain = append(set.PtrDomain, mech[len("ptr:"):])
		case strings.HasPrefix(mech, "include:"):
			if set.Includes == nil {
				set.Includes = map[string]*SPFRecord{}
			}
			set.Includes[mech[len("include:"):]] = nil
		case strings.HasPrefix(mech, "redirect="):
			set.r.Redirect = strings.TrimPrefix(mech, "redirect=")
		}
	}

	return record, nil
}

var (
	errSPFNotFound = errors.New("not found")
)

func GetSPF(domain string) (string, error) {
	rr, err := resolver.LookupTXT(context.Background(), domain)
	if err != nil {
		return "", errors.Wrap(err, "getting TXT record")
	}

	for _, r := range rr {
		if strings.HasPrefix(r, "v=spf1") {
			return r, nil
		}
	}

	return "", errSPFNotFound
}

func GetMX(domain string) ([]string, error) {
	mxs, err := resolver.LookupMX(context.Background(), domain)
	if err != nil {
		return nil, errors.Wrap(err, "getting MX record")
	}

	hosts := []string{}

	for _, mx := range mxs {
		hosts = append(hosts, mx.Host)
	}

	return hosts, nil
}

func GetIPs(domain string) ([]net.IP, error) {
	return resolver.LookupIP(context.Background(), "ip", domain)
}

func parseIPNet(ipStr string) (*net.IPNet, error) {
	fullMaskSize := 32
	if strings.Contains(ipStr, ":") {
		fullMaskSize = 128
	}

	ip, ipnet, err := net.ParseCIDR(ipStr)
	if err != nil {
		if !strings.Contains(ipStr, "/") { //doesn't include a prefix
			ip = net.ParseIP(ipStr)
			ipnet = &net.IPNet{IP: ip, Mask: net.CIDRMask(fullMaskSize, fullMaskSize)}
			if ip == nil {
				return nil, errors.Wrapf(err, "parsing IP %s", ipStr)
			}
		} else {
			return nil, errors.Wrapf(err, "parsing IPNet %s", ipStr)
		}
	}

	return &net.IPNet{IP: ip, Mask: ipnet.Mask}, nil
}

func splitAddrUserDomain(addr string) (string, string, error) {
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return "", "", errors.New("no at symbol in address")
	}

	local, domain := addr[:at], addr[at+1:]
	return local, domain, nil
}
