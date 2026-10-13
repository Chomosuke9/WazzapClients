package wa

import (
	"reflect"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A native flow single_select becomes a list button; a button with no
// name, and rows with no ID, are left out.
func TestSingleSelect(t *testing.T) {
	m := &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("Chat settings")},
		Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String("Tap a menu")},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
			Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
				{Name: proto.String("single_select"), ButtonParamsJSON: proto.String(`{"sections":[{"rows":[` +
					`{"description":"Tap to turn it off","id":"/trigger mention off","title":"Mention: on"},` +
					`{"title":"No ID"}],"title":"When I reply"}],"title":"When I reply"}`)},
				{Name: proto.String("single_select"), ButtonParamsJSON: proto.String(`{"sections":[{"rows":[` +
					`{"id":"/reset","title":"Reset chat"}],"title":"More"}]}`)},
				{Name: proto.String("")},
			},
		}},
	}}
	c := describe(m)
	want := []storedButton{
		{Button: model.Button{Kind: model.ButtonList, Label: "When I reply", Sections: []model.ListSection{
			{Title: "When I reply", Rows: []model.ListRow{{ID: "/trigger mention off", Title: "Mention: on", Description: "Tap to turn it off"}}},
		}}, Source: srcNative, Index: 0},
		{Button: model.Button{Kind: model.ButtonList, Label: "More", Sections: []model.ListSection{
			{Title: "More", Rows: []model.ListRow{{ID: "/reset", Title: "Reset chat"}}},
		}}, Source: srcNative, Index: 1},
	}
	if c.buttons == nil || !reflect.DeepEqual(c.buttons.Buttons, want) {
		t.Fatalf("buttons = %+v, want %+v", c.buttons, want)
	}
	if c.text != "Chat settings" || c.buttons.Footer != "Tap a menu" {
		t.Errorf("text %q, footer %q", c.text, c.buttons.Footer)
	}
}

// A ListMessage's button opens its rows too.
func TestListMessage(t *testing.T) {
	m := &waE2E.Message{ListMessage: &waE2E.ListMessage{
		Title:      proto.String("Menu"),
		ButtonText: proto.String("Pick one"),
		Sections: []*waE2E.ListMessage_Section{{
			Title: proto.String("Drinks"),
			Rows:  []*waE2E.ListMessage_Row{{RowID: proto.String("tea"), Title: proto.String("Tea")}},
		}},
	}}
	c := describe(m)
	if c.buttons == nil || len(c.buttons.Buttons) != 1 {
		t.Fatalf("buttons = %+v", c.buttons)
	}
	b := c.buttons.Buttons[0]
	if b.Kind != model.ButtonList || b.Label != "Pick one" || b.Source != srcList ||
		len(b.Sections) != 1 || b.Sections[0].Rows[0].ID != "tea" {
		t.Errorf("button = %+v", b)
	}
}
