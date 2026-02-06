package attach

import "testing"

func TestSelectRenderableViewPrefersVisibleWhenActiveHidden(t *testing.T) {
	active := &sessionView{id: "active-hidden", visible: false}
	visible := &sessionView{id: "visible", visible: true}
	views := map[string]*sessionView{
		active.id:  active,
		visible.id: visible,
	}

	view, id := selectRenderableView(views, active.id)
	if view != visible {
		t.Fatalf("expected visible view, got %+v", view)
	}
	if id != visible.id {
		t.Fatalf("expected resolved id %q, got %q", visible.id, id)
	}
}

func TestSelectRenderableViewFallsBackToSingleView(t *testing.T) {
	only := &sessionView{id: "only", visible: false}
	views := map[string]*sessionView{only.id: only}

	view, id := selectRenderableView(views, "missing")
	if view != only {
		t.Fatalf("expected single fallback view, got %+v", view)
	}
	if id != only.id {
		t.Fatalf("expected resolved id %q, got %q", only.id, id)
	}
}

func TestSelectRenderableViewFallsBackToAnyViewWhenNoVisible(t *testing.T) {
	first := &sessionView{id: "first", visible: false}
	second := &sessionView{id: "second", visible: false}
	views := map[string]*sessionView{
		first.id:  first,
		second.id: second,
	}

	view, id := selectRenderableView(views, "missing")
	if view == nil {
		t.Fatalf("expected fallback view")
	}
	if id == "" {
		t.Fatalf("expected resolved id")
	}
	if id != first.id && id != second.id {
		t.Fatalf("unexpected resolved id %q", id)
	}
}
