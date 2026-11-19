//go:build !linux && !darwin

package netbind

const supported = false

func (b *Binder) bind(uintptr, string) error { return ErrUnsupported }
