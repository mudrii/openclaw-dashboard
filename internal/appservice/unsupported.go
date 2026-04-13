//go:build !darwin && !linux

package appservice

import "context"

// New returns ErrUnsupported on platforms with no service backend.
func New() (Backend, error) {
	return nil, ErrUnsupported
}

// NewWithContext returns ErrUnsupported on platforms with no service backend.
func NewWithContext(context.Context) (Backend, error) {
	return nil, ErrUnsupported
}
