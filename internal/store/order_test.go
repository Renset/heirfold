package store

import (
	"strings"
	"testing"
)

func TestOrderEvents(t *testing.T) {
	event := func(id, kind string, sort int64) EventView { return EventView{ID: id, Type: kind, Sort: sort} }
	cases := []struct {
		name string
		in   []EventView
		want string
	}{
		{"dates decide, also across a death", []EventView{
			event("birth", "Birth", 19251108), event("service", "Military Service", 19430100), event("death", "Death", 19980312),
			event("wedding", "Marriage", 19520607), event("medal", "Award", 19450509),
		}, "birth service medal wedding death"},
		{"a year without a month stands at the start of the year, but a death at its end", []EventView{
			event("death", "Death", 19980000), event("letter", "Residence", 19980500), event("move", "Residence", 19980000),
			event("burial", "Burial", 19980000),
		}, "move letter death burial"},
		{"a month without a day works the same way", []EventView{
			event("death", "Death", 19980300), event("visit", "Residence", 19980320), event("burial", "Burial", 19980300),
			event("april", "Residence", 19980401),
		}, "visit death burial april"},
		{"an event without a date stays after the event that it followed", []EventView{
			event("birth", "Birth", 19251108), event("school", "Education", 0), event("death", "Death", 19980312),
			event("note", "Residence", 0), event("wedding", "Marriage", 19520607), event("child", "Residence", 0),
		}, "birth school wedding child death note"},
		{"without dates a birth goes first and a death last", []EventView{
			event("death", "Death", 0), event("job", "Occupation", 0), event("burial", "Burial", 0), event("birth", "Birth", 0),
			event("wedding", "Marriage", 19520607),
		}, "birth job wedding death burial"},
		{"events without dates before the first dated one stay first, after an undated birth", []EventView{
			event("job", "Occupation", 0), event("home", "Residence", 0), event("wedding", "Marriage", 19520607),
			event("birth", "Birth", 0),
		}, "birth job home wedding"},
		{"on one day a birth is first and a death is last", []EventView{
			event("death", "Death", 19000101), event("baptism", "Baptism", 19000101), event("birth", "Birth", 19000101),
		}, "birth baptism death"},
		{"nothing to order", nil, ""},
	}
	for _, c := range cases {
		OrderEvents(c.in)
		var ids []string
		for _, item := range c.in {
			ids = append(ids, item.ID)
		}
		if got := strings.Join(ids, " "); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}
