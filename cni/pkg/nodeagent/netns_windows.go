package nodeagent

import "io"

type WindowsNamespace struct {
	ID   uint32
	GUID string // TODO: Maybe use a more native guid type
}

type NamespaceCloser interface {
	NetnsCloser
	Namespace() WindowsNamespace
}

type namespaceCloser struct {
	nsCloser io.Closer
	ns       WindowsNamespace
}

func (n *namespaceCloser) Close() error {
	return n.nsCloser.Close()
}

func (n *namespaceCloser) Fd() uintptr {
	panic("not implemented on windows OS")
}

func (n *namespaceCloser) Inode() uint64 {
	panic("not implemented on windows OS")
}
