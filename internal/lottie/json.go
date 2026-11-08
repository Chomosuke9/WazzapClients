package lottie

import (
	"bytes"
	"encoding/json"
	"strings"
)

// The JSON of an animation. Properties (prop, pathProp) are parsed straight
// into what the renderer evaluates.

type jsonAnim struct {
	FR     float64     `json:"fr"`
	IP     float64     `json:"ip"`
	OP     float64     `json:"op"`
	W      float64     `json:"w"`
	H      float64     `json:"h"`
	Layers []jsonLayer `json:"layers"`
	Assets []jsonAsset `json:"assets"`
}

type jsonAsset struct {
	ID     json.RawMessage `json:"id"`
	Layers []jsonLayer     `json:"layers"`
	W      float64         `json:"w"`
	H      float64         `json:"h"`
	P      string          `json:"p"`
}

type jsonLayer struct {
	Ty     int             `json:"ty"`
	Ind    *int            `json:"ind"`
	Parent *int            `json:"parent"`
	IP     float64         `json:"ip"`
	OP     float64         `json:"op"`
	ST     float64         `json:"st"`
	SR     float64         `json:"sr"`
	KS     jsonTransform   `json:"ks"`
	Shapes []jsonShape     `json:"shapes"`
	RefID  json.RawMessage `json:"refId"`
	W      float64         `json:"w"`
	H      float64         `json:"h"`
	TM     *prop           `json:"tm"`
	SC     string          `json:"sc"`
	SW     float64         `json:"sw"`
	SH     float64         `json:"sh"`
	Masks  []jsonMask      `json:"masksProperties"`
	TT     int             `json:"tt"`
	TD     int             `json:"td"`
	TP     *int            `json:"tp"`
	HD     bool            `json:"hd"`
	BM     json.RawMessage `json:"bm"`
}

type jsonTransform struct {
	A  *prop        `json:"a"`
	P  positionProp `json:"p"`
	S  *prop        `json:"s"`
	R  *prop        `json:"r"`
	RZ *prop        `json:"rz"`
	RX *prop        `json:"rx"`
	RY *prop        `json:"ry"`
	OR *prop        `json:"or"`
	O  *prop        `json:"o"`
	SK *prop        `json:"sk"`
	SA *prop        `json:"sa"`
	SO *prop        `json:"so"`
	EO *prop        `json:"eo"`
}

// positionProp is a position, which may be split into x and y.
type positionProp struct {
	p, x, y *prop
}

func (pp *positionProp) UnmarshalJSON(b []byte) error {
	var split struct {
		S bool  `json:"s"`
		X *prop `json:"x"`
		Y *prop `json:"y"`
	}
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		if err := json.Unmarshal(b, &split); err == nil && split.S {
			pp.x, pp.y = split.X, split.Y
			return nil
		}
	}
	pp.p = new(prop)
	return pp.p.UnmarshalJSON(b)
}

// jsonShape is any item of a shape layer: the fields of every kind, told
// apart by Ty. Fields that are a property in one kind and a number in
// another stay raw.
type jsonShape struct {
	Ty  string          `json:"ty"`
	HD  bool            `json:"hd"`
	It  []jsonShape     `json:"it"`
	KS  *pathProp       `json:"ks"`
	P   *prop           `json:"p"`
	S   *prop           `json:"s"`
	E   *prop           `json:"e"`
	R   json.RawMessage `json:"r"`
	O   *prop           `json:"o"`
	A   *prop           `json:"a"`
	C   *prop           `json:"c"`
	D   json.RawMessage `json:"d"`
	W   *prop           `json:"w"`
	LC  int             `json:"lc"`
	LJ  int             `json:"lj"`
	ML  float64         `json:"ml"`
	ML2 *prop           `json:"ml2"`
	T   int             `json:"t"`
	G   *jsonGradient   `json:"g"`
	H   *prop           `json:"h"`
	M   int             `json:"m"`
	SY  int             `json:"sy"`
	PT  *prop           `json:"pt"`
	OR  *prop           `json:"or"`
	IR  *prop           `json:"ir"`
	OS  *prop           `json:"os"`
	IS  *prop           `json:"is"`
	SK  *prop           `json:"sk"`
	SA  *prop           `json:"sa"`
	TR  *jsonTransform  `json:"tr"`
}

type jsonGradient struct {
	P int  `json:"p"`
	K prop `json:"k"`
}

type jsonDash struct {
	N string `json:"n"`
	V *prop  `json:"v"`
}

type jsonMask struct {
	Mode string    `json:"mode"`
	Inv  bool      `json:"inv"`
	PT   *pathProp `json:"pt"`
	O    *prop     `json:"o"`
}

// idString reads an asset ID, a string or (in some files) a number.
func idString(b json.RawMessage) string {
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(b))
}

// rawProp reads a property kept raw because another kind of item uses the
// same field for a number.
func rawProp(b json.RawMessage) *prop {
	if len(b) == 0 {
		return nil
	}
	p := new(prop)
	if p.UnmarshalJSON(b) != nil {
		return nil
	}
	return p
}

// rawInt reads a number kept raw.
func rawInt(b json.RawMessage) int {
	v := floats(b)
	if len(v) == 0 {
		return 0
	}
	return int(v[0])
}

// propK returns a property's value: its "k", or the JSON itself when it
// isn't an object.
func propK(b []byte) (json.RawMessage, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' {
		return b, nil
	}
	var o struct {
		K json.RawMessage `json:"k"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, err
	}
	return o.K, nil
}

// isKeyframes reports whether a property's value is a list of keyframes.
func isKeyframes(k json.RawMessage) bool {
	k = bytes.TrimSpace(k)
	if len(k) == 0 || k[0] != '[' {
		return false
	}
	rest := bytes.TrimSpace(k[1:])
	return len(rest) > 0 && rest[0] == '{'
}

// floats reads a number or a list of numbers.
func floats(b json.RawMessage) []float64 {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] == 'n' {
		return nil
	}
	if b[0] == '[' {
		var v []float64
		if json.Unmarshal(b, &v) == nil {
			return v
		}
		var vv [][]float64
		if json.Unmarshal(b, &vv) == nil && len(vv) > 0 {
			return vv[0]
		}
		return nil
	}
	var f float64
	if json.Unmarshal(b, &f) == nil {
		return []float64{f}
	}
	return nil
}

func isTrue(b json.RawMessage) bool {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return false
	}
	if b[0] == 't' {
		return true
	}
	v := floats(b)
	return len(v) > 0 && v[0] != 0
}

type rawKeyframe struct {
	T  float64         `json:"t"`
	S  json.RawMessage `json:"s"`
	E  json.RawMessage `json:"e"`
	I  *rawEase        `json:"i"`
	O  *rawEase        `json:"o"`
	H  json.RawMessage `json:"h"`
	TO []float64       `json:"to"`
	TI []float64       `json:"ti"`
}

type rawEase struct {
	X json.RawMessage `json:"x"`
	Y json.RawMessage `json:"y"`
}

func (e *rawEase) xy() (x, y []float64) {
	if e == nil {
		return nil, nil
	}
	return floats(e.X), floats(e.Y)
}

func (p *prop) UnmarshalJSON(b []byte) error {
	k, err := propK(b)
	if err != nil {
		return err
	}
	if !isKeyframes(k) {
		p.static = floats(k)
		return nil
	}
	var raw []rawKeyframe
	if err := json.Unmarshal(k, &raw); err != nil {
		return err
	}
	p.kfs = make([]keyframe, len(raw))
	for i := range raw {
		r := &raw[i]
		kf := &p.kfs[i]
		kf.t = r.T
		kf.s, kf.e = floats(r.S), floats(r.E)
		kf.hold = isTrue(r.H)
		kf.ox, kf.oy = r.O.xy()
		kf.ix, kf.iy = r.I.xy()
		if len(kf.ox) == 0 || len(kf.oy) == 0 || len(kf.ix) == 0 || len(kf.iy) == 0 {
			kf.ox, kf.oy, kf.ix, kf.iy = nil, nil, nil, nil
		}
		if spatial(r.TO, r.TI) {
			kf.to, kf.ti = r.TO, r.TI
		}
	}
	return nil
}

func spatial(to, ti []float64) bool {
	if len(to) < 2 || len(ti) < 2 {
		return false
	}
	return to[0] != 0 || to[1] != 0 || ti[0] != 0 || ti[1] != 0
}

type rawBez struct {
	C bool        `json:"c"`
	V [][]float64 `json:"v"`
	I [][]float64 `json:"i"`
	O [][]float64 `json:"o"`
}

// parseBez reads a path's value: an object, or a list holding one.
func parseBez(b json.RawMessage) *bez {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil
	}
	if b[0] == '[' {
		var list []json.RawMessage
		if json.Unmarshal(b, &list) != nil || len(list) == 0 {
			return nil
		}
		return parseBez(list[0])
	}
	var r rawBez
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	z := &bez{closed: r.C}
	n := len(r.V)
	z.v = make([]vec, n)
	z.i = make([]vec, n)
	z.o = make([]vec, n)
	for i := 0; i < n; i++ {
		z.v[i] = pt(r.V[i])
		if i < len(r.I) {
			z.i[i] = pt(r.I[i])
		}
		if i < len(r.O) {
			z.o[i] = pt(r.O[i])
		}
	}
	return z
}

func pt(v []float64) vec {
	switch len(v) {
	case 0:
		return vec{}
	case 1:
		return vec{v[0], 0}
	}
	return vec{v[0], v[1]}
}

func (p *pathProp) UnmarshalJSON(b []byte) error {
	k, err := propK(b)
	if err != nil {
		return err
	}
	if !isKeyframes(k) {
		p.static = parseBez(k)
		return nil
	}
	var raw []rawKeyframe
	if err := json.Unmarshal(k, &raw); err != nil {
		return err
	}
	p.kfs = make([]pathKF, len(raw))
	for i := range raw {
		r := &raw[i]
		kf := &p.kfs[i]
		kf.t = r.T
		kf.s, kf.e = parseBez(r.S), parseBez(r.E)
		kf.hold = isTrue(r.H)
		ox, oy := r.O.xy()
		ix, iy := r.I.xy()
		if len(ox) > 0 && len(oy) > 0 && len(ix) > 0 && len(iy) > 0 {
			kf.ease = true
			kf.ox, kf.oy, kf.ix, kf.iy = ox[0], oy[0], ix[0], iy[0]
		}
	}
	return nil
}
