//go:build darwin

package appservice

// New returns ErrUnsupported until the launchd backend is implemented (Task 5).
func New() (Backend, error) {
	return nil, ErrUnsupported
}
