package model

import "testing"

func TestRichFromMarkdown(t *testing.T) {
	rr := RichFromMarkdown("Intro\n\n```py\nprint(1)\n```\n| a | b |\n|:--|--:|\n| 1 | 2 |\n\n```x``` end")
	if rr == nil || len(rr.Sections) != 4 {
		t.Fatalf("%+v", rr)
	}
	kinds := []RichKind{RichMarkdown, RichCode, RichTable, RichMarkdown}
	for i, k := range kinds {
		if rr.Sections[i].Kind != k {
			t.Errorf("section %d is %d, want %d", i, rr.Sections[i].Kind, k)
		}
	}
	if rr.Sections[1].Lang != "py" || rr.Sections[1].Code[0].Text != "print(1)" || len(rr.Sections[2].Rows) != 2 ||
		rr.Sections[3].Text != "```x``` end" {
		t.Errorf("%+v", rr.Sections)
	}
	if RichFromMarkdown("| not | a table |\nplain").Sections[0].Kind != RichMarkdown || RichFromMarkdown("  ") != nil {
		t.Error("a lone row or blank text")
	}
}
