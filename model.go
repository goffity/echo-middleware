package echomiddleware

type BodyDumpModel struct {
	Host          string `json:"host"`
	Path          string `json:"path"`
	Method        string `json:"method"`
	RemoteAddress string `json:"remoteAddress"`
	Header        string `json:"header"`
	Status        int    `json:"status"`
	Request       string `json:"request"`
	Response      string `json:"response"`
}
