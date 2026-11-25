package app

import "onrdp/internal/rdpclient"

// remoteClient is the desktop surface shared by the in-process protocol clients.
type remoteClient interface {
	Connect(rdpclient.Config)
	Status() rdpclient.Snapshot
	Flush() ([]rdpclient.Region, int, int, uint64)
	Close()
	KeyDown(rdpclient.Key)
	KeyUp(rdpclient.Key)
	SendSequence([]rdpclient.Key)
	SendText(string)
	pointerClient
	TakeClipboard() (string, bool)
	SetClipboard(string)
	ResizeDesktop(int, int)
}

type pointerClient interface {
	MouseMove(int, int)
	MouseButton(int, bool, int, int)
	Wheel(int, int, int, bool)
}
