//go:build (linux && !android && (amd64 || arm64)) || (darwin && !ios) || (windows && (amd64 || arm64))

package app

import "onrdp/internal/rustdesk"

const rustDeskSupported = true

func newRustDeskClient(wake func(), network rustdesk.Network) remoteClient {
	return rustdesk.New(wake, network)
}
