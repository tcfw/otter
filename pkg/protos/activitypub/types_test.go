package activitypub

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestUnmarshalOrderedCollection(t *testing.T) {
	obj := []byte(`{
  "@context": "https://www.w3.org/ns/activitystreams",
  "summary": "Page 1 of Sally's notes",
  "type": "OrderedCollectionPage",
  "id": "http://example.org/foo?page=1",
  "partOf": "http://example.org/foo",
  "orderedItems": [
    {
      "type": "Note",
      "name": "A Simple Note"
    },
    {
      "type": "Note",
      "name": "Another Simple Note"
    }
  ]
}`)

	oc := &OrderedCollectionPage[Object]{}

	if err := json.Unmarshal(obj, oc); err != nil {
		panic(err)
	}

	fmt.Printf("%+v", oc)
}
