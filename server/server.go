package server

type RemoteServer struct {
}

func NewRemoteServerFromURL(url string) (server *RemoteServer) {
	server = &RemoteServer{}
	return
}
