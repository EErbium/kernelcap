//go:build !linux

package proxy

type linuxProcResolver struct{}

var _ procResolverImpl = (*linuxProcResolver)(nil)

func newPlatformProcResolver() (procResolverImpl, error) {
	return nil, ErrProcResolverUnavailable
}

func (l *linuxProcResolver) start() error {
	return nil
}

func (l *linuxProcResolver) refresh() error {
	return nil
}

func (l *linuxProcResolver) resolveFromPort(localPort uint16, proxyPort int) (int, error) {
	return 0, ErrProcResolverUnavailable
}
