// Package mvu provides the canonical model-update-view runtime for terminal UI rendering.
//
// Host/attach callers should emit typed actions via Runtime.ApplyAction and
// snapshot inputs into this package, and use returned frame bytes directly.
// Rendering semantics such as top-row ownership, overlay visibility transitions,
// baseline masking, and final composition must be centralized here so
// regressions are testable in this package alone.
package mvu
