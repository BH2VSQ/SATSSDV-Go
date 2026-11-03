package template

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestDefaultTemplatesValidate(t *testing.T) {
	if err := Validate(DefaultTemplates()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsOutOfRangeComponent(t *testing.T) {
	items := DefaultTemplates()
	items[0].Components[0].X = 101
	if err := Validate(items); err == nil {
		t.Fatal("expected out-of-range component to be rejected")
	}
}

func TestValidateRejectsUnknownComponentType(t *testing.T) {
	items := DefaultTemplates()
	items[0].Components[0].Type = "script"
	if err := Validate(items); err == nil {
		t.Fatal("expected unknown component type to be rejected")
	}
}

func TestDefaultTemplatesMatchFourPresetSlots(t *testing.T) {
	items := DefaultTemplates()
	if len(items) != 4 {
		t.Fatalf("got %d slots, want 4", len(items))
	}
	want := []string{"CQ", "RST", "73", "Blank"}
	for i := range items {
		if items[i].Name != want[i] {
			t.Fatalf("slot %d name = %q, want %q", i+1, items[i].Name, want[i])
		}
	}
	if len(items[3].Components) != 0 {
		t.Fatalf("blank slot has %d components", len(items[3].Components))
	}
	if err := Validate(items); err != nil {
		t.Fatalf("default templates invalid: %v", err)
	}
}

func TestTemplateBackgroundAssetIDIsPersistable(t *testing.T) {
	items := DefaultTemplates()
	items[0].BackgroundAssetID = "asset-123"
	if err := Validate(items); err != nil {
		t.Fatalf("background asset reference invalid: %v", err)
	}
}

func TestValidateAllowsAdditionalTemplatePresets(t *testing.T) {
	items := DefaultTemplates()
	for i := 0; i < 3; i++ {
		items = append(items, Template{ID: "custom-" + string(rune('a'+i)), Name: "Custom", Components: []Component{}})
	}
	if err := Validate(items); err != nil {
		t.Fatalf("five or more presets should be valid: %v", err)
	}
}

func TestValidateRejectsMoreThan32TemplatePresets(t *testing.T) {
	items := make([]Template, 33)
	for i := range items {
		items[i] = Template{ID: fmt.Sprintf("template-%d", i), Name: "Preset", Components: []Component{}}
	}
	if err := Validate(items); err == nil {
		t.Fatal("expected more than 32 presets to be rejected")
	}
}

func TestValidateRejectsEmptyTemplateList(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("expected an empty preset list to be rejected")
	}
}

func TestValidateRejectsOutOfRangeProportionalScale(t *testing.T) {
	items := DefaultTemplates()
	items[0].Components[0].Scale = 101
	if err := Validate(items); err == nil {
		t.Fatal("expected proportional image scale above 100%% to be rejected")
	}
}

func TestValidateAllowsProportionalScaleOnImageComponents(t *testing.T) {
	items := DefaultTemplates()
	items[0].Components = append(items[0].Components, Component{
		ID: "scaled-image", Type: "image", AssetID: "asset-1",
		X: 10, Y: 10, Scale: 42, Visible: true, Outline: true,
		OutlineWidth: 2, OutlineColor: "#000000",
	})
	if err := Validate(items); err != nil {
		t.Fatalf("valid proportional image scale was rejected: %v", err)
	}
}

func TestComponentProportionalScaleJSONRoundTrip(t *testing.T) {
	component := Component{ID: "image", Type: "image", AssetID: "asset-1", X: 12, Y: 18, Scale: 37.5}
	data, err := json.Marshal(component)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Component
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Scale != component.Scale {
		t.Fatalf("scale = %v, want %v", decoded.Scale, component.Scale)
	}
}
