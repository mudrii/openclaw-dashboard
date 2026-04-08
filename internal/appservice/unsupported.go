//go:build !darwin && !linux

package appservice

// New returns ErrUnsupported on platforms with no service backend.
func New() (Backend, error) {
	return nil, ErrUnsupported
}
