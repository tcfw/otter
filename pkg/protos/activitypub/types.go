package activitypub

import (
	"bytes"
	"encoding/json"
	"time"
)

type JSONLD struct {
	// json.RawMessage

	Context any    `json:"@context"`
	Type    string `json:"type"`
}

type WebFingerJRD struct {
	Subject string   `json:"subject,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
	// Properties map[string]*string `json:"properties,omitempty"`
	Links []WebFingerJRDLink `json:"links,omitempty"`
}

type WebFingerJRDLink struct {
	Rel  string `json:"rel"`
	Type string `json:"type,omitempty"`
	Href string `json:"href,omitempty"`
	// Titles     map[string]string  `json:"titles,omitempty"`
	// Properties map[string]*string `json:"properties,omitempty"`
}

type Object struct {
	JSONLD

	Attachment   []Object          `json:"attachment,omitempty"`
	AttributedTo []Object          `json:"attributedTo,omitempty"`
	Audience     *Object           `json:"audience,omitempty"`
	Content      *string           `json:"content,omitempty"`
	Name         *string           `json:"name,omitempty"`
	NameMap      map[string]string `json:"nameMap,omitempty"`
	EndTime      *time.Time        `json:"endTime,omitempty"`
	Generator    *Object           `json:"generator,omitempty"`
	Icon         []Image           `json:"icon,omitempty"`
	Image        *Image            `json:"image,omitempty"`
	InReplyTo    *Object           `json:"inReplyTo,omitempty"`
	Location     *Location         `json:"location,omitempty"`
	Preview      *Object           `json:"preview,omitempty"`
	Published    *time.Time        `json:"published,omitempty"`
	Replies      *Collection[any]  `json:"replies,omitempty"`
	StartTime    *time.Time        `json:"startTime,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	SummaryMap   map[string]string `json:"summaryMap,omitempty"`
	Tag          []Object          `json:"tag,omitempty"`
	Updated      *time.Time        `json:"updated,omitempty"`
	Url          *LinkRef          `json:"url,omitempty"`
	To           *Link             `json:"to,omitempty"`
	Bto          *Link             `json:"bto,omitempty"`
	Cc           *Link             `json:"cc,omitempty"`
	Bcc          *Link             `json:"bcc,omitempty"`
	MediaType    string            `json:"mediaType,omitempty"`
	Duration     time.Duration     `json:"duration,omitempty"`
}

type Image struct{}
type Location struct{}

type Link struct {
	JSONLD
	Object

	Href      string            `json:"href,omitempty"`
	Rel       []string          `json:"rel,omitempty"`
	MediaType string            `json:"mediaType,omitempty"`
	Name      string            `json:"name,omitempty"`
	NameMap   map[string]string `json:"nameMap,omitempty"`
	Hreflang  string            `json:"hreflang,omitempty"`
	Height    uint              `json:"height,omitempty"`
	Width     uint              `json:"width,omitempty"`
	Preview   *Object           `json:"preview,omitempty"`
}

type GenericRef[T any | string] struct {
	Single string
	Multi  []T
}

type LinkRef GenericRef[Link]

func (r *LinkRef) UnmarshalJSON(d []byte) error {
	dec := json.NewDecoder(bytes.NewReader(d))
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	switch tt := tok.(type) {
	case string:
		r.Single = tt
		return nil
	default:
		return json.Unmarshal(d, &r.Multi)
	}
}

func (r *LinkRef) MarshalJSON() ([]byte, error) {
	if len(r.Multi) != 0 {
		return json.Marshal(r.Multi)
	}

	return json.Marshal(r.Single)
}

type ActorRef GenericRef[*Object]

func (r *ActorRef) UnmarshalJSON(d []byte) error {
	dec := json.NewDecoder(bytes.NewReader(d))
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	switch tt := tok.(type) {
	case string:
		r.Single = tt
		return nil
	default:
		return json.Unmarshal(d, &r.Multi)
	}
}

func (r *ActorRef) MarshalJSON() ([]byte, error) {
	if len(r.Multi) != 0 {
		return json.Marshal(r.Multi)
	}

	return json.Marshal(r.Single)
}

type Collection[T any] struct {
	JSONLD

	TotalItems uint     `json:"totalItems"`
	Current    *LinkRef `json:"current,omitempty"`
	First      *LinkRef `json:"first,omitempty"`
	Last       *LinkRef `json:"last,omitempty"`
	Items      []T      `json:"items,omitempty"`
}

type OrderedCollection[T any] struct {
	Collection[T]

	OrderedItems []T `json:"orderedItems,omitempty"`
}

type CollectionPage[T any] struct {
	Collection[T]

	PartOf *LinkRef `json:"partOf,omitempty"`
	Next   *LinkRef `json:"next,omitempty"`
	Prev   *LinkRef `json:"prev,omitempty"`
}

type OrderedCollectionPage[T any] struct {
	OrderedCollection[T]

	PartOf *LinkRef `json:"partOf,omitempty"`
	Next   *LinkRef `json:"next,omitempty"`
	Prev   *LinkRef `json:"prev,omitempty"`
}

type Actor struct {
	Object

	Id     string `json:"id"`
	Inbox  string `json:"inbox"`
	Outbox string `json:"outbox"`

	Following string `json:"following,omitempty"`
	Followers string `json:"followers,omitempty"`
	Liked     string `json:"liked,omitempty"`

	Streams           string           `json:"streams,omitempty"`
	PreferredUsername string           `json:"preferredUsername,omitempty"`
	Endpoints         []ActorEndpoints `json:"endpoints,omitempty"`

	ManuallyApprovesFollowers bool `json:"manuallyApprovesFollowers"`
	Discoverable              bool `json:"discoverable"`

	LastStatusAt time.Time `json:"last_status_at"`
}

type ActorEndpoints struct {
	ProxyUrl                   string `json:"proxyUrl"`
	OauthAuthorizationEndpoint string `json:"oauthAuthorizationEndpoint"`
	OauthTokenEndpoint         string `json:"oauthTokenEndpoint"`
	ProvideClientKey           string `json:"provideClientKey"`
	SignClientKey              string `json:"signClientKey"`
	SharedInbox                string `json:"sharedInbox"`
}

type Activity struct {
	Object

	Actor       *ActorRef `json:"actor,omitempty"`
	ChildObject *Object   `json:"object,omitempty"`
	Target      []LinkRef `json:"target,omitempty"`
	Result      []LinkRef `json:"result,omitempty"`
	Origin      []LinkRef `json:"origin,omitempty"`
	Instrument  []LinkRef `json:"instrument,omitempty"`
}
