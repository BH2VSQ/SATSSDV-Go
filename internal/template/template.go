package template

import "time"

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

// DefaultTemplates supplies usable, layered presets on a fresh install. Each
// design uses the live QSO fields, so the operator can transmit without first
// building a layout from an empty canvas. A portable pack can replace or extend
// these defaults after installation.
func DefaultTemplates() []Template {
	makeText := func(id, text string, y, size float64, color string) Component {
		return Component{ID: id, Type: "text", Text: text, X: 5, Y: y, Width: 90, Height: 16,
			FontSize: size, Color: color, Bold: true, Visible: true, Outline: true, OutlineWidth: 2, OutlineColor: "#101820"}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return []Template{
		{ID: "1", Name: "CQ", UpdatedAt: now, Components: []Component{
			makeText("cq-kicker", "SATELLITE QSO · CALLING", 8, 15, "#58d4e8"),
			makeText("cq-title", "CQ CQ CQ", 31, 46, "#ffffff"),
			makeText("cq-call", "{MYCALL}", 58, 30, "#ffffff"),
			makeText("cq-locator", "{LOCATOR}  ·  {DATE}", 84, 14, "#c7e8ef"),
		}},
		{ID: "2", Name: "RST", UpdatedAt: now, Components: []Component{
			makeText("rst-kicker", "SIGNAL REPORT", 12, 16, "#58d4e8"),
			makeText("rst-call", "{HISCALL}", 36, 28, "#ffffff"),
			makeText("rst-value", "RST {REPORT}", 61, 40, "#ffffff"),
			makeText("rst-footer", "FROM {MYCALL}  ·  {LOCATOR}", 86, 13, "#c7e8ef"),
		}},
		{ID: "3", Name: "73", UpdatedAt: now, Components: []Component{
			makeText("73-kicker", "THANK YOU FOR THE CONTACT", 10, 15, "#58d4e8"),
			makeText("73-main", "73", 33, 78, "#ffffff"),
			makeText("73-call", "{MYCALL}", 72, 29, "#ffffff"),
			makeText("73-locator", "{LOCATOR}  ·  {TIME}", 88, 13, "#c7e8ef"),
		}},
		{ID: "4", Name: "Blank", UpdatedAt: now, Components: []Component{}},
	}
}
