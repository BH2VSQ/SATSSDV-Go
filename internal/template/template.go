package template

type Component struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"` // text, image, last-rx-image
	Text         string  `json:"text,omitempty"`
	AssetID      string  `json:"assetId,omitempty"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	Width        float64 `json:"width,omitempty"`  // Deprecated, retained for existing template files.
	Height       float64 `json:"height,omitempty"` // Deprecated, retained for existing template files.
	Scale        float64 `json:"scale,omitempty"`
	FontSize     float64 `json:"fontSize,omitempty"`
	Bold         bool    `json:"bold,omitempty"`
	Color        string  `json:"color,omitempty"`
	Visible      bool    `json:"visible"`
	Outline      bool    `json:"outline"`
	OutlineWidth float64 `json:"outlineWidth"`
	OutlineColor string  `json:"outlineColor,omitempty"`
}
type Template struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	BackgroundAssetID string      `json:"backgroundAssetId,omitempty"`
	Components        []Component `json:"components"`
	UpdatedAt         string      `json:"updatedAt"`
}

// DefaultTemplates provides the four initial TX presets: CQ, RST, 73 and a
// blank canvas. Operators can add, duplicate, rename and delete presets later.
func DefaultTemplates() []Template {
	makeText := func(id, text string, y float64) Component {
		return Component{ID: id, Type: "text", Text: text, X: 5, Y: y, Width: 90, Height: 16, FontSize: 32, Color: "#ffffff", Visible: true, Outline: true, OutlineWidth: 2, OutlineColor: "#000000"}
	}
	return []Template{
		{ID: "1", Name: "CQ", Components: []Component{makeText("cq", "CQ {MYCALL}", 8)}},
		{ID: "2", Name: "RST", Components: []Component{makeText("rst", "{HISCALL}  RST {REPORT}", 72)}},
		{ID: "3", Name: "73", Components: []Component{makeText("73", "73 {MYCALL}", 78)}},
		{ID: "4", Name: "Blank", Components: []Component{}},
	}
}
