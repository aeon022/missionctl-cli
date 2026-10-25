package cmd

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func benchModel(w, h int) dashboardModel {
	m := newDashboardModel()
	m.width, m.height = w, h
	vals := make([]cardStatus, len(dashboardCards))
	for i := range vals {
		vals[i] = cardStatus{text: fmt.Sprintf("%d open\nnext thing\nand another line", i), items: []string{"a", "b", "c"}}
	}
	mi, _ := m.Update(cardsLoadedMsg{values: vals, at: time.Now()})
	return mi.(dashboardModel)
}

func BenchmarkView(b *testing.B) {
	m := benchModel(120, 24) // small terminal: the body scrolls
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkWheel(b *testing.B) {
	m := benchModel(120, 24)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mi, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		_ = mi.(dashboardModel).View()
	}
}

func TestWheelKeepsTheBodyCacheOtherMessagesDropIt(t *testing.T) {
	m := benchModel(120, 24)
	_ = m.gridBody()
	g := m.gen
	mi, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if mi.(dashboardModel).gen != g {
		t.Error("wheel scrolling must keep the cached grid")
	}
	if mi.(dashboardModel).scroll == 0 {
		t.Error("wheel must still scroll")
	}
	mi, _ = m.Update(tickMsg(time.Now()))
	if mi.(dashboardModel).gen == g {
		t.Error("any other message must invalidate the cache")
	}
}
