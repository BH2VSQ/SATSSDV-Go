package template

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate enforces safe, bounded template data before it is persisted or
// rendered. Coordinates and dimensions are expressed as canvas percentages.
func Validate(items []Template) error {
	if len(items) < 1 {
		return errors.New("at least one template preset is required")
	}
	if len(items) > 32 {
		return errors.New("at most 32 template presets are supported")
	}
	slots := make(map[string]struct{}, len(items))
	for i, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return fmt.Errorf("template slot %d has an empty ID", i+1)
		}
		if _, exists := slots[id]; exists {
			return fmt.Errorf("template slot ID %q is duplicated", id)
		}
		slots[id] = struct{}{}
		if len([]rune(item.Name)) > 48 {
			return fmt.Errorf("template slot %q name exceeds 48 characters", id)
		}
		if len(item.BackgroundAssetID) > 128 {
			return fmt.Errorf("template slot %q background asset ID is too long", id)
		}
		if len(item.Components) > 64 {
			return fmt.Errorf("template slot %q contains more than 64 components", id)
		}
		componentIDs := make(map[string]struct{}, len(item.Components))
		for j, c := range item.Components {
			if strings.TrimSpace(c.ID) == "" {
				return fmt.Errorf("template slot %q component %d has an empty ID", id, j+1)
			}
			if _, exists := componentIDs[c.ID]; exists {
				return fmt.Errorf("template slot %q contains duplicate component ID %q", id, c.ID)
			}
			componentIDs[c.ID] = struct{}{}
			switch c.Type {
			case "text":
				if len([]rune(c.Text)) > 4096 {
					return fmt.Errorf("template component %q text exceeds 4096 characters", c.ID)
				}
			case "image":
				if strings.TrimSpace(c.AssetID) == "" {
					return fmt.Errorf("template image component %q has no gallery asset", c.ID)
				}
			case "last-rx-image":
			default:
				return fmt.Errorf("template component %q has unsupported type %q", c.ID, c.Type)
			}
			if c.X < 0 || c.X > 100 || c.Y < 0 || c.Y > 100 || c.Width < 0 || c.Width > 100 || c.Height < 0 || c.Height > 100 {
				return fmt.Errorf("template component %q coordinates and dimensions must be 0..100%%", c.ID)
			}
			if c.Scale < 0 || c.Scale > 100 {
				return fmt.Errorf("template component %q scale must be 0..100%%", c.ID)
			}
			if c.FontSize != 0 && (c.FontSize < 6 || c.FontSize > 160) {
				return fmt.Errorf("template component %q font size must be 6..160 px", c.ID)
			}
			if c.OutlineWidth < 0 || c.OutlineWidth > 20 {
				return fmt.Errorf("template component %q outline width must be 0..20 px", c.ID)
			}
			if c.Color != "" && !colorPattern.MatchString(c.Color) {
				return fmt.Errorf("template component %q has an invalid text color", c.ID)
			}
			if c.OutlineColor != "" && !colorPattern.MatchString(c.OutlineColor) {
				return fmt.Errorf("template component %q has an invalid outline color", c.ID)
			}
		}
	}
	return nil
}
