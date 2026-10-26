package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"golang.design/x/x11"
)

func destroyEndpointWindow(t *testing.T, c *xconn, window uint32) {
	t.Helper()
	req := make([]byte, 8)
	req[0] = 4
	le.PutUint16(req[2:], 2)
	le.PutUint32(req[4:], window)
	if err := c.send(req); err != nil {
		t.Fatal(err)
	}
}

func convertedEndpointProperty(t *testing.T, c *xconn, selection, target, property uint32) uint32 {
	t.Helper()
	if err := c.send(x11.ConvertSelection(c.win, selection, target, property, 0)); err != nil {
		t.Fatal(err)
	}
	for {
		p, err := c.read()
		if err != nil {
			t.Fatal(err)
		}
		if p.EventCode() == x11.EventSelectionNotify {
			event := p.SelectionNotify()
			if event.Requestor == c.win && event.Target == target {
				return event.Property
			}
		}
	}
}

func TestClipboardEndpointDestroyWindowReleasesProperties(t *testing.T) {
	e := regressionEndpoint(t)
	c := endpointWireClient(t, e, x11.FamilyLocal)
	img := testImage(t, "image/png", false)
	if err := e.Set(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	selection := endpointAtom(t, c, "CLIPBOARD", false)
	target := endpointAtom(t, c, "image/png", false)
	property := endpointAtom(t, c, "WINDOW_IMAGE", false)
	first := c.win
	second := first + 1
	endpointConvert(t, c, selection, target, property)
	c.win = second
	if err := c.send(x11.CreateWindow(second, 1)); err != nil {
		t.Fatal(err)
	}
	endpointConvert(t, c, selection, target, property)
	destroyEndpointWindow(t, c, first)
	old := endpointReply(t, c, x11.GetProperty(false, first, property, target, 0, ^uint32(0)))
	if le.Uint32(old.Raw[8:]) != 0 || len(old.PropertyValue()) != 0 {
		t.Fatal("destroyed window retained image property")
	}
	sibling := endpointReply(t, c, x11.GetProperty(false, second, property, target, 0, ^uint32(0)))
	if !bytes.Equal(sibling.PropertyValue(), img.Data) {
		t.Fatal("destroying one window removed another window's property")
	}
}

func TestClipboardEndpointPropertyCapacityAndReclamation(t *testing.T) {
	e := regressionEndpoint(t)
	c := endpointWireClient(t, e, x11.FamilyLocal)
	img := testImage(t, "image/png", false)
	if err := e.Set(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	selection := endpointAtom(t, c, "CLIPBOARD", false)
	target := endpointAtom(t, c, "image/png", false)
	var properties []uint32
	for i := 0; i < 33; i++ {
		properties = append(properties, endpointAtom(t, c, fmt.Sprintf("PROPERTY_%d", i), false))
	}
	for _, property := range properties[:32] {
		endpointConvert(t, c, selection, target, property)
	}
	if got := convertedEndpointProperty(t, c, selection, target, properties[32]); got != 0 {
		t.Fatal("ConvertSelection bypassed the property limit")
	}
	if got := convertedEndpointProperty(t, c, selection, target, properties[0]); got != properties[0] {
		t.Fatal("existing property cannot be replaced at capacity")
	}
	if err := c.send(x11.ChangeProperty(c.win, properties[0], 31, 8, []byte("replacement"))); err != nil {
		t.Fatal(err)
	}
	changed, err := c.reply(x11.GetProperty(false, c.win, properties[0], 31, 0, 32))
	if err != nil || string(changed.PropertyValue()) != "replacement" {
		t.Fatal("ChangeProperty could not replace an existing property at capacity", err)
	}
	full := endpointReply(t, c, x11.ChangeProperty(c.win, properties[32], 31, 8, []byte("overflow")))
	if !full.IsError() || full.ErrorCode() != 11 {
		t.Fatal("new property did not return BadAlloc at capacity")
	}
	destroyEndpointWindow(t, c, c.win)
	for i := 0; i < 40; i++ {
		c.win++
		if err := c.send(x11.CreateWindow(c.win, 1)); err != nil {
			t.Fatal(err)
		}
		endpointConvert(t, c, selection, target, properties[0])
		destroyEndpointWindow(t, c, c.win)
		p := endpointReply(t, c, x11.GetProperty(false, c.win, properties[0], target, 0, 32))
		if le.Uint32(p.Raw[8:]) != 0 {
			t.Fatal("destroyed window did not release its property slot")
		}
	}
}
