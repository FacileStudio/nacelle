package nacelle

// Image is a picture the caller attached, carried as its raw bytes.
//
// MediaType is one of image/png, image/jpeg, image/gif or image/webp, the four
// every backend here accepts. It belongs on a user message only: no model API
// takes an image back from the assistant side.
type Image struct {
	MediaType string `json:"media_type"`
	Data      []byte `json:"data"`
}

// Document is a file the caller attached, carried as its raw bytes.
//
// MediaType is application/pdf. Name is the file name a backend passes along
// where its wire format has a field for one. Like Image, it belongs on a user
// message only, and a backend that cannot send it refuses the request rather
// than leaving the file out.
type Document struct {
	Name      string
	MediaType string
	Data      []byte
}

func (Image) part()    {}
func (Document) part() {}
