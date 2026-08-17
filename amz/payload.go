package amz

import (
	"bytes"
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

// Rung 2 of the ladder: the JavaScript payloads.
//
// These are located by byte offset in the raw body, before any DOM work. A
// detail page is 2.2 MB and a light read that wants a title and a price should
// not pay for a full image manifest, so nothing here runs unless a field asks
// for it. See notes/Spec/3007/02_extraction.md sections 3 and 9.

// Payload anchor names. Each is the registration name the owning widget gave its
// own data, which is the same class of contract as data-feature-name.
const (
	PayloadImageBlock = "ImageBlockATF"
	PayloadTwister    = "twister-js-init-dpx-data"
)

// Image is one product photo with the sizes Amazon actually published.
//
// A selector gives a thumbnail URL and an invitation to rewrite its size token
// by hand. The payload gives the full resolution URL, the real dimensions, and
// which of MAIN, PT01, PT02 the photo is, so the record can say which picture is
// the front of the box.
type Image struct {
	HiRes   string           `json:"hires,omitempty"`
	Large   string           `json:"large,omitempty"`
	Thumb   string           `json:"thumb,omitempty"`
	Variant string           `json:"variant,omitempty"`
	Main    map[string][]int `json:"main,omitempty"`
}

// URL returns the best available URL for this image.
func (i Image) URL() string {
	switch {
	case i.HiRes != "":
		return i.HiRes
	case i.Large != "":
		return i.Large
	default:
		return i.Thumb
	}
}

// Video is one inline product video.
type Video struct {
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Thumb    string `json:"thumb,omitempty"`
	Duration string `json:"duration,omitempty"`
}

// ImageBlock is the parsed ImageBlockATF payload.
type ImageBlock struct {
	ASIN        string            `json:"asin,omitempty"`
	Images      []Image           `json:"images,omitempty"`
	Videos      []Video           `json:"videos,omitempty"`
	ColorToASIN map[string]string `json:"color_to_asin,omitempty"`
}

type rawImage struct {
	HiRes   string           `json:"hiRes"`
	Thumb   string           `json:"thumb"`
	Large   string           `json:"large"`
	Variant string           `json:"variant"`
	Main    map[string][]int `json:"main"`
}

type rawVideo struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Slate     string `json:"slateUrl"`
	Duration  string `json:"durationTimestamp"`
	VariantID string `json:"variant"`
}

// ReadImageBlock parses the image manifest out of a raw page body.
func ReadImageBlock(body []byte) (*ImageBlock, error) {
	obj, err := FindJSObject(string(body), PayloadImageBlock)
	if err != nil {
		return nil, err
	}
	ib := &ImageBlock{ASIN: obj.String("asin")}

	var colorImages struct {
		Initial []rawImage `json:"initial"`
	}
	if err := obj.Unmarshal("colorImages", &colorImages); err == nil {
		for _, r := range colorImages.Initial {
			ib.Images = append(ib.Images, Image{
				HiRes: r.HiRes, Large: r.Large, Thumb: r.Thumb,
				Variant: r.Variant, Main: r.Main,
			})
		}
	}

	var vids []rawVideo
	if err := obj.Unmarshal("videos", &vids); err == nil {
		for _, v := range vids {
			ib.Videos = append(ib.Videos, Video{
				Title: collapseSpace(v.Title), URL: v.URL,
				Thumb: v.Slate, Duration: v.Duration,
			})
		}
	}

	var c2a map[string]string
	if err := obj.Unmarshal("colorToAsin", &c2a); err == nil {
		ib.ColorToASIN = c2a
	} else {
		// The richer form maps a swatch name to an object carrying the ASIN.
		var nested map[string]struct {
			ASIN string `json:"asin"`
		}
		if obj.Unmarshal("colorToAsin", &nested) == nil {
			ib.ColorToASIN = map[string]string{}
			for k, v := range nested {
				if v.ASIN != "" {
					ib.ColorToASIN[k] = v.ASIN
				}
			}
		}
	}
	return ib, nil
}

// Twister is the variant matrix: which dimensions exist, what values each has,
// and which ASIN sits at each combination.
type Twister struct {
	CurrentASIN string              `json:"current_asin,omitempty"`
	ParentASIN  string              `json:"parent_asin,omitempty"`
	Dimensions  []string            `json:"dimensions,omitempty"`
	Values      map[string][]string `json:"values,omitempty"`
	// ByASIN maps each sibling ASIN to its dimension values, in Dimensions order.
	ByASIN map[string][]string `json:"by_asin,omitempty"`
	// Total is num_total_variations, which is the count Amazon has rather than
	// the count it shipped. A record listing six siblings when Total says 88 is
	// incomplete, and carrying both numbers makes that visible instead of implied.
	Total int `json:"total,omitempty"`
}

// Shipped is how many siblings this page actually carried.
func (t *Twister) Shipped() int {
	if t == nil {
		return 0
	}
	return len(t.ByASIN)
}

// Complete reports whether the page shipped every variant it claims to have.
func (t *Twister) Complete() bool {
	return t != nil && t.Total > 0 && t.Shipped() >= t.Total
}

// ReadTwister parses the variant matrix out of a raw page body.
func ReadTwister(body []byte) (*Twister, error) {
	obj, err := FindJSObject(string(body), PayloadTwister)
	if err != nil {
		return nil, err
	}
	t := &Twister{
		CurrentASIN: obj.String("currentAsin"),
		ParentASIN:  obj.String("parentAsin"),
		Total:       obj.Int("num_total_variations"),
	}
	_ = obj.Unmarshal("dimensions", &t.Dimensions)
	_ = obj.Unmarshal("variationValues", &t.Values)
	_ = obj.Unmarshal("dimensionValuesDisplayData", &t.ByASIN)
	if t.CurrentASIN == "" {
		t.CurrentASIN = obj.String("current_asin")
	}
	if t.ParentASIN == "" {
		t.ParentASIN = obj.String("parent_asin")
	}
	return t, nil
}

// ASINs returns the sibling ASINs the page shipped, sorted for a stable record.
func (t *Twister) ASINs() []string {
	if t == nil {
		return nil
	}
	out := make([]string, 0, len(t.ByASIN))
	for a := range t.ByASIN {
		out = append(out, a)
	}
	sortStrings(out)
	return out
}

var aStateRe = regexp.MustCompile(`(?s)<script[^>]+type="a-state"[^>]*data-a-state="([^"]*)"[^>]*>(.*?)</script>`)

// AState is one <script type="a-state"> blob: a key in an escaped attribute and
// strict JSON in the body.
type AState struct {
	Key  string          `json:"key"`
	Data json.RawMessage `json:"data,omitempty"`
	Size int             `json:"size"`
}

// ReadAStates returns every a-state blob on the page, in document order.
//
// A detail page carries between 14 and 27 of them and one other page type
// carries one. Three are useful today: desktop-twister-sort-filter-data,
// btf-sub-nav-desktop-context and turbo-checkout-page-state. The rest are
// interface state.
//
// All of them are kept anyway, because a blob that is interface state today is a
// data payload after the next redesign, and the cost of keeping them is a map.
func ReadAStates(body []byte) []AState {
	var out []AState
	for _, m := range aStateRe.FindAllSubmatch(body, -1) {
		var key struct {
			Key string `json:"key"`
		}
		_ = json.Unmarshal([]byte(html.UnescapeString(string(m[1]))), &key)
		data := bytes.TrimSpace(m[2])
		st := AState{Key: key.Key, Size: len(data)}
		if json.Valid(data) {
			st.Data = append(json.RawMessage(nil), data...)
		}
		if st.Key == "" {
			st.Key = "(unnamed)"
		}
		out = append(out, st)
	}
	return out
}

// AStateMap keys the blobs by name, which is how a field asks for one.
func AStateMap(states []AState) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(states))
	for _, s := range states {
		if s.Data != nil {
			out[s.Key] = s.Data
		}
	}
	return out
}

// HasPayload reports whether an anchor is present, without parsing.
func HasPayload(body []byte, anchor string) bool {
	return bytes.Contains(body, []byte(anchor))
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// normalizeImageURL strips the size token Amazon bakes into an image path, so
// the many renderings of one photo collapse to one master URL.
func normalizeImageURL(u string) string {
	i := strings.Index(u, "._")
	j := strings.LastIndex(u, ".")
	if i < 0 || j <= i {
		return u
	}
	return u[:i] + u[j:]
}
